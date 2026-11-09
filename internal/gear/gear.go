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

	"github.com/kevin/ascii-adventure/internal/dice"
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

// MinLevelOf est le niveau minimal pour équiper un objet d'un palier (le palier est aussi le rang :
// nombre de dés de l'arme ou de réduction de l'armure).
var MinLevelOf = [MaxTier + 1]int{0, 1, 5, 10, 15, 20}

// Plafonds du bonus de DEF par emplacement et par palier (index 1 à 5). Un point de DEF vaut 5 points de chance de
// toucher sur 20 : ils sont réglés par TestBalance.
var defCap = map[Slot][MaxTier + 1]int{
	Armor:  {0, 1, 1, 2, 2, 3},
	Shield: {0, 1, 2, 2, 3, 4},
	Helm:   {0, 1, 1, 1, 2, 2},
}

// Item est un objet d'équipement.
//
// Arme : dégâts « Tier d(Die de la famille) + Quality » (Damage), style (Kind) de la famille.
// Armure : bonus de DEF (Def) et dés de réduction « Tier d(Die de la famille) » (Reduction).
// Bouclier et casque : bonus de DEF seulement.
type Item struct {
	ID      int64
	Name    string
	Desc    string
	Slot    Slot
	Tier    int
	Family  string // famille d'arme ou d'armure ; vide pour un bouclier ou un casque
	Quality int    // bonus fixe aux dégâts d'une arme
	Def     int    // bonus de DEF
	Price   int
	Kind    hero.Kind    // style d'une arme (mêlée, distance, magie) ; vide pour le reste
	Origin  world.NPCKey // vendeur d'origine
}

// MinLevel est le niveau requis pour équiper l'objet.
func (it Item) MinLevel() int { return MinLevelOf[max(1, min(it.Tier, MaxTier))] }

// SellPrice est ce que rapporte la revente.
func (it Item) SellPrice() int { return max(1, it.Price*SellPercent/100) }

// Rank est le rang de l'objet (son palier, 1 à 5).
func (it Item) Rank() int { return max(1, min(it.Tier, MaxTier)) }

// FamilyInfo renvoie la famille de l'objet, si elle existe.
func (it Item) FamilyInfo() (Family, bool) { return FamilyByID(it.Slot, it.Family) }

// TwoHanded : l'objet occupe les deux mains (arme).
func (it Item) TwoHanded() bool {
	f, ok := it.FamilyInfo()
	return ok && f.TwoHanded
}

// Damage renvoie les dés de dégâts d'une arme (zéro pour le reste).
func (it Item) Damage() dice.Dice {
	if f, ok := it.FamilyInfo(); ok && it.Slot == Weapon {
		return dice.D(it.Rank(), f.Die, it.Quality)
	}
	return dice.Dice{}
}

// Reduction renvoie les dés de réduction des dégâts d'une armure (zéro pour le reste).
func (it Item) Reduction() dice.Dice {
	if f, ok := it.FamilyInfo(); ok && it.Slot == Armor {
		return dice.D(it.Rank(), f.Die, 0)
	}
	return dice.Dice{}
}

// Power résume la puissance de l'objet pour fixer son prix.
func (it Item) Power() int {
	return it.Damage().Avg() + it.Reduction().Avg() + 4*it.Def
}

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

// Defense renvoie le bonus de DEF total de l'équipement porté.
func (e Equipped) Defense() (def int) {
	for _, s := range Slots {
		if it := e.Get(s); it != nil {
			def += it.Def
		}
	}
	return
}

// Reduction renvoie les dés de réduction de l'armure portée.
func (e Equipped) Reduction() dice.Dice {
	if e.Armor == nil {
		return dice.Dice{}
	}
	return e.Armor.Reduction()
}

// Bounds renvoie les bornes [min, max] du bonus principal d'un emplacement et d'un palier : la qualité pour
// une arme, la DEF pour le reste.
func Bounds(s Slot, tier int) (lo, hi int) {
	tier = max(1, min(tier, MaxTier))
	if s == Weapon {
		return 0, tier
	}
	hi = defCap[s][tier]
	return max(1, hi*2/3), hi
}

// PriceBounds renvoie les bornes de prix d'un objet de puissance power au palier tier.
func PriceBounds(tier, power int) (lo, hi int) {
	ref := 10 * power * (1 + tier)
	return max(5, ref*7/10), max(8, ref*3/2)
}

// Proposal est ce que dit le modèle pour un objet, déjà extrait du JSON mais pas encore vérifié.
type Proposal struct {
	Slot, Name, Desc string
	Tier             int
	Family           string // famille d'arme ou d'armure (inconnue ou absente: famille par défaut)
	Quality          int    // bonus aux dégâts d'une arme
	Def              int    // bonus de DEF
	Price            int
	Kind             string // ancien format: style de l'arme, sert à choisir une famille par défaut
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

// shape ramène famille, style, qualité et DEF d'un objet dans les valeurs permises à son emplacement et à son
// palier. Les dés ne sont jamais stockés: ils découlent du palier et de la famille.
func shape(it Item, rawKind hero.Kind) Item {
	it.Tier = max(1, min(it.Tier, MaxTier))
	fam, ok := FamilyByID(it.Slot, strings.ToLower(strings.TrimSpace(it.Family)))
	if !ok {
		if it.Slot == Weapon && !rawKind.Valid() {
			rawKind = hero.Melee
		}
		fam, _ = FamilyByID(it.Slot, DefaultFamily(it.Slot, rawKind))
	}
	it.Family, it.Kind = fam.ID, ""
	lo, hi := Bounds(it.Slot, it.Tier)
	if it.Slot == Weapon {
		it.Kind = fam.Kind
		it.Quality, it.Def = max(lo, min(it.Quality, hi)), 0
	} else {
		it.Quality = 0
		it.Def = max(lo, min(it.Def, hi))
	}
	return it
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
	it := shape(Item{
		Name: name, Desc: desc, Slot: slot, Tier: max(1, min(p.Tier, maxTier)), Family: p.Family,
		Quality: p.Quality, Def: p.Def, Origin: c.Origin,
	}, hero.Kind(strings.ToLower(strings.TrimSpace(p.Kind))))
	plo, phi := PriceBounds(it.Tier, it.Power())
	it.Price = max(plo, min(p.Price, phi))
	return it, nil
}

// Sanitize revalide un objet relu d'une sauvegarde : mêmes bornes que Validate, sans faire confiance
// aux valeurs stockées (un objet d'avant les familles reçoit la famille par défaut de son style).
// Renvoie faux si l'objet est inexploitable.
func Sanitize(it Item) (Item, bool) {
	if !it.Slot.Valid() || it.ID <= 0 {
		return Item{}, false
	}
	name, desc := textsafe.Clean(it.Name, MaxName), textsafe.Clean(it.Desc, MaxDesc)
	if name == "" {
		return Item{}, false
	}
	it.Name, it.Desc = name, desc
	it = shape(it, it.Kind)
	_, phi := PriceBounds(it.Tier, it.Power())
	it.Price = max(1, min(it.Price, phi))
	return it, true
}

// SlotsOfSmith et SlotsOfArmorer sont les emplacements vendus par chaque type de vendeur.
var (
	SlotsOfSmith   = []Slot{Weapon}
	SlotsOfArmorer = []Slot{Armor, Shield, Helm}
)
