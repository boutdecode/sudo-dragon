// Package gear définit l'équipement : arme, armure, bouclier et casque.
//
// Un modèle de langage peut PROPOSER des objets (nom, description, palier, bonus, prix) pour l'étalage
// d'un forgeron ou d'un armurier, mais Validate est la seule porte d'entrée : elle ramène chaque valeur
// dans des bornes calculées d'après l'emplacement et le palier, et ne garde du modèle que du texte.
package gear

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kevin/ascii-adventure/internal/hero"
	"github.com/kevin/ascii-adventure/internal/lore"
	"github.com/kevin/ascii-adventure/internal/textsafe"
	"github.com/kevin/ascii-adventure/internal/world"
)

// Slot est un emplacement d'équipement.
type Slot string

const (
	Weapon Slot = "weapon"
	Armor  Slot = "armor"
	Shield Slot = "shield"
	Helm   Slot = "helm"
)

// Slots liste les emplacements, dans l'ordre d'affichage.
var Slots = []Slot{Weapon, Armor, Shield, Helm}

// Valid indique si s est un emplacement connu.
func (s Slot) Valid() bool {
	switch s {
	case Weapon, Armor, Shield, Helm:
		return true
	}
	return false
}

// Limites.
const (
	MaxName     = 40
	MaxDesc     = 120
	MaxTier     = 5
	BagCapacity = 24
	SellPercent = 40 // prix de revente, en pour cent du prix
)

// MinLevelOf est le niveau minimal pour équiper un objet d'un palier.
var MinLevelOf = [MaxTier + 1]int{0, 1, 4, 8, 13, 19}

// Plafonds de bonus par emplacement et par palier (index 1 à 5). Ils sont réglés pour que le meilleur
// ensemble d'un palier, porté à son niveau minimal, aide sans rendre invulnérable : en combat les dégâts
// valent max(1, atk − def ± 1). Voir TestBalance.
var (
	atkCap = map[Slot][MaxTier + 1]int{Weapon: {0, 2, 4, 6, 9, 12}}
	defCap = map[Slot][MaxTier + 1]int{
		Armor:  {0, 1, 1, 2, 3, 4},
		Shield: {0, 1, 1, 1, 2, 2},
		Helm:   {0, 1, 1, 1, 1, 2},
	}
)

// Item est un objet d'équipement. Atk ne concerne que l'arme, Def que les autres emplacements.
type Item struct {
	ID       int64
	Name     string
	Desc     string
	Slot     Slot
	Tier     int
	Atk, Def int
	Price    int
	Kind     hero.Kind    // style d'une arme (mêlée, distance, magie) ; vide pour le reste
	Origin   world.NPCKey // vendeur d'origine
}

// MinLevel est le niveau requis pour équiper l'objet.
func (it Item) MinLevel() int { return MinLevelOf[max(1, min(it.Tier, MaxTier))] }

// SellPrice est ce que rapporte la revente.
func (it Item) SellPrice() int { return max(1, it.Price*SellPercent/100) }

// Equipped est l'équipement porté (un objet par emplacement).
type Equipped struct{ Weapon, Armor, Shield, Helm *Item }

// Get renvoie l'objet porté dans un emplacement.
func (e Equipped) Get(s Slot) *Item {
	switch s {
	case Weapon:
		return e.Weapon
	case Armor:
		return e.Armor
	case Shield:
		return e.Shield
	case Helm:
		return e.Helm
	}
	return nil
}

// Set place (ou retire, si it est nil) l'objet d'un emplacement.
func (e *Equipped) Set(s Slot, it *Item) {
	switch s {
	case Weapon:
		e.Weapon = it
	case Armor:
		e.Armor = it
	case Shield:
		e.Shield = it
	case Helm:
		e.Helm = it
	}
}

// Bonus renvoie les bonus d'attaque et de défense de l'équipement porté.
func (e Equipped) Bonus() (atk, def int) {
	for _, s := range Slots {
		if it := e.Get(s); it != nil {
			atk += it.Atk
			def += it.Def
		}
	}
	return
}

// Bounds renvoie les bornes [min, max] du bonus principal (attaque pour l'arme, défense sinon) d'un
// emplacement et d'un palier.
func Bounds(s Slot, tier int) (lo, hi int) {
	tier = max(1, min(tier, MaxTier))
	if s == Weapon {
		hi = atkCap[s][tier]
	} else {
		hi = defCap[s][tier]
	}
	return max(1, hi*2/3), hi
}

// PriceBounds renvoie les bornes de prix d'un objet de bonus b au palier tier.
func PriceBounds(tier, bonus int) (lo, hi int) {
	ref := 10 * bonus * (1 + tier)
	return max(5, ref*7/10), max(8, ref*3/2)
}

// Proposal est ce que dit le modèle pour un objet, déjà extrait du JSON mais pas encore vérifié.
type Proposal struct {
	Slot, Name, Desc string
	Tier             int
	Atk, Def         int
	Price            int
	Kind             string // style de l'arme, facultatif (inconnu ou absent: mêlée)
}

// Context est ce que le serveur autorise pour l'étalage d'un vendeur.
type Context struct {
	Slots   []Slot // emplacements que ce vendeur peut proposer
	MaxTier int    // palier maximal du lieu
	Origin  world.NPCKey
}

// ErrInvalid est l'erreur de toute proposition refusée.
var ErrInvalid = errors.New("objet invalide")

func reject(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Validate transforme une proposition en objet, ou la refuse. L'objet n'a pas encore d'identifiant.
// La modération des textes reste à faire.
func Validate(p Proposal, c Context) (Item, error) {
	slot := Slot(strings.ToLower(strings.TrimSpace(p.Slot)))
	if !slot.Valid() {
		return Item{}, reject("emplacement inconnu %q", p.Slot)
	}
	allowed := false
	for _, s := range c.Slots {
		allowed = allowed || s == slot
	}
	if !allowed {
		return Item{}, reject("emplacement %q non vendu ici", slot)
	}
	name, desc := textsafe.Clean(p.Name, MaxName), textsafe.Clean(p.Desc, MaxDesc)
	if name == "" {
		return Item{}, reject("nom vide")
	}
	if lore.LooksLikeLink(name) || lore.LooksLikeLink(desc) {
		return Item{}, reject("lien ou adresse dans le texte")
	}
	maxTier := max(1, min(c.MaxTier, MaxTier))
	tier := max(1, min(p.Tier, maxTier))

	lo, hi := Bounds(slot, tier)
	it := Item{Name: name, Desc: desc, Slot: slot, Tier: tier, Origin: c.Origin}
	if slot == Weapon {
		it.Atk = max(lo, min(p.Atk, hi))
		it.Kind = hero.Kind(strings.ToLower(strings.TrimSpace(p.Kind))).OrMelee()
	} else {
		it.Def = max(lo, min(p.Def, hi))
	}
	plo, phi := PriceBounds(tier, it.Atk+it.Def)
	it.Price = max(plo, min(p.Price, phi))
	return it, nil
}

// Sanitize revalide un objet relu d'une sauvegarde : mêmes bornes que Validate, sans faire confiance
// aux valeurs stockées. Renvoie faux si l'objet est inexploitable.
func Sanitize(it Item) (Item, bool) {
	if !it.Slot.Valid() || it.ID <= 0 {
		return Item{}, false
	}
	name, desc := textsafe.Clean(it.Name, MaxName), textsafe.Clean(it.Desc, MaxDesc)
	if name == "" {
		return Item{}, false
	}
	it.Name, it.Desc = name, desc
	it.Tier = max(1, min(it.Tier, MaxTier))
	_, hi := Bounds(it.Slot, it.Tier)
	lo := 1
	if it.Slot == Weapon {
		it.Atk, it.Def = max(lo, min(it.Atk, hi)), 0
		it.Kind = it.Kind.OrMelee()
	} else {
		it.Def, it.Atk = max(lo, min(it.Def, hi)), 0
		it.Kind = ""
	}
	_, phi := PriceBounds(it.Tier, it.Atk+it.Def)
	it.Price = max(1, min(it.Price, phi))
	return it, true
}

// SlotsOfSmith et SlotsOfArmorer sont les emplacements vendus par chaque type de vendeur.
var (
	SlotsOfSmith   = []Slot{Weapon}
	SlotsOfArmorer = []Slot{Armor, Shield, Helm}
)
