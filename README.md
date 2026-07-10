# tachyne

> tachyne is an unofficial fan project, not affiliated with Mojang,
> Microsoft, or Minecraft's developer/publisher in any way. See the
> Disclaimer at the bottom.


A Minecraft **Java Edition** server written from scratch in pure Go. No Paper,
no JVM, no protocol libraries — world generation, lighting, and the whole game
simulation are original code with zero runtime dependencies.

This repo is the **world engine** of the *tachyne* cluster: it speaks **no
Minecraft wire format at all** ("worlds are versionless"). It emits typed
domain events over the **attach protocol** (`-attach :25500`); per-protocol
**gateway pods** terminate real clients, render events into canonical **1.21.5
(770)** wire via the shared `tachyne-common/render770` package, and apply a
translation chain for newer clients. Deployed today: 1.21.5–1.21.8 clients via
`tachyne-gw-java-770`, 26.2 (776) via `tachyne-gw-java-776`, both behind the
version-routing `tachyne-ingress` front door. See `docs/DOMAIN-EVENTS.md`
(the architecture and how it got this way) and `docs/SHARDING.md` (the
multi-pod plan).

## What works today

**World**
- Seeded procedural terrain: continents, erosion, ridged peaks, 3D caves with
  natural surface entrances, ore veins (coal → diamond)
- **The full biome system** (`worldgen/biomes.go`): a temperature × humidity ×
  elevation climate model places the whole 1.21.5 overworld set — plains,
  forests (oak/birch/dark-oak/flower/cherry), taigas (spruce + old-growth),
  jungles (+ bamboo/sparse), savanna, deserts, badlands (banded terracotta),
  swamps + mangrove, meadows, mushroom islands, snowy plains/taiga/slopes,
  peak tiers (frozen/jagged/stony), windswept hills, every ocean temperature
  variant, **carved rivers**, and vertical cave biomes (dripstone/lush/deep_dark);
  the Nether and End are split into their sub-biomes too. Each biome lays its own
  surface blocks and decorates with the matching trees + ground flora, which in
  turn drives the habitat-specific mob spawns.
- Real two-pass lighting (sky + block); torches light caves, dawn light ramps
- Block breaking/placement with orientation (stairs, slabs, logs, furnaces),
  tool-gated drops, falling sand/gravel, flowing water/lava, crop growth
- Fire (flint & steel, burn-out, a real burning status doused by water/rain)
  and TNT — primed-entity fuse, chain reactions, blast resistance honored
- Persistent world: block edits survive restarts (edits-as-diffs); generated
  chunks cached in Valkey/Redis or a local dir (pure cache — always safe)

**Survival**
- Health, hunger, saturation/exhaustion, fall/drown/lava/cactus/void damage,
  eating with the vanilla chew timer, death drops + bed respawn points
- Full inventory + 2×2/3×3 crafting, furnaces (fuel + smelting, persisted),
  chests (27 slots, persisted), beds (sleep skips night with the vanilla fade)
- Tool durability + functional armor (damage reduction, visible on others)
- **XP**: orbs from player kills, coal/diamond mining and smelting (paid when
  you collect the output), orb magnetism, the vanilla level curve, 7×level
  dropped on death; levels persist with the inventory
- **Status effects**: regeneration, poison, strength/weakness, speed,
  fire resistance and instant health/harm — golden apples work, witches
  poison, /effect for ops
- **Enchanting**: real enchanting table UI — lapis + levels, three rolled
  offers scaled by surrounding bookshelves; sharpness, efficiency, protection,
  unbreaking, Fortune (multiplies ore yields), Looting (fatter mob drops),
  Silk Touch (the block itself) — all with server-side effects, glinting on
  every supported client version; plain books enchant into enchanted books
- **Anvil + grindstone**: merge enchantments (books included, equal levels
  combine upward), repair from a sacrifice, rename items — for enforced level
  costs; the grindstone strips enchantments and refunds XP

**Combat**
- 1.9-style attack cooldown (spam-clicks scaled to a fifth), jump crits at
  ×1.5 with particles, sword sweep, real knockback that sends mobs flying
  (sprint hits shove harder), full swing/crit/sweep sound set
- Player bows: charge-held draw (speed, damage and pitch scale with it),
  arrows consume ammo, hit mobs with knockback + XP credit, and stick in
  walls where you can walk over and retrieve them; snowballs + eggs throw

**Mobs**
- Farm animals — cows, chickens (eggs, and thrown eggs hatch chicks), pigs,
  sheep (shear for wool, it regrows) — in herds that wander, graze and rest;
  feed a pair their love-food and they breed a baby that grows up
- Hostiles: zombies, skeletons (kiting bow AI, real arrows), spiders,
  creepers (fuse + craters), biome variants (desert husks, snowy strays,
  shoreline drowned), slimes that split when killed, neutral endermen that
  teleport and drop pearls (throwable — with the 5 HP landing toll), and
  potion-lobbing witches
- **The full 1.21.5 creature roster (`species.go`)** — every remaining vanilla
  species as a data table, attributes matching the vanilla
  server source: land passives (wolves/foxes/goats/rabbits/horses/pandas/
  polar bears…), water life (squid, cod/salmon/tropical fish, dolphins,
  guardians), flyers (bats, parrots, bees, phantoms), the illager patrol
  (pillager/vindicator/evoker/illusioner + vexes), nether mobs (piglin,
  piglin brute, hoglin/zoglin, strider, wither skeleton, ghast), and the
  wither boss. Each carries its real health/speed/armor/follow-range, melee
  or projectile attack, loot table, XP value and sounds; mapped onto shared
  archetypes (swim/fly/walk locomotion, skittish flight, neutral-pack
  retaliation, ranged kiting). Biome-aware natural spawning stocks the
  countryside, seas and Nether; all are `/summon`-able by name.
- Signature behaviors from source: cave-spider/bee poison bites (normal+hard
  only), wither-skeleton wither, wolf/bee packs that turn on an attacker
  together, ghast/breeze/shulker/wither projectiles, the guardian charge-beam,
  phantoms diving from the night sky
- **Riding + taming**: saddle and ride horses/donkeys/mules/camels/pigs/striders
  (client-simulated movement over the vehicle path, sneak to dismount); tame
  wolves with bones, cats with cod, parrots with seeds — tamed pets follow you,
  sit on an empty-handed right-click, and teleport to you when left too far behind
- Interest-managed relaying + periodic absolute resync; measured to ~150
  concurrent players on one box (measured with the since-deleted `cmd/swarm`
  TCP load tester — pre-`c15e1e4` in git history)

**Feel**
- Weather: rain and thunderstorms on vanilla cycles, real lightning strikes
  (flash + thunder + damage), rain shields the undead at dawn, sleeping
  clears the storm, /weather for ops
- Sounds throughout — mob growls/hurt/death, combat, explosions (with the
  real boom + mushroom cloud), XP dings, level-ups, chests, enchanting —
  sent inline-by-name so they work identically on every client version
- Block-break particles + sound for other players (world events)

**Redstone (complete tier 1)**
- Power sources (levers, buttons with real press timers, redstone torches
  that invert, redstone blocks), dust that carries decaying 15-block signal,
  and consumers: lamps, iron doors, and TNT that primes when powered —
  simulated as a one-block-per-tick ripple on the server tick loop
- Repeaters (directional, right-click cycles the 1-4 delay, refresh signal
  to 15), comparators (compare + subtract modes, and they measure container
  fullness through their back), observers (watch a block, 2-tick pulse on
  change), pressure plates (entity occupancy scanned every tick; weighted
  plates count standers) and daylight detectors (track the sun, invertible)
- Pistons + sticky pistons: push up to 12 blocks, crush fragile ones, sticky
  retraction pulls; obsidian/bedrock/containers are immovable
- Dispensers (shoot arrows, prime TNT, pour buckets), droppers, and hoppers
  (pull from the container above, vacuum dropped items, push into the
  container they face — furnaces take input from above, fuel from the side;
  powering a hopper pauses it) — all with real openable windows, contents
  persisted like chests

**The End (complete)**
- A third dimension: the floating end-stone island with its ring of ten
  obsidian pillars over the void, black-sky End ambience via the dimension
  registry, the vanilla 5×5 obsidian spawn platform — `/end` (op) travels
  there
- Strongholds: buried stone-brick portal rooms far from spawn holding the
  twelve-frame end-portal ring over lava; throw eyes of ender to find them
  (the eye flies toward the nearest stronghold), fill the frames — twelve
  eyes open the portal, step in to travel; the End's exit portal comes home
- The dragon fight: the ender dragon circles the pillar ring, swoops at
  survival players, and heals while any end crystal survives — pop the
  crystals (they explode), bring it down, and the exit portal opens with
  the dragon egg, an XP shower, and the elytra as the prize (glide speeds
  are honored by the movement authority)

**The Nether (foundation)**
- A real second dimension: nether-mode terrain generator (cavern sponge
  through netherrack, lava sea, glowstone crusts, soul sand, quartz ore,
  bedrock floor), its own persisted world file and chunk cache, no sky
  light, vanilla nether fog/visuals via the dimension registry — `/nether`
  (op) toggles dimensions with vanilla 8:1 coordinate scaling
- Real obsidian portals: build a frame (up to 21×21), light it with flint &
  steel, stand inside 4 seconds (instant-ish in creative) and travel — the
  far side builds a linked return portal on arrival
- Brewing: wild nether wart on soul-sand floors (plantable — farms grow in
  the overworld), water bottles from any water, and the brewing stand: wart
  makes Awkward, then blaze powder/sugar/glistering melon/spider eye/magma
  cream/golden carrot brew Strength/Swiftness/Healing/Poison/Fire
  Resistance/Night Vision — all drinkable, bottle returned
- Nether mobs: zombified piglins (neutral until you hit one — then the pack
  holds a grudge), magma cubes (split on death, drop magma cream), and
  blazes (fireballs that set you alight; player kills drop blaze rods) —
  every entity system is now dimension-aware, so mobs, drops, arrows and XP
  stay in the world they belong to

**Villages**
- Generated villages — a well with a bell, **furnished plank cottages** (each
  with a bed, a profession workstation, glass windows, a real oak door and a
  **peaked stair roof**), wheat farms and dirt paths — populated with villagers
  (farmer/fletcher/toolsmith professions with real emerald trading through the
  vanilla merchant screen, server-validated) and an iron golem that hunts
  hostiles and launches them skyward. Tuned to be common and findable (they were
  far too rare on the varied biome terrain — nearest was ~940 blocks from spawn)
- Villagers **use doors and pathfind** — they roam their village on an A* route
  (goal-directed, not random wandering), open the wooden door in their way and
  shut it behind them, and climb the one-block step out of a doorway. Fixes
  villagers boxed inside their own houses (they used to bounce forever on the
  random-wander AI until a player dug them out). Iron/copper doors stay shut —
  only wooden doors are mob-openable, as in vanilla
- Villagers follow a **daily schedule** — they stand at their profession
  workstation through the morning, gather at the village bell around midday, roam
  in the afternoon, and walk home to their bed at night (lying down to sleep, up
  again at sunrise). Anchored to each house's deterministic bed + workstation and
  the village bell

**World structures**
- Surface lakes (water, occasionally lava), buried dungeons — cobblestone
  spawner rooms with live mob spawners and deterministic loot chests —
  mineshaft networks (plank corridors, fence supports, cobwebs, rails) and
  crumbling surface ruins; all pure functions of the seed, so chunks agree
  without shared state

**Vehicles**
- Rails with auto-shaping (corners, slopes; powered/detector/activator
  variants), minecarts that ride rails, and boats for all nine woods —
  riding is client-simulated like vanilla with the server validating every
  vehicle move; detector rails press under carts; punch a vehicle to pop it
  back into an item

**Access control**
- Cluster logins are authorized at the GATEWAY by the `tachyne-access` policy
  service (whitelist/bans/roles, fail-closed); sessions arrive at the engine
  with gateway-stamped identity claims. Skins: the textures blob rides the
  `PlayerInfo` domain event, so everyone renders everyone's skin.
- The engine keeps its own `gatekeeper.json` layer for `/whitelist` +
  `/ban`/`/pardon` op commands. (The old in-engine Mojang session auth +
  hand-rolled AES/CFB-8 encryption were deleted with the wire layer — when
  online mode returns it is gateway work; the code is in git history
  pre-`c15e1e4`.)

**Multiplayer & authority**
- Central 20-TPS hub goroutine owns all state; connections are I/O-only
- The server validates everything a client claims: creative-slot conjuring,
  melee reach, window-click fabrication, movement (speed budget, teleport
  cap, fly detection, noclip — all rubber-banded), mining speed (fast-break
  cheats revert), and chunk streaming follows only the validated position;
  suffocation damage backs it all up
- Chat + commands: `/give /kill /xp /summon /effect /weather /difficulty
  /gamerule /time /tp /gamemode /list /say` — with client-side tab-completion
  (the brigadier command tree is sent on join); difficulty scales mob damage
  (peaceful clears hostiles), gamerules (keepInventory, doDaylightCycle,
  doMobSpawning, mobGriefing) persist in settings.json
- Optional NATS plugin bus (subscribe to game events / send commands from any
  language) and LLM-driven NPC villagers (OpenAI-compatible endpoint)

## Build & run

```bash
go build ./...   # tachyne-common is a private module
go test ./...                                # headless game tests (add -race for hub work)
ATTACH_TOKEN=dev go run ./cmd/server         # attach-only engine on :25500
```

The engine has **no Minecraft port** (`-addr` is a deprecated no-op). A real
client connects through a gateway: run `tachyne-gw-java-770` against
`localhost:25500` with the same token, or use the cluster entry
(`<server-ip>:25565` → dispatch → gateways → world pod).

Useful flags: `-attach :25500` (gateway listener; `ATTACH_TOKEN` env is the
shared secret, empty = refuse all), `-seed`, `-world world.gob` (edit
persistence), `-gamemode survival` (default for new players),
`-ops Name1,Name2`, `-valkey localhost:6379` (chunk cache; falls back to
`-chunkdir chunks`), `-nats nats://localhost:4222` (plugin bus),
`-llm http://…/v1` (NPCs), `-spawn x,y,z`, `-hud=false`.

Vanilla parity: a real vanilla 1.21.5 server ran beside tachyne as an oracle
(RCON via `scripts/oracle_rcon.py`), with Mojang's datagen dumps checked into
`reference/1.21.5/`. Oracle-verified: join sequence, mob idle cycle, zombie
combat (damage/cadence/knockback velocities), tick rate, all block-state ids.
Vanilla 1.21.5 behavior (via Mojang's officially published mappings) is a facts-only
reference — read formulas, reimplement in Go, never copy. Methodology and
scorecard: `docs/MECHANICS.md`. (The `cmd/swarm` load tester and
`cmd/diffprobe` oracle probe were 770-TCP clients and died with the wire
layer — git history pre-`c15e1e4`; a load tester returns as an attach-protocol
tool when needed.)

## Layout

`internal/worldgen` (terrain math, leaf) → `internal/world` (mutable world +
persistence + light) → `internal/server` (hub tick loop + all gameplay; no
wire) → `internal/attach` (serves the domain attach protocol to gateways).
The Minecraft wire format lives in the separate **`tachyne-common`** module
(`protocol/` + `render770/`), consumed by the gateway repos. See `CLAUDE.md`
for the deep architecture notes and `docs/MECHANICS.md` for the
vanilla-vs-ours tuning scorecard.

## Deployment (this repo IS the world pod)

Consolidated 2026-07-09: the engine source and its cluster deployment are one
repo. `Dockerfile` builds the local source (`go vet` + `go test` + `go build
./cmd/server`) into a scratch image; `deploy/` holds the k8s manifests;
`.forgejo/workflows/build.yml` builds + pushes
the world image (`latest` + short-sha tags) on every push
to main (org secret `REGISTRY_TOKEN`). Rolling out engine work is one loop:

```bash
git push origin main                                     # → CI rebuilds :latest
kubectl rollout restart sts/tachyne-world
```

**Runtime** (`deploy/statefulset.yaml` authoritative): `-addr ""` (no Minecraft
socket) `-attach :25500` (`ATTACH_TOKEN` from secret `tachyne-attach-token`)
`-ops <op1>,<op2>` (from the flag, not players.json) `-nats
tachyne-nats.tachyne.svc:4222` (dedicated pod; shared `nats.nats.svc` needs
auth) `-valkey tachyne-cache.tachyne.svc:6379`.

**Persistence**: PVC `data-tachyne-world-0` at `/var/world` holds
`{world,nether,end}.gob`, `players.json`, `inventories.json`,
`containers.json`, `spawns.json`, `settings.json`, `npc-mem-*.txt` — the world
migrated from the retired VM monolith, the single precious artifact (chunks
regenerate deterministically). Nightly backup CronJob `tachyne-world-backup`
(03:00, keeps 7, excludes `chunks/`). Gateways (`tachyne-gw-*`, separate repos)
sit behind `tachyne-ingress` (<server-ip>:25565) and render this pod's event
stream.

## Deployment

`Dockerfile` builds a static Go binary into a minimal image. `deploy/` holds
working Kubernetes manifests (the ones this project actually runs) — treat
them as examples: substitute your own image registry, hostnames, namespaces
and secrets before applying them to your cluster.

## Credits

Built from scratch in Go, standing on the shoulders of open data and research:

- **[PrismarineJS/minecraft-data](https://github.com/PrismarineJS/minecraft-data)** —
  packet layouts, block/item/entity id tables (MIT).
- **[misode/mcmeta](https://github.com/misode/mcmeta)** — Mojang-generated
  registry, tag and data reports, repackaged for easy consumption.
- **[Minecraft Wiki](https://minecraft.wiki)** — protocol and mechanics
  documentation (CC BY-NC-SA; used as a factual reference).
- **Vanilla parity research** — game formulas and constants were verified
  against vanilla behavior via Mojang's officially published mappings and
  live oracle experiments; all code here is an independent reimplementation.
- **[ViaVersion](https://github.com/ViaVersion/ViaVersion)** — multi-version
  protocol differences used as a factual reference (no code reused; GPL).
- **[Copernicus DEM GLO-30](https://spacedata.copernicus.eu/collections/copernicus-digital-elevation-model)** —
  earth-mode elevation data: © DLR e.V. 2010-2014 and © Airbus Defence and
  Space GmbH 2014-2018, provided under COPERNICUS by the European Union and
  ESA; free for any use with this attribution.
- **[GeyserMC/mappings](https://github.com/GeyserMC/mappings)** — Java↔Bedrock
  id mapping data consumed by the Bedrock table generator (MIT).
- **[nats.go](https://github.com/nats-io/nats.go)** — the plugin/event bus
  client (Apache-2.0).

## Development transparency

tachyne is built by its maintainer working with an AI coding agent
(Anthropic's Claude): substantial portions of the implementation were written
by the model under human direction, and every change is reviewed, tested and
deployed by the maintainer. The project's engineering discipline is designed
for exactly this workflow — byte-oracle tests pin the wire format, full test
suites gate every image build, and real-client verification signs off
gameplay. Disclosed here for transparency; judge the code on its behavior.

## License

Licensed under the **Apache License, Version 2.0** — see [LICENSE](LICENSE)
and [NOTICE](NOTICE). Note §6: the license grants no rights to the tachyne
name or any trademarks.

## Disclaimer

tachyne is an unofficial, independent project. It is **not** affiliated with,
endorsed, sponsored, or approved by Mojang Studios, Mojang Synergies AB,
Microsoft Corporation, or any of their subsidiaries — the developer and
publisher of Minecraft have no involvement with this project. "Minecraft" is
a trademark of Mojang Synergies AB. This project contains no Minecraft game
code; all game behavior is independently reimplemented, and data tables are
built from openly licensed community datasets (see Credits).
