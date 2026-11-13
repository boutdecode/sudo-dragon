// Command server: runs the game server.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kevin/ascii-adventure/internal/llm"
	"github.com/kevin/ascii-adventure/internal/server"
	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "listen address")
	seedFlag := flag.Int64("seed", 1, "world seed (when the database is created; it is fixed afterwards)")
	dbPath := flag.String("db", "sudo-dragon.db", "SQLite database file")
	tlsCert := flag.String("tls-cert", "", "TLS certificate (enables wss://)")
	tlsKey := flag.String("tls-key", "", "TLS private key")
	loginWorkers := flag.Int("login-workers", 0, "concurrent password hashes (0 = half the cores); caps the login throughput")
	pprofAddr := flag.String("pprof", "", "profiling listen address (e.g. 127.0.0.1:6060); empty = disabled")
	maxReg := flag.Int("max-registrations", 10, "account creations per hour and per IP address (0 = unlimited)")

	// NPC dialogues through a language model. The API key is given through the
	// MISTRAL_API_KEY environment variable, never as an option.
	var lo llmOptions
	flag.StringVar(&lo.Mode, "llm", "auto", "NPC dialogues through a language model: auto (if MISTRAL_API_KEY is set), on (requires the key) or off")
	flag.StringVar(&lo.Model, "llm-model", "", "chat model (default: "+llm.DefaultModel+"); names change, see the provider's documentation")
	flag.StringVar(&lo.ModerationModel, "llm-moderation-model", "", "moderation model (default: "+llm.DefaultModerationModel+")")
	flag.StringVar(&lo.BaseURL, "llm-base-url", "", "API address (development: local fake server); empty = official API")
	flag.IntVar(&lo.DailyTokens, "llm-daily-tokens", 0, "token budget per UTC day (0 = default: 1,000,000)")
	flag.IntVar(&lo.Workers, "llm-workers", 0, "concurrent calls to the provider (0 = default: 4)")

	// Operator commands on NPC memory; the server is not started.
	loreList := flag.Bool("lore-list", false, "print the recorded player introductions, then exit")
	loreAccount := flag.String("lore-account", "", "with -lore-list: restrict to this account")
	lorePurge := flag.String("lore-purge", "", "delete introductions and memories: account:NAME or place:RX,RY, then exit")
	waresList := flag.Bool("wares-list", false, "print the generated wares of blacksmiths and armorers, then exit")
	waresReset := flag.String("wares-reset", "", "delete a vendor's wares (RX,RY,RANK; see -wares-list) so they are regenerated, then exit")
	flag.Parse()

	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("-tls-cert and -tls-key go together")
	}
	seedSet := false
	flag.Visit(func(f *flag.Flag) { seedSet = seedSet || f.Name == "seed" })

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	var storeOpts []store.Option
	if *loginWorkers > 0 {
		storeOpts = append(storeOpts, store.WithHashConcurrency(*loginWorkers))
	}
	st, err := store.Open(*dbPath, storeOpts...)
	if err != nil {
		return fmt.Errorf("opening %s: %w", *dbPath, err)
	}
	defer st.Close()

	if *waresList {
		return runWaresList(context.Background(), st, os.Stdout)
	}
	if *waresReset != "" {
		return runWaresReset(context.Background(), st, *waresReset, os.Stdout)
	}
	if *loreList {
		return runLoreList(context.Background(), st, *loreAccount, os.Stdout)
	}
	if *lorePurge != "" {
		return runLorePurge(context.Background(), st, *lorePurge, os.Stdout)
	}

	seed, gen, err := resolveSeed(st, *seedFlag, seedSet)
	if err != nil {
		return err
	}

	persister := store.NewPersister(st, 2*time.Second, log)
	be := store.Backend{Store: st, Persister: persister}
	wld := world.NewVersion(seed, gen)
	srv := server.New(wld, log, be)
	srv.SetRegistrationLimit(*maxReg)
	hs := &http.Server{Addr: *addr, Handler: srv.Handler(), ReadHeaderTimeout: 5 * time.Second}

	if *pprofAddr != "" {
		// Separate, standalone server: profiling must never be exposed on
		// the game's public address. Bind it to localhost.
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		go func() {
			if err := http.ListenAndServe(*pprofAddr, mux); err != nil {
				log.Error("profiling", "err", err)
			}
		}()
		log.Info("profiling enabled", "addr", *pprofAddr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// NPC dialogues: wired in before the game loop starts.
	talker, err := setupTalker(ctx, lo, st, wld, log, os.Getenv)
	if err != nil {
		return err
	}
	if talker != nil {
		srv.SetTalker(talker)
	}

	// Shutdown in three steps: no more new connections, the game loop
	// saves all players as it stops, then the writer drains its queue.
	gameDone := make(chan struct{})
	go func() { srv.Run(ctx); close(gameDone) }()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()

	log.Info("server started", "addr", *addr, "seed", seed, "db", *dbPath, "tls", *tlsCert != "")
	if *tlsCert != "" {
		err = hs.ListenAndServeTLS(*tlsCert, *tlsKey)
	} else {
		err = hs.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		stop()
		<-gameDone
		if talker != nil {
			talker.Wait()
		}
		_ = persister.Close()
		return err
	}

	<-gameDone
	if talker != nil {
		talker.Wait() // in-flight requests finish before the database is closed
	}
	if err := persister.Close(); err != nil {
		return fmt.Errorf("some saves could not be written: %w", err)
	}
	log.Info("clean shutdown, data saved")
	return nil
}

// resolveSeed determines the world seed and generator version, and pins them in the
// database. Changing the seed on an existing database is refused: saved positions
// would point to different terrain (water, mountain...). A database without a generator version
// is a pre-versioning world: it stays on v1; a new world takes the latest.
func resolveSeed(st *store.Store, flagSeed int64, flagSet bool) (seed int64, gen int, err error) {
	stored, ok, err := st.Meta("seed")
	if err != nil {
		return 0, 0, err
	}
	if !ok {
		if err := st.SetMeta("seed", strconv.FormatInt(flagSeed, 10)); err != nil {
			return 0, 0, err
		}
		if err := st.SetMeta("gen", strconv.Itoa(world.GenLatest)); err != nil {
			return 0, 0, err
		}
		return flagSeed, world.GenLatest, nil
	}
	seed, err = strconv.ParseInt(stored, 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("corrupted seed in the database (%q): %w", stored, err)
	}
	if flagSet && flagSeed != seed {
		return 0, 0, fmt.Errorf("the database was created with seed %d; refusing to start with -seed %d "+
			"(saved positions would be inconsistent). Use another database (-db) for a new world", seed, flagSeed)
	}
	gen = world.GenV1
	if g, ok, err := st.Meta("gen"); err != nil {
		return 0, 0, err
	} else if ok {
		if gen, err = strconv.Atoi(g); err != nil {
			return 0, 0, fmt.Errorf("corrupted generator version in the database (%q): %w", g, err)
		}
	}
	return seed, gen, nil
}
