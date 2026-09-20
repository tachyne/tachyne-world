# Vanilla parity — where tachyne stands (scorecard, 2026-09-19; defects updated 2026-09-20)

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

Each of these marks dozens of otherwise-correct units PARTIAL, so they are worked first.
The totals above are the 2026-09-19 audit's; the fixes dated below landed after it and are
not yet re-graded (a re-grade means re-enumerating, not editing the numbers by hand).

1. ~~**Overworld hard-wiring.**~~ **Fixed 2026-09-19.** Block use, redstone, comparators, lecterns, tripwires, plates and detector rails run in the dimension the block is in; redstone works in the Nether and the End, and a scheduled update no longer writes overworld blocks from Nether coordinates.
2. ~~**Support loss reacts only to a player's edit.**~~ **Fixed 2026-09-19.** Every block change — pistons, explosions, fluids, falling blocks, mobs, worldgen — drops what it was holding up.
3. **Menus trust the client** — part fixed. Slot rules, per-item stack caps, quick-move and result-taking are server-side now (2026-09-19), as are chest/shulker lids, decorated-pot inserts and the brewing stand's bars (2026-09-20). A full port of vanilla's click logic (`doClick`/`quickMoveStack`) is still outstanding.
4. ~~**Mob AI defaults.**~~ **Fixed 2026-09-20.** Idle head tracking, per-species stroll speeds, kin alerted when one is hurt, non-player targets (golems, villagers, turtles, axolotls, squid) and the monsters' preference for dark ground are all in.
5. ~~**The off-hand is dead.**~~ **Fixed 2026-09-20.** The use-item event carries the hand; a shield raises in either.
6. ~~**Status-effect icons never render.**~~ **Fixed 2026-09-20.** Effects carry vanilla's ambient/visible/show-icon flags, and infinite effects pass through.
7. ~~**A stack holds at most four enchantments.**~~ **Fixed 2026-09-19.** Eight, as vanilla allows.
8. **Commands are a word splitter** — part fixed. Target selectors (`@s @p @a @r @e` with `type=`, `distance=`, `limit=`, `sort=`, `name=`) and `~`/`^` coordinates landed 2026-09-20. Still 34 verbs of vanilla's 95, no brigadier tree (so no client-side completion), no `/execute`, no `/data`.
9. **Loot functions dropped at bake time** — mostly fixed 2026-09-19: treasure maps, potions, names, instruments, stew effects and ominous bottles are baked and evaluated. `copy_components` (block-entity data on the drop) and `set_components` (trial-chamber gear) remain; banner patterns now ride a broken banner's drop (2026-09-20).
10. **Eleven silent species, flat eye heights, no step sounds, babies drop nothing** — mostly fixed 2026-09-19: voices, per-type eye heights, ambient cadence, step sounds and baby drops/XP are in. Splash and fall sounds remain.
11. **Seed parity is out of reach by construction**: terrain is a 2-D heightmap with hand-tuned noise, not vanilla's noise router, so no seed reproduces vanilla's terrain, biomes, structures or ores. That is a decision (vanilla-feeling vs vanilla-identical worldgen), not a backlog item.

## What is strong

Weather, explosions (rays, exposure, damage), fluids, redstone components, the natural spawner (biome data, caps, local caps, spawn costs), 85 of 92 entity loot tables and 1,078 of 1,083 block loot tables baked from the data, every shaped/shapeless/stonecutting/cooking/smithing recipe, all enchantment data, all brewing recipes and potions, the damage-type tables, gossip/restock/demand, the signal getter, the removal dispatch, connections and placement of fences/walls/stairs/rails/signs, trial spawners and vaults, beacons, bells, shelves, crafters, jukeboxes, sculk sensors, the template-based structures, and the signature mechanics of nearly every mob (creeper charge, enderman teleport and carry, slime split, shulker peek, guardian beam, bee sting and pollen, turtle eggs, frog eating, sniffer digging, armadillo roll, camel dash, breeze wind, bogged shearing, creaking heart, copper golem oxidation, nautilus dash).

## Work queue (top of each dimension's list, by player impact)

Struck-through rows have landed since the audit; the date says when.

1. ~~Support loss on every block change~~ (2026-09-19)
2. ~~Dimension-correct block use, redstone and comparators; the Nether-writes-overworld bug~~ (2026-09-19)
3. ~~Explosion drops through the real loot tables; flaming arrows prime TNT and light campfires~~ (2026-09-19)
4. ~~Wall torches; lever, button and grindstone attach faces; hanging lanterns; hopper facing; double slabs and the count-cycling placements~~ (2026-09-19)
5. ~~Iron doors and trapdoors by hand; door sounds; the missing button and plate kinds; hoppers under a chest~~ (2026-09-19)
6. ~~Redstone timing: sub-delay repeater pulses, the lamp's four-tick hold, crafter scheduling, plate and rail hold, lightning-rod power~~ (2026-09-19)
7. ~~Anvil material repair and book-on-book merging; grindstone curse-keeping, durability merge and XP; a persistent enchanting seed and bookshelf air gaps~~ (2026-09-20)
8. Decorated pots ~~(one item per insert, persistence)~~, brewing-stand feedback and persistence, per-item stack caps ~~(2026-09-20)~~ — pot sherds and a generic spawner block entity remain
9. ~~The hand on use-item (off-hand, shields)~~ (2026-09-20); serialising the components the engine already models: ~~potions, stews, repair cost~~ (2026-09-20), instruments, shulker contents and bottles remain
10. Fireworks: stars and fades, flight duration, explosions
11. ~~Voices for the eleven silent species; per-type eye heights; ambient cadence and step sounds; babies' drops and XP~~ (2026-09-19)
12. Monster goals: ~~zombie village pathing and targeting~~ (2026-09-20), ~~drowned water goals~~ (2026-09-20), ~~the spider light rule~~ (2026-09-20), ~~skeleton weapon reassessment~~ (2026-09-20), ~~enderman stare and teleport~~ (2026-09-20), ~~ghast flight~~ (2026-09-20) and phantom flight, the raider base goals, wither phases
13. Creature brains: villager trading look/follow, POI acquisition and play; ~~frog spawn~~ (2026-09-20); per-species panic; head tracking; breeding approach; nautilus, happy ghast and fish AI
14. Loot functions ~~(exploration map, set damage)~~ (2026-09-19), copy/set components; advancement predicate fidelity; per-recipe smelting XP
15. Effect HUD flags ~~(2026-09-20)~~; ~~trading XP~~ (2026-09-20); the mason's trade pool; ~~the hunger effect's rate; the enchantment cap; invulnerability frames; Unbreaking on armour~~ (2026-09-19/20)
16. Aquifers, lava lakes, ravines; ~~springs, ore blobs and the ground-cover features~~ (2026-09-20); ~~dust propagation within the tick~~ (2026-09-19); sky light through translucent blocks
17. Default spawn position; a play-state disconnect; hand swap; explosion, section-update and light packets; elytra start; suffocation; ~~selectors and relative coordinates~~ (2026-09-20) with the brigadier tree still to come; titles, tab list and boss-bar styles

## Versions

The engine's content is canonical 1.21.11; clients on 1.21.5–1.21.8, 26.2 and 26.3 are served through the translation chain (26.3 since 2026-09-19). Moving canonical to 26.2 was sized alongside this audit: it is a content move (most block-state and item ids renumber, forty generated tables regenerate, the translation direction flips for older clients) with one unconfirmed dependency in the Bedrock gateway's block map; it is independent of serving 26.3 and is deferred until that dependency is confirmed.

## Method, kept

- Enumerate from the reference, never from memory; cross-check registry counts against the data-generator reports.
- Grade both sides read, never a name grep; a name that exists proves nothing.
- Re-run the enumeration on a new reference version and the diff is the version bump's work list.
- Conformance tests are the definition of done for every row that moves.
