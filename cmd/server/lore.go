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
	deeds, err := st.ListDeeds(ctx, account, 100)
	if err != nil {
		return err
	}
	if len(cards) == 0 && len(deeds) == 0 {
		fmt.Fprintln(w, "aucune présentation enregistrée")
		return nil
	}
	for _, d := range deeds {
		fmt.Fprintf(w, "%-16s région (%d,%d)  %s  quête rendue : « %s »\n",
			d.Account, d.Place.X, d.Place.Y, d.DoneAt.Local().Format(time.DateTime), d.Title)
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
	var cards, mems, quests int64
	var err error
	switch kind {
	case "account":
		cards, mems, quests, err = st.PurgeAccount(ctx, value)
	case "place":
		rx, ry, perr := parseRegion(value)
		if perr != nil {
			return perr
		}
		cards, mems, quests, err = st.PurgePlace(ctx, world.RegionPos{X: rx, Y: ry})
	default:
		return fmt.Errorf("cible invalide %q : utilisez account:NOM ou place:RX,RY", target)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%d présentation(s), %d souvenir(s) et %d quête(s) supprimé(s) pour %s\n", cards, mems, quests, target)
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

// runWaresList affiche les étalages générés (un vendeur par ligne, puis ses objets).
func runWaresList(ctx context.Context, st *store.Store, w io.Writer) error {
	list, err := st.ListWares(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(w, "aucun étalage généré")
		return nil
	}
	for _, wr := range list {
		fmt.Fprintf(w, "vendeur %d,%d,%d  « %s »  (%s)\n", wr.NPC.Region.X, wr.NPC.Region.Y, wr.NPC.Index, wr.Specialty, wr.CreatedAt.Local().Format(time.DateTime))
		for _, it := range wr.Items {
			fmt.Fprintf(w, "    %-8s palier %d  ATQ %d DÉF %d  %d or  %s\n", it.Slot, it.Tier, it.Atk, it.Def, it.Price, it.Name)
		}
	}
	return nil
}

// runWaresReset efface l'étalage d'un vendeur ("RX,RY,RANG") : il sera régénéré à la prochaine visite.
func runWaresReset(ctx context.Context, st *store.Store, target string, w io.Writer) error {
	parts := strings.Split(target, ",")
	if len(parts) != 3 {
		return fmt.Errorf("cible invalide %q : attendu RX,RY,RANG", target)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return fmt.Errorf("cible invalide %q : %q n'est pas un entier", target, p)
		}
		n[i] = v
	}
	ok, err := st.DeleteWares(ctx, world.NPCKey{Region: world.RegionPos{X: n[0], Y: n[1]}, Index: n[2]})
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(w, "aucun étalage enregistré pour %s\n", target)
		return nil
	}
	fmt.Fprintf(w, "étalage de %s effacé : il sera régénéré à la prochaine visite\n", target)
	return nil
}
