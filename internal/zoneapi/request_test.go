package zoneapi

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// The /domain filters are query parameters rather than headers, and an unset
// filter has to cost the caller nothing.
func TestQueryOptionsReachTheURL(t *testing.T) {
	t.Parallel()

	var got string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))

	_, err := client.do(context.Background(), http.MethodGet, "/domain", nil,
		withQuery("name", "ex ample"),
		withQuery("empty", ""),
		withBoolQuery("renewable", true),
	)
	if err != nil {
		t.Fatalf("do: %v", err)
	}

	values, err := url.ParseQuery(got)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", got, err)
	}
	if values.Get("name") != "ex ample" {
		t.Errorf("name = %q, want %q", values.Get("name"), "ex ample")
	}
	if values.Get("renewable") != "true" {
		t.Errorf("renewable = %q, want true", values.Get("renewable"))
	}
	if _, present := values["empty"]; present {
		t.Error("an empty value should be dropped, not sent as a blank parameter")
	}
}

// The DNS paths were verified against the live API without a query string, so
// an option-free call must still produce a bare path.
func TestNoOptionsSendsNoQueryString(t *testing.T) {
	t.Parallel()

	var raw string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		_, _ = w.Write([]byte(`[]`))
	}))

	if _, err := client.ListRecords(context.Background(), "example.com", RecordTypeA); err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if raw != "" {
		t.Errorf("RawQuery = %q, want empty", raw)
	}
}

// The pager is documented in lowercase and Go canonicalises header names, so
// this pins the spelling and values the server actually receives.
func TestPagerHeadersAreSentAsTheAPISpellsThem(t *testing.T) {
	t.Parallel()

	var gotLimit, gotPage string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotLimit = r.Header.Get("x-pager-limit")
		gotPage = r.Header.Get("x-pager-page")
		_, _ = w.Write([]byte(`[]`))
	}))

	if _, err := client.listPaged(context.Background(), "/vserver/s/mail/account"); err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if gotLimit != "100" {
		t.Errorf("x-pager-limit = %q, want 100, the maximum, so one request serves a whole page", gotLimit)
	}
	if gotPage != "1" {
		t.Errorf("x-pager-page = %q, want 1", gotPage)
	}
}

// Asking an endpoint to limit a response it was never going to paginate is how
// a listing silently loses items, and DNS returns every record of a type in one
// unpaginated response. This is the guard against that.
func TestUnpaginatedListingSendsNoPagerHeaders(t *testing.T) {
	t.Parallel()

	var sent []string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name := range r.Header {
			if strings.HasPrefix(strings.ToLower(name), "x-pager") {
				sent = append(sent, name)
			}
		}
		_, _ = w.Write([]byte(`[]`))
	}))

	if _, err := client.ListRecords(context.Background(), "example.com", RecordTypeA); err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(sent) != 0 {
		t.Errorf("ListRecords sent pager headers %v, want none", sent)
	}

	sent = nil
	if _, err := client.list(context.Background(), "/vserver/s/crontab"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sent) != 0 {
		t.Errorf("list sent pager headers %v, want none", sent)
	}
}

// Options are resolved once, before the retry loop, so a retry repeats
// byte-identically rather than accumulating a second copy of each value.
func TestRetryRepeatsTheIdenticalRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	var queries, limits []string
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		queries = append(queries, r.URL.RawQuery)
		limits = append(limits, strings.Join(r.Header.Values("x-pager-limit"), "|"))
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}), WithMaxRetries(1))

	if _, err := client.listPaged(context.Background(), "/domain", withQuery("name", "ex")); err != nil {
		t.Fatalf("listPaged: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 (one 429, one success)", got)
	}
	if queries[0] != queries[1] {
		t.Errorf("query changed between attempts: %q then %q", queries[0], queries[1])
	}
	if limits[0] != limits[1] || limits[1] != "100" {
		t.Errorf("x-pager-limit = %v, want one 100 on each attempt", limits)
	}
}

// One assembled listing has to serve every caller of a collection however it
// was paged, or the cache never hits.
func TestListCacheKeyIgnoresThePager(t *testing.T) {
	t.Parallel()

	plain := newRequestSpec(nil)
	if a, b := listCacheKey("/domain", plain), listCacheKey("/domain", plain.forPage(3, 100)); a != b {
		t.Errorf("paging changed the cache key:\n%q\n%q", a, b)
	}
}

func TestListCacheKeyDistinguishesFiltersAndOrdering(t *testing.T) {
	t.Parallel()

	keys := map[string]string{
		"unfiltered": listCacheKey("/domain", newRequestSpec(nil)),
		"filtered":   listCacheKey("/domain", newRequestSpec([]requestOption{withQuery("name", "a")})),
		"other":      listCacheKey("/domain", newRequestSpec([]requestOption{withQuery("name", "b")})),
		"ordered":    listCacheKey("/domain", newRequestSpec([]requestOption{withHeader("x-order-by", "name")})),
		"elsewhere":  listCacheKey("/vserver/s/ssl", newRequestSpec(nil)),
	}

	seen := make(map[string]string, len(keys))
	for name, key := range keys {
		if other, clash := seen[key]; clash {
			t.Errorf("%s and %s share a cache key %q", name, other, key)
		}
		seen[key] = name
	}
}

// 402 means "buy an upgrade" and 403 means "this service is not yours to read".
// Neither is a missing resource, and reporting either as one would make
// Terraform drop the resource from state and then offer to recreate it.
func TestPaymentRequiredAndForbiddenAreNotNotFound(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		check  func(error) bool
	}{
		{"payment required", http.StatusPaymentRequired, IsPaymentRequired},
		{"forbidden", http.StatusForbidden, IsForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(`{"messages":["nope"]}`))
			}))

			_, err := client.do(context.Background(), http.MethodPost, "/vserver/s/mail/account", nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !tc.check(err) {
				t.Errorf("the %s predicate did not match %v", tc.name, err)
			}
			if IsNotFound(err) {
				t.Error("must not be reported as a missing resource")
			}
		})
	}
}
