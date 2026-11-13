# Client / server audit and cheating risks

## Context
Map every feature of FEATURES.md (server, client, or both) and assess the cheating risk. The analysis was done by reading the code, without running it: "verified" = read, "assumed" = deduced.

## 1. Breakdown by feature

| Feature | Where | Authority | Risk with a modified client |
|---|---|---|---|
| World, terrain, places, roads, climates, region level, territories | **Both** (`internal/world`, deterministic from the seed) | Server for collisions | Low: the client sees the whole world (map hack), with no server effect |
| Dungeons: entrances, layouts, tier | **Both** (layout recomputed on both sides) | Server for instance, monsters, chests, boss | Medium (spoiler): all entrances and layouts are readable, the "secret" is cosmetic |
| Dungeon instance, state, reset, chests, loot | **Server** | Server | No client lever |
| Entrance discovery (`discoveries`) | **Server** (remembers) + client (display) | Server | Cosmetic: stepping on the tile is enough to enter |
| Movement | **Server** (2-tick cooldown, collisions); client only sends dx/dy ∈ {-1,0,1} | Server | No speedhack or teleport (verified) |
| `/goto`, A* pathfinding | **Client** only | Server judges the steps | None: produces normal `move`s |
| `M` map, landmarks, fog, `map.json` | **Client** only | n/a | Local (file with no size bound) |
| Rendering, FX, display i18n, illustrations | **Client** | n/a | None |
| Monsters, AI, packs, garrisons, scaling | **Server** | Server | None |
| Combat (dice, initiative, damage, fleeing, potions) | **Server**; client = display and action choice | Server | None: all rolls are server-side (but see RNG) |
| Character, creation, 3d6 roll, levels, mana | **Server** | Server | Reroll can be bypassed by reconnecting |
| Inventory, equipment, shops, prices, charisma, reputation | **Server** | Server | No duplication or negative price found |
| NPCs, services, quests (validation, rewards) | **Server** | Server (the model only proposes) | No lever; NPCs are placed deterministically so known to the client |
| LLM dialogues, lore, rumors | **Server** | Server | Prompt injection possible but with no power over the game |
| Chat, commands, DMs | **Server** relays; client displays | Server | Escape-sequence injection (see below) |
| Accounts, quotas, TLS, SQLite persistence | **Server** | Server | See flaws |

Summary: the architecture is sound, with an authoritative server. The client sends no position, no gold, no dice result. No economy or combat cheating found. The risks are mostly: information leakage (seed), network abuse, unsanitized text.

## 2. Flaws by severity

### High
1. **Unsanitized chat** (verified): `server.go:621-627` only does TrimSpace + truncation, and the client displays it as is (`model.go:649-652`, `936-953`). A player can send ANSI/OSC/bidi/fake lines to everyone (`/g`, local, `/w`). `textsafe.Clean` is only applied to NPC replies.
2. **Login/registration quotas can be bypassed in parallel** (verified): `blocked(ip)` is checked before bcrypt, the counter is incremented after (`server.go:268-304`, `292`). N simultaneous connections = N attempts or N accounts. Only the bcrypt semaphore slows this down.

### Medium
3. **No rate limit** on `move`, `combat_action`, `ping`, and `buy/sell/heal/talk/turnin`; inbox of 1024 (`game.go:296`): a flood makes the blocking `Join/Leave/Act` of other players wait. No connection cap. Exact impact assumed.
4. **Public seed + RNG derived from the seed** (`game.go:197`, `Welcome.Seed`): dungeon entrances/layouts are readable (accepted), but dice rolls are potentially predictable (assumed) since the stream is shared and the rolls are displayed.
5. **No level required to enter a dungeon** (`game.go:529`): a level 1 can reach a tier 5 and its chests.
6. **No `recover()`** in game/server: a panic in the game loop kills the server (no concrete path found).
7. **Double login**: loading happens before the nickname is reserved (`server.go:308` vs `335-346`), narrow window, risk of lost progress (no duplication).
8. **Global LLM budget** (1 M tokens/day) can be exhausted with many accounts; `Stock` has no per-account/IP limit.
9. **Plaintext password over `ws://`**; the client does not warn outside localhost (`net.go`, default `ws://localhost:8080/ws`).

### Low
10. Disconnecting during combat = free 100% flee (`leave` → `abortFight`).
11. Combat opening HP not synchronized (`fight.go:80`); loot potions overwritten in group combat (`fight.go:130,185`): integrity bugs.
12. Unlimited stat rerolls by reconnecting before confirmation.
13. `enterDungeon` forcibly drags free players within 5 tiles (griefing).
14. Account enumeration at registration; Unicode collisions `NOCASE` vs `EqualFold`.
15. `map.json`: `Explored` unbounded on read (local).
16. `clientIP` = `RemoteAddr` only (proxy = shared quota); `/stats` is public; no kick of idle players; shared `allowSay` hinders quick equipping.
17. LLM: one player's rumors injected into other players' prompts (safeguards: sanitizing, link rejection, closed moderation); private memory not moderated; alias can be imitated.

### Verified without defect
No speedhack/teleport, no negative prices or quantities, gold capped, indices checked, no IDOR, parameterized SQL, snapshots limited to the view, single-goroutine game loop, login without account enumeration.

Not audited in detail: `store/{quests,gear,lore,discoveries}.go`, `world/*`, `gear/{loot,fallback}.go`, `llm/mistral.go`.

## 3. Proposed fixes (in priority order)
1. `textsafe.Clean(text, MaxChatLen)` in `readLoop`/`handleChat` (`server.go`, `chat.go`) + sanitizing on receipt in `client/model.go`.
2. Reserve the login/registration quota before bcrypt (`server.go`, `limiter.go`), limit unauthenticated connections per IP.
3. Per-session rate limiter for `move`, `combat_action`, `ping`, `npc_action`; connection cap.
4. Initialize the game RNG with `crypto/rand` (`game.go:197`).
5. Warn about or refuse `ws://` outside localhost in the client (`client/net.go`).
6. Reserve the nickname before `LoadCharacter`; `recover()` around the game-loop tasks.
7. Fix the combat integrity bugs (points 10-11), bound `Explored` on read.
8. Design decision: require the tier level (or discovery) to enter a dungeon.

## Verification
Unit tests per fix (`make test`, `make race`, `make check` with Go 1.27 in `~/sdk/go1.27.3/bin`), a flood test with `make load`, and an update of FEATURES.md in the same commit (AGENTS.md rule).
