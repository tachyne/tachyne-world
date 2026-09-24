# Vanilla parity — where tachyne stands (re-graded 2026-09-24)

The goal is one-for-one behavioural parity with vanilla Java (the engine's canonical
content version is 26.3, and so is the reference behaviour) *before* anything that is not
vanilla is added. This page is the scorecard: every unit of vanilla's server-side surface,
enumerated mechanically (registries, data files, and the behaviour hooks each class
overrides), graded against the engine as it stands today. It replaces the 2026-07 plan
that used to live here; the per-unit ledgers with file-and-line evidence are kept outside
the repository and refreshed the same way (re-enumerate, re-grade, diff).

**Grades.** OK — matches vanilla in normal play. PARTIAL — exists, but a rule, branch or
value differs (the row says which). MISSING — not implemented. N-A — command-block or
creative-only constructs, or client-side only.

## Totals

| Dimension | Units | OK | PARTIAL | MISSING | N-A | OK share | OK+PARTIAL |
|---|---:|---:|---:|---:|---:|---:|---:|
| Block behaviour hooks A (random tick, scheduled tick, neighbour change, use, place, entity inside, comparator read) | 297 | 155 | 127 | 3 | 12 | 54% | 99% |
| Block behaviour hooks B (signals, projectile hit, removal, step/fall, survival, drops, explosions, placement state, shape updates) | 412 | 320 | 84 | 1 | 7 | 79% | 100% |
| Block entities (49) and menus (25) | 75 | 51 | 14 | 0 | 10 | 78% | 100% |
| Item behaviours and item components | 103 | 67 | 24 | 1 | 11 | 73% | 99% |
| Entity roster (attributes, spawn rules, drops, sounds, signature mechanics) | 157 | 62 | 87 | 0 | 8 | 42% | 100% |
| Monster AI (goal lists) | 46 | 4 | 42 | 0 | 0 | 9% | 100% |
| Creature, villager and golem AI (goals and brains) | 48 | 0 | 48 | 0 | 0 | 0% | 100% |
| Recipes, loot tables, advancements, statistics, tags | 162 | 125 | 35 | 2 | 0 | 77% | 99% |
| Game rules, enchantments, effects, attributes, damage types, brewing, villagers, small registries | 478 | 409 | 44 | 14 | 11 | 88% | 97% |
| World systems and worldgen | 279 | 134 | 101 | 39 | 5 | 49% | 86% |
| Player mechanics, commands, chat/social, protocol coverage | 356 | 166 | 51 | 117 | 22 | 50% | 65% |
| **All** | **2413** | **1493** | **657** | **177** | **86** | **64%** | **92%** |

Of 2327 gradeable units, 1493 (64%) are one-for-one with vanilla today, 657 (28%) exist
with a deviation, and 177 (8%) are absent. On 2026-09-23 it was 57%, 32% and 11%.

Every dimension was re-graded on 2026-09-24 against the current engine and 26.3. Every
PARTIAL, MISSING and N-A row was re-read in the code, and OK rows were re-checked where
their code had changed. The rest keep their earlier evidence. About one in five of the
OK rows that were re-checked turned out to be PARTIAL, so the OK count is an upper bound
until the unchecked rows are swept too.

## Largest remaining gaps (2026-09-24)

1. **Item data through menus.** A click in an inventory window rebuilds the stack from
   the item and count and restores only a few components. So potions, shulker and bundle
   contents, dye colours, lodestone targets and prior-work cost can be lost when an item is
   moved. Dropped items and death drops keep only damage and enchantments.
2. **Creature and monster AI.** Almost every goal list is PARTIAL: villager work and
   social behaviours, the wandering trader's avoid goals, fox behaviours, the raider base
   goals, the warden's melee, the piglin brain's crossbow and hunting. Hostile mobs ignore
   adventure-mode players, and only the zombie family shows its attack pose.
3. **Protocol coverage.** Other players do not see a player crouch or swim. There are no
   explosion or bulk block-update packets. Creative inventory items arrive without their
   components, chat is not signed, and keep-alive replies are not read, so latency shows 0.
4. **World generation.** No aquifers or ravines, and many ground-cover features. The Nether
   lava sea sits too low and has no bedrock roof. Changing the generator rewrites unedited
   terrain under existing builds, so these need a decision first.
5. **Shape updates and placement.** Connections re-wire only on player edits. Some
   blocks never recompute their shape (cut kelp and vines, dripleaf, bamboo, dripstone
   thickness). Several placements face the wrong way, and the soil and light rules for
   plants are too loose.
6. **Adventure mode and the sneak rule.** Adventure players can use redstone controls and
   sign editors. They cannot pick items up, and they use throwables without spending them.

## Cross-cutting defects

Each of these marks dozens of otherwise-correct units PARTIAL, so they are worked first.
The totals above are the 2026-09-24 re-grade's; the fixes dated below are counted in them.

1. ~~**Overworld hard-wiring.**~~ **Fixed 2026-09-19.** Block use, redstone, comparators, lecterns, tripwires, plates and detector rails run in the dimension the block is in; redstone works in the Nether and the End, and a scheduled update no longer writes overworld blocks from Nether coordinates.
2. ~~**Support loss reacts only to a player's edit.**~~ **Fixed 2026-09-19.** Every block change — pistons, explosions, fluids, falling blocks, mobs, worldgen — drops what it was holding up.
3. **Menus trust the client** — part fixed. Slot rules, per-item stack caps, quick-move and result-taking are server-side now (2026-09-19), as are chest/shulker lids, decorated-pot inserts and the brewing stand's bars (2026-09-20). A full port of vanilla's click logic (`doClick`/`quickMoveStack`) is still outstanding.
4. ~~**Mob AI defaults.**~~ **Fixed 2026-09-20.** Idle head tracking, per-species stroll speeds, kin alerted when one is hurt, non-player targets (golems, villagers, turtles, axolotls, squid) and the monsters' preference for dark ground are all in.
5. ~~**The off-hand is dead.**~~ **Fixed 2026-09-20.** The use-item event carries the hand; a shield raises in either.
6. ~~**Status-effect icons never render.**~~ **Fixed 2026-09-20.** Effects carry vanilla's ambient/visible/show-icon flags, and infinite effects pass through.
7. ~~**A stack holds at most four enchantments.**~~ **Fixed 2026-09-19.** Eight, as vanilla allows.
8. **Commands are a word splitter** — part fixed. Target selectors (`@s @p @a @r @e` with `type=`, `distance=`, `limit=`, `sort=`, `name=`) and `~`/`^` coordinates landed 2026-09-20. Still 34 verbs of vanilla's 95, no brigadier tree (so no client-side completion), no `/execute`, no `/data`.
9. **Loot functions dropped at bake time** — mostly fixed 2026-09-19: treasure maps, potions, names, instruments, stew effects and ominous bottles are baked and evaluated. `copy_components` (block-entity data on the drop) and `set_components` (trial-chamber gear) remain; banner patterns now ride a broken banner's drop (2026-09-20).
10. **Eleven silent species, flat eye heights, no step sounds, babies drop nothing** — mostly fixed 2026-09-19: voices, per-type eye heights, ambient cadence, step sounds and baby drops/XP are in. ~~Splash and fall sounds remain~~ (2026-09-20).
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
8. Decorated pots ~~(one item per insert, persistence)~~, brewing-stand feedback and persistence, per-item stack caps ~~(2026-09-20)~~ — ~~pot sherds~~ (2026-09-20: the pot had no recipe at all; four sherds in a diamond now make one that wears them); spawners now show the mob turning in the cage (2026-09-20), though block entities that come from world generation still carry no data of their own to the client
9. ~~The hand on use-item (off-hand, shields)~~ (2026-09-20); serialising the components the engine already models: ~~potions, stews, repair cost, shulker contents, ominous bottles~~ (2026-09-20), instruments and charged projectiles remain
10. ~~Fireworks: stars and fades, flight duration; the rocket's own blast~~ (2026-09-20 — a rocket full of stars did no damage at all)
11. ~~Voices for the eleven silent species; per-type eye heights; ambient cadence and step sounds; babies' drops and XP~~ (2026-09-19)
12. Monster goals: ~~zombie village pathing and targeting~~ (2026-09-20), ~~drowned water goals~~ (2026-09-20), ~~the spider light rule~~ (2026-09-20), ~~skeleton weapon reassessment~~ (2026-09-20), ~~enderman stare and teleport~~ (2026-09-20), ~~ghast flight~~ (2026-09-20) and phantom flight, the raider base goals, wither phases
13. Creature brains: villager trading look/follow, POI acquisition and play; ~~frog spawn~~ (2026-09-20); per-species panic; head tracking; breeding approach; nautilus, happy ghast and fish AI
14. Loot functions ~~(exploration map, set damage)~~ (2026-09-19), copy/set components; advancement predicate fidelity; per-recipe smelting XP
15. Effect HUD flags ~~(2026-09-20)~~; ~~trading XP~~ (2026-09-20); ~~the mason's trade pool~~ (2026-09-20); ~~every villager listing type — enchanted gear, explorer maps, dyed leather, suspicious stew, tipped arrows, the biome boat and the combined-cost trades — plus per-listing prices, the shuffled tier draw, the forty-tick level-up and the restock day~~ (2026-09-20); ~~the hunger effect's rate; the enchantment cap; invulnerability frames; Unbreaking on armour~~ (2026-09-19/20)
16. Aquifers, lava lakes and ravines (the only substantial world-generation gap left; changing the generator rewrites land under existing builds, so it needs a decision first); ~~springs, ore blobs and the ground-cover features~~ (2026-09-20); ~~dust propagation within the tick~~ (2026-09-19); ~~sky light through translucent blocks~~ (2026-09-20: light was costing double for water and leaves, so everything under them went dark at half the true depth)
17. ~~Death messages: thirteen ways to die read "<name> died", and three written messages were never passed for anything. Both halves come from the vanilla data now — the message id from the damage type, the English from the game's language file — with the killer, weapon and "while trying to escape" forms~~ (2026-09-20)
18. ~~Difficulty scaling: it multiplied a hostile mob's bite and nothing else. It now scales as damage reaches a player, from the damage type's own rule — Easy softens rather than halves, Peaceful erases the four always-scaled types, and mob-on-mob damage is no longer scaled at all~~ (2026-09-20)
19. Default spawn position; a play-state disconnect; hand swap; explosion, section-update and light packets; elytra start; suffocation; ~~selectors and relative coordinates~~ (2026-09-20) with the brigadier tree still to come; titles, tab list and boss-bar styles

## Versions

The engine's canonical content version is 26.3, and Java clients on 26.2 and 26.3 are served
through the translation chain. Bedrock clients are served at the current Bedrock release. The
gameplay data (loot, recipes, advancements, worldgen) is still partly 1.21.11's; where 26.3
changed it, the grades above say so.

## Method, kept

- Enumerate from the reference, never from memory; cross-check registry counts against the data-generator reports.
- Grade both sides read, never a name grep; a name that exists proves nothing.
- Re-run the enumeration on a new reference version and the diff is the version bump's work list.
- Conformance tests are the definition of done for every row that moves.
