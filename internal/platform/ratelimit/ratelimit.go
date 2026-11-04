// Package ratelimit counts requests per key (usually a client IP) in fixed time windows, in
// memory. That is right for one API instance; with several, each would count on its own and
// the limits would need a shared store.
//
// It is the same design as the login limiter in internal/auth, which predates it.
package ratelimit

import (
	"sync"
	"time"
)

// maxKeys bounds memory: random keys can't grow the map without limit.
const maxKeys = 50_000

// Window allows Limit hits per key in each window of Every.
type Window struct {
	mu        sync.Mutex
	limit     int
	every     time.Duration
	now       func() time.Time
	hits      map[string]*hits
	lastSweep time.Time
}

type hits struct {
	count   int
	resetAt time.Time
}

// New returns a Window. now may be nil (time.Now).
func New(limit int, every time.Duration, now func() time.Time) *Window {
	if now == nil {
		now = time.Now
	}
	return &Window{limit: limit, every: every, now: now, hits: map[string]*hits{}}
}

// Allow counts one hit for key. When it returns false the request must be refused, and
// retryAfter says when the key may try again.
func (w *Window) Allow(key string) (ok bool, retryAfter time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.sweep(now)

	h, found := w.hits[key]
	if !found || !now.Before(h.resetAt) {
		if !found && len(w.hits) >= maxKeys {
			return true, 0 // full: let it through rather than refuse everyone
		}
		h = &hits{resetAt: now.Add(w.every)}
		w.hits[key] = h
	}
	if h.count >= w.limit {
		return false, h.resetAt.Sub(now)
	}
	h.count++
	return true, 0
}

// sweep drops expired keys, at most once a minute.
func (w *Window) sweep(now time.Time) {
	if now.Sub(w.lastSweep) < time.Minute {
		return
	}
	w.lastSweep = now
	for k, h := range w.hits {
		if !now.Before(h.resetAt) {
			delete(w.hits, k)
		}
	}
}
