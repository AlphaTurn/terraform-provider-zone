package zoneapi

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Header names, spelled as the API documents them: lowercase on requests,
// canonical on responses. http.Header canonicalises the lookup key either way,
// so one constant serves both directions.
const (
	headerPagerPage    = "x-pager-page"
	headerPagerLimit   = "x-pager-limit"
	headerPagerPages   = "X-Pager-Pages"
	headerPagerItems   = "X-Pager-Items"
	headerPagerEnabled = "X-Pager-Enabled"
)

// requestOption customises one request.
//
// These exist because the non-DNS endpoints take their pagination as request
// headers and their filters as query parameters, neither of which fits in a
// path. Options are resolved once per call, before the retry loop, so that a
// retry repeats exactly the request that failed and no option is applied twice.
type requestOption func(*requestSpec)

// requestSpec is the resolved form of a call's options. Nothing mutates a spec
// once it is built; [requestSpec.forPage] returns a copy.
type requestSpec struct {
	query  url.Values
	header http.Header
}

func newRequestSpec(opts []requestOption) requestSpec {
	var spec requestSpec
	for _, opt := range opts {
		opt(&spec)
	}
	return spec
}

// withQuery sets a query parameter. An empty value is dropped, so an unset
// filter can be passed straight through without special-casing it.
func withQuery(key, value string) requestOption {
	return func(s *requestSpec) {
		if value == "" {
			return
		}
		if s.query == nil {
			s.query = make(url.Values, 2)
		}
		s.query.Set(key, value)
	}
}

// withBoolQuery sets a boolean filter, which the API spells "true" and "false".
func withBoolQuery(key string, value bool) requestOption {
	return withQuery(key, strconv.FormatBool(value))
}

// withHeader sets a request header.
func withHeader(key, value string) requestOption {
	return func(s *requestSpec) {
		if s.header == nil {
			s.header = make(http.Header, 2)
		}
		s.header.Set(key, value)
	}
}

// forPage returns a copy of the spec addressing one page of a collection.
//
// A limit of zero sends no pager headers at all for the first page, which is
// what keeps [Client.list] honest: an endpoint that implements no pager is
// asked nothing about paging, so it cannot answer a truncated response. Later
// pages do carry x-pager-page, but by then the server has volunteered a pager
// of its own accord, so naming a page is only agreeing with it.
func (s requestSpec) forPage(page, limit int) requestSpec {
	out := requestSpec{query: s.query, header: make(http.Header, len(s.header)+2)}
	for name, values := range s.header {
		out.header[name] = values
	}
	if limit > 0 {
		out.header.Set(headerPagerLimit, strconv.Itoa(limit))
	}
	if limit > 0 || page > 1 {
		out.header.Set(headerPagerPage, strconv.Itoa(page))
	}
	return out
}

// listCacheKey identifies a listing by the request that produces it: the
// collection's path plus anything that changes which items come back.
//
// The pager is excluded deliberately. It changes how the items arrive, not
// which ones, so one assembled listing serves every caller of that collection
// regardless of how it was paged. Ordering and filters are included, because
// two callers asking for different subsets must not read each other's results.
func listCacheKey(path string, spec requestSpec) string {
	var b strings.Builder
	b.WriteString(path)
	if len(spec.query) > 0 {
		b.WriteString("?")
		b.WriteString(spec.query.Encode())
	}

	names := make([]string, 0, len(spec.header))
	for name := range spec.header {
		switch strings.ToLower(name) {
		case headerPagerPage, headerPagerLimit:
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b.WriteString("\n")
		b.WriteString(name)
		b.WriteString(": ")
		b.WriteString(strings.Join(spec.header[name], ","))
	}
	return b.String()
}
