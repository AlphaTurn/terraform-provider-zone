package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

const (
	// maxPageSize is the largest page the API serves. Asking for it up front is
	// the difference between one request and ten for any collection that fits
	// in a single page, which matters because 60 requests a minute is the whole
	// budget across every resource type at once.
	maxPageSize = 100

	// maxPages is a backstop rather than a policy: a listing this long has
	// already spent the minute's budget several times over, and something is
	// wrong. Erroring is right here, because silently truncating a listing
	// would make Terraform propose deleting whatever fell off the end.
	maxPages = 100

	// listPasses is how many times an assembly is attempted when the
	// collection changes underneath it.
	listPasses = 2
)

// pager is the server's description of a paginated collection, read from the
// X-Pager-* response headers.
type pager struct {
	items, page, pages int
}

// parsePager reports the pager a response carried, if it carried one. The
// endpoints that do not paginate send no X-Pager-* headers at all, so their
// absence is meaningful rather than a parse failure.
func parsePager(h http.Header) (pager, bool) {
	pages, err := strconv.Atoi(h.Get(headerPagerPages))
	if err != nil || pages < 1 {
		return pager{}, false
	}
	if enabled := h.Get(headerPagerEnabled); enabled == "0" {
		return pager{}, false
	}

	p := pager{pages: pages}
	p.items, _ = strconv.Atoi(h.Get(headerPagerItems))
	p.page, _ = strconv.Atoi(h.Get(headerPagerPage))
	return p, true
}

// list returns a collection's items, cached and shared between concurrent
// callers.
//
// It sends no pager headers. The DNS record endpoints, SSH keys, crontabs and
// the various singletons return everything in one response and implement no
// pager at all, and asking a server to limit a response it was never going to
// page is how a listing silently loses items. If such an endpoint pages anyway
// it says so in the response headers, and the pages are followed — so
// mislabelling an endpoint here degrades to extra requests, never to wrong
// data.
func (c *Client) list(ctx context.Context, path string, opts ...requestOption) ([]json.RawMessage, error) {
	spec := newRequestSpec(opts)
	return c.cache.fetch(listCacheKey(path, spec), func() ([]json.RawMessage, error) {
		return c.fetchAllPages(ctx, path, 0, spec)
	})
}

// listPaged is [Client.list] for the endpoints that do paginate, asking for the
// largest page the API allows so a collection of up to 100 items costs one
// request instead of ten.
func (c *Client) listPaged(ctx context.Context, path string, opts ...requestOption) ([]json.RawMessage, error) {
	spec := newRequestSpec(opts)
	return c.cache.fetch(listCacheKey(path, spec), func() ([]json.RawMessage, error) {
		return c.fetchAllPages(ctx, path, maxPageSize, spec)
	})
}

// invalidateList drops a collection's cached listing.
//
// Every write beneath a collection has to call this, including writes to one
// item or to a sub-resource: the listing is where item reads are answered from,
// so a write that does not invalidate it is a write the next read cannot see.
func (c *Client) invalidateList(path string, opts ...requestOption) {
	c.cache.invalidate(listCacheKey(path, newRequestSpec(opts)))
}

// fetchAllPages GETs a collection and returns every item in it. limit is the
// page size to request; zero sends no x-pager-limit.
func (c *Client) fetchAllPages(ctx context.Context, path string, limit int, spec requestSpec) ([]json.RawMessage, error) {
	for pass := 1; ; pass++ {
		items, whole, err := c.fetchPagesOnce(ctx, path, limit, spec)
		if err != nil {
			return nil, err
		}
		if whole || pass >= listPasses {
			return items, nil
		}
		// The collection changed while it was being paged, so this assembly is
		// torn: offset pagination shifts items across page boundaries, which
		// can repeat one item or skip it entirely. One more pass usually lands
		// in a quiet moment, and if it does not, the best available answer is
		// better than failing a refresh over somebody else's write.
	}
}

// fetchPagesOnce assembles one pass over a collection. whole reports whether
// the result is known to be self-consistent.
func (c *Client) fetchPagesOnce(
	ctx context.Context, path string, limit int, spec requestSpec,
) (items []json.RawMessage, whole bool, err error) {
	first, err := c.doSpec(ctx, http.MethodGet, path, nil, spec.forPage(1, limit))
	if err != nil {
		return nil, false, err
	}

	page, paged := parsePager(first.header)
	if !paged || page.pages <= 1 {
		// One request answered the whole collection, so nothing can have torn.
		return first.records, true, nil
	}

	items = append(make([]json.RawMessage, 0, page.items), first.records...)
	pages := page.pages
	for n := 2; n <= pages; n++ {
		if n > maxPages {
			return nil, false, fmt.Errorf("zone.eu: listing %s: more than %d pages", path, maxPages)
		}

		next, err := c.doSpec(ctx, http.MethodGet, path, nil, spec.forPage(n, limit))
		if err != nil {
			// Some endpoints answer a page past the end with a 400 rather than
			// an empty array. Page 1 succeeded with this same spec, so that
			// means the collection shrank while it was being read, not that the
			// request was malformed.
			if isPagerBounds(err) {
				return items, page.items == len(items), nil
			}
			return nil, false, err
		}
		if len(next.records) == 0 {
			// Running out of pages early is only a torn assembly if items are
			// actually missing; a server that overstates its page count is not
			// worth a second pass.
			return items, page.items == len(items), nil
		}
		later, ok := parsePager(next.header)
		if ok && later.page != 0 && later.page != n {
			// The server did not serve the page it was asked for, so it is not
			// paging on our terms and following further would just re-read the
			// same items. Keep the one page we know is real.
			return first.records, false, nil
		}

		items = append(items, next.records...)

		if ok && later.pages < pages {
			pages = later.pages // The collection shrank; stop where it now ends.
		}
	}
	return items, page.items == len(items), nil
}

func isPagerBounds(err error) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.StatusCode == http.StatusBadRequest
}

// decodeList decodes an assembled listing, dropping any item whose identity has
// already been seen and keeping first-seen order.
//
// The dedupe is not defensive programming: pagination is offset based, so a
// write landing between two page fetches shifts items across the boundary and
// the same item can genuinely arrive on two pages. identity may be nil for the
// collections the API does not paginate.
func decodeList[T any](raws []json.RawMessage, identity func(*T) string) ([]T, error) {
	out := make([]T, 0, len(raws))
	var seen map[string]struct{}
	if identity != nil {
		seen = make(map[string]struct{}, len(raws))
	}

	for _, raw := range raws {
		var item T
		if err := decode(raw, &item); err != nil {
			return nil, err
		}
		if identity != nil {
			key := identity(&item)
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
		}
		out = append(out, item)
	}
	return out, nil
}
