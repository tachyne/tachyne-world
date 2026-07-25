# Changelog

All notable changes to **tachyne** — the from-scratch, versionless Minecraft
server — are recorded here. tachyne is one system split across several
repositories (the world engine, the shared protocol library, and the
per-edition gateways); this is the **whole-system** timeline, so a single entry
may span more than one repo.

Entries are grouped by date, newest first, and curated for readers — iteration
and dependency-bump commits are collapsed into the feature they delivered. The
format follows [Keep a Changelog](https://keepachangelog.com/). This log covers
the public history since the project was open-sourced on 2026-07-10.

## 2026-07-25

### Added
- **Farming by hand.** A hoe now tills dirt, grass and dirt paths into farmland
  (and coarse dirt into dirt, rooted dirt into dirt plus hanging roots), and
  seeds can be planted: wheat, carrots, potatoes, beetroot, melon and pumpkin
  seeds, torchflower, and nether wart on soul sand. Previously neither worked,
  so a farm could only be laid out in creative by placing farmland and crop
  blocks directly.
- **Shovels flatten ground into dirt paths** — grass, dirt, podzol, coarse dirt,
  mycelium and rooted dirt — and put out a lit campfire.
- **Pitcher pods and torchflower seeds** can be planted and now actually grow;
  a pitcher plant becomes two blocks tall as it matures.
- The 3D map now shows **what players have built** (not just generated
  terrain), updates **live** as blocks change, and draws **player and mob
  markers**. It also covers far more ground at once: skipping the cave walls
  and deep strata that can't be seen from above cut a chunk's geometry by ~3x
  and its render time by ~16x, which bought a much larger visible area.

### Changed
- **The 3D map no longer looks grainy at a distance.** Block textures are now
  mipmapped, so terrain far from the camera resolves cleanly instead of
  shimmering as you pan. Blocks stay crisp and pixel-sharp up close.
- **Map markers can be shown and hidden individually.** A panel in the corner
  of the map lists players, player name labels, and each mob category with its
  colour and a live count, and clicking one toggles that layer. Name labels are
  also smaller than before, so they cover less of what a player is building.

### Fixed
- **Lit redstone ore goes dark again**, instead of staying lit forever once
  something disturbed it, and **nylium reverts to netherrack** when covered.
- **Amethyst geodes grow.** Budding amethyst now buds on its faces and
  advances them small → medium → large → cluster, so a geode is a renewable
  source rather than a fixed decoration. Buds grown into water stay
  waterlogged.
- **Ice and snow melt.** Neither ever did, so a torch beside a frozen pond or
  a lit path through snow changed nothing and cold biomes stayed exactly as
  generated. Melting follows the block light only, as in vanilla — daylight
  will not thaw a lake, but a torch will, and snow melting drops snowballs.
  Freezing now also stops near a light source, which it previously ignored.
- **Plants grow by light level, not by open sky.** Crops, stems, saplings and
  berry bushes used to need an unobstructed view of the sky, so torch-lit
  indoor and underground farms never grew. They now use brightness, as vanilla
  does, and read it in the right place (above the plant for saplings and berry
  bushes).
- **Saplings grew about seven times too fast** — vanilla only advances them on
  one random tick in seven, and that roll was missing.
- **Recent building no longer goes missing from the 3D map.** The map read its
  copy of the world before it started listening for changes, so anything built
  in the half-minute before it started was in neither — and stayed missing
  until it next restarted. It now subscribes first and asks the engine to flush
  the world to disk before reading it, so a restart can't lose work.
- **Placing or breaking a block no longer makes the area around you blink on
  the 3D map.** The affected terrain used to vanish while its replacement was
  fetched; it now stays on screen until the new geometry is ready, so only the
  block that actually changed appears to change.
- **Saplings grow their own tree.** Acacia, cherry, dark oak, jungle and pale
  oak saplings never grew at all, and oak, birch and spruce all produced an
  *oak* tree. Every species now grows itself, using the same shapes the world
  generator uses for its forests, so a planted spruce matches a wild one. Dark
  oak and pale oak need four saplings in a square, as in vanilla — a lone one
  will not grow.
- **The world simulates each dimension separately.** Block growth and updates
  only ever ran in the Overworld, and were driven by every player's position
  regardless of where they actually were — so standing in the Nether grew an
  Overworld farm at the same coordinates, while nothing in the Nether or the
  End ticked at all. Farms, fluids and fire now behave the same in every
  dimension.
- **Sugar cane and cactus grow again.** Both were matched against block ids
  from an older Minecraft version, so neither ever grew, and hanging signs
  could occasionally stack a copy of themselves.
- **Nether wart grows at the vanilla rate** and now respects the
  `randomTickSpeed` game rule, including `0`, which previously did not stop it.
- **Mycelium spreads and reverts to dirt** when covered, which it never did.
  Grass spreading was corrected at the same time: it now makes four attempts a
  tick instead of one, can creep down a slope, and needs light to spread.
- **All copper weathers.** Copper bars, chain, lanterns, lightning rods,
  chests and golem statues never aged; only nine of the fifteen copper block
  lines were wired up.

### Removed
- **BlueMap and the last JVM.** The Java 3D-map renderer, its bundled runtime,
  and the daemon that exported the world to the vanilla Anvil format to feed it
  are all gone — `tachyne-map` renders natively in Go. tachyne now runs with no
  Java anywhere. `cmd/anvil-export` remains as a standalone tool for exporting
  a tachyne world as a vanilla Minecraft save.

## 2026-07-24

### Added
- **A native 3D web map** (new component, `tachyne-map`) — the world rendered in
  the browser, with no Java anywhere in the pipeline. Blockstates, block models,
  the texture atlas, and biome colormaps are parsed and meshed in pure Go, with
  face culling, per-block light, and biome tint baked into the geometry. The
  viewer streams tiles around the camera and unloads them behind you, so the
  whole world is explorable with bounded memory. It follows the running server:
  blocks placed in game appear within about a second, existing builds are read
  from the engine's edit overlay, and players show as live markers. The engine
  is never disturbed — the map asks it for the world seed over the bus and
  reads the world read-only through a new public `worldread` facade.
- **Enderman block-carry** — endermen pick up holdable blocks from the world and
  set them back down elsewhere, rendered held in their hands (mob-griefing
  gated, and persisted across restarts).
- **Dispensers**: egg variants, spectral and tipped arrows (a tipped arrow
  applies its potion's effects on a hit), the powder-snow bucket, and
  armor-stand placement.

### Changed
- **Dispensers and droppers** now fire on vanilla's 4-tick delay instead of
  instantly, and respond to quasi-connectivity (a redstone signal on the block
  directly above them).

## 2026-07-22

### Added
- **Zombie sieges** — on a random night, a horde of zombies gathers at the edge
  of a village and attacks it.

## 2026-07-20

### Added
- **Shulker bullets** now home in on their target and inflict Levitation on a
  hit, instead of flying straight.

## 2026-07-19

### Added
- **Structures from real vanilla templates**: woodland mansions (with their
  evoker/vindicator/allay occupants), ocean monuments, trial chambers,
  shipwrecks, ruined portals, and village variants for the desert, savanna,
  snowy, and taiga biomes.
- **Thrown potions** — splash area-of-effect and lingering effect clouds.
- **Beach waves** — an opt-in cosmetic overlay (`-waves`): water washes up over
  the sand and rolls back into the ocean. Purely visual and client-only; it is
  never written to the world.
- More **dispenser** behaviors: wind charge, water-bottle-to-mud, glass-bottle
  filling, wither-skull placement, and equipping wearables.
- **Auto-crafter** full menu — recipe-result preview and per-slot disable
  toggles.

### Changed
- **Fluid flow** rewritten to follow vanilla's algorithm, fixing leveling,
  spread, and left-behind-water artifacts.
- **Mob population** now capped at vanilla's per-player ceiling.
- A large **vanilla-fidelity** pass across combat (enchantments, criticals,
  sweep, knockback), crop and stem growth, the anvil (prior-work cost and the
  "Too Expensive" limit), brewing fuel, villager trading, survival mechanics,
  and spawning.

## 2026-07-18

### Added
- **Jigsaw structure assembler** — villages, pillager outposts, ancient cities,
  and igloos generate from real vanilla jigsaw templates.
- **Sculk & the Warden** — the full deep-dark ecosystem: game-event vibrations,
  sculk sensors, shriekers, and catalyst, plus Warden AI (darkness aura, sonic
  boom, dig-away).
- **Ocean structures and Guardian AI** — shipwrecks, buried treasure, and ocean
  monuments.
- **Copper bulb** redstone component.
- Players now **respawn at their last position** on login.

### Changed
- **Chat** now delivers reliably under load and shows other players' messages
  correctly on offline-mode and 26.2 clients.

### Fixed
- Exponential mob duplication that could occur on autosave-then-unload.

## 2026-07-17

### Added
- **Fishing** — the rod, the bobber's full state machine, and the vanilla loot
  pools (fish / junk / treasure, with Lure and Luck of the Sea).
- **Buckets and cauldrons** — scoop and pour water, lava, and powder snow;
  cauldrons fill from rain/snow and drain.
- **Data-driven loot** for structure chests and village house chests.
- **Mob persistence** — villagers keep their trades and villages stay populated
  across restarts.

### Fixed
- Dispenser bucket handling and dropper-to-container item piping.
- Loot rolls capped at the real vanilla enchantment maxima.

## 2026-07-16

### Added
- **The mace** — the smash attack, with Density, Breach, and Wind Burst.
- **Mob persistence** — mobs and their state survive a server restart.

## 2026-07-15

### Added
- **Crossbows** — charge / load / fire, with Quick Charge, Multishot, and
  Piercing.
- **Tridents** — throwing, Loyalty, Riptide, and Impaling.
- A selectable exact-vanilla mob spawner.

## 2026-07-14

### Added
- **Redstone, tier 2** — the auto-crafter, target block, tripwire and tripwire
  hooks, note blocks, repeater locking, piston quasi-connectivity, and
  comparators reading containers through a solid block.
- **Amethyst geodes**, plus emerald ore (mountain biomes) and redstone/lapis ore
  distribution.
- **Mob behaviors** — zombie↔drowned and husk↔zombie water conversions, husk
  Hunger bites, stray Slowness arrows, drowned throwing tridents, and mobs that
  pick up, wear, and drop equipment.
- More dispenser behaviors — spawn eggs, shears, boats and minecarts, honeycomb
  waxing, bone meal, and flint & steel.

### Fixed
- Reliable delivery for entity and player lifecycle updates, eliminating frozen
  "ghost" mobs.

## 2026-07-13

### Added
- **Workstations** — blast furnace, smoker, campfire cooking, loom, smithing
  table, stonecutter, cartography table, and the lectern + chiseled bookshelf.
- **Beacon** — pyramid tiers, the payment menu, and area effects.
- **Books** (writing, signing, reading), **mount inventories** (horse, donkey,
  mule, llama, camel), **double chests**, and **armor stands**.
- **Data-driven loot tables** for blocks and entities.
- **Growth** — cocoa, sweet berries, and melon/pumpkin stems, plus bone meal.
- **Snow and ice formation**, farmland hydration and trampling, and
  lava-adjacency fire.
- **Fluids** — infinite sources, concrete, and waterlogging.
- **Copper oxidation** over time.
- Command and gamerule parity passes.

## 2026-07-12

### Added
- **Plugin system** — an in-process Go plugin API (Bukkit-shaped events and
  facades, compiled in), an out-of-process message bus, a hot-reloading plugin
  manager, and an in-game `/plugin` browser.
- **BlueMap** — a 3D web map, served by exporting the world to the Anvil format.
- **Filled maps**, **item frames** (regular and glow, on all six faces, framed
  maps included), and **note blocks + jukeboxes**.

## 2026-07-11

### Added
- **Advancements** — the vanilla 1.21.11 tree with an engine-side criteria
  tracker and vanilla frontier-only visibility.
- **Recipe book** with vanilla unlock progression.
- **Statistics**, **scoreboards, and teams**.
- **Natural mob spawning** — cave spawns, light rules, mob caps, and spawn pools.
- **Weather** — the vanilla two-timer cycle, lightning that seeks rods, and
  persistence.
- **Signs** (placement, edit GUI, persistence), **banners and mob heads**,
  **paintings**, **walls** and **stairs** with full vanilla connection/corner
  shapes, flower pots, and bell attachment.

## 2026-07-10

### Added
- **Open-sourced** — initial public release of every component (the world
  engine, the shared `tachyne-common` protocol library, the Java gateways, the
  Bedrock gateway, the ingress front door, and the access service) under
  Apache-2.0, each with CI that publishes container images on every push.
- **Tall worlds** — a configurable overworld ceiling (`-ceiling`) for true-scale
  terrain, carried end-to-end through the chunk codec and renderers.
- Chunks now **stream nearest-first** from the player, with paced delivery and a
  configurable render-distance cap.

### Fixed
- Inverted rain game-event ids that made rain invisible.
- Per-dimension chunk-cache budgets to prevent Nether out-of-memory.
