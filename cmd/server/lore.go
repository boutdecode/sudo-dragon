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

// Operator commands on NPC memory. They run on the database then the
// program exits: the server is not started. They also work while the server is running
// (SQLite handles concurrent access), but an NPC may then rewrite a card right afterwards.

// runLoreList prints the recorded introductions (most recent first).
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
		fmt.Fprintln(w, "no recorded introductions")
		return nil
	}
	for _, d := range deeds {
		fmt.Fprintf(w, "%-16s region (%d,%d)  %s  quest handed in: \"%s\"\n",
			d.Account, d.Place.X, d.Place.Y, d.DoneAt.Local().Format(time.DateTime), d.Title)
	}
	for _, c := range cards {
		fmt.Fprintf(w, "%-16s region (%d,%d)  %s  \"%s\" %s | %s | %s | %s  [lvl %d, lords %d, deaths %d]\n",
			c.Account, c.Place.X, c.Place.Y, c.UpdatedAt.Local().Format(time.DateTime),
			c.Alias, c.Role, c.Origin, c.Goal, c.Trait, c.Level, c.BossKills, c.Deaths)
	}
	return nil
}

// runLorePurge deletes the introductions and memories of a player ("account:NAME") or of a
// place ("place:RX,RY").
func runLorePurge(ctx context.Context, st *store.Store, target string, w io.Writer) error {
	kind, value, ok := strings.Cut(target, ":")
	if !ok || value == "" {
		return fmt.Errorf("invalid target %q: use account:NAME or place:RX,RY", target)
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
		return fmt.Errorf("invalid target %q: use account:NAME or place:RX,RY", target)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%d introduction(s), %d memory(ies) and %d quest(s) deleted for %s\n", cards, mems, quests, target)
	return nil
}

func parseRegion(s string) (int, int, error) {
	xs, ys, ok := strings.Cut(s, ",")
	x, errX := strconv.Atoi(strings.TrimSpace(xs))
	y, errY := strconv.Atoi(strings.TrimSpace(ys))
	if !ok || errX != nil || errY != nil {
		return 0, 0, fmt.Errorf("invalid region %q: expected RX,RY (two integers)", s)
	}
	return x, y, nil
}

// runWaresList prints the generated wares (one vendor per line, then their items).
func runWaresList(ctx context.Context, st *store.Store, w io.Writer) error {
	list, err := st.ListWares(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(w, "no generated wares")
		return nil
	}
	for _, wr := range list {
		fmt.Fprintf(w, "vendor %d,%d,%d  \"%s\"  (%s)\n", wr.NPC.Region.X, wr.NPC.Region.Y, wr.NPC.Index, wr.Specialty, wr.CreatedAt.Local().Format(time.DateTime))
		for _, it := range wr.Items {
			fmt.Fprintf(w, "    %-8s tier %d  %-10s %-8s quality %d DEF %d  %d gold  %s\n", it.Slot, it.Tier, it.Family, it.Damage().String(), it.Quality, it.Def, it.Price, it.Name)
		}
	}
	return nil
}

// runWaresReset deletes a vendor's wares ("RX,RY,RANK"): they will be regenerated on the next visit.
func runWaresReset(ctx context.Context, st *store.Store, target string, w io.Writer) error {
	parts := strings.Split(target, ",")
	if len(parts) != 3 {
		return fmt.Errorf("invalid target %q: expected RX,RY,RANK", target)
	}
	var n [3]int
	for i, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return fmt.Errorf("invalid target %q: %q is not an integer", target, p)
		}
		n[i] = v
	}
	ok, err := st.DeleteWares(ctx, world.NPCKey{Region: world.RegionPos{X: n[0], Y: n[1]}, Index: n[2]})
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintf(w, "no wares recorded for %s\n", target)
		return nil
	}
	fmt.Fprintf(w, "wares of %s deleted: they will be regenerated on the next visit\n", target)
	return nil
}
