package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/kevin/ascii-adventure/internal/store"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Commandes de l'opérateur sur la mémoire des PNJ. Elles s'exécutent sur la base puis le
// programme s'arrête : le serveur n'est pas démarré. Elles marchent aussi serveur allumé
// (SQLite gère l'accès concurrent), mais un PNJ peut alors réécrire une fiche juste après.

// runLoreList affiche les présentations enregistrées (les plus récentes d'abord).
func runLoreList(ctx context.Context, st *store.Store, account string, w io.Writer) error {
	cards, err := st.ListCards(ctx, account, 100)
	if err != nil {
		return err
	}
	if len(cards) == 0 {
		fmt.Fprintln(w, "aucune présentation enregistrée")
		return nil
	}
	for _, c := range cards {
		fmt.Fprintf(w, "%-16s région (%d,%d)  %s  « %s » %s | %s | %s | %s  [niv %d, seigneurs %d, morts %d]\n",
			c.Account, c.Place.X, c.Place.Y, c.UpdatedAt.Local().Format(time.DateTime),
			c.Alias, c.Role, c.Origin, c.Goal, c.Trait, c.Level, c.BossKills, c.Deaths)
	}
	return nil
}

// runLorePurge supprime les présentations et souvenirs d'un joueur ("account:NOM") ou d'un
// lieu ("place:RX,RY").
func runLorePurge(ctx context.Context, st *store.Store, target string, w io.Writer) error {
	kind, value, ok := strings.Cut(target, ":")
	if !ok || value == "" {
		return fmt.Errorf("cible invalide %q : utilisez account:NOM ou place:RX,RY", target)
	}
	var cards, mems int64
	var err error
	switch kind {
	case "account":
		cards, mems, err = st.PurgeAccount(ctx, value)
	case "place":
		rx, ry, perr := parseRegion(value)
		if perr != nil {
			return perr
		}
		cards, mems, err = st.PurgePlace(ctx, world.RegionPos{X: rx, Y: ry})
	default:
		return fmt.Errorf("cible invalide %q : utilisez account:NOM ou place:RX,RY", target)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%d présentation(s) et %d souvenir(s) supprimé(s) pour %s\n", cards, mems, target)
	return nil
}

func parseRegion(s string) (int, int, error) {
	xs, ys, ok := strings.Cut(s, ",")
	x, errX := strconv.Atoi(strings.TrimSpace(xs))
	y, errY := strconv.Atoi(strings.TrimSpace(ys))
	if !ok || errX != nil || errY != nil {
		return 0, 0, fmt.Errorf("région invalide %q : attendu RX,RY (deux entiers)", s)
	}
	return x, y, nil
}
