package zoneapi

import (
	"encoding/json"
	"sync"
	"time"
)

// listCache holds record listings for a short window, collapsing the many
// per-resource reads Terraform issues during a refresh into one request per
// zone and record type.
//
// This is what makes the provider usable against a real zone. zone.eu allows 60
// requests per minute per IP and returns every record of a type from a single
// unpaginated GET, while Terraform refreshes each resource in state
// individually and does so at a default parallelism of 10. Without this cache a
// plan over a 100-record zone would issue 100 requests in a few seconds and
// spend most of that time being rate limited; with it, the same plan costs one
// request per record type.
//
// The TTL is deliberately short. It only needs to span the burst of reads
// within a single refresh, not to survive between commands, and every write
// invalidates its key immediately so an apply never reads its own stale data.
type listCache struct {
	ttl time.Duration

	mu      sync.Mutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	// ready is non-nil while a fetch is in flight and closed when it settles.
	// Waiters block on it so that N concurrent reads of the same key produce
	// one request rather than N.
	ready    chan struct{}
	records  []json.RawMessage
	err      error
	storedAt time.Time
}

func newListCache(ttl time.Duration) *listCache {
	return &listCache{ttl: ttl, entries: make(map[string]*cacheEntry)}
}

// fetch returns the cached listing for key, calling load at most once across
// all concurrent callers that miss.
func (c *listCache) fetch(key string, load func() ([]json.RawMessage, error)) ([]json.RawMessage, error) {
	if c.ttl <= 0 {
		return load()
	}

	c.mu.Lock()
	if entry, ok := c.entries[key]; ok {
		if entry.ready != nil {
			// A fetch is already in flight; wait for it instead of starting another.
			ready := entry.ready
			c.mu.Unlock()
			<-ready

			c.mu.Lock()
			records, err := entry.records, entry.err
			c.mu.Unlock()
			return records, err
		}
		if time.Since(entry.storedAt) < c.ttl {
			records := entry.records
			c.mu.Unlock()
			return records, nil
		}
	}

	entry := &cacheEntry{ready: make(chan struct{})}
	c.entries[key] = entry
	c.mu.Unlock()

	records, err := load()

	c.mu.Lock()
	entry.records, entry.err, entry.storedAt = records, err, time.Now()
	close(entry.ready)
	entry.ready = nil
	if err != nil {
		// Never cache a failure: the next caller should get a real attempt.
		delete(c.entries, key)
	}
	c.mu.Unlock()

	return records, err
}

// invalidate drops the entry for key. Called after every write so a subsequent
// read reflects the change.
//
// The delete is unconditional, including while a fetch is in flight. Callers
// already waiting on that fetch hold the entry directly and still receive its
// result, but detaching it from the map means the listing it returns — taken
// before the write landed — is never handed to anyone new.
func (c *listCache) invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}
