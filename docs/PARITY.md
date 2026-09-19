# Vanilla parity — where tachyne stands (scorecard, 2026-09-19)

The goal is one-for-one behavioural parity with vanilla Java (the engine's canonical
content version is 1.21.11; the reference behaviour is 26.2) *before* anything that is not
vanilla is added. This page is the scorecard: every unit of vanilla's server-side surface,
enumerated mechanically (registries, data files, and the behaviour hooks each class
overrides), graded against the engine as it stands today. It replaces the 2026-07 plan
that used to live here; the per-unit ledgers with file-and-line evidence are kept outside
the repository and refreshed the same way (re-enumerate, re-grade, diff).

**Grades.** OK — matches vanilla in normal play. PARTIAL — exists, but a rule, branch or
value differs (the row says which). MISSING — not implemented. N-A — 26.2-only content,
command-block/creative-only constructs, or client-side only.

## Totals

| Dimension | Units | OK | PARTIAL | MISSING | N-A | OK share | OK+PARTIAL |
|---|---:|---:|---:|---:|---:|---:|---:|
| Block behaviour hooks A (random tick, scheduled tick, neighbour change, use, place, entity inside, comparator read) | 297 | 138 | 119 | 27 | 13 | 49% | 90% |
| Block behaviour hooks B (signals, projectile hit, removal, step/fall, survival, drops, explosions, placement state, shape updates) | 412 | 214 | 141 | 49 | 8 | 53% | 88% |
| Block entities (49) and menus (25) | 75 | 37 | 29 | 0 | 9 | 56% | 100% |
| Item behaviours and item components | 103 | 57 | 28 | 6 | 12 | 63% | 93% |
| Entity roster (attributes, spawn rules, drops, sounds, signature mechanics) | 157 | 36 | 112 | 1 | 8 | 24% | 99% |
| Monster AI (goal lists) | 46 | 2 | 41 | 2 | 1 | 4% | 96% |
| Creature, villager and golem AI (goals and brains) | 48 | 2 | 40 | 5 | 1 | 4% | 89% |
| Recipes, loot tables, advancements, statistics, tags | 162 | 81 | 44 | 37 | 0 | 50% | 77% |
| Game rules, enchantments, effects, attributes, damage types, brewing, villagers, small registries | 478 | 286 | 113 | 63 | 16 | 62% | 86% |
| World systems and worldgen | 279 | 124 | 88 | 60 | 7 | 46% | 78% |
| Player mechanics, commands, chat/social, protocol coverage | 356 | 130 | 54 | 150 | 22 | 39% | 55% |
| **All** | **2413** | **1107** | **809** | **400** | **97** | **48%** | **83%** |

Of 2316 gradeable units, 1107 (48%) are one-for-one with vanilla today, 809 (35%) exist with a
deviation, and 400 (17%) are absent. The PARTIAL column is where the work is, and most of it
traces back to a dozen cross-cutting defects; fixing each moves many rows at once.

## Cross-cutting defects

1. **Overworld hard-wiring.** Block use, redstone, comparators, lecterns, tripwires, plates and detector rails read and write the overworld whatever dimension the player is in; redstone does not work in the Nether or the End, and a scheduled update at Nether coordinates can write overworld blocks (a correctness bug, first in the queue).
2. **Support loss reacts only to a player's edit.** Pistons, explosions, fluids, falling blocks, mobs and worldgen leave torches, rails, plants and signs floating.
3. **Menus trust the client.** Slot layouts are vanilla's, but server-side slot rules, quick-move and result-taking are not vanilla's click logic; shift-click never quick-moves.
4. **Mob AI defaults.** No idle head tracking, no per-species stroll speeds, no alerting of kin when hurt, no non-player targets (golems, villagers, turtles, axolotls, squid), no preference for dark ground — five defaults that mark most mobs PARTIAL on top of their own missing goals.
5. **The off-hand is dead.** The engine's use-item event carries no hand, so a shield in the off-hand never raises.
6. **Status-effect icons never render**, because the effect packet's flags are fixed; ambient and infinite effects cannot be expressed.
7. **A stack holds at most four enchantments**, so a normal end-game sword cannot exist.
8. **Commands are a word splitter**: 34 verbs of vanilla's 95, no selectors, no relative coordinates, no `/execute`, no `/data`.
9. **Seven loot functions are dropped when the tables are baked** (treasure maps are blank paper, chest gear is always pristine, bee nests, pots and spawners lose their data).
10. **Eleven species are silent, every mob's eye height is a flat fraction of its hitbox, there are no step sounds, and babies drop nothing.**
11. **Seed parity is out of reach by construction**: terrain is a 2-D heightmap with hand-tuned noise, not vanilla's noise router, so no seed reproduces vanilla's terrain, biomes, structures or ores. That is a decision (vanilla-feeling vs vanilla-identical worldgen), not a backlog item.

## What is strong

Weather, explosions (rays, exposure, damage), fluids, redstone components, the natural spawner (biome data, caps, local caps, spawn costs), 85 of 92 entity loot tables and 1,078 of 1,083 block loot tables baked from the data, every shaped/shapeless/stonecutting/cooking/smithing recipe, all enchantment data, all brewing recipes and potions, the damage-type tables, gossip/restock/demand, the signal getter, the removal dispatch, connections and placement of fences/walls/stairs/rails/signs, trial spawners and vaults, beacons, bells, shelves, crafters, jukeboxes, sculk sensors, the template-based structures, and the signature mechanics of nearly every mob (creeper charge, enderman teleport and carry, slime split, shulker peek, guardian beam, bee sting and pollen, turtle eggs, frog eating, sniffer digging, armadillo roll, camel dash, breeze wind, bogged shearing, creaking heart, copper golem oxidation, nautilus dash).

## Work queue (top of each dimension's list, by player impact)

1. Support loss on every block change, not only a player's edits
2. Dimension-correct block use, redstone and comparators; the Nether-writes-overworld scheduled-update bug
3. Explosion drops through the real loot tables; flaming arrows prime TNT and light campfires
4. Wall torches; lever, button and grindstone attach faces; hanging lanterns; hopper facing from the clicked face; double slabs and the other count-cycling placements
5. Iron doors and trapdoors not openable by hand; door sounds; the twelve missing button kinds and ten plate kinds; hoppers collecting items under a chest
6. Redstone timing: sub-delay repeater pulses, the lamp's four-tick hold, crafter scheduling, plate and rail hold, lightning-rod power
7. Anvil material repair and book-on-book merging; grindstone curse-keeping, durability merge and XP; a persistent enchanting seed and bookshelf air gaps
8. Decorated pots (one item per insert, sherds, persistence); brewing-stand feedback and persistence; per-item stack caps in hoppers and comparators; a generic spawner block entity
9. The hand on use-item (off-hand, shields); serialising the components the engine already models (potions, stews, instruments, shulker contents, bottles, repair cost)
10. Fireworks: stars and fades, flight duration, explosions
11. Voices for the eleven silent species; per-type eye heights; ambient cadence and step sounds; babies' drops and XP
12. Monster goals: zombie village pathing and targeting, drowned water goals, the spider light rule, skeleton weapon reassessment, enderman stare and teleport, ghast and phantom flight, the raider base goals, wither phases
13. Creature brains: villager trading look/follow, POI acquisition and play; frog spawn; per-species panic; head tracking; breeding approach; nautilus, happy ghast and fish AI
14. Loot functions (exploration map, copy components, set damage); advancement predicate fidelity; per-recipe smelting XP
15. Effect HUD flags; trading XP; the mason's trade pool; the hunger effect's rate; the enchantment cap; invulnerability frames; Unbreaking on armour
16. Aquifers, springs, lava lakes, ore blobs, ravines and the ground-cover features; dust propagation within the tick; sky light through translucent blocks
17. Default spawn position; a play-state disconnect; hand swap; explosion, section-update and light packets; elytra start; suffocation; a real command parser; titles, tab list and boss-bar styles

## Versions

The engine's content is canonical 1.21.11; clients on 1.21.5–1.21.8, 26.2 and 26.3 are served through the translation chain (26.3 since 2026-09-19). Moving canonical to 26.2 was sized alongside this audit: it is a content move (most block-state and item ids renumber, forty generated tables regenerate, the translation direction flips for older clients) with one unconfirmed dependency in the Bedrock gateway's block map; it is independent of serving 26.3 and is deferred until that dependency is confirmed.

## Method, kept

- Enumerate from the reference, never from memory; cross-check registry counts against the data-generator reports.
- Grade both sides read, never a name grep; a name that exists proves nothing.
- Re-run the enumeration on a new reference version and the diff is the version bump's work list.
- Conformance tests are the definition of done for every row that moves.
