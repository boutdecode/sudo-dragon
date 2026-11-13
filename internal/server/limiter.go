package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Memory bounds of the limiter.
const (
	sweepAbove  = 1_000   // below this number of keys, no global cleanup
	defaultKeys = 100_000 // hard cap on tracked keys
)

// limiter counts events per key over a sliding window.
type limiter struct {
	max     int
	window  time.Duration
	maxKeys int

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
	now       func() time.Time // injectable in tests
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, maxKeys: defaultKeys, hits: make(map[string][]time.Time), now: time.Now}
}

// recent returns the events still within the window (lock held).
func (l *limiter) recent(key string) []time.Time {
	cutoff := l.now().Add(-l.window)
	hits := l.hits[key]
	i := 0
	for i < len(hits) && !hits[i].After(cutoff) {
		i++
	}
	hits = hits[i:]
	if len(hits) == 0 {
		delete(l.hits, key)
		return nil
	}
	l.hits[key] = hits
	return hits
}

// blocked tells whether the key has reached its quota.
func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.max
}

// add records an event for the key.
//
// The global cleanup walks all keys: it only happens at most once
// per quarter window, otherwise an address sweep would make every
// add linear (the protection would itself become a way to overload the
// server). If the key cap is reached despite the cleanup, new
// keys are no longer tracked: better to let an unknown through
// than to exhaust memory.
func (l *limiter) add(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	if len(l.hits) > sweepAbove && now.Sub(l.lastSweep) > l.window/4 {
		for k := range l.hits {
			l.recent(k)
		}
		l.lastSweep = now
	}
	if _, known := l.hits[key]; !known && len(l.hits) >= l.maxKeys {
		return
	}
	l.hits[key] = append(l.recent(key), now)
}

// clientIP extracts the caller's address. Behind a reverse proxy, all
// connections share the proxy's address: quotas then apply
// to all players together.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
