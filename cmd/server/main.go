// Commande server : lance le serveur de jeu.
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
		fmt.Fprintln(os.Stderr, "erreur:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", ":8080", "adresse d'écoute")
	seedFlag := flag.Int64("seed", 1, "seed du monde (à la création de la base; ensuite elle est fixée)")
	dbPath := flag.String("db", "sudo-dragon.db", "fichier de base de données SQLite")
	tlsCert := flag.String("tls-cert", "", "certificat TLS (active wss://)")
	tlsKey := flag.String("tls-key", "", "clé privée TLS")
	loginWorkers := flag.Int("login-workers", 0, "hachages de mot de passe simultanés (0 = moitié des cœurs); borne le débit de connexion")
	pprofAddr := flag.String("pprof", "", "adresse d'écoute du profilage (ex. 127.0.0.1:6060); vide = désactivé")
	maxReg := flag.Int("max-registrations", 10, "créations de compte par heure et par adresse IP (0 = illimité)")

	// Dialogues de PNJ par modèle de langage. La clé d'API se donne par la variable
	// d'environnement MISTRAL_API_KEY, jamais en option.
	var lo llmOptions
	flag.StringVar(&lo.Mode, "llm", "auto", "dialogues de PNJ par modèle de langage : auto (si MISTRAL_API_KEY est définie), on (exige la clé) ou off")
	flag.StringVar(&lo.Model, "llm-model", "", "modèle de chat (défaut : "+llm.DefaultModel+") ; les noms changent, voir la documentation du fournisseur")
	flag.StringVar(&lo.ModerationModel, "llm-moderation-model", "", "modèle de modération (défaut : "+llm.DefaultModerationModel+")")
	flag.StringVar(&lo.BaseURL, "llm-base-url", "", "adresse de l'API (développement : faux serveur local) ; vide = API officielle")
	flag.IntVar(&lo.DailyTokens, "llm-daily-tokens", 0, "budget de jetons par jour UTC (0 = défaut : 1 000 000)")
	flag.IntVar(&lo.Workers, "llm-workers", 0, "appels simultanés au fournisseur (0 = défaut : 4)")

	// Commandes de l'opérateur sur la mémoire des PNJ ; le serveur n'est pas lancé.
	loreList := flag.Bool("lore-list", false, "affiche les présentations de joueurs enregistrées, puis quitte")
	loreAccount := flag.String("lore-account", "", "avec -lore-list : se limite à ce compte")
	lorePurge := flag.String("lore-purge", "", "supprime présentations et souvenirs : account:NOM ou place:RX,RY, puis quitte")
	waresList := flag.Bool("wares-list", false, "affiche les étalages générés des forgerons et armuriers, puis quitte")
	waresReset := flag.String("wares-reset", "", "efface l'étalage d'un vendeur (RX,RY,RANG ; voir -wares-list) pour qu'il soit régénéré, puis quitte")
	flag.Parse()

	if (*tlsCert == "") != (*tlsKey == "") {
		return errors.New("-tls-cert et -tls-key vont ensemble")
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
		return fmt.Errorf("ouverture de %s: %w", *dbPath, err)
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
		// Serveur séparé et à part: le profilage ne doit jamais être exposé sur
		// l'adresse publique du jeu. À lier à localhost.
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		go func() {
			if err := http.ListenAndServe(*pprofAddr, mux); err != nil {
				log.Error("profilage", "err", err)
			}
		}()
		log.Info("profilage actif", "addr", *pprofAddr)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Dialogues de PNJ : branchés avant le démarrage de la boucle de jeu.
	talker, err := setupTalker(ctx, lo, st, wld, log, os.Getenv)
	if err != nil {
		return err
	}
	if talker != nil {
		srv.SetTalker(talker)
	}

	// Arrêt en trois temps: plus de nouvelles connexions, la boucle de jeu
	// sauvegarde tous les joueurs en s'arrêtant, puis l'écrivain vide sa file.
	gameDone := make(chan struct{})
	go func() { srv.Run(ctx); close(gameDone) }()
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()

	log.Info("serveur démarré", "addr", *addr, "seed", seed, "db", *dbPath, "tls", *tlsCert != "")
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
		talker.Wait() // les demandes en cours se terminent avant la fermeture de la base
	}
	if err := persister.Close(); err != nil {
		return fmt.Errorf("des sauvegardes n'ont pas pu être écrites: %w", err)
	}
	log.Info("arrêt propre, données enregistrées")
	return nil
}

// resolveSeed détermine la seed et la version du générateur du monde, et les fixe dans la
// base. Changer de seed sur une base existante est refusé: les positions sauvegardées
// pointeraient vers un autre terrain (eau, montagne…). Une base sans version de générateur
// est un monde d'avant le versionnage: elle reste en v1; un nouveau monde prend la dernière.
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
		return 0, 0, fmt.Errorf("seed corrompue dans la base (%q): %w", stored, err)
	}
	if flagSet && flagSeed != seed {
		return 0, 0, fmt.Errorf("la base a été créée avec la seed %d; refus de démarrer avec -seed %d "+
			"(les positions sauvegardées seraient incohérentes). Utilisez une autre base (-db) pour un nouveau monde", seed, flagSeed)
	}
	gen = world.GenV1
	if g, ok, err := st.Meta("gen"); err != nil {
		return 0, 0, err
	} else if ok {
		if gen, err = strconv.Atoi(g); err != nil {
			return 0, 0, fmt.Errorf("version de générateur corrompue dans la base (%q): %w", g, err)
		}
	}
	return seed, gen, nil
}
