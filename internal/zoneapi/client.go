// Package zoneapi is a client for the zone.eu ZoneID API v2.
//
// It deliberately contains no Terraform types so it can be tested — and reused —
// on its own. Two API behaviours shape everything here:
//
//   - Every response is a JSON array, even for a single resource. A 200 carrying
//     an empty array is how the API says "not found".
//   - Requests are limited to 60 per minute per IP, which is low enough that a
//     naive client is unusable from Terraform. See ratelimit.go and cache.go.
package zoneapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const (
	// DefaultBaseURL is the production ZoneID API v2 endpoint.
	DefaultBaseURL = "https://api.zone.eu/v2"

	// DefaultRateLimit is the sustained rate, in requests per minute, the client
	// holds itself to. zone.eu documents a hard limit of 60 per minute per IP;
	// staying below it leaves room for retries and for anything else sharing the
	// address.
	DefaultRateLimit = 55

	// DefaultMaxRetries bounds how often a retryable failure is repeated.
	DefaultMaxRetries = 3

	// DefaultListCacheTTL is how long a record listing is reused. It only has to
	// span the burst of reads in one refresh; writes invalidate it immediately.
	DefaultListCacheTTL = 10 * time.Second

	// maxResponseBytes caps how much of a response is read, so a misbehaving
	// endpoint cannot exhaust memory.
	maxResponseBytes = 16 * 1024 * 1024
)

// Client talks to the zone.eu API. It is safe for concurrent use, and should be
// shared: the rate limiter and the listing cache are per-client, so one client
// per provider instance is what keeps a whole Terraform run inside the budget.
type Client struct {
	baseURL    *url.URL
	username   string
	token      string
	userAgent  string
	httpClient *http.Client
	limiter    *rate.Limiter
	maxRetries int
	cache      *listCache
	budget     *budgetTracker
}

// Option customises a [Client].
type Option func(*config)

type config struct {
	baseURL      string
	userAgent    string
	httpClient   *http.Client
	ratePerMin   int
	maxRetries   int
	listCacheTTL time.Duration
}

// WithBaseURL overrides the API endpoint. Mainly useful for pointing tests at a
// local server.
func WithBaseURL(raw string) Option {
	return func(c *config) {
		if raw != "" {
			c.baseURL = raw
		}
	}
}

// WithHTTPClient supplies the underlying HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *config) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithRateLimit sets the sustained request rate in requests per minute.
func WithRateLimit(perMinute int) Option {
	return func(c *config) {
		if perMinute > 0 {
			c.ratePerMin = perMinute
		}
	}
}

// WithMaxRetries sets how many times a retryable failure is repeated.
func WithMaxRetries(n int) Option {
	return func(c *config) {
		if n >= 0 {
			c.maxRetries = n
		}
	}
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *config) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithListCacheTTL sets how long record listings are reused. Zero disables
// caching, which is useful in tests that assert on request counts.
func WithListCacheTTL(ttl time.Duration) Option {
	return func(c *config) { c.listCacheTTL = ttl }
}

// New builds a client authenticating as the given ZoneID user.
func New(username, token string, opts ...Option) (*Client, error) {
	if username == "" {
		return nil, errors.New("zone.eu: username is required")
	}
	if token == "" {
		return nil, errors.New("zone.eu: API token is required")
	}

	cfg := &config{
		baseURL:      DefaultBaseURL,
		userAgent:    "terraform-provider-zone",
		httpClient:   &http.Client{Timeout: 60 * time.Second},
		ratePerMin:   DefaultRateLimit,
		maxRetries:   DefaultMaxRetries,
		listCacheTTL: DefaultListCacheTTL,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	parsed, err := url.Parse(strings.TrimRight(cfg.baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("zone.eu: invalid base URL %q: %w", cfg.baseURL, err)
	}

	// A burst of 1 keeps requests evenly paced. Allowing a burst would let
	// Terraform's parallel workers fire together and exhaust the minute's
	// allowance in one go.
	limiter := rate.NewLimiter(rate.Limit(float64(cfg.ratePerMin)/60.0), 1)

	return &Client{
		baseURL:    parsed,
		username:   username,
		token:      token,
		userAgent:  cfg.userAgent,
		httpClient: cfg.httpClient,
		limiter:    limiter,
		maxRetries: cfg.maxRetries,
		cache:      newListCache(cfg.listCacheTTL),
		budget:     &budgetTracker{},
	}, nil
}

// do issues a request, repeating the failures that are worth repeating, and
// returns the elements of the response's array envelope.
func (c *Client) do(ctx context.Context, method, path string, payload any) ([]json.RawMessage, error) {
	var encoded []byte
	if payload != nil {
		var err error
		if encoded, err = json.Marshal(payload); err != nil {
			return nil, fmt.Errorf("zone.eu: encoding %s %s body: %w", method, path, err)
		}
	}

	for attempt := 0; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, fmt.Errorf("zone.eu: waiting for a rate limit slot: %w", err)
		}
		if err := c.pace(ctx); err != nil {
			return nil, err
		}

		records, retryAfter, err := c.attempt(ctx, method, path, encoded)
		if err == nil {
			return records, nil
		}
		if attempt >= c.maxRetries || !isRetryable(method, err) {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff(attempt, retryAfter)):
		}
	}
}

// pace slows down when the server says the budget is nearly spent. The limiter
// alone cannot see this: the budget is per IP, so anything else calling the API
// from this address spends from the same allowance. Burning extra limiter
// tokens drops the effective rate until the window resets, which is cheaper
// than discovering the collision as a 429.
func (c *Client) pace(ctx context.Context) error {
	remaining, ok := c.budget.snapshot()
	if !ok || remaining > lowWaterMark {
		return nil
	}

	for i := remaining; i < lowWaterMark; i++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return fmt.Errorf("zone.eu: waiting out a nearly exhausted rate limit budget: %w", err)
		}
	}
	return nil
}

// attempt performs one HTTP round trip. The returned duration is a
// server-requested retry delay, if there was one.
func (c *Client) attempt(ctx context.Context, method, path string, body []byte) ([]json.RawMessage, time.Duration, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, reader)
	if err != nil {
		return nil, 0, fmt.Errorf("zone.eu: building %s %s request: %w", method, path, err)
	}
	req.SetBasicAuth(c.username, c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("zone.eu: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, 0, fmt.Errorf("zone.eu: reading %s %s response: %w", method, path, err)
	}

	c.budget.observe(resp.Header)
	retryAfter := parseRetryAfter(resp.Header)

	if resp.StatusCode >= http.StatusBadRequest {
		messages, fieldErrors := parseErrorBody(raw)
		message := statusMessage(resp.Header)
		if message == "" {
			message = strings.Join(messages, "; ")
		}
		return nil, retryAfter, &APIError{
			StatusCode:  resp.StatusCode,
			Method:      method,
			Path:        path,
			Message:     message,
			FieldErrors: fieldErrors,
			Body:        string(raw),
		}
	}

	trimmed := bytes.TrimSpace(raw)
	if resp.StatusCode == http.StatusNoContent || len(trimmed) == 0 {
		return nil, 0, nil
	}

	// The documented envelope is an array, but a few endpoints answer with a
	// bare object; treat that as a one-element result rather than failing.
	if trimmed[0] == '{' {
		return []json.RawMessage{trimmed}, 0, nil
	}

	var list []json.RawMessage
	if err := json.Unmarshal(trimmed, &list); err != nil {
		return nil, 0, fmt.Errorf("zone.eu: decoding %s %s response: %w", method, path, err)
	}
	return list, 0, nil
}

// doOne issues a request expecting a single resource and decodes it into out.
// An empty array is the API's way of reporting a missing resource.
func (c *Client) doOne(ctx context.Context, method, path string, payload, out any) error {
	records, err := c.do(ctx, method, path, payload)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("zone.eu: %s %s: %w", method, path, ErrNotFound)
	}
	if out == nil {
		return nil
	}
	return decode(records[0], out)
}

// decode unmarshals with UseNumber so integer-ish fields survive the trip. The
// API's own schema is loose here — it documents integer fields whose examples
// and defaults are quoted strings — so numbers are kept in a form that can be
// read either way rather than being flattened to float64.
func decode(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("zone.eu: decoding response: %w", err)
	}
	return nil
}

// statusMessage reads the API's human-readable status line.
//
// The messages are in Estonian, and X-Status-Message mangles anything outside
// ASCII — "vastu võetud" arrives with the õ corrupted. The API sends the same
// text base64-encoded alongside it, which survives intact, so that is preferred
// when present.
func statusMessage(h http.Header) string {
	if encoded := h.Get("X-Status-Message-Base64"); encoded != "" {
		if decoded, err := base64.StdEncoding.DecodeString(encoded); err == nil {
			return string(decoded)
		}
	}
	return h.Get("X-Status-Message")
}
