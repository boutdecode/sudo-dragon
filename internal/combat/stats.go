package combat

import (
	"math/rand/v2"

	"github.com/kevin/ascii-adventure/internal/dice"
	"github.com/kevin/ascii-adventure/internal/hero"
)

// Stats est la progression d'un personnage.
type Stats struct {
	Level, XP int
	MaxHP     int // somme des dés de vie lancés (voir hero), stockée: elle ne se recalcule pas
	MaxMana   int
	Potions   int
	Gold      int

	// Base et caractéristiques tirées à la création. FOR, AGI et INT donnent le bonus de toucher et de
	// dégâts (selon le style de l'arme), AGI la DEF ; CON et SAG ont déjà servi à fixer MaxHP et MaxMana.
	Class  hero.Class
	Scores hero.Scores

	// Faits établis par le serveur sur l'histoire du personnage. Ils ne servent
	// pas au combat : les PNJ peuvent les citer comme certains, contrairement à
	// ce que le joueur dit de lui-même.
	Kills     int // monstres vaincus
	BossKills int // seigneurs de guerre vaincus
	Deaths    int // défaites
}

// MaxGold plafonne l'or d'un personnage (évite tout débordement d'entier).
const MaxGold = 999_999

// Valeurs de départ et gains par niveau.
const (
	startPotions = 2
)

// BareHands sont les dégâts à mains nues (avant le modificateur de FOR).
var BareHands = dice.D(1, 4, 0)

// NewStats renvoie un personnage de niveau 1: guerrier aux caractéristiques neutres (10).
func NewStats() Stats { return NewStatsFor(hero.Warrior, hero.Default()) }

// NewStatsFor renvoie un personnage de niveau 1 de la classe et des caractéristiques données.
func NewStatsFor(class hero.Class, scores hero.Scores) Stats {
	class = class.OrDefault()
	if !scores.Set() {
		scores = hero.Default()
	}
	scores = scores.Sanitize()
	return Stats{
		Level: 1, MaxHP: hero.FirstHP(class, scores), MaxMana: hero.MaxMana(class, scores, 1),
		Potions: startPotions, Class: class, Scores: scores,
	}
}

// Mod renvoie le modificateur d'attaque du style k (FOR, AGI ou INT).
func (s Stats) Mod(k hero.Kind) int { return s.Scores.Mod(k.OrMelee().Stat()) }

// Defense est la DEF d'un personnage sans équipement: 10 (taille moyenne) plus le modificateur d'AGI.
func (s Stats) Defense() int { return hero.DefBase(hero.Medium) + s.Scores.DefenseBonus() }

// averageHP est le gain de PV « moyen » d'un niveau (la moitié du dé, plus un, plus le mod de CON).
func (s Stats) averageHP() int {
	return max(1, s.Class.OrDefault().HitDie()/2+1+s.Scores.Mod(hero.CON))
}

// XPToNext est l'expérience à accumuler (dans le niveau courant) pour passer au suivant.
func (s Stats) XPToNext() int { return 10 * s.Level }

// AddXP ajoute de l'expérience et applique les montées de niveau: chaque niveau lance le dé
// de vie de la classe (plus le mod de CON). Avec rng nil, le gain est la valeur moyenne du dé
// (déterministe, pour les tests). Renvoie le nombre de niveaux gagnés.
func (s *Stats) AddXP(n int, rng *rand.Rand) (levels int) {
	s.XP += max(0, n)
	for s.XP >= s.XPToNext() {
		s.XP -= s.XPToNext()
		s.Level++
		if rng != nil {
			s.MaxHP += hero.LevelHP(s.Class, s.Scores, rng)
		} else {
			s.MaxHP += s.averageHP()
		}
		s.MaxMana = hero.MaxMana(s.Class, s.Scores, s.Level)
		levels++
	}
	return levels
}

// Penalize retire 25 % de l'expérience du niveau courant (mort). Le niveau ne baisse jamais.
func (s *Stats) Penalize() { s.XP = s.XP * 3 / 4 }

// AddGold ajoute de l'or jusqu'au plafond et renvoie le montant réellement ajouté.
func (s *Stats) AddGold(n int) int {
	added := max(0, min(n, MaxGold-s.Gold))
	s.Gold += added
	return added
}

// SpendGold retire n pièces d'or. Renvoie false, sans rien retirer, si l'or manque
// ou si n est négatif.
func (s *Stats) SpendGold(n int) bool {
	if n < 0 || n > s.Gold {
		return false
	}
	s.Gold -= n
	return true
}

// WithGold renvoie la fiche avec l'or donné, ramené dans [0, MaxGold]. Sert à
// restaurer une sauvegarde sans faire confiance à la valeur lue.
func (s Stats) WithGold(gold int) Stats {
	s.Gold = max(0, min(gold, MaxGold))
	return s
}

// WithRecord renvoie la fiche avec l'historique donné, chaque compteur ramené à 0 ou plus.
func (s Stats) WithRecord(kills, bossKills, deaths int) Stats {
	s.Kills, s.BossKills, s.Deaths = max(0, kills), max(0, bossKills), max(0, deaths)
	return s
}

// AddPotions ajoute des potions jusqu'au plafond et renvoie le nombre réellement ajouté.
func (s *Stats) AddPotions(n int) int {
	added := max(0, min(n, MaxPotions-s.Potions))
	s.Potions += added
	return added
}

// Fighter convertit la fiche en combattant avec hp points de vie actuels.
func (s Stats) Fighter(hp int) Fighter {
	m := s.Mod(hero.Melee)
	return Fighter{HP: hp, MaxHP: s.MaxHP, Def: s.Defense(), Hit: m, Dmg: BareHands, DmgMod: m, Kind: hero.Melee}
}

// StatsAt reconstruit la fiche d'un guerrier aux caractéristiques neutres à partir de son
// niveau, de son expérience dans ce niveau et de ses potions, avec des PV « moyens ».
func StatsAt(level, xp, potions int) Stats {
	return StatsFor(level, xp, potions, hero.Warrior, hero.Default(), 0)
}

// StatsFor reconstruit la fiche d'un personnage sauvegardé. Les valeurs hors bornes sont
// ramenées dans les bornes valides (sauvegarde corrompue); maxHP <= 0 donne des PV moyens.
func StatsFor(level, xp, potions int, class hero.Class, scores hero.Scores, maxHP int) Stats {
	level = max(1, level)
	s := NewStatsFor(class, scores)
	if maxHP <= 0 {
		maxHP = s.MaxHP + (level-1)*s.averageHP()
	}
	s.Level = level
	s.MaxHP = hero.SanitizeHP(maxHP, s.Class, s.Scores, level)
	s.MaxMana = hero.MaxMana(s.Class, s.Scores, level)
	s.Potions = max(0, min(potions, MaxPotions))
	s.XP = max(0, min(xp, s.XPToNext()-1))
	return s
}
