package combat

// Stats est la progression d'un personnage.
type Stats struct {
	Level, XP int
	MaxHP     int
	Atk, Def  int
	Potions   int
	Gold      int

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
	startHP      = 30
	startAtk     = 6
	startDef     = 2
	startPotions = 2

	hpPerLevel  = 6
	atkPerLevel = 2
	defPerLevel = 1
)

// NewStats renvoie un personnage de niveau 1.
func NewStats() Stats {
	return Stats{Level: 1, MaxHP: startHP, Atk: startAtk, Def: startDef, Potions: startPotions}
}

// XPToNext est l'expérience à accumuler (dans le niveau courant) pour passer au suivant.
func (s Stats) XPToNext() int { return 10 * s.Level }

// AddXP ajoute de l'expérience et applique les montées de niveau.
// Renvoie le nombre de niveaux gagnés.
func (s *Stats) AddXP(n int) (levels int) {
	s.XP += max(0, n)
	for s.XP >= s.XPToNext() {
		s.XP -= s.XPToNext()
		s.Level++
		s.MaxHP += hpPerLevel
		s.Atk += atkPerLevel
		s.Def += defPerLevel
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
	return Fighter{HP: hp, MaxHP: s.MaxHP, Atk: s.Atk, Def: s.Def}
}

// StatsAt reconstruit la fiche d'un personnage à partir de son niveau, de son
// expérience dans ce niveau et de ses potions (les caractéristiques de combat
// ne dépendent que du niveau). Les valeurs hors bornes sont ramenées dans les
// bornes valides, ce qui protège contre une sauvegarde corrompue.
func StatsAt(level, xp, potions int) Stats {
	level = max(1, level)
	s := Stats{
		Level:   level,
		MaxHP:   startHP + (level-1)*hpPerLevel,
		Atk:     startAtk + (level-1)*atkPerLevel,
		Def:     startDef + (level-1)*defPerLevel,
		Potions: max(0, min(potions, MaxPotions)),
	}
	s.XP = max(0, min(xp, s.XPToNext()-1))
	return s
}
