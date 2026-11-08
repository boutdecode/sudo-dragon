package protocol

import (
	"strconv"
	"unicode/utf8"
)

// Encodage JSON écrit à la main des instantanés.
//
// Les instantanés sont les messages les plus fréquents et les plus gros: avec
// 250 joueurs au même endroit, chacun en reçoit dix par seconde contenant
// 250 entités. Le profil a montré que l'encodage par réflexion représentait
// 70 % du CPU du serveur. Cet encodeur produit exactement les mêmes octets que
// encoding/json pour ces types (vérifié par les tests), sans réflexion.
//
// Toute modification des champs ou balises json de Snapshot, Entity, Monster
// ou Stats doit être répercutée ici; TestFastSnapshotMatchesEncodingJSON
// échoue sinon.

// appender est implémenté par les charges utiles qui savent s'encoder seules.
type appender interface{ AppendJSON(b []byte) []byte }

// sizeHinter permet d'allouer le tampon d'un coup, sans agrandissements successifs.
type sizeHinter interface{ SizeHint() int }

// SizeHint estime la taille encodée de l'instantané (entités de ~60 octets).
func (s *Snapshot) SizeHint() int { return 256 + 64*(len(s.Others)+len(s.Monsters)) }

const hexDigits = "0123456789abcdef"

// appendString ajoute s sous forme de chaîne JSON, avec les mêmes échappements
// qu'encoding/json (dont ceux de <, > et & et des séparateurs U+2028/U+2029).
func appendString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' && c != '<' && c != '>' && c != '&' {
				i++
				continue
			}
			b = append(b, s[start:i]...)
			switch c {
			case '"', '\\':
				b = append(b, '\\', c)
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default: // contrôles, <, >, &
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xf])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, `�`...)
			i += size
			start = i
			continue
		}
		if r == ' ' || r == ' ' {
			b = append(b, s[start:i]...)
			b = append(b, '\\', 'u', '2', '0', '2', hexDigits[r&0xf])
			i += size
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

func appendInt(b []byte, key string, v int) []byte {
	b = append(b, key...)
	return strconv.AppendInt(b, int64(v), 10)
}

// AppendJSON encode l'entité.
func (e Entity) AppendJSON(b []byte) []byte {
	b = appendInt(b, `{"id":`, e.ID)
	b = append(b, `,"name":`...)
	b = appendString(b, e.Name)
	b = appendInt(b, `,"x":`, e.X)
	b = appendInt(b, `,"y":`, e.Y)
	if e.Fighting {
		b = append(b, `,"fight":true`...)
	}
	return append(b, '}')
}

// AppendJSON encode le monstre.
func (m Monster) AppendJSON(b []byte) []byte {
	b = appendInt(b, `{"id":`, m.ID)
	b = append(b, `,"kind":`...)
	b = appendString(b, m.Kind)
	b = appendInt(b, `,"x":`, m.X)
	b = appendInt(b, `,"y":`, m.Y)
	if m.Fighting {
		b = append(b, `,"fight":true`...)
	}
	return append(b, '}')
}

// AppendJSON encode la fiche.
func (s Stats) AppendJSON(b []byte) []byte {
	b = appendInt(b, `{"lvl":`, s.Level)
	b = appendInt(b, `,"xp":`, s.XP)
	b = appendInt(b, `,"xpn":`, s.XPNext)
	b = appendInt(b, `,"hp":`, s.HP)
	b = appendInt(b, `,"mhp":`, s.MaxHP)
	b = appendInt(b, `,"atk":`, s.Atk)
	b = appendInt(b, `,"def":`, s.Def)
	b = appendInt(b, `,"pot":`, s.Potions)
	b = appendInt(b, `,"gold":`, s.Gold)
	return append(b, '}')
}

// AppendJSON encode l'instantané.
func (s *Snapshot) AppendJSON(b []byte) []byte {
	b = append(b, `{"tick":`...)
	b = strconv.AppendUint(b, s.Tick, 10)
	b = append(b, `,"self":`...)
	b = s.Self.AppendJSON(b)
	if len(s.Others) > 0 {
		b = append(b, `,"others":[`...)
		for i, e := range s.Others {
			if i > 0 {
				b = append(b, ',')
			}
			b = e.AppendJSON(b)
		}
		b = append(b, ']')
	}
	if len(s.Monsters) > 0 {
		b = append(b, `,"monsters":[`...)
		for i, m := range s.Monsters {
			if i > 0 {
				b = append(b, ',')
			}
			b = m.AppendJSON(b)
		}
		b = append(b, ']')
	}
	b = append(b, `,"stats":`...)
	b = s.Stats.AppendJSON(b)
	return append(b, '}')
}
