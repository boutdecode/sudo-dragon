// Commande bot : lance des joueurs automatiques qui se promènent au hasard.
// Sert à tester la tenue en charge et à observer le monde (monstres, etc.).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kevin/ascii-adventure/internal/client"
	"github.com/kevin/ascii-adventure/internal/protocol"
)

// serverStats reprend la forme JSON de l'endpoint /stats du serveur.
type serverStats struct {
	Players     int     `json:"players"`
	Monsters    int     `json:"monsters"`
	Ticks       uint64  `json:"ticks"`
	SlowTicks   uint64  `json:"slow_ticks"`
	TickTotalMS float64 `json:"tick_total_ms"`
	TickMaxMS   float64 `json:"tick_max_ms"`

	TickP99MS       float64 `json:"tick_p99_ms"`
	TickRecentMaxMS float64 `json:"tick_recent_max_ms"`
}

// statsURL déduit l'adresse de /stats de celle du WebSocket.
func statsURL(wsAddr string) string {
	u := strings.Replace(wsAddr, "wss://", "https://", 1)
	u = strings.Replace(u, "ws://", "http://", 1)
	return strings.TrimSuffix(u, "/ws") + "/stats"
}

func fetchStats(url string) (serverStats, error) {
	var st serverStats
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return st, fmt.Errorf("/stats: HTTP %d", resp.StatusCode)
	}
	return st, json.NewDecoder(resp.Body).Decode(&st)
}

type stats struct {
	snapshots  atomic.Int64
	bytes      atomic.Int64
	maxMonster atomic.Int64
	kinds      sync.Map // espèce -> struct{}
	failures   atomic.Int64

	connMu    sync.Mutex
	connTimes []time.Duration // durée de connexion (Dial + login + welcome) de chaque bot
}

func (s *stats) recordConnect(d time.Duration) {
	s.connMu.Lock()
	s.connTimes = append(s.connTimes, d)
	s.connMu.Unlock()
}

// connSummary renvoie la médiane et le maximum des durées de connexion.
func (s *stats) connSummary() (median, maxD time.Duration) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	if len(s.connTimes) == 0 {
		return 0, 0
	}
	t := append([]time.Duration(nil), s.connTimes...)
	slices.Sort(t)
	return t[len(t)/2], t[len(t)-1]
}

func main() {
	addr := flag.String("addr", "ws://localhost:8080/ws", "adresse du serveur")
	n := flag.Int("n", 5, "nombre de bots")
	dur := flag.Duration("d", 30*time.Second, "durée")
	verbose := flag.Bool("v", false, "afficher les monstres vus par le premier bot")
	pass := flag.String("pass", "bot-password-123", "mot de passe commun des comptes bots")
	warmup := flag.Duration("warmup", 0, "délai avant de mesurer la cadence (laisse passer la vague de connexions)")
	minRate := flag.Float64("min-tick-rate", 0, "échoue si le serveur tient moins de N ticks/s (0 = ne pas vérifier)")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *dur)
	defer cancel()

	var st stats
	var wg sync.WaitGroup
	for i := 0; i < *n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runBot(ctx, *addr, fmt.Sprintf("bot%03d", i), *pass, &st, *verbose && i == 0)
		}(i)
		time.Sleep(20 * time.Millisecond) // évite un pic de connexions simultanées
	}

	// Mesure de la cadence du serveur sur la période où tous les bots sont connectés.
	url := statsURL(*addr)
	select {
	case <-time.After(*warmup):
	case <-ctx.Done():
	}
	before, errBefore := fetchStats(url)
	t0 := time.Now()
	<-ctx.Done()
	after, errAfter := fetchStats(url)
	elapsed := time.Since(t0).Seconds()
	wg.Wait()

	secs := dur.Seconds()
	var kinds []string
	st.kinds.Range(func(k, _ any) bool { kinds = append(kinds, k.(string)); return true })
	med, worst := st.connSummary()
	fmt.Printf("bots: %d (échecs de connexion: %d); durée de connexion: médiane %v, max %v\n",
		*n, st.failures.Load(), med.Round(time.Millisecond), worst.Round(time.Millisecond))
	fmt.Printf("snapshots reçus: %d (%.1f/s au total), %.1f Ko/s\n",
		st.snapshots.Load(), float64(st.snapshots.Load())/secs, float64(st.bytes.Load())/1024/secs)
	fmt.Printf("monstres vus en même temps (max): %d; espèces: %v\n", st.maxMonster.Load(), kinds)

	exit := 0
	if errBefore != nil || errAfter != nil {
		fmt.Fprintf(os.Stderr, "stats du serveur indisponibles (%v / %v)\n", errBefore, errAfter)
		if *minRate > 0 {
			exit = 1
		}
	} else if dt := after.Ticks - before.Ticks; dt > 0 && elapsed > 0 {
		rate := float64(dt) / elapsed
		avg := (after.TickTotalMS - before.TickTotalMS) / float64(dt)
		fmt.Printf("serveur: %d joueurs, %d monstres\n", after.Players, after.Monsters)
		fmt.Printf("cadence: %.2f ticks/s (cible 10), ticks lents: %d\n", rate, after.SlowTicks-before.SlowTicks)
		fmt.Printf("durée d'un tick (budget 100 ms): moyenne %.2f ms, p99 %.2f ms, max sur 60 s %.2f ms, max depuis le démarrage %.2f ms\n",
			avg, after.TickP99MS, after.TickRecentMaxMS, after.TickMaxMS)
		if *minRate > 0 && rate < *minRate {
			fmt.Fprintf(os.Stderr, "ÉCHEC: %.2f ticks/s < %.2f exigés\n", rate, *minRate)
			exit = 1
		}
	}
	if st.failures.Load() > 0 {
		exit = 1
	}
	os.Exit(exit)
}

func runBot(ctx context.Context, addr, name, pass string, st *stats, verbose bool) {
	// Crée le compte au premier passage, s'y connecte ensuite.
	started := time.Now()
	c, _, err := client.Dial(ctx, addr, name, pass, true)
	if err != nil && strings.Contains(err.Error(), "existe déjà") {
		c, _, err = client.Dial(ctx, addr, name, pass, false)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
		st.failures.Add(1)
		return
	}
	st.recordConnect(time.Since(started))
	defer c.Close()

	rng := rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(len(name))))
	dx, dy := 0, 0
	turn := time.NewTicker(time.Second)
	defer turn.Stop()
	move := time.NewTicker(200 * time.Millisecond)
	defer move.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-turn.C:
			dx, dy = rng.IntN(3)-1, rng.IntN(3)-1
		case <-move.C:
			c.Send(protocol.TMove, protocol.Move{DX: dx, DY: dy})
		case ev := <-c.In:
			if ev.Err != nil {
				return
			}
			if ev.Env.T != protocol.TSnapshot {
				continue
			}
			var s protocol.Snapshot
			if ev.Env.Payload(&s) != nil {
				continue
			}
			st.snapshots.Add(1)
			st.bytes.Add(int64(len(ev.Env.D)))
			if m := int64(len(s.Monsters)); m > st.maxMonster.Load() {
				st.maxMonster.Store(m)
			}
			for _, m := range s.Monsters {
				st.kinds.Store(m.Kind, struct{}{})
			}
			if verbose && len(s.Monsters) > 0 && s.Tick%50 == 0 {
				fmt.Printf("[%s tick %d] en (%d,%d):", name, s.Tick, s.Self.X, s.Self.Y)
				for _, m := range s.Monsters {
					fmt.Printf(" %s(%+d,%+d)", m.Kind, m.X-s.Self.X, m.Y-s.Self.Y)
				}
				fmt.Println()
			}
		}
	}
}
