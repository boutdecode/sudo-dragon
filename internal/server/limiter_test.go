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
			t.Fatalf("bloqué trop tôt (après %d événements)", i)
		}
		l.add("ip")
	}
	if !l.blocked("ip") {
		t.Fatal("devrait être bloqué au quota")
	}

	*now = now.Add(59 * time.Second)
	if !l.blocked("ip") {
		t.Fatal("encore dans la fenêtre: doit rester bloqué")
	}
	*now = now.Add(2 * time.Second)
	if l.blocked("ip") {
		t.Fatal("fenêtre écoulée: doit être débloqué")
	}
}

func TestLimiterSlidingWindow(t *testing.T) {
	l, now := fakeLimiter(2, time.Minute)
	l.add("ip")
	*now = now.Add(40 * time.Second)
	l.add("ip")
	if !l.blocked("ip") {
		t.Fatal("2 événements dans la fenêtre: bloqué")
	}
	*now = now.Add(30 * time.Second) // le premier (à 70 s) est sorti, le second (à 30 s) reste
	if l.blocked("ip") {
		t.Fatal("un seul événement reste: ne doit plus être bloqué")
	}
}

func TestLimiterKeysAreIndependent(t *testing.T) {
	l, _ := fakeLimiter(1, time.Minute)
	l.add("a")
	if !l.blocked("a") || l.blocked("b") {
		t.Fatal("les quotas doivent être indépendants par clé")
	}
}

func TestLimiterSweepsExpiredKeys(t *testing.T) {
	l, now := fakeLimiter(5, time.Minute)
	for i := 0; i < 5_000; i++ {
		l.add(fmt.Sprintf("ip-%d", i))
	}
	*now = now.Add(2 * time.Minute) // tout est périmé
	l.add("nouveau")                // déclenche le nettoyage
	l.mu.Lock()
	n := len(l.hits)
	l.mu.Unlock()
	if n > 1 {
		t.Fatalf("%d clés conservées après expiration, une seule attendue", n)
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
		t.Fatalf("%d clés suivies, plafond 50", n)
	}

	// Une clé déjà suivie continue d'être comptée même au plafond.
	l.add("ip-0")
	l.add("ip-0")
	l.add("ip-0")
	l.add("ip-0")
	if !l.blocked("ip-0") {
		t.Fatal("une clé suivie doit rester limitée quand le plafond est atteint")
	}
	// Une clé inconnue, elle, n'est pas bloquée (fail-open assumé).
	if l.blocked("jamais-vue") {
		t.Fatal("une clé non suivie ne doit pas être bloquée")
	}
}

// Le coût d'un ajout ne doit pas croître avec le nombre de clés suivies.
func TestLimiterAddCostIsNotQuadratic(t *testing.T) {
	l := newLimiter(5, time.Minute) // horloge réelle: aucune clé n'expire pendant le test
	start := time.Now()
	for i := 0; i < 50_000; i++ {
		l.add(fmt.Sprintf("ip-%d", i))
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("50 000 ajouts en %v: le coût semble quadratique", d)
	}
}

func TestClientIP(t *testing.T) {
	for addr, want := range map[string]string{
		"203.0.113.9:51234": "203.0.113.9",
		"[2001:db8::1]:80":  "2001:db8::1",
		"sans-port":         "sans-port",
	} {
		if got := clientIP(&http.Request{RemoteAddr: addr}); got != want {
			t.Errorf("clientIP(%q) = %q, attendu %q", addr, got, want)
		}
	}
}
