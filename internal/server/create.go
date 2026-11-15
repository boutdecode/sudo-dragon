package server

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/coder/websocket"

	"github.com/kevin/ascii-adventure/internal/combat"
	"github.com/kevin/ascii-adventure/internal/gear"
	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/i18n"
	"github.com/kevin/ascii-adventure/internal/protocol"
	"github.com/kevin/ascii-adventure/internal/store"
)

// Character creation: as long as an account has no abilities, the connection stays in
// this phase, before the game loop. The server rolls the dice; the client chooses which
// ability to reroll (3 rerolls in total), the class and the race (+2 to one ability). Nothing is saved before
// confirmation: a disconnection gives a new roll at the next login.

const (
	createIdle     = 10 * time.Minute // inactivity tolerated during creation
	createMaxExcha = 40               // messages read at most (bounds the work of a chatty client)
)

// ensureStarter gives the class's starting weapon (equipped) to a character who has no weapon yet
// in their bag. No effect otherwise, nor for a character without a class.
func ensureStarter(c *store.Character, lang i18n.Lang) {
	class := hero.Class(c.Class)
	if !class.Valid() {
		return
	}
	for _, it := range c.Gear.Items {
		if it.Slot == gear.Weapon {
			return
		}
	}
	w, err := c.Gear.Add(gear.Starter(lang, class))
	if err != nil {
		return // bag full of other items: the player will equip something at the blacksmith
	}
	if c.Gear.Worn == nil {
		c.Gear.Worn = map[gear.Slot]int64{}
	}
	c.Gear.Worn[gear.Weapon] = w.ID
	c.GearDirty = true
}

var errCreateAborted = errors.New("creation aborted")

// needsCreation tells whether the loaded character must go through creation.
func needsCreation(ch *store.Character) bool {
	return ch == nil || !hero.Scores(ch.Scores).Set()
}

func createView(c hero.Creation, level int) protocol.Create {
	out := protocol.Create{Scores: c.Scores[:], Left: c.Left(), Level: max(1, level)}
	for _, cl := range hero.Classes {
		out.Classes = append(out.Classes, protocol.ClassInfo{ID: string(cl), Die: cl.HitDie(), Mana: cl.ManaBase()})
	}
	for _, r := range hero.Races {
		out.Races = append(out.Races, protocol.RaceInfo{ID: string(r), Ability: int(r.Ability()), Bonus: hero.RaceBonus})
	}
	return out
}

// runCreation drives the creation up to confirmation and returns the completed character (already
// saved). An existing character keeps everything except their HP, class and abilities.
func (s *Server) runCreation(ctx context.Context, conn *websocket.Conn, name string, lang i18n.Lang, ch *store.Character) (*store.Character, error) {
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	cr := hero.NewCreation(rng)
	var c store.Character
	if ch != nil {
		c = *ch
	} else { // new account: same start as before creation existed (starting potions, spawn point)
		spawn := s.Game.World.FindSpawn()
		c = store.Character{Potions: combat.NewStats().Potions, X: spawn.X, Y: spawn.Y}
	}
	level := max(1, c.Level)

	send := func() error {
		data, err := protocol.Encode(protocol.TCreate, createView(cr, level))
		if err != nil {
			return err
		}
		wctx, cancel := context.WithTimeout(ctx, writeTimeout)
		defer cancel()
		return conn.Write(wctx, websocket.MessageText, data)
	}
	if err := send(); err != nil {
		return nil, err
	}
	for range createMaxExcha {
		rctx, cancel := context.WithTimeout(ctx, createIdle)
		_, data, err := conn.Read(rctx)
		cancel()
		if err != nil {
			return nil, err
		}
		env, err := protocol.Decode(data)
		if err != nil || env.T != protocol.TCreateAction {
			continue
		}
		var a protocol.CreateAction
		if env.Payload(&a) != nil {
			continue
		}
		switch a.Action {
		case protocol.CreateReroll:
			cr.Reroll(hero.Ability(a.Index), rng) // invalid index or rerolls exhausted: no effect
			if err := send(); err != nil {
				return nil, err
			}
		case protocol.CreateConfirm:
			class, race := hero.Class(a.Class), hero.Race(a.Race)
			if a.Race == "" {
				race = hero.Human // client predating races
			}
			if !class.Valid() || !race.Valid() {
				if err := send(); err != nil { // unknown class or race: send the roll again
					return nil, err
				}
				continue
			}
			scores := race.Apply(cr.Scores) // rerolls work on the raw dice, the bonus comes last
			c.Class, c.Race, c.Scores = string(class), string(race), [6]int(scores)
			c.MaxHP = hero.RollHP(class, scores, level, rng)
			c.Level = level
			c.Mana = hero.MaxMana(class, scores, level)
			c.HP = c.MaxHP
			ensureStarter(&c, lang)
			s.Log.Info("character created", "name", name, "class", c.Class, "race", c.Race, "scores", c.Scores)
			return &c, nil
		}
	}
	return nil, errCreateAborted
}
