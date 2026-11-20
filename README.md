# sudo Dragon

An old-school, open-world multiplayer RPG, entirely in ASCII, in your terminal.
UI built with [Bubble Tea](https://charm.land/) and Lip Gloss; authoritative server in Go.

```
╭───────────────────────────────────────────────╮╭────────────────────────╮
│"'"""""""""'"""""""'".,:~~~~~~~--~~~~~~~~~~~~~~~││hero                    │
│""""'""""""""'""".'"".""..:.~~~~~-~~~~~~~~~~~~~~~││Pos  4, 2               │
│"""""""."""""".""."."'""""""..~~~~~~~~~~~~~~~~~~~││Terrain  Plain          │
│""".""""""""""r"""""""""".'""",.~~-~~~~~~~~~~~~~~││Lvl 1   XP 4/120        │
│""""""""'"'""."".""""'"'""'""".,.~~~~~~~~~~~~~~~~││HP ########## 30/30     │
│"""""""""""""""@""""""""""""""..~~--~~~~~~~~~~~~~││Def 11 Dmg 1d6 Pot 2    │
╰───────────────────────────────────────────────╯╰────────────────────────╯
* hero joined the world
Enter: chat (/help) · arrows/ZQSD/hjkl: move · Ctrl+C: quit
```

*(schematic illustration; colours are not rendered here)*

- **Infinite world** generated from a seed with simplex noise: oceans, continents, islands, rivers, mountain ranges, and biomes that follow the climate
  (beaches, plains, forests, hills, mountains, snow, tundra, desert, swamp, jungle). Day / night cycle.
- **Villages, towns and fortresses**, also generated from the seed: safe zones, merchants, healers and guards in the former, garrisons and a warlord in the latter; **roads** between neighbouring places and travelling **caravans**.
- **Secret dungeons**, **points of interest** (campfires, shrines, lairs, ruins…) and **treasure maps** to discover.
- **Multiplayer**: everyone shares the same world, sees each other, chats and moves in real time.
- **Monsters** by biome, stronger the farther you go from the start, that wander and chase you; **turn-based combat** (sometimes against a group) with potions, fleeing, XP and levels.
- **Six-ability characters**, three classes, and five ranks of **equipment** that you buy, find or take from monsters.
- **Gathering**: wood, ore, plants and fish, with four professions that level up.
- **Gold and shops**, **quests** and **reputation**; NPCs you can chat with freely (language model, optional).
- **Accounts and saving**: username and password (bcrypt), persistent character (SQLite).

The detailed list of what has shipped is in [FEATURES.md](FEATURES.md).

## Quick start

Requirements: **Go 1.27 or later** (`go version`). On a machine where `go` is older,
point to the right one: `make GO=/path/to/go1.27/bin/go build`.

```sh
make build                 # builds server, client, bot and map tool into ./bin

./bin/sudo-dragon-server               # listens on :8080, database ./sudo-dragon.db
./bin/sudo-dragon               # in another terminal (or several, to play together)
./bin/sudo-dragon --transparent   # keeps your terminal background (otherwise the dark theme background is painted)
```

> The game used to be called "ASCII Adventure": the default database is now `sudo-dragon.db`. To keep an old
> save, rename `ascii-adventure.db` or start the server with `-db ascii-adventure.db`.

On first launch, press **Ctrl+R** to switch to "Account creation", pick a
username and a password (8 to 72 characters), then press Enter. Afterwards, just log in
with the same credentials: you pick up where you left off.

Client options: `-addr` (server address), `-name` (pre-filled username), `-transparent`, `-lang`, `-version`.
The four binaries (`sudo-dragon`, `-server`, `-bot`, `-worldview`) all accept `--version`.
To play on another server: `./bin/sudo-dragon -addr wss://game.example.org/ws -name myname`.
`SUDO_DRAGON_NO_FX=1` turns off the combat screen shake.

### Language

The game comes in **French** and **English**. The client picks the system language (variables `LC_ALL`, `LC_MESSAGES`,
`LANGUAGE`, then `LANG`, as `gettext` does): `fr_*` gives French, `en_*` gives English, and any other language gives English.
`-lang fr` or `-lang en` forces a language (`./bin/sudo-dragon -lang en`).

The language is announced to the server at login and each player is served in their own, on a single server: errors, chat
help, arrival announcements, combat log, merchants, healers, NPC titles and traits, and even the instruction given to the language
model (the NPC answers in the player's language). Proper nouns (places, first names) are shared; only common words change
("Citadelle" becomes "Citadel", "Sœur" becomes "Sister"). Shortcuts follow the mnemonics of each language
(buy `A`/`B`, sell `V`/`S`, heal `S`/`H`, talk `D`/`T`, consent `O`/`Y`, work `T`/`W`). Server logs, command-line options and tool output are in English.

The texts live in `internal/i18n` (`fr.go`, `en.go`); a test checks that both catalogues have the same keys and the same
formatting directives. To add a language: a complete catalogue, registered in `catalogs`, `Langs` and `Parse`.

## Playing

**Moving**: arrows, `Z Q S D` (AZERTY), `W A S D`, or `h j k l`. Diagonals: `y u b n`.
A player moves 5 tiles per second; every monster is slower, so you can always outrun them.

**Keys**:

| Key | Screen |
|---|---|
| `Enter` | chat |
| `I` | bag (equipment, potions, materials) |
| `P` | character sheet |
| `M` | world map |
| `Q` | quest log |
| `G` | gather (or, on the map, walk to the cursor) |
| `Ctrl+C` | quit |

**Chat**: `Enter` opens the input, `Enter` sends, `Esc` cancels. `PgUp`/`PgDn` scroll the history, `Ctrl+↑`/`Ctrl+↓` resize the chat area,
and `↑`/`↓` in the input recall the lines you sent last.

| Input | Effect |
|---|---|
| `message` | **local** chat: only nearby players (inside your view) see it |
| `/g message` | **global** chat, everyone |
| `/me action` | local emote |
| `/w username message` | **private** message |
| `/goto x y` or `/goto name` | **auto-walk** to coordinates, or to a landmark, place or dungeon entrance you have discovered (Tab completes the name) |
| `/menhir [n or name]` | travel from one activated standing stone to another (see *Points of interest*) |
| `/help` | command reminder |

**World map (`M`)**: zoom (`+` / `-`), cursor with the arrows, `0` recentres; only explored terrain is shown. `W` places or renames a **landmark** (9 at most),
`X` removes it, `G` starts auto-walking to the cursor. The map, landmarks and exploration are stored on the client side.

**Day / night cycle**: the header shows the game time (one game hour = one real minute) and the terrain darkens at night. Purely visual.

### Combat

Walk into a monster to attack it, or let it hit you. Free monsters within 5 tiles join the fight: up to **3 monsters** per fight (2 for a garrison or a dungeon).
An **initiative** roll (d20 + AGI) decides who opens: monsters that are strictly faster strike before your first turn.

The combat screen offers: `A` attack (strike, shoot or cast depending on the weapon) · `I` drink a potion (+15 HP) · `F` flee (a 50% chance).
Keys `1` to `3` do the same; up/down arrows + Enter pick the action, left/right pick the target. After a victory, `Enter` closes the screen.

- Winner: XP, gold, a 30% chance of finding a potion, sometimes a piece of equipment (see *Equipment*). Each level rolls the class hit die (+ CON bonus) and restores all HP and mana; it changes neither to-hit nor damage, which come from abilities and equipment.
- Defeated: you respawn at the last **campfire** you used (otherwise at the starting point, or at the dungeon entrance) with full HP, without losing any XP.
- Fleeing: after a flight, **no monster engages you for 5 seconds**.
- HP regenerate by one point every 3 seconds outside combat.
- During a fight, you and your opponents stay in place, out of reach of others, and appear in red to other players.
  A player idle for 60 seconds is considered to have fled.

**Rules**: every creature has a **DEF** (8 for a small creature, 10 for a humanoid or medium creature, 12 for a large one, plus the AGI modifier). To hit, you roll a **d20** and add
your modifier (STR in melee, AGI at range, INT for magic): you must roll **strictly more** than the target's DEF. A natural 20 always hits and **doubles the dice** of damage (not the bonuses); a natural 1 always misses.
Damage is the weapon's (`x d y + z`) plus the ability modifier, **minus the target's armour reduction dice** (at least 1 if the blow lands). The combat log shows the roll, the targeted DEF and the reduction.
Without mana, a magic weapon serves as a club (1d4 + STR).

### Monsters

| Monster | Glyph | Habitat | HP | DEF | To-hit | Damage | Reduction | XP | Detection |
|---|---|---|---|---|---|---|---|---|---|
| Rat | `r` | plains | 8 | 8 | +1 | 1d4 | – | 4 | 5 tiles |
| Crab | `c` | beach | 12 | 8 | +2 | 1d6 | 1d4 | 6 | 3 tiles |
| Wolf | `w` | forest | 16 | 11 | +3 | 1d6+1 | – | 10 | 9 tiles |
| Goblin | `g` | hills | 22 | 10 | +3 | 1d8 | – | 15 | 8 tiles |
| Yeti | `Y` | snow | 34 | 12 | +5 | 2d6 | 1d4 | 30 | 7 tiles |
| Scorpion | | desert | 14 | 8 | +3 | 1d6 | 1d4 | 12 | 4 tiles |
| Panther | | jungle | 24 | 12 | +4 | 1d8+1 | – | 20 | 9 tiles |
| Toad | | swamp | 14 | 8 | +2 | 1d6 | – | 8 | 4 tiles |
| Bear | | tundra | 30 | 12 | +4 | 2d6 | 1d4 | 22 | 6 tiles |
| Skeleton | `S` | fortress, dungeon | 36 | 10 | +4 | 2d6 | 1d4 | 25 | 7 tiles |
| Knight | `K` | fortress, dungeon | 90 | 12 | +7 | 2d8 | 1d8 | 50 | 7 tiles |
| Warlord | `B` | fortress | 180 | 13 | +8 | 3d10+3 | 2d6 | 250 | 6 tiles |
| Wraith | | dungeon | 30 | 12 | +5 | 2d6 | 1d4 | 30 | 7 tiles |
| Golem | | dungeon | 110 | 11 | +6 | 2d10 | 1d10 | 60 | 5 tiles |
| Lich | | dungeon (boss) | 200 | 12 | +8 | 3d10+4 | 2d6 | 300 | 7 tiles |

These are base statistics. Each monster is **scaled to its region's level** when it spawns (see *Region level*): HP and XP +25% per level beyond the first, +1 to-hit every 3 levels, +1 to damage dice every 4 levels, +1 DEF every 5 levels. Dungeon monsters follow the dungeon's tier.
Monsters are sometimes accompanied by **companions** (rats with wolves, wolves with goblins…): one more companion for every 3 player levels.

A defeated monster drops gold (between half and all of its XP). A character starts at level 1 with their class HP (see "Abilities, classes and races"), a DEF of 10 + AGI modifier, their **starting weapon** (the warrior's short sword, the rogue's short bow, the mage's staff: 1d6), 2 potions and no gold; it takes `120 × level²` XP to reach the next level (about 60 hours of active play to reach level 20).
A starting warrior beats a rat or a wolf, but not a yeti: you have to progress and gear up (this is checked by a balance test; the rogue and especially the mage, being frailer, avoid the wolf).

## The world

### Region level, regions and territories

The world rests on several subdivisions, all computed from the seed (the client recovers them without being sent anything):

- **The region grid** (256 × 256 tile squares) is the technical unit: at most one place per region, rumours that travel 1 region (the place and its neighbours), reputation per region.
- **The region level** (1 to 20) grows with distance from the starting point (one level every ~154 tiles). It sets monster strength, the rank of the equipment sold and dropped, and the tier of dungeons. The log announces when you enter a more dangerous band.
- **Territories** are named countries with irregular borders (a Voronoi diagram with jittered seeds, about 1,000 tiles wide). Their name reflects the dominant terrain
  ("Forêts d'Aube", "Plaines de Solbrune", "Côtes de Kerhaven"…) and exists in French and English. The places of a territory share a family likeness: their names draw on the same
  small family of syllables and never repeat within 3 regions. Each territory has a **climate** (temperate, tropical, arid, boreal) that changes the terrain colours; it is purely visual.

At the top of the map, the UI shows `territory · Lvl N · place · terrain (x, y)`; the place ("village Tourelle", "fortress…") only appears inside the footprint or the safe zone of a village,
town or fortress. A line is added to the log when you enter a new territory or place. NPCs know the territory of their place.
`worldview -territories -step 100` draws the territories as letters with their legend.

### Villages, towns and fortresses

Each region hosts **at most one place** (about 70% have one),
chosen according to terrain: villages on plains or in forests, towns on plains and preferably near water, fortresses on hills.
A village is always a few dozen tiles from the starting point. Places are **computed identically by the server and the client**
from the seed: nothing is sent or saved. Two neighbouring places are linked by a **road**, which crosses rivers over a bridge.

| Place | Size | Contents |
|---|---|---|
| Village | ~32 × 26 | 4 to 8 houses linked to a square by paths, a **healer**, 3 **villagers**, a **chief**, a smith or an armourer, 2 **guards** |
| Town | ~58 × 46 | wall with four gates, two avenues, a square, about twenty buildings, a **merchant**, a **healer**, 5 **villagers**, a **mayor**, a **smith**, an **armourer**, 5 **guards** |
| Fortress | 48 × 40 | thick wall with corner towers and one gate, courtyard, two barracks, partitioned keep; **8 soldiers and the warlord** |

On the map: `#` wall, `+` door, `:` path, `_` interior floor, `M` merchant, `H` healer, `F` smith, `R` armourer, `C` village chief or mayor (knows the places within 3 regions, with direction and distance, and answers even without a Mistral key), `V` villager, `!` villager who hands out quests (no trade: you can only talk to them; without a Mistral key they say an ambient line), `G` guard, `E` caravan.

**Safe zones.** A village or town, plus three tiles of margin, is a safe zone: no monster spawns or enters there,
a player inside is neither chased nor attacked by monsters, and regains 1 HP per second there (instead of one every 3 seconds).

**NPCs.** Walk into an NPC to talk to it (you stay still during the conversation; `Esc` leaves it, and it is dropped automatically after 60 seconds).

| NPC | Action | Price |
|---|---|---|
| Merchant | `B` buy a potion | 20 gold |
| Merchant | `S` sell a potion | +8 gold |
| Merchant | pick pockets (see *Theft*) | |
| Healer | `H` full healing | free at level 1, then 4 gold × level |
| Guard | where to find a resident? neighbouring places? | |

The server decides prices and refusals (not enough gold, full bag, already at full health: nothing is charged then). Every transaction is saved immediately.

**Guards.** A village has 2 guards, a town 5: they patrol the paths of the place, even in the safe zone. Walking into a guard speaks to them: they tell you where to find the healer, merchant, smith, armourer or chief, and the neighbouring places (without a Mistral key).
A player who is **Wanted** (see *Reputation*) in that place is chased and fought by the guards, even in the safe zone; killing them costs reputation.

**Theft.** The merchant offers "Pick pockets": 50% chance, modulated by charisma; a success brings in a few coins, a failure costs 3 reputation points in the place, and a merchant won't let you rob them twice within 3 minutes.

**Caravans.** On one road in two between neighbouring places travels a caravan (`E`): a travelling smith or armourer that goes back and forth. It sells equipment one rank above the forges of its region, with a stock that changes every game day.

**Fortresses.** The garrison spawns when a player approaches (within 40 tiles) and never leaves the fortress: running away is enough to escape it.
Each soldier holds a post; once defeated, it is only replaced after **5 minutes** (15 for the warlord). Moving away triggers no delay.
The **warlord**, at the centre of the keep, drops a guaranteed treasure: **100 gold and 3 potions**. As a rule of thumb, a player is comfortable against
a skeleton around level 4, a knight around 8, and the warlord around 16 (these thresholds are locked in by balance tests).

`go run ./cmd/worldview -seed 1 -sites -x 0 -y 0 -w 400 -h 0` lists the places of an area; with a non-zero height, it draws them
(`S` soldier, `K` elite, `B` warlord) to plan an expedition.

### Secret dungeons

About one region in five hides a **dungeon** whose entrance is an ordinary terrain tile: you **discover** it by passing within 3 tiles (for that character only), by turning in certain quests, or by exploring ruins or a watchtower.
A discovered entrance is shown as `>` on the map and can be reached with `/goto <name>`. Each dungeon has a unique name, a **tier** from 1 to 5 (stronger far from the start) and a layout of rooms and corridors drawn from its own seed.

Walking onto the entrance takes you in, along with free players within 5 tiles; everyone in a dungeon shares it. Guards (skeletons, wraiths), elites (knights, golems) and the **lich** (the boss) live there; **chests** give gold, potions and equipment of the dungeon's tier,
and the boss's chest, locked while the lich lives, always holds an item one rank higher. You leave by the stairs; if defeated, you wake up in front of the entrance.
A dungeon keeps its state as long as the lich lives; once it has fallen, the dungeon reforms one game day later. `M` there opens the part of the plan you have already explored.

### Points of interest and treasure maps

Small one-tile landmarks, visible on the map from the start, that you use by **walking onto them**:

| Point | Glyph | Effect |
|---|---|---|
| Campfire | `&` | heals half of your HP and mana, and becomes your **respawn point** |
| Shrine | `o` | 5-minute blessing depending on the climate (DEF, regeneration, to-hit or XP) |
| Lair | `x` | three guardians to defeat, then a chest (gold, potions, equipment) |
| Watchtower | `I` | reveals nearby dungeon entrances and explores 96 tiles of map |
| Standing stone | `\|` | activates on first visit; `/menhir` teleports to another activated stone for gold |
| Spring | `v` | restores HP and mana (an oasis in an arid climate) |
| Ruins | `/` | an elite guardian protects a chest, a lore fragment and the entrance to the nearest dungeon |

Chests from lairs and ruins, and surface monsters, sometimes drop a **treasure map**: reach the exact tile it points to (`/goto x y`) and you dig up gold, potions and equipment.
The detail of effects, delays and values is in [FEATURES.md](FEATURES.md).

### Resources and gathering

**Resource nodes** dot the terrain: trees, ore veins, plants (on plains) and fish shoals (in rivers). They keep the glyph of the terrain they sit on; only their colour (in bold) tells the resource.
Press **`G`**, on the node's tile or next to it, to attempt a harvest: a success roll (40% at profession level 1, up to 94%), then the node is exhausted for everyone for 10 minutes.
Four **professions** (lumberjack, miner, herbalist, fisher, level 1 to 10) progress with every success and enlarge the category's bag. Resources, 18 varieties plus fish, depend on the territory's climate and the region level;
they go into a **materials bag** (listed on the bag screen `I`) and aren't used yet: crafting comes next.

## Conversational NPCs (Mistral)

The **Talk** choice (key `T`) opens a free conversation with an NPC. The LLM only produces text: it never acts on
gold, items or stats; trade and healing stay in the fixed menus. Without a key the feature is absent and the game
runs normally.

**Enabling**: `export MISTRAL_API_KEY=...` then start the server (`-llm auto` by default; `-llm on` requires the key; `-llm off` disables). 
The key is never passed as an option. At startup, a probe tests chat and moderation; if the API is down, the service stays
disabled and is re-tested every 5 minutes. Options: `-llm-model`, `-llm-moderation-model` (model names change: see the
Mistral documentation), `-llm-daily-tokens` (budget per UTC day, default 1,000,000), `-llm-workers`, `-llm-base-url` (fake development server).
The budget only counts the maximum requested output tokens: it can be slightly exceeded.

**Memory and lore**: an NPC may ask the player to introduce themselves. The card (name, role, origin, goal, trait), declared by the player and therefore
unverified, is remembered per place (8 places at most per account). Other players hear it as **rumours**: visible within
1 region at most (the place and adjacent regions), with a 1-hour delay to reach the neighbouring region, forgotten after 14 days. Each NPC also keeps
a **private** summary of its conversations with each player, never shown to others: an NPC who has never spoken to the player only knows them through rumours and treats them as a stranger.
The server adds verified facts (race, level, bosses killed, deaths, quests turned in).

**Races and prejudice**: NPCs have a race too, shown in the dialogue title. Villages are almost single-race, towns are mixed. Each NPC hates
each other race to some degree (0-100 %, more in villages than in towns, the starting village being tolerant): from 20 % it is suspicious with a player of that race,
from 50 % contemptuous, from 70 % openly hostile, and it then **gives them no quest** at all (its `!` disappears). The hatred value itself stays hidden.

**Privacy**: a notice is shown at the first conversation and requires explicit consent (once per account). Without consent, nothing is sent to the provider.

**Moderation and operations**: Mistral moderation of inputs and cards (on failure, nothing is written), per-account limits and a bounded queue.
`/stats` exposes an `llm` block. On the database (server stopped):
`server -lore-list [-lore-account NAME]` and `server -lore-purge account:NAME` or `place:RX,RY`.

`make llm-smoke` runs a real API check (skipped without a key).

## Abilities, classes and races

On first login, a **creation** screen offers six abilities rolled by the server (**3d6**, i.e. 3 to 18):

| | Role |
|---|---|
| STR (strength) | **melee** attack and damage |
| AGI (agility) | **ranged** attack and damage |
| INT (intelligence) | **magic** attack and damage |
| CON (constitution) | hit points |
| WIS (wisdom) | mana |
| CHA (charisma) | how NPCs perceive you: prices, and the first impression given to their language model |

Each value gives a **modifier**: 3 → −4, 4-5 → −3, 6-7 → −2, 8-9 → −1, 10-11 → 0, 12-13 → +1, 14-15 → +2… (20-21 → +5).
You can **reroll 3 times in total** (one ability at a time, key `R`), then choose a **class** (move down past the abilities with `↑↓` to the *Class* row, then `←→`):

| Class | Hit die | Starting mana |
|---|---|---|
| Warrior | d12 | 2 |
| Rogue | d8 | 4 |
| Mage | d6 | 8 |

and a **race** (*Race* row, `←→`), which adds **+2** to one ability once the roll is confirmed (rerolls apply to the raw dice):

| Race | Bonus |
|---|---|
| Human | +2 STR |
| Elf | +2 INT |
| Dwarf | +2 CON |
| Halfling | +2 AGI |

- **HP**: level 1 = maximum die + CON modifier; each following level = rolled die + CON modifier (at least 1). Maximum HP are saved.
- **Mana** = class base + 2 per level + level × WIS modifier. A spell costs 2 mana; it comes back with HP (rest, healer, level-up).
- **Attack style = weapon carried**: sword, axe… (melee, STR); bow, crossbow (ranged, AGI); staff, wand (magic, INT, 2 mana per spell). Bare-handed: melee. The modifier adds to the to-hit roll and to damage.
- **Charisma**: ±2% per modifier point on NPC prices (capped at ±10%, after the reputation discount).
- Characters created before this version keep their level, gold, quests and bag, but go through creation again on their next login; their HP are re-rolled level by level. Characters created before races become **Humans** (+2 STR).
- The `P` key opens the **character sheet**: race and class, level, HP, mana, attack and defence (with the equipment's share), style and hit chance, charisma effect, gold, position, record, detailed abilities, worn equipment, active blessings, treasure maps, gathering professions and reputation. A summary of the abilities stays in the side panel.

## Equipment and forges

Four slots: **weapon**, **armour**, **shield** and **helmet**. Items have a **tier** (= **rank**) from 1 to 5, which is also the **required level** (1, 5, 10, 15, 20).

- **Weapon**: damage `x d y + z` where **x = rank** (1 die at rank 1, 2 at rank 2…), **y = family** (damage type) and **z = quality** (0 up to the rank). Families: piercing d4 (dagger, spear, wand), slashing d6 (swords, axe, short bow, staff),
  bludgeoning d8 (mace, crossbow), **two-handed** d12 (greatsword, great axe). The short bow, crossbow and staff are two-handed. The style (melee, ranged, magic) follows from the family.
- **Armour**: DEF bonus **and** reduction dice (rank × d4 light, d6 medium or d8 heavy, subtracted from every blow received). **Shield** and **helmet**: DEF bonus only.
- A **two-handed** weapon rules out a shield (and vice versa).

The **bag** (key `I`, 24 items) keeps everything you buy or find: `E` equips or removes the selected item, one item per slot, impossible in the middle of a fight. The side panel, the sheet and combat use the effective values.
A worn item can't be resold; resale pays 40% of the price. Gold is capped at 999,999.

**Loot**: monsters sometimes drop equipment of their region's rank (5% for a biome monster, 15% for a garrison soldier, always for the warlord); dungeon, lair and ruin chests, and treasure maps, give some too.

**Vendors**: each town has a **smith** (weapons, `F`) and an **armourer** (armour, shields, helmets, `R`); each village has only one of the two; fortresses have none. Buy with the digits `1`-`9`,
resell with `S`. The place's reputation also lowers equipment prices. The **rank on sale depends on the region**: a distant smith no longer sells beginner gear.

**Generated specialty and stock**: on the first visit, the language model invents in JSON the vendor's specialty and 6 to 8 items (names, descriptions, tier, **family**, quality, DEF bonus, price). **The server decides**: slot
sold, tier bounded by the region, family among those of the slot (the dice are never chosen by the model), quality, DEF and price brought back within bounds computed from the tier, texts cleaned and moderated, at least 4 valid items or the stock is discarded. The stock
is saved (`npc_wares`) and identical for every player; no player text enters this request. Without a Mistral key, or while the stock isn't ready, a deterministic **fallback assortment**
is offered: the game is complete without AI. A purchased item is copied whole into the bag and never changes.

Operator side: `server -wares-list` shows the generated stocks, `server -wares-reset RX,RY,RANK` clears a vendor's stock so it is regenerated. `/stats` counts generated and rejected stocks.

## Quests and reputation

Some villagers (marked with a `!` on the map; at least one per village or town) hand out **quests**. You need the AI dialogues (see above) to
receive new ones, except dungeon quests, which are offered without a key; quests in progress advance and can be turned in without a key.

**Flow**: while chatting ("I'm looking for work"), the NPC may offer a quest. The language model formalises it as JSON (title, story, objective, reward), but
**the server decides**: it only accepts objectives it can verify itself, brings the reward within caps computed from the difficulty, cleans and moderates the
texts, then presents an **Accept / Decline** card (`A` / `R`). Nothing exists until the player has accepted. The model never judges that a quest is completed.

- **Objectives**: defeat 3 to 12 monsters of one species (only victories *after* acceptance count), enter a neighbouring place (within 3 regions), defeat the warlord
  (level 10 at least, fortress within reach), or defeat the **lich of a nearby secret dungeon** (required level depends on the tier; accepting the quest reveals its entrance).
- **Reward**: gold ≤ 2 × the monsters' total experience, XP ≤ 25%, potions ≤ 2 (3 for the warlord); the thank-you text has no effect.
- **Limits**: 5 open quests at most, one per giver, 30 minutes before a new quest from the same giver, expiry after 7 days, abandoning possible. A giver with nothing to offer loses their `!`.
- **Quest log** (`Q`): quests in progress with progress and real reward, reputation and history tab (`Tab`), abandon with confirmation (`X` twice).
- **Turn-in**: you hand the quest back to the giver (keys `1`, `2`… in their menu). Gold, XP, potions and reputation are paid out at once and saved immediately.

**Reputation**: each completed quest earns 2 to 4 points in the giver's place. Tiers: Wanted (≤ −10), Suspect (≤ −5), Unknown, Known (3), Esteemed (8), Honored (15). At the Esteemed and Honored tiers, healing, potions
and equipment there cost 20% then 40% less. It **drops** by 1 for abandoning a quest, by 3 for getting caught stealing and by 5 for killing a guard, down to a minimum of −20. The place's NPCs know the player's reputation and turned-in quests; the rumour of these deeds travels like that of introductions
(1 region, 1 hour to reach the neighbouring region, forgotten after 14 days). Completed quests are facts established by the server, not player statements.

Operator side, `-lore-list` also shows the completed quests and `-lore-purge` deletes quests and reputation along with the rest. The `quests` and `reputation` data are in the SQLite database.

## Server

```sh
./bin/sudo-dragon-server -addr :8080 -db /var/lib/sudo-dragon/game.db
```

| Option | Default | Role |
|---|---|---|
| `-addr` | `:8080` | listen address |
| `-db` | `sudo-dragon.db` | SQLite database (accounts, characters) |
| `-seed` | `1` | world seed, **fixed when the database is created** |
| `-tls-cert`, `-tls-key` | | enable TLS (`wss://`) |
| `-max-registrations` | `10` | account creations per hour and per IP address (`0` = unlimited) |
| `-login-workers` | half the cores | simultaneous password hashes (see *Performance*) |
| `-version` | | print the version, then exit |
| `-pprof` | disabled | Go profiling, e.g. `127.0.0.1:6060` (never expose publicly) |
| `-llm`, `-llm-*` | `auto` | NPC dialogues through a language model (see *Conversational NPCs*) |
| `-lore-list`, `-lore-purge`, `-wares-list`, `-wares-reset` | | database administration tools, then exit |

**Data.** All persistent state lives in the SQLite file (WAL mode: `.db`, `.db-wal`, `.db-shm`).
The server saves each player on disconnect, after every fight, every 30 seconds and at shutdown
(`Ctrl+C` / `SIGTERM`: clean stop, saves flushed). For a hot backup:
`sqlite3 game.db ".backup backup.db"`, or stop the server and copy the three files.
Restarting with a different `-seed` on an existing database is refused, because the saved positions would no longer make sense;
use another database (`-db`) for a new world.

**Security.**
- Passwords are hashed with bcrypt; an unknown username and a wrong password give the same message.
- Per-IP quotas: 8 failed logins per minute, 10 account creations per hour; chat limited to a burst of 5 messages then 1 per second.
- **The password travels in clear text over `ws://`.** Outside your machine, use `wss://` (the `-tls-*` options, or a reverse proxy).
- Behind a reverse proxy, every connection appears to come from the proxy: per-IP quotas then apply to all players together.

**Reverse proxy (nginx example)**:

```nginx
location /ws {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
}
```

**Monitoring.** `GET /healthz` answers `ok`. `GET /stats` returns JSON: players, monsters, tick count,
slow ticks (over 100 ms), average/maximum tick duration, and p99 and maximum over the last 60 seconds.
These two addresses are not authenticated.

## Architecture

```
cmd/server      game server               cmd/client    TUI client
cmd/bot         automated players         cmd/worldview draws an area of the world in ASCII
internal/
  world         procedural generation: terrain in 32×32 chunks, places, roads, dungeons, points of interest (deterministic from the seed)
  resource      resource catalogue and gathering professions
  game          10 Hz game loop: players, monsters, garrisons, guards, dungeons, fights, NPCs, snapshots
  combat        turn-based rules and progression (pure, injected randomness)
  dice          dice and rolls
  hero          abilities, classes, races, modifiers
  gear          equipment, fallback stocks, loot
  quest, lore   quests, reputation, NPC memory and rumours
  llm, npcai    Mistral client, moderation, dialogues and stock generation
  textsafe      cleaning of model-produced texts
  protocol      JSON messages and fast snapshot encoder
  server        WebSocket, sessions, authentication, chat, quotas
  store         SQLite: accounts, characters, asynchronous writer
  client        Bubble Tea model and network connection
  ui, render    UI components, coloured ASCII rendering, combat sprites
  i18n          system language, French and English catalogues
```

- **Authoritative server.** The client only sends intents (movement, combat action, message); the server validates everything.
- **A single goroutine owns the game state** (`game.Run`): no lock on the state, sessions pass it commands.
- **The client generates the terrain itself** from the seed received at login: snapshots only contain the entities
  (players, monsters) within the view (±40 × ±20 tiles), and are only sent if their content has changed.
- **Merged snapshots.** A slow client skips frames instead of being disconnected; only reliable messages (chat, combat)
  piling up cause the connection to be closed. JSON encoding happens in each session's writer goroutine.
- **Asynchronous persistence.** The game loop never touches the disk; a writer batches the saves, and an immediate
  reconnection first reads back the saves still pending.

## Releases

Pre-built archives (linux and macOS on amd64/arm64) are on the GitHub *Releases* page.
Verify a download with `sha256sum -c checksums.txt --ignore-missing`.

To publish one (the tag must be on `main`; the CI runs first):

```sh
git tag v1.2.3 && git push origin v1.2.3   # a tag with a "-" (v1.2.3-rc1) is a pre-release
```

## Development

```sh
make check        # format, static analysis, tests with the race detector
make test         # tests only
make load N=250   # load test
```

The tests cover combat rules, balance, world generation, the protocol (the fast encoder is compared byte for byte
with `encoding/json`), authentication, quotas, persistence (including immediate reconnection) and the UI.

`go run ./cmd/worldview -seed 42 -x 0 -y 0 -w 120 -h 40` shows an area of the world without starting the game (`-raw -zoom N`, `-sites`, `-territories` for other views).

The list of shipped features is in [FEATURES.md](FEATURES.md); it is updated with every gameplay change.

## Performance

`make load N=…` starts an ephemeral server, creates `N` bots, gathers them at the starting point (the worst case: everyone sees everyone)
and measures the server's real tick rate in steady state. Measured on an 8-core machine, with **bots and server on the same machine**
(the bots consume a large share of the CPU: a dedicated server would do better):

| Players gathered | Tick rate | Mean tick | p99 tick | Slow ticks | Outgoing traffic |
|---|---|---|---|---|---|
| 100 | 10.0 /s | 2.7 to 3.2 ms | 8 to 23 ms | 0 | ~2.6 to 3 MB/s |
| 250 | 10.0 /s | 6.7 ms | 85 ms | 0 | ~13 MB/s |

A tick's budget is 100 ms. **Login** throughput is deliberately bounded: each connection requires a bcrypt hash
of about 60 ms of CPU, capped by `-login-workers` so as not to starve the game loop. With 250 clients connecting within
a few seconds, some wait several seconds; raising `-login-workers` speeds up logins at the cost of slower ticks.
*(These measurements predate guards, dungeons and points of interest.)*

## Known limits and ideas

- **Traffic grows as the square of the number of players in the same area**: every snapshot contains all visible entities.
  Delta encoding (additions, removals, moves) and a spatial index of players would be the next optimisation.
- **No skills or spells to learn**: magic is an attack style tied to the weapon. Gathered resources aren't used yet (no crafting).
- **The world can't be modified**: it always regenerates identically from the seed, so no chunk or place is persisted.
- **Existing worlds**: the seed is fixed when the database is created; however, places or dungeons added by a code change may appear where there was only terrain.
  A character saved in what has become a wall is moved to the nearest free tile on login.
- **In-memory state only**: fortress garrisons, guards, dungeons (defeated monsters, opened chests), lairs, points-of-interest cooldowns and exhausted resource nodes are not saved; a server restart puts everything back, including the warlord and the liches.
- **Dungeons are shared by everyone** (no private per-group instance) and a long-running server keeps unfinished ones in memory.
- **A fight pits one player against monsters**; there is no group fight between players.
- **No automatic reconnection**: after a drop, the client goes back to the login screen.
- A player who drops the connection mid-fight keeps their current HP without suffering a possible defeat.
- A single server process, with no horizontal scaling.
- Server logs, command-line options and tool output are in English; the messages sent to players are translated (see "Language").
