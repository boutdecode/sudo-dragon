// Command bot: runs automatic players that wander at random.
// Used to test behaviour under load and to observe the world (monsters, etc.).
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

// serverStats mirrors the JSON shape of the server's /stats endpoint.
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

// statsURL derives the /stats address from the WebSocket one.
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
	kinds      sync.Map // species -> struct{}
	failures   atomic.Int64

	connMu    sync.Mutex
	connTimes []time.Duration // connection time (Dial + login + welcome) of each bot
}

func (s *stats) recordConnect(d time.Duration) {
	s.connMu.Lock()
	s.connTimes = append(s.connTimes, d)
	s.connMu.Unlock()
}

// connSummary returns the median and maximum of the connection times.
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
	addr := flag.String("addr", "ws://localhost:8080/ws", "server address")
	n := flag.Int("n", 5, "number of bots")
	dur := flag.Duration("d", 30*time.Second, "duration")
	verbose := flag.Bool("v", false, "print the monsters seen by the first bot")
	pass := flag.String("pass", "bot-password-123", "shared password of the bot accounts")
	warmup := flag.Duration("warmup", 0, "delay before measuring the tick rate (lets the connection wave pass)")
	minRate := flag.Float64("min-tick-rate", 0, "fail if the server holds fewer than N ticks/s (0 = do not check)")
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
		time.Sleep(20 * time.Millisecond) // avoids a spike of simultaneous connections
	}

	// Measure the server tick rate over the period when all the bots are connected.
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
	fmt.Printf("bots: %d (connection failures: %d); connection time: median %v, max %v\n",
		*n, st.failures.Load(), med.Round(time.Millisecond), worst.Round(time.Millisecond))
	fmt.Printf("snapshots received: %d (%.1f/s in total), %.1f KB/s\n",
		st.snapshots.Load(), float64(st.snapshots.Load())/secs, float64(st.bytes.Load())/1024/secs)
	fmt.Printf("monsters seen at the same time (max): %d; species: %v\n", st.maxMonster.Load(), kinds)

	exit := 0
	if errBefore != nil || errAfter != nil {
		fmt.Fprintf(os.Stderr, "server stats unavailable (%v / %v)\n", errBefore, errAfter)
		if *minRate > 0 {
			exit = 1
		}
	} else if dt := after.Ticks - before.Ticks; dt > 0 && elapsed > 0 {
		rate := float64(dt) / elapsed
		avg := (after.TickTotalMS - before.TickTotalMS) / float64(dt)
		fmt.Printf("server: %d players, %d monsters\n", after.Players, after.Monsters)
		fmt.Printf("tick rate: %.2f ticks/s (target 10), slow ticks: %d\n", rate, after.SlowTicks-before.SlowTicks)
		fmt.Printf("tick duration (100 ms budget): average %.2f ms, p99 %.2f ms, max over 60 s %.2f ms, max since startup %.2f ms\n",
			avg, after.TickP99MS, after.TickRecentMaxMS, after.TickMaxMS)
		if *minRate > 0 && rate < *minRate {
			fmt.Fprintf(os.Stderr, "FAILED: %.2f ticks/s < %.2f required\n", rate, *minRate)
			exit = 1
		}
	}
	if st.failures.Load() > 0 {
		exit = 1
	}
	os.Exit(exit)
}

func runBot(ctx context.Context, addr, name, pass string, st *stats, verbose bool) {
	// Create the account on the first run, then log in afterwards.
	started := time.Now()
	c, _, err := client.Dial(ctx, addr, name, pass, true)
	if err != nil && (strings.Contains(err.Error(), "existe déjà") || strings.Contains(err.Error(), "already exists")) {
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
				fmt.Printf("[%s tick %d] at (%d,%d):", name, s.Tick, s.Self.X, s.Self.Y)
				for _, m := range s.Monsters {
					fmt.Printf(" %s(%+d,%+d)", m.Kind, m.X-s.Self.X, m.Y-s.Self.Y)
				}
				fmt.Println()
			}
		}
	}
}
