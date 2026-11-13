package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func fakeLimiter(max int, window time.Duration) (*limiter, *time.Time) {
	now := time.Unix(1_000_000, 0)
	l := newLimiter(max, window)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestLimiterBlocksAtMaxAndExpires(t *testing.T) {
	l, now := fakeLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if l.blocked("ip") {
			t.Fatalf("blocked too early (after %d events)", i)
		}
		l.add("ip")
	}
	if !l.blocked("ip") {
		t.Fatal("should be blocked at the quota")
	}

	*now = now.Add(59 * time.Second)
	if !l.blocked("ip") {
		t.Fatal("still within the window: must stay blocked")
	}
	*now = now.Add(2 * time.Second)
	if l.blocked("ip") {
		t.Fatal("window elapsed: must be unblocked")
	}
}

func TestLimiterSlidingWindow(t *testing.T) {
	l, now := fakeLimiter(2, time.Minute)
	l.add("ip")
	*now = now.Add(40 * time.Second)
	l.add("ip")
	if !l.blocked("ip") {
		t.Fatal("2 events in the window: blocked")
	}
	*now = now.Add(30 * time.Second) // the first (at 70 s) has left, the second (at 30 s) stays
	if l.blocked("ip") {
		t.Fatal("a single event remains: must no longer be blocked")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l, _ := fakeLimiter(1, time.Minute)
	l.add("a")
	if !l.blocked("a") || l.blocked("b") {
		t.Fatal("quotas must be independent per key")
	}
}

func TestLimiterSweepsExpiredKeys(t *testing.T) {
	l, now := fakeLimiter(5, time.Minute)
	for i := 0; i < 5_000; i++ {
		l.add(fmt.Sprintf("ip-%d", i))
	}
	*now = now.Add(2 * time.Minute) // everything is stale
	l.add("nouveau")                // triggers the cleanup
	l.mu.Lock()
	n := len(l.hits)
	l.mu.Unlock()
	if n > 1 {
		t.Fatalf("%d keys kept after expiry, only one expected", n)
	}
}

func TestLimiterHardKeyCap(t *testing.T) {
	l, _ := fakeLimiter(5, time.Minute)
	l.maxKeys = 50
	for i := 0; i < 500; i++ {
		l.add(fmt.Sprintf("ip-%d", i))
	}
	l.mu.Lock()
	n := len(l.hits)
	l.mu.Unlock()
	if n > 50 {
		t.Fatalf("%d keys tracked, cap 50", n)
	}

	// A key already tracked keeps being counted even at the cap.
	l.add("ip-0")
	l.add("ip-0")
	l.add("ip-0")
	l.add("ip-0")
	if !l.blocked("ip-0") {
		t.Fatal("a tracked key must stay limited when the cap is reached")
	}
	// An unknown key, however, is not blocked (accepted fail-open).
	if l.blocked("jamais-vue") {
		t.Fatal("an untracked key must not be blocked")
	}
}

// The cost of an add must not grow with the number of tracked keys.
func TestLimiterAddCostIsNotQuadratic(t *testing.T) {
	l := newLimiter(5, time.Minute) // real clock: no key expires during the test
	start := time.Now()
	for i := 0; i < 50_000; i++ {
		l.add(fmt.Sprintf("ip-%d", i))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("50,000 adds in %v: the cost looks quadratic", d)
	}
}

func TestClientIP(t *testing.T) {
	for addr, want := range map[string]string{
		"203.0.113.9:51234": "203.0.113.9",
		"[2001:db8::1]:80":  "2001:db8::1",
		"sans-port":         "sans-port",
	} {
		if got := clientIP(&http.Request{RemoteAddr: addr}); got != want {
			t.Errorf("clientIP(%q) = %q, expected %q", addr, got, want)
		}
	}
}
