package zoneapi

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// lowWaterMark is the server-reported remaining-request count below which
	// the client deliberately slows down.
	lowWaterMark = 8

	baseBackoff = time.Second
	maxBackoff  = 30 * time.Second
)

// budgetTracker records the rate-limit budget the server reports on every
// response.
//
// The client's own limiter keeps it under the documented 60 requests per
// minute, but that budget is per IP: another process behind the same address —
// a colleague running terraform, a CI job, a cron script — eats into the same
// allowance, and nothing but the response headers reveals it. Watching
// X-Ratelimit-Remaining lets the client back off before it collides rather than
// discovering the collision as a 429.
type budgetTracker struct {
	mu        sync.Mutex
	limit     int
	remaining int
	seen      bool
}

func (b *budgetTracker) observe(h http.Header) {
	remaining, err := strconv.Atoi(h.Get("X-Ratelimit-Remaining"))
	if err != nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.remaining = remaining
	b.seen = true
	if limit, err := strconv.Atoi(h.Get("X-Ratelimit-Limit")); err == nil {
		b.limit = limit
	}
}

// snapshot returns the last reported remaining budget, and whether the server
// has reported one at all yet.
func (b *budgetTracker) snapshot() (int, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.remaining, b.seen
}

// parseRetryAfter reads the Retry-After header, which may be either a delay in
// seconds or an HTTP date.
func parseRetryAfter(h http.Header) time.Duration {
	value := h.Get("Retry-After")
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// backoff returns how long to wait before retrying. A server-supplied
// Retry-After always wins; otherwise the delay grows exponentially with jitter,
// because Terraform drives many resources concurrently and an unjittered retry
// would send the whole batch back at the API in lockstep.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		if retryAfter > maxBackoff {
			return maxBackoff
		}
		return retryAfter
	}

	d := baseBackoff << attempt
	if d > maxBackoff {
		d = maxBackoff
	}
	// Jitter across the upper half of the window: still backing off meaningfully,
	// but spread out.
	return d/2 + rand.N(d/2)
}

// isRetryable reports whether a failed request is worth repeating.
//
// 429 is the rate limit and 409 is documented as a concurrent-request conflict;
// both clear on their own. Transport errors are retried only for methods that
// are safe to repeat — a POST that failed mid-flight may well have created the
// record, and retrying it would duplicate the record rather than report it.
func isRetryable(method string, err error) bool {
	apiErr, ok := AsAPIError(err)
	if !ok {
		return method == http.MethodGet || method == http.MethodPut || method == http.MethodDelete
	}

	switch apiErr.StatusCode {
	case http.StatusTooManyRequests, http.StatusConflict:
		return true
	}
	return apiErr.StatusCode >= 500
}
