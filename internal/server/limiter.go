package server

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Bornes de mémoire du limiteur.
const (
	sweepAbove  = 1_000   // en dessous de ce nombre de clés, aucun nettoyage global
	defaultKeys = 100_000 // plafond dur de clés suivies
)

// limiter compte des événements par clé sur une fenêtre glissante.
type limiter struct {
	max     int
	window  time.Duration
	maxKeys int

	mu        sync.Mutex
	hits      map[string][]time.Time
	lastSweep time.Time
	now       func() time.Time // injectable en test
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, maxKeys: defaultKeys, hits: make(map[string][]time.Time), now: time.Now}
}

// recent renvoie les événements encore dans la fenêtre (verrou tenu).
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

// blocked indique si la clé a atteint son quota.
func (l *limiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(key)) >= l.max
}

// add enregistre un événement pour la clé.
//
// Le nettoyage global parcourt toutes les clés: il n'a lieu qu'au plus une
// fois par quart de fenêtre, sinon un balayage d'adresses rendrait chaque
// ajout linéaire (la protection deviendrait elle-même un moyen de saturer le
// serveur). Si le plafond de clés est atteint malgré le nettoyage, les
// nouvelles clés ne sont plus suivies: on préfère laisser passer un inconnu
// que d'épuiser la mémoire.
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

// clientIP extrait l'adresse de l'appelant. Derrière un proxy inverse, toutes
// les connexions partagent l'adresse du proxy: les quotas s'appliquent alors
// à l'ensemble des joueurs.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
