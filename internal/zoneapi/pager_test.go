package zoneapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// item is the smallest thing that can be paged and identified.
type item struct {
	ID int64 `json:"id"`
}

func itemsJSON(from, count int) []string {
	out := make([]string, 0, count)
	for i := from; i < from+count; i++ {
		out = append(out, fmt.Sprintf(`{"id":%d}`, i))
	}
	return out
}

// pagedItems serves a slice the way the API's pager does: it reads
// x-pager-page and x-pager-limit, defaults the limit to 10 exactly as the API
// does, and sets the X-Pager-* response headers.
func pagedItems(items []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 10
		if v, err := strconv.Atoi(r.Header.Get(headerPagerLimit)); err == nil && v > 0 {
			limit = v
		}
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil && v > 0 {
			page = v
		}

		pages := (len(items) + limit - 1) / limit
		if pages < 1 {
			pages = 1
		}
		writePagerHeaders(w, len(items), limit, page, pages)
		_, _ = w.Write([]byte("[" + strings.Join(pageSlice(items, page, limit), ",") + "]"))
	}
}

func writePagerHeaders(w http.ResponseWriter, items, limit, page, pages int) {
	w.Header().Set(headerPagerEnabled, "1")
	w.Header().Set(headerPagerItems, strconv.Itoa(items))
	w.Header().Set(headerPagerLimit, strconv.Itoa(limit))
	w.Header().Set(headerPagerPage, strconv.Itoa(page))
	w.Header().Set(headerPagerPages, strconv.Itoa(pages))
}

func pageSlice(items []string, page, limit int) []string {
	start := (page - 1) * limit
	if start > len(items) {
		start = len(items)
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

func ids(t *testing.T, raws []json.RawMessage) []int64 {
	t.Helper()

	items, err := decodeList(raws, func(i *item) string { return strconv.FormatInt(i.ID, 10) })
	if err != nil {
		t.Fatalf("decodeList: %v", err)
	}
	out := make([]int64, 0, len(items))
	for _, i := range items {
		out = append(out, i.ID)
	}
	return out
}

// This is the whole leverage of asking for the maximum page size: a collection
// that fits in one page costs one request out of the 60 available in a minute,
// not ten.
func TestSinglePageCollectionCostsOneRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		pagedItems(itemsJSON(1, 40))(w, r)
	}))

	raws, err := client.listPaged(context.Background(), "/vserver/s/mail/account")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
	if got := len(ids(t, raws)); got != 40 {
		t.Errorf("items = %d, want 40", got)
	}
}

func TestFollowsPagerToTheLastPage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		pagedItems(itemsJSON(1, 250))(w, r)
	}))

	raws, err := client.listPaged(context.Background(), "/domain")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (250 items at 100 a page)", got)
	}

	got := ids(t, raws)
	if len(got) != 250 {
		t.Fatalf("items = %d, want 250", len(got))
	}
	for i, id := range got {
		if id != int64(i+1) {
			t.Fatalf("item %d has id %d; pages are out of order", i, id)
			break
		}
	}
}

// The endpoints that do not paginate send no X-Pager-* headers at all, and
// their responses must pass through untouched.
func TestIgnoresMissingPagerHeaders(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON(1, 3), ",") + "]"))
	}))

	raws, err := client.list(context.Background(), "/vserver/s/crontab")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
	if got := len(ids(t, raws)); got != 3 {
		t.Errorf("items = %d, want 3", got)
	}
}

// The safety net: /ssl paginates on the wire although its published schema
// declares no pager, so a listing that asked for no paging still has to follow
// one the server volunteers.
func TestFollowsAPagerTheServerVolunteers(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		pagedItems(itemsJSON(1, 25))(w, r)
	}))

	raws, err := client.list(context.Background(), "/vserver/s/ssl")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// No limit was requested, so the server applied its default of 10.
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 (25 items at the server default of 10)", got)
	}
	if got := len(ids(t, raws)); got != 25 {
		t.Errorf("items = %d, want 25 — a volunteered pager was not followed", got)
	}
}

func TestStopsWhenThePageCountShrinks(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil {
			page = v
		}
		// Page 1 claims three pages; by page 2 the collection has shrunk to the
		// 200 items it reports all along, so the assembly is complete at two.
		pages := 3
		if n > 1 {
			pages = 2
		}
		writePagerHeaders(w, 200, 100, page, pages)
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON((page-1)*100+1, 100), ",") + "]"))
	}))

	if _, err := client.listPaged(context.Background(), "/domain"); err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 — the shrunken page count was ignored", got)
	}
}

func TestStopsOnAnEmptyPage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil {
			page = v
		}
		// The page count is overstated, but the item count is honest, so
		// stopping early is the complete answer rather than a torn one.
		writePagerHeaders(w, 100, 100, page, 5)
		if page == 1 {
			_, _ = w.Write([]byte("[" + strings.Join(itemsJSON(1, 100), ",") + "]"))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))

	raws, err := client.listPaged(context.Background(), "/domain")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 — an empty page should end the listing", got)
	}
	if got := len(ids(t, raws)); got != 100 {
		t.Errorf("items = %d, want 100", got)
	}
}

// If the server ignores the page it was asked for, following the pager would
// re-read page one over and over. One real page beats the same page five times.
func TestStopsWhenTheServerIgnoresTheRequestedPage(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		writePagerHeaders(w, 500, 100, 1, 5) // Always page 1, whatever was asked.
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON(1, 100), ",") + "]"))
	}))

	raws, err := client.listPaged(context.Background(), "/domain")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := len(ids(t, raws)); got != 100 {
		t.Errorf("items = %d, want the 100 of the one page the server served", got)
	}
	if got := calls.Load(); got > 4 {
		t.Errorf("calls = %d, want the listing to give up quickly rather than loop", got)
	}
}

// Some endpoints answer a page past the end with a 400 rather than an empty
// array. Page 1 succeeded with the same request, so that means the collection
// shrank, not that the request was wrong.
func TestOutOfBoundsPageEndsTheListing(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil {
			page = v
		}
		if page > 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"messages":["Out of pager bounds"]}`))
			return
		}
		writePagerHeaders(w, 100, 100, 1, 3)
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON(1, 100), ",") + "]"))
	}))

	raws, err := client.listPaged(context.Background(), "/domain")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := len(ids(t, raws)); got != 100 {
		t.Errorf("items = %d, want the 100 gathered before the bound", got)
	}
}

func TestBadRequestOnTheFirstPageIsAnError(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"messages":["nope"]}`))
	}))

	if _, err := client.listPaged(context.Background(), "/domain"); err == nil {
		t.Error("a 400 on the first page is a real failure and must be reported")
	}
}

// A listing assembled from several pages can be torn by a concurrent write,
// because offset paging shifts items across page boundaries. The item count the
// server reports is what makes that detectable.
func TestRefetchesOnceWhenTheCollectionChangesMidListing(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil {
			page = v
		}
		writePagerHeaders(w, 150, 100, page, 2)

		// The first pass loses one item between its two pages; the second pass
		// is consistent.
		count := 50
		if n > 2 && page == 2 {
			count = 50
		} else if page == 2 {
			count = 49
		}
		if page == 1 {
			count = 100
		}
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON((page-1)*100+1, count), ",") + "]"))
	}))

	raws, err := client.listPaged(context.Background(), "/vserver/s/mail/account")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Errorf("calls = %d, want 4 (a torn pass of 2, then a clean pass of 2)", got)
	}
	if got := len(ids(t, raws)); got != 150 {
		t.Errorf("items = %d, want 150 from the second, consistent pass", got)
	}
}

// If it is still torn on the second pass, the best available answer beats
// failing a refresh over somebody else's write — but it must not loop.
func TestAcceptsAStillTornListing(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil {
			page = v
		}
		writePagerHeaders(w, 150, 100, page, 2)
		count := 49
		if page == 1 {
			count = 100
		}
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON((page-1)*100+1, count), ",") + "]"))
	}))

	raws, err := client.listPaged(context.Background(), "/vserver/s/mail/account")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 4 {
		t.Errorf("calls = %d, want exactly 4: two passes and no more", got)
	}
	if got := len(ids(t, raws)); got != 149 {
		t.Errorf("items = %d, want the 149 actually served", got)
	}
}

// The same item genuinely arrives twice when a write shifts it across a page
// boundary, so identity has to be deduplicated where it is known.
func TestDuplicateItemsAcrossPagesAppearOnce(t *testing.T) {
	t.Parallel()

	raws := []json.RawMessage{
		json.RawMessage(`{"id":1}`),
		json.RawMessage(`{"id":2}`),
		json.RawMessage(`{"id":1}`),
		json.RawMessage(`{"id":3}`),
	}

	got := ids(t, raws)
	want := []int64{1, 2, 3}
	if len(got) != len(want) {
		t.Fatalf("ids = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ids = %v, want %v (first-seen order)", got, want)
		}
	}
}

// Truncating silently would make Terraform propose deleting whatever fell off
// the end, so an absurd page count has to be loud.
func TestPageFollowingIsBoundedAndLoud(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := 1
		if v, err := strconv.Atoi(r.Header.Get(headerPagerPage)); err == nil {
			page = v
		}
		writePagerHeaders(w, 100_000, 100, page, 1000)
		_, _ = w.Write([]byte("[" + strings.Join(itemsJSON(page, 1), ",") + "]"))
	}))

	_, err := client.listPaged(context.Background(), "/domain")
	if err == nil {
		t.Fatal("expected an error rather than a truncated listing")
	}
	if !strings.Contains(err.Error(), "pages") {
		t.Errorf("error should say the listing was too long, got: %v", err)
	}
}

func TestPagedListingIsCachedAsAWhole(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		pagedItems(itemsJSON(1, 250))(w, r)
	}))

	for range 2 {
		if _, err := client.listPaged(context.Background(), "/domain"); err != nil {
			t.Fatalf("listPaged: %v", err)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 — the assembled listing should be cached, not each page", got)
	}
}

func TestConcurrentPagedReadsIssueOneAssembly(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	release := make(chan struct{})
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			<-release // Hold the first page open so the others pile up behind it.
		}
		pagedItems(itemsJSON(1, 250))(w, r)
	}))

	var wg sync.WaitGroup
	for range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.listPaged(context.Background(), "/domain"); err != nil {
				t.Errorf("listPaged: %v", err)
			}
		}()
	}
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 3 {
		t.Errorf("calls = %d, want 3 — ten readers should share one assembly", got)
	}
}

func TestListCacheDisabledStillAssemblesPages(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		pagedItems(itemsJSON(1, 150))(w, r)
	}), WithListCacheTTL(0))

	raws, err := client.listPaged(context.Background(), "/domain")
	if err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2", got)
	}
	if got := len(ids(t, raws)); got != 150 {
		t.Errorf("items = %d, want 150 even with caching off", got)
	}
}

// The listing is where item reads are answered from, so a write that does not
// invalidate it is a write the next read cannot see.
func TestInvalidateListForcesARefetch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		pagedItems(itemsJSON(1, 5))(w, r)
	}))

	path := "/vserver/s/mail/account"
	if _, err := client.listPaged(context.Background(), path); err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	client.invalidateList(path)
	if _, err := client.listPaged(context.Background(), path); err != nil {
		t.Fatalf("listPaged: %v", err)
	}

	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 — invalidation did not take effect", got)
	}
}

func TestParsePagerReadsTheHeaders(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		header http.Header
		paged  bool
		pages  int
		items  int
	}{
		{
			name:   "a real paged response",
			header: http.Header{"X-Pager-Enabled": {"1"}, "X-Pager-Pages": {"5"}, "X-Pager-Items": {"42"}, "X-Pager-Page": {"2"}},
			paged:  true, pages: 5, items: 42,
		},
		{
			name:   "no pager headers at all",
			header: http.Header{},
			paged:  false,
		},
		{
			name:   "explicitly disabled",
			header: http.Header{"X-Pager-Enabled": {"0"}, "X-Pager-Pages": {"3"}},
			paged:  false,
		},
		{
			name:   "unparseable page count",
			header: http.Header{"X-Pager-Pages": {"lots"}},
			paged:  false,
		},
		{
			name:   "a page count of zero",
			header: http.Header{"X-Pager-Pages": {"0"}},
			paged:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, paged := parsePager(tc.header)
			if paged != tc.paged {
				t.Fatalf("paged = %v, want %v", paged, tc.paged)
			}
			if !tc.paged {
				return
			}
			if got.pages != tc.pages {
				t.Errorf("pages = %d, want %d", got.pages, tc.pages)
			}
			if got.items != tc.items {
				t.Errorf("items = %d, want %d", got.items, tc.items)
			}
		})
	}
}
