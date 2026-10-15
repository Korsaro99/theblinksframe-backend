package auth

import (
	"sync"
	"time"
)

// Login rate limits. Counted in memory, which is right for one API instance; with several
// instances each would count separately and the limits would need a shared store.
const (
	loginWindow = 15 * time.Minute
	// Every attempt from one IP counts, so a burst of parallel guesses is cut off too.
	loginAttemptsPerIP = 30
	// Failed attempts on one account. Kept above what a person mistyping would hit.
	// Trade-off: someone who knows the owner's phone can lock that login for 15 minutes.
	// The per-IP limit applies as well, so that takes many IP addresses.
	loginFailuresPerAccount = 10
	// Upper bound on remembered keys so that random logins cannot exhaust memory.
	maxLimiterKeys = 50_000
)

// LoginLimiter slows down password guessing on POST /auth/login.
type LoginLimiter struct {
	ip      *windowCounter
	account *windowCounter
}

// NewLoginLimiter returns a limiter with the production limits.
func NewLoginLimiter(now func() time.Time) *LoginLimiter {
	return &LoginLimiter{
		ip:      newWindowCounter(loginAttemptsPerIP, loginWindow, now),
		account: newWindowCounter(loginFailuresPerAccount, loginWindow, now),
	}
}

// Begin records a login attempt. When ok is false the attempt must be refused, and
// retryAfter says when to try again. The account slot is taken up front, so parallel
// guesses cannot all slip through while bcrypt is busy; Succeeded gives it back.
func (l *LoginLimiter) Begin(ip, login string) (ok bool, retryAfter time.Duration) {
	ipOK, ipWait := l.ip.take(ip)
	accOK, accWait := l.account.take(login)
	if ipOK && accOK {
		return true, 0
	}
	return false, max(ipWait, accWait)
}

// Succeeded clears the failure count of an account after a correct password.
func (l *LoginLimiter) Succeeded(login string) {
	l.account.reset(login)
}

// windowCounter allows limit hits per key in each fixed window.
type windowCounter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	now       func() time.Time
	hits      map[string]*windowHits
	lastSweep time.Time
}

type windowHits struct {
	count   int
	resetAt time.Time
}

func newWindowCounter(limit int, window time.Duration, now func() time.Time) *windowCounter {
	return &windowCounter{limit: limit, window: window, now: now, hits: map[string]*windowHits{}}
}

// take counts one hit for key if the key is under its limit.
func (w *windowCounter) take(key string) (bool, time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := w.now()
	w.sweep(now)

	h, ok := w.hits[key]
	if !ok || !now.Before(h.resetAt) {
		if !ok && len(w.hits) >= maxLimiterKeys {
			// Full: let the request through rather than lock everyone out. The other
			// counter (per IP or per account) still applies.
			return true, 0
		}
		h = &windowHits{resetAt: now.Add(w.window)}
		w.hits[key] = h
	}
	if h.count >= w.limit {
		return false, h.resetAt.Sub(now)
	}
	h.count++
	return true, 0
}

func (w *windowCounter) reset(key string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.hits, key)
}

// sweep drops expired keys, at most once a minute.
func (w *windowCounter) sweep(now time.Time) {
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
