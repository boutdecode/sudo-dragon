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

// Création du personnage : tant qu'un compte n'a pas de caractéristiques, la connexion reste dans
// cette phase, avant la boucle de jeu. Le serveur lance les dés ; le client choisit quelle
// caractéristique relancer (3 relances en tout) et la classe. Rien n'est enregistré avant la
// validation : une déconnexion donne un nouveau tirage à la connexion suivante.

const (
	createIdle     = 10 * time.Minute // inactivité tolérée pendant la création
	createMaxExcha = 40               // messages lus au plus (bornes le travail d'un client bavard)
)

// ensureStarter donne l'arme de départ de la classe (équipée) à un personnage qui n'a encore aucune arme
// dans son sac. Sans effet sinon, ni pour un personnage sans classe.
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
		return // sac plein d'autres objets: le joueur s'équipera chez le forgeron
	}
	if c.Gear.Worn == nil {
		c.Gear.Worn = map[gear.Slot]int64{}
	}
	c.Gear.Worn[gear.Weapon] = w.ID
	c.GearDirty = true
}

var errCreateAborted = errors.New("création abandonnée")

// needsCreation indique si le personnage chargé doit passer par la création.
func needsCreation(ch *store.Character) bool {
	return ch == nil || !hero.Scores(ch.Scores).Set()
}

func createView(c hero.Creation, level int) protocol.Create {
	out := protocol.Create{Scores: c.Scores[:], Left: c.Left(), Level: max(1, level)}
	for _, cl := range hero.Classes {
		out.Classes = append(out.Classes, protocol.ClassInfo{ID: string(cl), Die: cl.HitDie(), Mana: cl.ManaBase()})
	}
	return out
}

// runCreation mène la création jusqu'à la validation et renvoie le personnage complété (déjà
// enregistré). Un personnage existant garde tout sauf ses PV, sa classe et ses caractéristiques.
func (s *Server) runCreation(ctx context.Context, conn *websocket.Conn, name string, lang i18n.Lang, ch *store.Character) (*store.Character, error) {
	rng := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	cr := hero.NewCreation(rng)
	var c store.Character
	if ch != nil {
		c = *ch
	} else { // nouveau compte: mêmes débuts qu'avant la création (potions de départ, point d'apparition)
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
			cr.Reroll(hero.Ability(a.Index), rng) // indice invalide ou relances épuisées: sans effet
			if err := send(); err != nil {
				return nil, err
			}
		case protocol.CreateConfirm:
			class := hero.Class(a.Class)
			if !class.Valid() {
				if err := send(); err != nil { // classe inconnue: on redonne le tirage
					return nil, err
				}
				continue
			}
			c.Class, c.Scores = string(class), [6]int(cr.Scores)
			c.MaxHP = hero.RollHP(class, cr.Scores, level, rng)
			c.Level = level
			c.Mana = hero.MaxMana(class, cr.Scores, level)
			c.HP = c.MaxHP
			ensureStarter(&c, lang)
			s.Log.Info("personnage créé", "name", name, "class", c.Class, "scores", c.Scores)
			return &c, nil
		}
	}
	return nil, errCreateAborted
}
