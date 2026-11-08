package render

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/kevin/ascii-adventure/internal/world"
)

// monsterArt contient les sprites de l'écran de combat: ASCII pur, chaque
// sprite a des lignes de même largeur (vérifié par un test).
var monsterArt = map[string][]string{
	"rat": {
		`   _    __ `,
		`  (o)__/  \`,
		` <(__   ) /`,
		`    "--"`,
	},
	"wolf": {
		`  /\_/\   `,
		` ( o.o )__`,
		`  > ^ <  \`,
		` /|  |\_/ `,
		` ""  ""   `,
	},
	"crab": {
		` (\/)  (\/)`,
		`  \ \__/ / `,
		`  (o    o) `,
		` _/|_||_|\_`,
		`  ^ ^  ^ ^ `,
	},
	"goblin": {
		`  ,/\  /\, `,
		` (  o  o  )`,
		`  \  ww  / `,
		` --/|__|\--`,
		`   _/  \_  `,
	},
	"skeleton": {
		`   .-.    `,
		`  (o o)   `,
		`   |=|    `,
		`  /|||\   `,
		`   / \    `,
	},
	"knight": {
		`    _/\_   `,
		`   |[__]|  `,
		`  /|    |\ `,
		` ] |____| [`,
		`   /    \  `,
	},
	"warlord": {
		` \  /\  /  `,
		`  \/  \/   `,
		` ( >  < )  `,
		`  \_/\_/   `,
		` /|=##=|\  `,
		`  /_  _\   `,
	},
	"yeti": {
		`  .-"""-.  `,
		` / o   o \ `,
		`|  \___/  |`,
		`|/|  *  |\|`,
		` \_/   \_/ `,
	},
}

// npcArt contient les portraits des PNJ pour l'écran de dialogue.
var npcArt = map[world.NPCKind][]string{
	world.NPCMerchant: {
		`   ,---.   `,
		`  ( o o )  `,
		`   \ - /   `,
		`  /|$$$|\  `,
		`   |___|   `,
	},
	world.NPCHealer: {
		`    _+_    `,
		`   (o o)   `,
		`   ( v )   `,
		`  /|   |\  `,
		`   |_+_|   `,
	},
	world.NPCSmith: {
		`   ,---.   `,
		`  ( o o )  `,
		`  /\_=_/\  `,
		`   |   |/  `,
		`  _|___|   `,
	},
	world.NPCArmorer: {
		`   .-^-.   `,
		`  [ o o ]  `,
		`  /[ = ]\  `,
		`   |###|   `,
		`   |___|   `,
	},
	world.NPCChief: {
		`   _/^\_   `,
		`  ( o o )  `,
		`   \ = /   `,
		`  /|\_/|\  `,
		`   |___|   `,
	},
	world.NPCMayor: {
		`   _/^\_   `,
		`  ( o o )  `,
		`   \ - /   `,
		`  /|$_$|\  `,
		`   |___|   `,
	},
	world.NPCVillager: {
		`   .---.   `,
		`  ( ^ ^ )  `,
		`   \ ~ /   `,
		`  /|   |\  `,
		`   |   |   `,
	},
}

var unknownNPCArt = []string{
	`   .---.   `,
	`  ( ? ? )  `,
	`   \ - /   `,
	`   /|_|\   `,
	`    / \    `,
}

// NPCArt renvoie le portrait d'un PNJ, en lignes de même largeur.
func NPCArt(kind world.NPCKind) []string {
	art, ok := npcArt[kind]
	if !ok {
		art = unknownNPCArt
	}
	return padArt(art)
}

// NPCArtStyle renvoie le style de couleur d'un PNJ.
func NPCArtStyle(kind world.NPCKind) lipgloss.Style { return npcLookOf(kind).style }

var unknownArt = []string{
	`  ?????  `,
	`  ?   ?  `,
	`    ??   `,
	`   ?     `,
	`   ?     `,
}

// MonsterArt renvoie le sprite de combat d'une espèce, en lignes de même largeur.
func MonsterArt(kind string) []string {
	art, ok := monsterArt[kind]
	if !ok {
		art = unknownArt
	}
	return padArt(art)
}

// padArt complète les lignes d'un sprite à la largeur de la plus longue.
func padArt(art []string) []string {
	width := 0
	for _, l := range art {
		width = max(width, len([]rune(l)))
	}
	out := make([]string, len(art))
	for i, l := range art {
		out[i] = l + strings.Repeat(" ", width-len([]rune(l)))
	}
	return out
}

// ArtStyle renvoie le style de couleur d'une espèce.
func ArtStyle(kind string) lipgloss.Style { return lookOf(kind).style }
