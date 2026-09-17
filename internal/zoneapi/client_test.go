package zoneapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires a client to a test server. The rate limit is raised out of
// the way so tests exercise behaviour rather than wait out the real 60/minute
// budget; the pacing itself is covered separately.
func newTestClient(t *testing.T, handler http.Handler, opts ...Option) (*Client, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	base := []Option{
		WithBaseURL(server.URL),
		WithRateLimit(600_000),
		WithMaxRetries(0),
	}
	client, err := New("user", "token", append(base, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, server
}

func TestNewRequiresCredentials(t *testing.T) {
	t.Parallel()

	if _, err := New("", "token"); err == nil {
		t.Error("expected an error when username is empty")
	}
	if _, err := New("user", ""); err == nil {
		t.Error("expected an error when token is empty")
	}
}

func TestSendsBasicAuthAndJSONHeaders(t *testing.T) {
	t.Parallel()

	var gotUser, gotPass, gotAccept, gotAgent string
	var gotOK bool
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, gotOK = r.BasicAuth()
		gotAccept = r.Header.Get("Accept")
		gotAgent = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`[{"identificator":"example.com","active":true,"ipv6":false}]`))
	}))

	if _, err := client.GetZone(context.Background(), "example.com"); err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if !gotOK || gotUser != "user" || gotPass != "token" {
		t.Errorf("basic auth = (%q, %q, %v), want (user, token, true)", gotUser, gotPass, gotOK)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if !strings.HasPrefix(gotAgent, "terraform-provider-zone") {
		t.Errorf("User-Agent = %q, want it to identify the provider", gotAgent)
	}
}

// The API wraps every resource in an array, including single ones.
func TestUnwrapsArrayEnvelope(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"identificator":"example.com","active":true,"ipv6":true}]`))
	}))

	zone, err := client.GetZone(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if zone.Identificator != "example.com" || !zone.Active || !zone.IPv6 {
		t.Errorf("zone = %+v, want example.com active and ipv6", zone)
	}
}

// A 200 carrying an empty array is how the API reports a missing resource.
func TestEmptyArrayIsNotFound(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}))

	_, err := client.GetZone(context.Background(), "example.com")
	if !IsNotFound(err) {
		t.Fatalf("GetZone error = %v, want not found", err)
	}
}

func TestStatus404IsNotFound(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	_, err := client.GetZone(context.Background(), "example.com")
	if !IsNotFound(err) {
		t.Fatalf("GetZone error = %v, want not found", err)
	}
}

// 422 bodies mirror the submitted resource, mapping each bad field to its
// messages. Keeping them per-field is what lets the provider point at the
// offending attribute instead of printing one opaque error.
func TestValidationErrorsAreKeyedByField(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Status-Message", "Validation failed")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"name":["is not a valid hostname"],"destination":["is required"]}`))
	}))

	_, err := client.CreateRecord(context.Background(), "example.com", RecordTypeA, Record{Name: "bad"})
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
	if apiErr.Message != "Validation failed" {
		t.Errorf("Message = %q, want it taken from X-Status-Message", apiErr.Message)
	}
	if got := apiErr.FieldErrors["name"]; len(got) != 1 || got[0] != "is not a valid hostname" {
		t.Errorf("FieldErrors[name] = %v", got)
	}
	if got := apiErr.FieldErrors["destination"]; len(got) != 1 || got[0] != "is required" {
		t.Errorf("FieldErrors[destination] = %v", got)
	}
}

func TestParsesMessagesErrorShape(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"messages":["operation not supported"]}`))
	}))

	_, err := client.GetZone(context.Background(), "example.com")
	apiErr, ok := AsAPIError(err)
	if !ok {
		t.Fatalf("error = %v, want an *APIError", err)
	}
	if apiErr.Message != "operation not supported" {
		t.Errorf("Message = %q, want it taken from the messages body", apiErr.Message)
	}
}

func TestRetriesRateLimitAndHonoursRetryAfter(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`[{"identificator":"example.com","active":true}]`))
	}), WithMaxRetries(2))

	start := time.Now()
	if _, err := client.GetZone(context.Background(), "example.com"); err != nil {
		t.Fatalf("GetZone: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("calls = %d, want 2 (one 429, one success)", got)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("elapsed = %v, want the Retry-After delay to be respected", elapsed)
	}
}

func TestDoesNotRetryClientErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
	}), WithMaxRetries(3))

	if _, err := client.CreateRecord(context.Background(), "example.com", RecordTypeA, Record{}); err == nil {
		t.Fatal("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("calls = %d, want 1 — a validation failure is not retryable", got)
	}
}

// The cache exists so that Terraform refreshing many records costs one request
// per record type, not one per record. Concurrency matters here: Terraform reads
// resources in parallel, so the misses must collapse into a single fetch.
func TestConcurrentReadsIssueOneRequest(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	release := make(chan struct{})
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-release // hold the first request open so the others pile up behind it
		_, _ = w.Write([]byte(`[
			{"id":1,"name":"a.example.com","destination":"1.1.1.1","modify":true,"delete":true},
			{"id":2,"name":"b.example.com","destination":"2.2.2.2","modify":true,"delete":true}
		]`))
	}))

	const readers = 10
	var wg sync.WaitGroup
	errs := make([]error, readers)
	for i := range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = client.GetRecord(context.Background(), "example.com", RecordTypeA, 1)
		}()
	}

	time.Sleep(50 * time.Millisecond) // let every reader reach the cache
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("reader %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("HTTP calls = %d, want 1 for %d concurrent reads", got, readers)
	}
}

func TestWriteInvalidatesCache(t *testing.T) {
	t.Parallel()

	var lists atomic.Int32
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`[{"id":9,"name":"new.example.com","destination":"9.9.9.9"}]`))
			return
		}
		lists.Add(1)
		_, _ = w.Write([]byte(`[{"id":1,"name":"a.example.com","destination":"1.1.1.1"}]`))
	}))

	ctx := context.Background()
	if _, err := client.ListRecords(ctx, "example.com", RecordTypeA); err != nil {
		t.Fatalf("first list: %v", err)
	}
	if _, err := client.ListRecords(ctx, "example.com", RecordTypeA); err != nil {
		t.Fatalf("cached list: %v", err)
	}
	if got := lists.Load(); got != 1 {
		t.Fatalf("list calls before write = %d, want 1 (second served from cache)", got)
	}

	if _, err := client.CreateRecord(ctx, "example.com", RecordTypeA, Record{Name: "new.example.com"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := client.ListRecords(ctx, "example.com", RecordTypeA); err != nil {
		t.Fatalf("list after write: %v", err)
	}
	if got := lists.Load(); got != 2 {
		t.Errorf("list calls after write = %d, want 2 — the write must invalidate the cache", got)
	}
}

func TestCachesPerZoneAndType(t *testing.T) {
	t.Parallel()

	var paths sync.Map
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths.Store(r.URL.Path, true)
		_, _ = w.Write([]byte(`[]`))
	}))

	ctx := context.Background()
	for _, recordType := range []RecordType{RecordTypeA, RecordTypeMX} {
		for _, zone := range []string{"example.com", "example.org"} {
			if _, err := client.ListRecords(ctx, zone, recordType); err != nil {
				t.Fatalf("list %s/%s: %v", zone, recordType, err)
			}
		}
	}

	count := 0
	paths.Range(func(any, any) bool { count++; return true })
	if count != 4 {
		t.Errorf("distinct paths = %d, want 4 — cache keys must not collide across zones or types", count)
	}
}

func TestGetRecordMissingIDIsNotFound(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":1,"name":"a.example.com","destination":"1.1.1.1"}]`))
	}))

	_, err := client.GetRecord(context.Background(), "example.com", RecordTypeA, 404)
	if !IsNotFound(err) {
		t.Fatalf("error = %v, want not found", err)
	}
}

// Destroying a record someone already removed by hand should converge, not fail.
func TestDeleteToleratesMissingRecord(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	if err := client.DeleteRecord(context.Background(), "example.com", RecordTypeA, 1); err != nil {
		t.Errorf("DeleteRecord on a missing record = %v, want nil", err)
	}
}

func TestDeleteAcceptsNoContent(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	if err := client.DeleteRecord(context.Background(), "example.com", RecordTypeA, 1); err != nil {
		t.Errorf("DeleteRecord = %v, want nil", err)
	}
}

// Writes must not echo server-owned fields back.
func TestRecordMarshalOmitsReadOnlyFields(t *testing.T) {
	t.Parallel()

	record := Record{
		ID:          7,
		Name:        "mail.example.com",
		Destination: "mx.example.com",
		ResourceURL: "https://api.zone.eu/v2/dns/example.com/mx/7",
		Comment:     "server side note",
		Modifiable:  true,
		Deletable:   true,
		Extra:       map[string]any{"priority": 10},
	}

	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, field := range []string{"id", "resource_url", "comment", "modify", "delete"} {
		if _, present := payload[field]; present {
			t.Errorf("payload contains read-only field %q: %s", field, encoded)
		}
	}
	if payload["name"] != "mail.example.com" || payload["destination"] != "mx.example.com" {
		t.Errorf("payload = %s, want name and destination carried through", encoded)
	}
	if payload["priority"] != float64(10) {
		t.Errorf("payload[priority] = %v, want the type-specific field carried through", payload["priority"])
	}
}

func TestRecordUnmarshalSplitsExtras(t *testing.T) {
	t.Parallel()

	var record Record
	body := `{"id":3,"name":"_sip._tcp.example.com","destination":"sip.example.com",
		"resource_url":"https://api.zone.eu/v2/dns/example.com/srv/3","comment":"note",
		"modify":true,"delete":false,"priority":10,"weight":20,"port":443}`
	if err := decode(json.RawMessage(body), &record); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if record.ID != 3 || record.Name != "_sip._tcp.example.com" {
		t.Errorf("record = %+v", record)
	}
	if !record.Modifiable || record.Deletable {
		t.Errorf("Modifiable = %v, Deletable = %v, want true and false", record.Modifiable, record.Deletable)
	}
	for field, want := range map[string]int64{"priority": 10, "weight": 20, "port": 443} {
		got, ok := record.ExtraInt64(field)
		if !ok || got != want {
			t.Errorf("ExtraInt64(%q) = (%d, %v), want (%d, true)", field, got, ok, want)
		}
	}
	// Known fields must not leak into Extra.
	for _, field := range []string{"id", "name", "destination", "resource_url", "comment", "modify", "delete"} {
		if _, present := record.Extra[field]; present {
			t.Errorf("Extra contains shared field %q", field)
		}
	}
}

// The API documents integers but sends quoted numbers in places — its own spec
// gives the URL record's redirect type a default of "301" on an integer field.
func TestRecordUnmarshalToleratesQuotedNumbers(t *testing.T) {
	t.Parallel()

	var record Record
	body := `{"id":"123","name":"go.example.com","destination":"https://example.com","type":"301"}`
	if err := decode(json.RawMessage(body), &record); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if record.ID != 123 {
		t.Errorf("ID = %d, want 123 parsed from a quoted number", record.ID)
	}
	if got, ok := record.ExtraInt64("type"); !ok || got != 301 {
		t.Errorf("ExtraInt64(type) = (%d, %v), want (301, true)", got, ok)
	}
}

func TestPaceSlowsDownWhenBudgetIsLow(t *testing.T) {
	t.Parallel()

	// A deliberately slow limiter, so any extra token the pacing burns is
	// measurable: 60/minute is one per second.
	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[]`))
	}), WithRateLimit(60))

	// Pretend the server just reported an almost exhausted budget.
	client.budget.observe(http.Header{"X-Ratelimit-Remaining": []string{"1"}})

	start := time.Now()
	if err := client.pace(context.Background()); err != nil {
		t.Fatalf("pace: %v", err)
	}
	// lowWaterMark - 1 extra tokens at one per second.
	if elapsed := time.Since(start); elapsed < time.Duration(lowWaterMark-2)*time.Second {
		t.Errorf("pace returned after %v, want it to throttle when the budget is nearly spent", elapsed)
	}
}

func TestPaceIsANoOpWithHealthyBudget(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}), WithRateLimit(60))
	client.budget.observe(http.Header{"X-Ratelimit-Remaining": []string{"59"}})

	start := time.Now()
	if err := client.pace(context.Background()); err != nil {
		t.Fatalf("pace: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("pace took %v with a healthy budget, want it to return immediately", elapsed)
	}
}

func TestBackoffPrefersRetryAfter(t *testing.T) {
	t.Parallel()

	if got := backoff(0, 5*time.Second); got != 5*time.Second {
		t.Errorf("backoff = %v, want the server's Retry-After", got)
	}
	if got := backoff(0, time.Hour); got != maxBackoff {
		t.Errorf("backoff = %v, want it capped at %v", got, maxBackoff)
	}
	for attempt := range 5 {
		if got := backoff(attempt, 0); got <= 0 || got > maxBackoff {
			t.Errorf("backoff(%d) = %v, want a positive delay no greater than %v", attempt, got, maxBackoff)
		}
	}
}
