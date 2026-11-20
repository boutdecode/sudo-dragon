<!--
  WORLD LORE TEMPLATE (English)

  The lore is the knowledge shared by ALL NPCs: what every inhabitant of this world knows, from the
  blacksmith to the guard. It is added to every dialogue and to the generation of the forges' stock.

  HOW TO USE
  1. Copy this file next to the server as `world-lore.en.md` (or into the directory given by
     `-world-lore DIR`). `world-lore.*.md` files are ignored by git.
  2. Write under each heading. Every HTML comment (like this block) is removed
     before sending to the model, and a heading with no text under it is removed too: leave the
     sections you do not need empty.
  3. Write the French version `world-lore.fr.md` (template: `docs/world-lore.fr.template.md`)
     and KEEP THE TWO ALIGNED. If only one file exists it serves every language and the model
     translates it on the fly.
  4. `kill -HUP <server pid>` rereads the files without a restart; an invalid file (broken UTF-8,
     unclosed comment) keeps the previous version. Forge stocks already generated do not change:
     `server -wares-reset RX,RY,RANK` regenerates them.

  WRITING TIPS
  - Maximum size: 8000 characters per language (the rest is cut). Every character costs tokens on
    every reply: keep to the essentials, in short factual sentences.
  - Write FACTS in the third person, not orders to the NPC. Each NPC's tone, speech and character
    already come from their own sheet.
  - Any NPC may repeat what is written here to a player: no secrets, no spoilers.
  - The game's peoples: Humans, Elves, Dwarves, Halflings. Grudges between peoples are already
    handled by the game (hatred percentage): describe their culture and history, not their insults.
  - Do not write the names of the game's real places (the server gives them to chiefs, mayors and
    guards) nor game numbers (gold, damage, levels): the game decides. Distant or legendary
    regions and kingdoms of this lore are welcome.
  - No real-world references (countries, religions, people, brands).
-->

# World lore

## 1. The world in a few sentences
<!-- The summary anyone could give a stranger. 3 to 5 sentences: the world's name, what sets it
     apart, the general mood. E.g. "The Broken Lands still bear the scars of the Fall of Stars..." -->

## 2. Tone and limits
<!-- Intended register (grim, folksy, wondrous, humorous...), technology level (medieval, no
     gunpowder?), subjects NPCs avoid or know nothing about. -->

## 3. Origins and cosmology
<!-- How the world began, according to common belief. The sun, the moon, the seasons, the stars;
     the ages of the world. -->

## 4. History: the great dates
<!-- 4 to 8 events everyone knows, dated relatively ("three centuries ago"). Wars, fallen
     empires, foundations, cataclysms, the last great peace. -->

## 5. The peoples
<!-- One paragraph per game people: Humans, Elves, Dwarves, Halflings.
     For each: origin, life and customs, favoured trades, values, what the others think of
     them, old quarrels (no insults). Add legendary or vanished peoples if needed. -->

### Humans

### Elves

### Dwarves

### Halflings

## 6. Magic and the supernatural
<!-- Does magic exist, who practises it, is it feared, rare, forbidden? Its price. Liches,
     spirits, curses: what common folk know. -->

## 7. Beliefs and religions
<!-- Deities or revered forces, cults, religious orders, rites (birth, death), taboos, oaths.
     Do healers belong to a cult? -->

## 8. Power and society
<!-- Who rules (kings, councils, lords), how villages, towns and fortresses live, justice and
     guards, guilds, taxes, social classes, slavery or freedom. -->

## 9. Economy and trades
<!-- The currency (the game's gold: what is it called?), what is precious or rare, renowned
     materials (metals, woods, leathers), roads and caravans, the standing of blacksmiths and
     armourers. Also used to name and describe the forges' items. -->

## 10. Dangers and creatures
<!-- Monsters as inhabitants see them: where they come from, what is feared, warlords, secret
     dungeons and their masters, beast legends. Stay qualitative: the game decides the real
     creatures and their strength. -->

## 11. General geography
<!-- Great regions, seas, mountains, climates, distant kingdoms, unknown lands. No precise
     position of a game place: guards, chiefs and mayors are given the real list of
     neighbouring places. -->

## 12. Customs and daily life
<!-- Greetings, manners, festivals and seasons, food and drink, games, proverbs, harmless
     curses, superstitions, ways of counting time. -->

## 13. Common legends and mysteries
<!-- Stories everyone tells: heroes, artefacts, lost treasures, prophecies, unsolved mysteries.
     Part true, part doubtful: NPCs may doubt them. No secrets players should discover for
     themselves. -->
