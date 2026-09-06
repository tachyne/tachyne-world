# tachyne

> tachyne is an unofficial fan project, not affiliated with Mojang,
> Microsoft, or Minecraft's developer/publisher in any way. See the
> Disclaimer at the bottom.

## Project status

**Work in progress.** tachyne is young and moving fast: a full survival game
runs today, but expect rough edges, missing vanilla features, and breaking
changes between updates. **Bug reports are genuinely useful** — please open a
GitHub Issue with your client version/edition and what you saw. Contributions
are welcome too: see [CONTRIBUTING.md](CONTRIBUTING.md).

**Just want to run a server?** The [quickstart repo](https://github.com/tachyne/tachyne)
brings up the whole stack in one command — Docker Compose or Kubernetes,
classic infinite survival by default, real-Cape-Town earth mode as a variant.




A Minecraft **Java Edition** server written from scratch in pure Go. No Paper,
no JVM, no protocol libraries — world generation, lighting, and the whole game
simulation are original code; the only third-party runtime dependency is the NATS client behind the optional plugin bus.

## What to expect (vanilla parity at a glance)

tachyne's goal is **full vanilla feature parity**, and it is not there yet.
This table is the honest summary for anyone deciding whether to run it; the
detailed inventory follows in [What works today](#what-works-today).
✅ solid · 🟡 works with gaps · ❌ not yet.

| Area | Status | Notes |
|---|---|---|
| Terrain, biomes, caves, lighting | ✅ | Original generator: the full overworld biome set, rivers, cave biomes (incl. deep_dark with sculk-carpeted floors, scattered sensors/catalysts and can-summon shriekers), amethyst geodes, real sky+block light. Each biome grows its own wood — including the pale garden's bone-white pale oak over a floor of pale moss and eyeblossoms — and trees are grown by ports of vanilla's own trunk and foliage placers: acacias lean and fork, cherries branch sideways, large oaks run limbs out to scattered foliage clusters, and a planted sapling grows the identical tree the terrain does, with the vanilla growers' odds and the vanilla rule that a tree measures its room before growing rather than punching through a roof. Leaves live and die by the vanilla distance rule — fell a trunk and the canopy rots outward from the cut, leaf bridges within six of a log survive, and stripped wood still holds a canopy up. Wild jungle trees are hung with vines and the occasional cocoa pod, swamps grow their vine-draped oaks, and forests mix their species at vanilla's odds — a fifth birch and the odd large oak in a regular forest, dark-oak-led in a dark forest with vanilla's huge red and brown mushrooms towering among them — with leaf litter scattered on the ground around them. Mushroom fields raise huge mushrooms of both colours as their only flora, and bone meal grows a small mushroom into a huge one at vanilla's odds. Every biome rolls vanilla's own species mix — a third of plains trees are large oaks, meadows split between large oaks and super birches, jungles tower with mega jungle trees over their bushes, wild mangroves are mostly tall — and fallen logs lie on the forest floors, mushrooms sprouting along them. Bee nests hang at vanilla's odds — rare on plains oaks, guaranteed on meadow giants — and their bees live real lives: foraging the nearest flowers, then flying a real route home — an air-node A* ported from vanilla's own flying pathfinder, so a nest up a trunk and behind leaves is climbed to rather than pressed against, with vanilla's give-up timer and blacklist for one it truly cannot reach, and vanilla's soft leash keeping a colony near its nest; a bee that arrives to find its hive full drops it and goes to find another, the way vanilla does, rather than treating a busy hive as no hive at all — filling the hive as they come and go, sheltering from night and rain, surviving restarts inside their hive, breeding over any flower held out to them, and spending their sting (which costs the bee its life) — visibly: a pollen-laden bee wears its dusted coat and drips falling nectar, an angry one its red eyes, a spent one its lost stinger. A bee flying home with pollen boosts the crops it crosses — wheat, stems, berry bushes, cave vines — up to vanilla's ten per trip. Dispensers work a full hive the vanilla way (shears cut honeycomb, a bottle draws honey, and the bees come out calm — a machine has nobody to blame), and a hive broken with Silk Touch moves house with its bees and honey aboard the dropped item, while a bare-handed break spills them out angry; a bee nest without Silk Touch drops nothing at all. An oak, birch or cherry sapling grown next to a flower has vanilla's one-in-twenty chance of coming up carrying a nest of its own. Old-growth taigas roll vanilla's odds for giant 2×2 spruces and pines that podzol the ground around their base, mangroves stand on stilted roots — muddying in mud, waterlogging in water — and dangle young propagules beneath their canopies, and one wild pale oak in ten is a candidate for a creaking heart (fewer end up with one — the heart needs a log walled in by logs) — placed by vanilla's rule, a log walled in by logs on every side, so hearts are the same rare find they are in vanilla. Wild pale oaks also grow their moss: a pale-moss patch in the ground at the foot, carpet creeping up the trunk, and hanging strands trailing from the canopy. Deliberately *not* seed-compatible with vanilla worldgen. |
| Mining, crafting, smelting, containers | ✅ | Bundles in all seventeen colours (one stack's worth of anything by vanilla's per-item weight, contents in the tooltip, scroll to pick what comes out, and they nest at vanilla's surcharge). 1,678 recipes with vanilla recipe-book progression (recipes unlock as you gather ingredients, with the toast), furnaces, blast furnaces (ores at double speed), smokers (food), campfire cooking (four slots, food pops off the fire), chests, ender chests (per-player storage, shared between every ender chest and across dimensions), shulker boxes (contents survive being broken and travel with the item), decorated pots, conduits (prismarine frame → Conduit Power, and a full frame attacks), hoppers/droppers/dispensers; stonecutter with the full vanilla recipe list. |
| Combat | ✅ | 1.9 cooldown model, crits, sweep, knockback, shields, bows, crossbows (quick_charge/multishot/piercing), tridents (loyalty/riptide/impaling), the mace (smash attack + density/breach/wind_burst), TNT. Every hit carries its vanilla damage type — including each projectile's own, so a fireball is not absorbed like an arrow — and the type decides what happens to it: whether armour absorbs the blow and wears from it, whether a shield, Resistance or an enchantment gets a say, and what it costs in hunger. So armour protects against lava, fire and a cactus but not a fall, the Warden's sonic boom shrugs off protection, A raised shield catches anything vanilla lets it catch — an explosion, a fireball, a wither skull, a bee sting — from its front arc, wearing by what it stopped, and a blow caught on it delivers nothing it was carrying. Enchantments are all but complete — the full protection family (fire/blast/projectile/feather falling, each keyed to its own damage type), Smite and Bane of Arthropods, Fire Aspect, Thorns, the utility and boots enchantments, and both curses; only trident Channeling is missing. Fifteen of the implemented enchantments (the crossbow and trident sets, knockback, sweeping edge, the mace trio, soul speed and both curses) have no drop or table source yet, so they are reachable only by command. |
| Trial chambers | 🟡 | Generated chambers with working trial spawners (waves scaled to how many players walk in, reward ejection, half-hour cooldown) and vaults that pay each player one reward for a trial key.  Vault claims and spawner cooldowns persist, so a restart neither re-arms a spent spawner nor hands out a second reward.|
| Mobs | ✅ | The complete vanilla living roster with vanilla attributes on a real modifier pipeline (base values plus stacking modifiers, as vanilla computes them) and the full status-effect system — mobs can be poisoned, slowed, strengthened or healed like any living thing, with the undead's inversions intact — and they take damage down the same tag-driven path a player does, so a mob's own armour protects it against lava and a cactus but not a fall — breeding/taming/riding, leading them about on a lead — tie one to you, walk it, tie it to a fence and collect it again later, with vanilla's six-block elastic pull, its twelve-block snap, and hostiles and most water life refusing a lead as they do in vanilla — and vanilla natural spawning: per-category caps scaled by loaded chunks, spawns at any height (caves populate day and night), vanilla light rules (torch light blocks spawns, storms darken the sky enough for daytime monsters), per-biome weighted pools and pack sizes with every roster species in its vanilla place — mooshrooms on mycelium, turtles on beach sand, armadillos on savanna and badlands ground, camels in deserts, frogs in swamps and mangroves (cold, warm or temperate by the biome they spawn in), axolotls in lush-cave water over clay in their five colours (the rare blue one in 1200, and bred ones taking a parent's colour) — and distance-based despawning. Mobs take up room: vanilla shoves overlapping ones apart every tick and now so does tachyne, so a herd spreads out instead of collapsing to a point and a bee cannot rest inside another bee — every species carries its real vanilla hitbox (babies at half, slimes scaled by size), the mobs vanilla exempts are exempt, and packing a pen past the cramming limit crushes what is in it. Mobs persist across restarts (`mobs.json`) — herds, farm animals with their gear and age, tamed pets, and villagers (profession, merchant tier and their exact trade stock included) survive a reboot; bosses are not saved. Signature hostile AI is in: the Warden (Darkness aura, armor-piercing sonic boom, digs away when it loses you) and guardians/elder guardians (the lock-on beam that charges then fires, drawn on the client from the synced target, + the elder's Mining Fatigue curse). Cows, mooshrooms and goats can be milked, mooshrooms fill a bowl with stew, and a provoked llama spits rather than bites. Horses vary the way vanilla varies them — health, speed and jump are each rolled per animal and a foal lands between its parents, so breeding for a good one means something. Sniffer eggs hatch, cracking twice before opening, and moss underneath halves the wait. Evokers cast both their spells — fangs erupting from the ground and summoned vexes — and the ender dragon runs its full phase machine: circling, strafing runs with the fireball, landing on the exit portal, and the breath attack from its perch. The creaking haunts the pale garden: a heart inside a pale oak sends one out after dark, it freezes while you look at it, your blows land on the heart instead of on it, and breaking that heart is the only thing that stops it. Spiders climb the wall they walk into and drowned swim rather than trudging along the seabed; endermen carry blocks about. A few behaviours are still simplified (no spider wall-climb for cave spiders on ceilings, and a drowned's dry-land pathing does not seek water). |
| Survival loop | ✅ | Hunger/saturation, XP with the vanilla curve, death/respawn, beds (anchored to the head half like vanilla, with the occupied blanket and a proper stand-up beside the bed on waking) and respawn anchors (a bed only works in the overworld and an anchor only in the Nether — anywhere else, using one detonates, exactly as vanilla intends), and the full status-effect registry — every effect vanilla has, applied as vanilla applies it (most are attribute modifiers), bar the trial-chamber omen that needs a trial spawner to mean anything. |
| **Advancements** | ✅ | The full vanilla advancement tree with vanilla reveal rules, toasts, chat announces and XP rewards. Nearly every criterion fires from live play now — items used on blocks (vault keys, anchor charging, jukeboxes, glowing signs, smoked hives), interactions with mobs, crafting and smithing recipes, crafters, archaeology loot, projectile hits and kills (including a crossbow's multi-kills), mace smashes, shield deflections, lightning, levitation, falls, Nether fast travel, end gateways, target blocks, sculk catalysts, spyglass sightings, effect sets, structure visits (stronghold, trial chambers) and what you stand on. Nine criteria stay unobtainable until their mechanics exist (channeling, honey-block slides, wolf armour, piglin bartering, allays, a goat in a boat, spears, sneaking past sculk), and a few more wait on mechanics not built yet (dragon respawn, Nether and End structures). |
| Enchanting / anvil / brewing | 🟡 | The vanilla enchantment engine: the table's three rows come from vanilla's own cost roll and weighted selection over every enchantment's real level windows, item sets and exclusive sets (a gold sword rolls higher than diamond, an axe never offers Smite, treasure enchantments never come from a table), loot chests and the fishing treasure book use the same roll (ancient cities hand out Swift Sneak, bastions Soul Speed, ominous vaults Wind Burst), and the anvil applies only what the item supports and what its current enchantments allow, at vanilla's per-enchantment prices. 41 of vanilla's 42 enchantments have server-side effects (only trident Channeling is missing); librarians sell an enchanted book at every tier from one to four (vanilla's random tradeable enchantment and price, emeralds plus a book), and an item carries up to four enchantments. Grindstone, anvil merge/repair/rename. Brewing is vanilla's full recipe table: every potion (night vision, invisibility, leaping, fire resistance, swiftness, slowness, the turtle master, water breathing, healing, harming, poison, regeneration, strength, weakness, slow falling, and the 1.21 four), redstone for the long and glowstone for the strong forms, the fermented spider eye corruptions, mundane and thick, gunpowder for splash and dragon's breath for lingering — brewed per bottle, as the stand does. Splash and lingering potions throw and work (proximity-scaled splash, real shrinking effect clouds, dispenser- and witch-thrown). |
| Villages & trading | 🟡 | Generated villages in all five biome styles (plains/desert/savanna/snowy/taiga, chosen by the biome they sit in), villager schedules/pathfinding, iron golem (the village's own, or one a player builds from iron blocks and a carved pumpkin — snow golems build the same way), merchant screen trading; 13 professions with vanilla tier leveling (novice→master, trades unlock as the villager earns XP) and daily restocks. Villagers persist with their profession, tier and exact trade stock. Prices react like vanilla's too: a per-player trading-reputation discount, a Hero of the Village discount, and demand markup on heavily-used trades (reputation is per-session for now). The wider gossip system and zombie-villager curing chains aren't there yet. |
| The Nether / The End | 🟡 | Both dimensions with portals, nether mobs, brewing ingredients, the full dragon fight + elytra (with firework rockets to boost it), and the outer End islands with the gateway ring out to them; no fortresses, bastions or end cities yet. |
| Block support | 🟡 | Blocks need what vanilla says they need: a floor, plantable soil, tilled farmland, the wall behind them, a ceiling above, or the face they grew on. Applied both at placement (no rails or flowers in mid-air) and when a neighbour changes (mine a wall and its torches, ladders and signs come down, cascading). Vines, glow lichen, sculk veins and resin clumps attach by the face and hang from a vine above (vines spread along, round, up and down walls on random ticks as vanilla's do), and scaffolding carries vanilla's distance-from-support (the seventh out falls). Sugar cane needs water beside the block it stands on, a cactus refuses a solid or lava neighbour and stands only on sand or cactus. Bamboo roots in sand, dirt, gravel or bamboo, and chorus stems and flowers follow vanilla's own rules (a stem on end stone or a stem, or hanging off a supported one; a flower on a stem or beside exactly one). |
| PvP | ✅ | Melee between players runs the full swing — weapon damage, the 1.9 attack cooldown, criticals, the mace's full smash — the fall-distance damage bonus, the shockwave that throws everyone standing near whoever you hit, and wind_burst launching you back up to chain them, Knockback and Fire Aspect — against the same armour, protection, shield and absorption pipeline mobs hit. Kills count on the scoreboard, on both the player and total criteria. Arrows count too, and Thorns bites back at whoever landed the blow, reaching the archer who loosed it as readily as the fist that threw it. All of it gated on the `pvp` gamerule. |
| World border | ✅ | A real border with `/worldborder` (get/set/add/center, damage amount and buffer, warning distance and time), persisted with the world. Straying past it hurts, scaled by how far out you are and starting only beyond the buffer. A border can be set to move over a number of seconds, and a player joining mid-move picks up the tail of the animation rather than seeing it jump. |
| Explosions & sponges | ✅ | Creepers, TNT, ghast fireballs and the wither's spawn charge cast vanilla's explosion rays (its skulls shatter without one), so craters are ragged and blast resistance genuinely attenuates — obsidian stops a blast and shields what stands behind it, soft ground lets one punch further. Sponges drink up to 64 blocks of water around them (kelp and seagrass included) and turn wet. |
| Fluids | ✅ | Water and lava flow with vanilla slope-seeking spread and recede; lava meets water as obsidian/cobblestone/stone; infinite water sources form from a 2×2; concrete powder sets to concrete on water contact; blocks placed into water waterlog (and release it when broken). Buckets scoop sources and pour them back (water boils off in the nether), and a water bucket scoops a fish, axolotl or tadpole into a mob bucket that releases it — persistent, never despawning — wherever it is poured; cauldrons hold water, lava and powder snow — bucket and bottle transfer, banner washing, and slow rain/snow fill under open sky. |
| Growth | 🟡 | Hoe dirt, grass or a dirt path into farmland and plant it (a shovel flattens the same ground into a dirt path, or puts out a lit campfire; an axe strips logs, wood, stems and bamboo to their stripped forms, scrapes an oxidation stage off copper or takes wax off it, honeycomb waxes copper against the weather, and a compass held to a lodestone becomes a lodestone compass that points home): wheat, carrots, potatoes, beetroot, melon and pumpkin seeds, torchflower, pitcher pods, and nether wart on soul sand. Plants grow by **brightness**, so a torch-lit indoor or underground farm works. Crops, sugar cane, cactus, nether wart, grass and mycelium spread, copper weathering and leaf decay all run on the random tick, in whichever dimension you are standing in. Saplings grow **their own species** — oak, birch, spruce, jungle, acacia and cherry singly; dark oak and pale oak from a 2×2, as in vanilla. Bone meal advances crops, grows saplings, turns an azalea bush into the azalea tree (flowering patches, rooted dirt and all), and scatters grass and flowers; melon and pumpkin stems grow fruit. Ice and snow freeze and melt by block light, so a torch keeps a pond liquid. Budding amethyst grows buds through to full clusters. Kelp and the three vine families climb or hang; bamboo grows to full height; mushrooms spread across dark ground. Chorus flowers climb, branch and die back in the End; mangrove propagules ripen and grow. Composters take vanilla's full list of plant matter at vanilla's chances and pay out bone meal. Pointed dripstone grows — stalactites lengthen and raise stalagmites — drips water or lava into a cauldron under the tip, and impales anything that lands on a stalagmite. |
| Redstone | 🟡 | Power on vanilla's own model — sources power their neighbours weakly, the strong ones (a lever or button into the block it hangs from, a torch into the block above, plates, detector rails, lecterns and sculk sensors into the block beneath, diodes and observers out of their front, dust into the block it points at) drive a solid block that then powers everything it touches, and whether a block conducts is decided per state from the game's collision data with vanilla's exemptions — so a lever behind a wall, dust ending in a block, or a torch under one all work. Dust connects as vanilla connects it (recomputed from its surroundings; a lone connection extends into a line) and sits wherever vanilla lets it — a full block, a top or double slab, an upside-down stair, a closed top trapdoor, a hopper — and nowhere else. A trapped chest is a source at the strength of its viewer count, driving the block beneath it strongly, and a torch flipped eight times in sixty ticks burns out with vanilla's fizz until its restart delay passes. Dust, torches, repeaters (with locking), comparators (container fullness, subtract mode, and the non-container readings — cake bites, composter level, cauldron and honey levels, respawn-anchor charge, end-portal-frame eye, detector rail, and which disc a jukebox is playing), observers, pistons with quasi-connectivity, pressure plates, buttons, levers, targets, tripwires, note blocks, dispensers/droppers/hoppers, crafters (auto-craft on a redstone edge, with the full menu — a live result preview, per-slot disable toggles, hopper fill that targets the emptiest enabled slot, and a filled-or-disabled comparator read), redstone-fired TNT, copper bulbs (rising-edge toggle + light + comparator), and the sculk family — a game-event vibration system driving sculk sensors (distance-scaled power + frequency comparator), calibrated sensors (frequency-tuned), shriekers (warning levels → Warden), and catalysts (mob-death sculk bloom). A deep tier 1+2. |
| Structures | 🟡 | Villages in all five biome styles (plains/desert/savanna/snowy/taiga, assembled from the real vanilla jigsaw templates — houses/streets/town-centre, with villagers spawned per bed, professions claimed from job-site blocks, a bell meeting point, and per-house loot chests), dungeons, mineshafts, strongholds, ruins, desert temples, ruined portals, pillager outposts (assembled from the real vanilla jigsaw templates), deep-dark ancient cities (assembled from the real vanilla jigsaw templates — deepslate halls, sculk, and ~20 loot chests), ocean monuments (a block-for-block port of the vanilla MonumentBuilding shell (the interior room maze is not reproduced) — the full 58×58 prismarine shell with wings, entrance archways, roof and sea-lantern lighting, the hidden gold-block treasure, patrolled by guardians and elder guardians), shipwrecks (supply/treasure/map chests), and beach buried treasure. Structure chests fill from the real vanilla loot tables — weighted, with enchanted gear — deterministically per chest. Igloos, shipwrecks (all 20 hull variants, random rotation, template-marked supply/map/treasure chests), ruined portals (10 standard + 3 giant variants, rotated, with the vanilla decay pass — in the overworld and, with vanilla's blackstone masonry, on the Nether's cavern floors), pillager outposts, ancient cities, villages (all five biome styles) and trial chambers are now generated from the **real vanilla structure templates** (a pipeline bakes the 1.21.11 NBT templates; outposts, cities, villages and chambers assemble via the vanilla jigsaw system) — the dungeons, mineshafts, strongholds, surface ruins, desert temples and buried treasure remain hand-built stand-ins being migrated onto it. Trial chambers stamp their full vanilla layout (corridors, chambers, spawner/vault rooms, supply chests) and both mechanics run: trial spawners cycle their six states, scaling waves to the number of players and ejecting rewards, and vaults unlock with a key and pay each player once. Desert wells dot the desert, each hiding two caches of suspicious sand for a brush to find — the loot tables behind them are the real vanilla archaeology ones. Woodland mansions are a faithful port of the vanilla grid solver + placer (three floors of real room templates, corridors, staircases and roof, assembled with per-piece rotation/mirror) in dark forests, populated with evokers, vindicators and allays at the vanilla marker positions when a player arrives (a cleared mansion stays cleared across restarts). |
| Vehicles | 🟡 | Boats in all nine woods and the bamboo raft, plus their chest variants (sneak-click opens the cargo, which spills when the boat breaks); minecarts on auto-shaping rails, but a cart only moves while a player rides it — powered rails don't propel it, and there are no chest/furnace/hopper/TNT carts. Parked boats and carts survive restarts, and they exist in every dimension — a boat launched on a Nether lake or a cart on an End rail behaves like an overworld one, and each player only sees the vehicles of the dimension they are in. |
| Statistics | ✅ | The vanilla Statistics screen: blocks mined, items crafted/used/broken/picked up/dropped, mobs killed and killed by, and the custom counters — play time and the clocks, every distance by how you moved (walking, sprinting, crouching, jumping, falling, climbing, swimming, flying, gliding, riding), the damage family, every block menu opened, and the one-shot events — persisted per player. |
| Scoreboard & teams | ✅ | /scoreboard objectives (incl. auto criteria: deaths, kills, health) on sidebar/list/below-name, /team with colors, prefixes and name-tag rules; persists with the world. |
| Signs | ✅ | All wood types as standing/wall/hanging/wall-hanging signs with the vanilla edit GUI, both text sides, dyes, glow ink, waxing; text persists with the world and rides chunk loads. |
| Paintings | ✅ | All 47 placeable variants with vanilla selection (largest that fits, random among ties), pop on punch or lost support, persist with the world. |
| **Plugins** | ✅ | In-process Go plugin API: Bukkit-style events (priority ladder, cancellable/mutable), weather/time/creature-stat mutations (plus five of the gamerules), custom commands with tab-completion, tick scheduler, per-plugin config + storage — see `docs/PLUGINS.md`. Out-of-process NATS bus for any-language observers. |
| Filled maps | ✅ | Craft an empty map and it draws the world vanilla-style as you explore (terrain colors, depth-shaded water, slope shading), with live player and item-frame markers, and the off-map edge arrow. Cloning (map + empty maps) and zoom-out (map + 8 paper) work in the crafting grid, and the cartography table zooms, clones, and locks maps; maps persist with the world. Banner markers are still to come. |
| Item frames | ✅ | Regular + glow frames on any block face: insert any item (8-step rotation on click), two-stage punch (item pops first, then the frame), support-loss drops, persist with the world. A framed map renders and pins the green frame marker on that map. |
| Note blocks & jukebox | 🟡 | Note blocks with the full instrument set (block-below picks the instrument, mob heads above imitate), vanilla pitch/particles, right-click tunes and a punch plays; muffled when covered. Note blocks fire on a redstone rising edge too. Jukeboxes take all music discs, play/stop for everyone nearby, eject on click, drop the disc when broken, and persist.|
| Books, lectern, bookshelf | 🟡 | Book and quill: write up to 100 pages, sign to a titled written book, read via the vanilla screen; contents persist and follow the book everywhere. Lecterns hold a book for shared reading (page turns broadcast, take button returns it); chiseled bookshelves store six books addressed by where you click.|
| Armor stands | ✅ | Placed from the item (45° yaw snapping), dressed by clicking with armor (swaps preserve enchantments and trims), undressed empty-handed, broken with the vanilla quick double punch — the stand and its gear pop. Persist with the world. No poses or arms yet. |
| Mount inventories | ✅ | Sneak + right-click a horse-family mount for its inventory: saddle and armor slots (llamas take carpets), chest-on-donkey/mule (15 slots) and llamas sized by strength; everything drops on death. Mount inventories share the mobs' current lifetime (persisted across restarts with the mob). |
| Loom & banners | 🟡 | The vanilla loom: banner + dye (+ optional pattern item) with the full selectable-pattern list, up to 6 layers; patterned banners render in inventories and keep their patterns when placed. A broken patterned banner currently drops plain.|
| Smithing table | ✅ | Netherite upgrades (diamond gear keeps its enchantments, damage, name and trim) and all 18 armor trims with the 11 materials — identical re-trims refused, different trims replace. |
| Beacon | 🟡 | Vanilla pyramid tiers (1–4 layers of iron/gold/diamond/emerald/netherite), the beam needs an open sky column (glass passes), the menu takes a payment item and a power choice (tier-gated: speed/haste → resistance/jump boost → strength → regeneration or level II), and effects pulse to everyone in range every 4 seconds. Powers persist with the world.|
| Locator bar | ✅ | 26.2 clients see other players as direction markers on the locator bar (the world's positions ride a new engine frame the gateway renders only for 1.21.6+ clients; the `locator_bar` gamerule toggles it (turning it off stops new updates; existing markers clear on relog)). |
| Commands & gamerules | 🟡 | ~35 commands including /msg /tell /kick /clear /spawnpoint /playsound /particle plus the world admin set; 28 gamerules wired to real systems, under vanilla's snake_case names with the legacy camelCase spellings still accepted as aliases (keep_inventory, advance_time, advance_weather, spawn_mobs, spawn_phantoms, spawn_patrols, spawn_wardens, raids, mob_griefing, fire_ticks, tnt_explodes, block_drops, mob_drops, natural_health_regeneration, fall/drowning/fire damage, water/lava_source_conversion, player/elytra_movement_check, pvp, show_advancement_messages, show_death_messages, immediate_respawn, random_tick_speed, players_sleeping_percentage, locator_bar). |
| Fishing | ✅ | The vanilla bobber (cast physics, bobbing, the wait → wake → nibble sequence with rain/sky modifiers), the fish/junk/treasure loot pools at vanilla weights, Lure and Luck of the Sea, the open-water treasure rule, rod durability, XP per catch — and reeling a hooked mob yanks it toward you. |
| Raids | 🟡 | Killing a pillager-patrol captain grants Bad Omen; carrying it into a village converts it to Raid Omen — vanilla's thirty-second fuse — and the raid starts when that runs out. Vanilla's wave tables and per-difficulty wave counts (3/5/7), all five raider types with their mounted riders, and the raid boss bar; winning grants Hero of the Village (40 min), which discounts villager trades. The warning bell, villager gift-giving, per-difficulty bonus spawns and raid persistence across restarts are follow-ups. |
| Online-mode auth / chat signing | ❌ | Run offline-mode behind your own access control (the cluster setup ships one: `tachyne-access`). |

Multi-version is a headline feature: **Java 1.21.5–1.21.8 and 26.2** clients
share one world (1.21.9–26.1 are currently rejected at login), and **Bedrock**
(latest release) joins through its own gateway with Bedrock-specific limits —
see each gateway's README. Content newer than a client's own version is
downgraded rather than sent regardless: blocks and items it has never heard of
arrive as air, and newer mobs as their nearest sensible stand-in, so an older
client is never handed an id its registry cannot resolve.

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
- Full inventory + 2×2/3×3 crafting, furnaces + blast furnaces + smokers
  (fuel + per-recipe cook times, persisted), campfire cooking (lit-fire slots
  render the food, standing on one burns you),
  chests (27 slots, persisted), beds (sleep skips night with the vanilla fade)
- Tool durability + functional armor (damage reduction, visible on others)
- **XP**: orbs from player kills, coal/diamond mining and smelting (paid when
  you collect the output), orb magnetism, the vanilla level curve, 7×level
  dropped on death; levels persist with the inventory
- **Status effects**: the complete vanilla registry — all 38, from the
  familiar regeneration/poison/strength/speed/fire resistance and instant
  health/harm through levitation, resistance, absorption, slow falling,
  conduit power, dolphin's grace, darkness and the trial/raid omens to the
  1.21 set (wind charged, weaving, oozing, infested) — golden apples work,
  witches poison, /effect for ops; splash and lingering potions dose mobs too, and a hostile that picks up gear stops despawning
- **Enchanting**: real enchanting table UI — lapis + levels, three rolled
  offers scaled by surrounding bookshelves. 41 of vanilla's 42 enchantments
  have real server-side effects — the whole protection family, Sharpness,
  Smite and Bane of Arthropods, Fire Aspect, Thorns, Fortune, Looting, Silk
  Touch, Efficiency, Unbreaking, Mending, the bow/crossbow/trident sets, and
  the boots and utility enchantments; only trident Channeling is missing.
  The table, loot and anvil all run vanilla's own selection engine over the
  real enchantment data (weights, level windows, item sets, exclusive sets,
  treasure and loot tags); an item carries up to four. Glinting works on
  every supported client version; plain books enchant into enchanted books
- **Anvil + grindstone**: merge enchantments (books included, equal levels
  combine upward), repair from a sacrifice, rename items — for enforced level
  costs; the grindstone strips enchantments and refunds XP
- **Cartography table**: zoom a map out with paper (up to the vanilla scale
  cap), lock it with a glass pane (a frozen copy that stops updating), or
  duplicate it with an empty map (copies share the same live picture)
- **Beacon**: vanilla pyramid detection with all four tiers, sky-column
  check, the payment menu with tier-gated powers, area effects on the
  vanilla 80-tick cadence, and the client-rendered beam

**Progression**
- **Recipe book**: vanilla unlock progression — a fresh player's book starts
  empty and recipes reveal themselves as you first obtain their ingredients,
  with the "new recipes unlocked" toast and the badge; clicking an entry
  auto-fills the crafting grid from your inventory; book tabs' open/filter
  state and your unlocks persist across sessions
- **Advancements**: the complete vanilla advancement tree (story, nether, end,
  adventure, husbandry — 125 advancements) with vanilla semantics end to end:
  the tree reveals itself as you progress (earned advancements plus a two-step
  frontier; hidden ones appear only once earned), toasts pop, completions
  announce in chat with the vanilla task/goal/challenge phrasing, challenge
  XP rewards pay out, and progress persists per player. Criteria fire from
  live gameplay — items obtained and used, mobs killed and interacted with,
  biomes and structures visited, dimensions entered, animals bred/tamed,
  trades, enchants, brews, sleep, recipes crafted, projectiles landed, falls,
  lightning, effects — the whole trigger set bar nine criteria whose mechanics
  don't exist yet (channeling, honey slides, wolf armour, bartering, allays,
  goats in boats, spears, sneaking past sculk); those stay visible but
  unobtainable until they land.

**Combat**
- 1.9-style attack cooldown (spam-clicks scaled to a fifth), jump crits at
  ×1.5 with particles, sword sweep, real knockback that sends mobs flying
  (sprint hits shove harder), full swing/crit/sweep sound set
- Player bows: charge-held draw (speed, damage and pitch scale with it),
  arrows consume ammo, hit mobs with knockback + XP credit, and stick in
  walls where you can walk over and retrieve them; snowballs + eggs throw
- Crossbows: hold to charge (quick_charge shortens it), stays loaded until
  you fire, then looses a fixed-power bolt; multishot fires three at once and
  piercing punches a bolt through several mobs
- Tridents: charge-held throw that sticks and is retrievable; loyalty flies it
  back to your hand, riptide launches you through water or rain, and impaling
  hits wet targets harder — enchantments survive the round trip
- The mace: a smash attack — hit a mob while falling and the drop adds massive
  bonus damage (and negates your own fall damage), with a shockwave that flings
  nearby mobs; density scales the bonus, breach punches through armor, and
  wind_burst launches you back up to chain smashes

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
  retaliation, ranged kiting). Natural spawning is the vanilla model: every
  loaded chunk rolls spawn attempts at a random height through the whole
  column — caves fill with monsters around the clock while the surface only
  spawns them in darkness (or under a thunderstorm's darkened sky); torch
  light is absolute protection; per-category mob caps (monster/creature/
  ambient/water) scale with loaded chunks; species come from per-biome
  weighted pools with vanilla pack sizes (husks in deserts, strays in the
  snow, drowned in oceans and rivers, slime chunks below y40 on the vanilla
  chunk seed); the far-away despawn distances match vanilla per category.
  All are `/summon`-able by name. Two spawners are selectable with `-spawner`:
  the default `tachyne` sampler (a cheaper 1-in-8-chunks rate paired with a
  herd top-up, tuned to feel right at lower hub cost) or `vanilla` — the exact
  NaturalSpawner: one attempt per chunk per tick, the three-group pack loop,
  the full distance gates, and one-time chunk-generation herds as land loads.
- **Mobs collide with each other** — vanilla's `pushEntities`/`push`: every
  living entity shoves the pushable ones its box touches apart each tick, with
  vanilla's own arithmetic quirks (the shove weakens as a pair converges, and
  vanishes entirely inside a hundredth of a block), its exemptions (bats, the
  ridden, a watched creaking) and its `maxEntityCramming` crush. Each species
  carries the hitbox from its vanilla entity-type registration. Players are not
  pushed: the client owns its own position here, so there is no server-side
  velocity to add to
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
- Weather: the vanilla two-timer cycle (independent rain and thunder spells —
  a thunderstorm is their rare overlap), levels that fade in/out, state that
  survives restarts; lightning at vanilla odds prefers lightning rods within
  128 blocks, then sky-exposed creatures, starts fires on normal+, and can
  drop a skeleton trap horse; no rain or bolts over deserts or snowfields;
  rain shields the undead at dawn, sleeping resets the cycle,
  `/weather <clear|rain|thunder> [duration]` + `doWeatherCycle` for ops
- Beach waves (`-waves`, **opt-in, deliberately non-vanilla**): a thin sheet of
  water rolls in from the ocean, washes up the beach slope, and recedes back
  out — pulsed swells (a quick wash-in, a gentler roll-back, then a pause before
  the next) with a gently scalloped waterline, rendered as flowing water that
  thins to a soft sloped film at its edges and spills over each step of the beach
  as falling water. It climbs a beach one block at a time (never scaling a 2-block
  step) and animates along the whole visible shoreline. It is a pure client
  overlay — the water is
  shown to nearby players but never written to the world (no persistence, no
  fluid simulation, no collision), so it can't touch the vanilla water model or
  the save. Off by default because it departs from vanilla on purpose
- Sounds throughout — mob growls/hurt/death, combat, explosions (with the
  real boom + mushroom cloud), XP dings, level-ups, chests, enchanting —
  sent inline-by-name so they work identically on every client version
- Block-break particles + sound for other players (world events)

**Redstone (tier 1 + 2)**
- Power sources (levers, buttons with real press timers, redstone torches
  that invert, redstone blocks), dust that carries decaying 15-block signal,
  and consumers: lamps, iron doors, and TNT that primes when powered —
  simulated as a one-block-per-tick ripple on the server tick loop
- Strong and weak power, vanilla's way: a source powers its neighbours weakly;
  a lever or button drives the block it hangs from, a torch the block above
  it, plates, detector rails, lecterns and sculk sensors the block beneath,
  diodes and observers the block in front, dust the block it points at — and
  a solid block so driven powers everything around it. Conductors are decided
  per block state from the game's own collision shapes, with vanilla's
  exemptions (glass, ice, glowstone, sea lanterns, beacons, redstone blocks,
  pistons, TNT, leaves, copper grates and bulbs never conduct; soul sand and
  mud always do)
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
- Generated villages — assembled from the REAL vanilla structure templates
  and jigsaw pools in all five biome styles (plains, desert, savanna, snowy,
  taiga), chosen by the biome they sit in — populated with villagers across
  13 professions, trading emeralds through the vanilla merchant screen
  (server-validated, with tier leveling and daily restocks), and an iron
  golem that spawns on vanilla's five-villager quorum, hunts hostiles and
  launches them skyward
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
  spawner rooms with live mob spawners and loot chests — mineshaft networks
  (plank corridors, fence supports, cobwebs, rails) and crumbling surface
  ruins; all pure functions of the seed, so chunks agree without shared state
- Structure chests fill on first open from the real vanilla loot tables
  (dungeon, desert temple, ruined portal, pillager outpost, and per-profession
  village-house chests) — weighted entries, enchanted books and gear, scattered
  across the chest the way vanilla does, and deterministic per chest so a chest
  always holds the same haul

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

**Scoreboard & teams**
- Full op-driven scoreboard: objectives with dummy or automatic criteria
  (deaths, kill counts, health as a live hearts gauge), shown on the sidebar,
  player list or below name tags; per-player scores with set/add/remove/reset
- Teams with display names, colors (name tags recolor for everyone), prefixes/
  suffixes, friendly-fire and visibility/collision rules; joining a team moves
  you out of the old one, and the whole board persists with the world

**Signs**
- Every wood type as standing (16-way rotation), wall, ceiling-hanging
  (attached under chains/non-full blocks) and wall-hanging signs; the edit
  GUI opens on placement and on right-click, per vanilla's single-editor rule
- Both text sides independently editable (walk around the sign to edit the
  back), with dye colors, glow ink / regular ink, and honeycomb waxing to
  lock the text; §-format codes are stripped server-side like vanilla
- Text rides the chunk packet's block-entity data on load and live
  `block_entity_data` updates on edit, and persists with the world
  (`signs.json`)

**Multiplayer & authority**
- Central 20-TPS hub goroutine owns all state; connections are I/O-only
- The server validates everything a client claims: creative-slot conjuring,
  melee reach, window-click fabrication, movement (speed budget, teleport
  cap, fly detection, noclip — all rubber-banded), mining speed (fast-break
  cheats revert), and chunk streaming follows only the validated position;
  suffocation damage backs it all up
- Chat + commands: `/scoreboard /team /give /kill /xp /summon /effect /weather /difficulty
  /gamerule /time /tp /gamemode /list /say` — with client-side tab-completion
  (the brigadier command tree is sent on join); difficulty scales mob damage
  (peaceful clears hostiles), and 28 gamerules — under vanilla's snake_case
  names, with the legacy camelCase spellings still accepted — persist in
  settings.json
- Optional NATS plugin bus (subscribe to game events / send commands from any
  language) and LLM-driven NPC villagers (OpenAI-compatible endpoint)

## Build & run

```bash
go build ./...
go test ./...                                # headless game tests (add -race for hub work)
ATTACH_TOKEN=dev go run ./cmd/server         # attach-only engine on :25500
```

The engine has **no Minecraft port** (`-addr` is a deprecated no-op). A real
client connects through a gateway: run `tachyne-gw-java-770` against
`localhost:25500` with the same token, or use the cluster entry
(`<server-ip>:25565` → ingress → gateways → world pod).

Useful flags: `-attach :25500` (gateway listener; `ATTACH_TOKEN` env is the
shared secret, empty = refuse all), `-seed`, `-world world.gob` (edit
persistence), `-gamemode survival` (default for new players),
`-ops Name1,Name2`, `-valkey localhost:6379` (chunk cache; falls back to
`-chunkdir chunks`), `-nats nats://localhost:4222` (plugin bus),
`-llm http://…/v1` (NPCs), `-spawn x,y,z`, `-hud=false`,
`-spawner tachyne|vanilla` (natural-spawn model; default tachyne),
`-waves` (opt-in cosmetic beach waves; off by default — see below),
`-health :8081` (opt-in health/metrics listener: `/healthz` answers 503 once
the tick loop has stalled 5 s — point a liveness probe at it — plus
`/debug/vars` with players, mobs, cache sizes, tick p50/p99 and which chunk
cache and bus the pod actually connected to, and `/debug/pprof`; bind it to
the pod, never the public ingress).

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

## Plugins

tachyne has a first-class plugin API. Plugins are ordinary Go packages
**compiled into the server binary** — the model Go servers converged on
(Caddy, CoreDNS, Dragonfly): no jar loading, no interpreter, full type
safety. If you've written Bukkit/Paper plugins the shape will feel familiar:
event listeners with a priority ladder and cancellation, commands, a tick
scheduler, and per-plugin config + storage.

A minimal plugin:

```go
package myplugin

import "github.com/tachyne/tachyne-world/plugin"

func init() { plugin.Register(&MyPlugin{}) }

type MyPlugin struct{}

func (p *MyPlugin) Name() string { return "myplugin" }
func (p *MyPlugin) Disable()     {}

func (p *MyPlugin) Enable(ctx plugin.Context) error {
    srv := ctx.Server()

    // Cancel block breaks below y=40 by non-ops (region protection).
    plugin.On(ctx.Events(), plugin.Normal, true, func(e *plugin.BlockBreakEvent) {
        if e.Y < 40 && !srv.IsOp(e.Name) {
            e.SetCancelled(true)
        }
    })

    // Double every zombie's melee damage as it spawns (creature stats).
    plugin.On(ctx.Events(), plugin.Normal, true, func(e *plugin.MobSpawnEvent) {
        if e.TypeName == "zombie" {
            if m, ok := srv.Mob(e.EID); ok {
                m.SetMeleeDamage(m.MeleeDamage() * 2)
            }
        }
    })

    // A command: /storm starts a thunderstorm.
    return ctx.RegisterCommand(plugin.Command{
        Name: "storm", OpOnly: true, Help: "start a thunderstorm",
        Run: func(c plugin.CommandContext) { c.Server().SetWeather("thunder", -1) },
    })
}
```

Enable it with a blank import in `cmd/server/plugins.go` and rebuild — or,
for plugins living in their own modules, assemble a custom binary with
**tachyne-build** (no fork needed):

```bash
go install github.com/tachyne/tachyne-world/cmd/tachyne-build@latest
tachyne-build --with github.com/you/yourplugin -o my-world
```

Operators configure each plugin under `-plugindir` (default `plugins/`, next
to `settings.json`): `plugins/<name>/config.json` is read by `ctx.Config`
(the reserved key `"enabled": false` turns a compiled-in plugin off), and
`plugins/<name>/data.json` is the plugin's persistent KV store, flushed with
world saves.

What you can hook today: player join/quit/chat/commands/movement, block
break/place (veto or swap the placed block), melee damage in both directions
(mutate the amount), mob spawns (cancel, or grab the new mob and change its
max health / speed / damage), mob deaths (rewrite drops and XP), weather and
time changes, gamerules. Plus mutations for all of the above, `/help` +
tab-completion integration for your commands, and `NextTick/After/Every`
scheduling. The one rule: handlers run on the engine's tick goroutine —
never block in one; from your own goroutines, `Scheduler().NextTick` is the
way back in.

The shipped **`plugins/example`** exercises the whole surface (configurable
join greeting + visit counter, depth protection, scheduled announcements,
`/storm`, `/sun`, `/buff`) and is the recommended starting template — it's
compiled into the default binary but inert until configured. Full API
reference, the event table, and the threading contract: **`docs/PLUGINS.md`**.

**Daemon plugins** run beside the server instead of inside it: standalone
programs attached to the NATS bus (`-nats`), which publishes the same event
catalog as JSON on `mc.event.<name>` and accepts commands + request-reply
queries on `mc.cmd.*`. Distribution is the Go model — a daemon's module
path is its repository, and `tachyne-plugin-manager` pulls, builds, boots, and
supervises it in one command:

```bash
go install github.com/tachyne/tachyne-plugin-manager@latest
tachyne-plugin-manager run github.com/tachyne/tachyne-world/daemons/webmap
```

`daemons/webmap` (a live web map of the world) is the shipped example, and
the `busplugin` package is the Go kit. For a full **3D web map**, see
[tachyne-map](https://github.com/tachyne/tachyne-map) — a separate pod that
renders the world natively in Go (no Java), reads it read-only through the
`worldread` package, and follows block changes over this same bus so the map
updates live. `cmd/anvil-export` remains as a standalone tool if you want your
tachyne world as a vanilla Minecraft save. Running under `-config`, the manager
takes live `install`/`uninstall`/`restart`/`list` commands over the bus —
in game that's the op-only `/plugin` command — bare `/plugin` opens a chest-style plugin browser (labelled items, click to install/upgrade/rate) — so plugins hot-install,
hot-remove, and hot-reload while the server runs. Discovery comes from the
[plugin registry](https://github.com/tachyne/tachyne-registry) (`/plugin search`,
install by name, out-of-date flags), and on sharded worlds `/plugin`
spans the whole fleet — including `/plugin upgrade <name>`, a progressive
shard-by-shard rollout that verifies health before each step. Daemons
observe and command in any language; only tick-veto hooks (protection,
combat tuning) need the compiled-in plugin API above.

## Layout

`internal/worldgen` (terrain math, leaf) → `internal/world` (mutable world +
persistence + light) → `internal/server` (hub tick loop + all gameplay; no
wire) → `internal/attach` (serves the domain attach protocol to gateways).
The Minecraft wire format lives in the separate **`tachyne-common`** module
(`protocol/` + `render770/`), consumed by the gateway repos. `plugin/` is the
public plugin API (compiled-in Go plugins; `plugins/` holds them — see
`docs/PLUGINS.md`). See `docs/DOMAIN-EVENTS.md` (the domain-event
architecture), `docs/SHARDING.md` (the multi-pod design), `docs/EARTH.md`
(real-elevation worlds) and `docs/MECHANICS.md` for the vanilla-vs-ours
tuning scorecard; `CHANGELOG.md` is the whole-system timeline.

## Deployment (this repo IS the world pod)

Consolidated 2026-07-09: the engine source and its cluster deployment are one
repo. `Dockerfile` builds the local source (`go vet` + `go test` + `go build
./cmd/server`) into a scratch image; `deploy/` holds the k8s manifests;
`.github/workflows/build.yml` builds + pushes
the world image (`latest` + short-sha tags) on every push
to main (`GITHUB_TOKEN`); `.github/workflows/ci.yml` gates gofmt/vet/test on pushes and PRs. Rolling out engine work is one loop:

```bash
git push origin main                                     # → CI rebuilds :latest
kubectl rollout restart sts/tachyne-world
```

**Runtime** (`deploy/statefulset.yaml` is a sanitized example of what runs):
`-attach :25500` (`ATTACH_TOKEN` from secret `tachyne-attach-token`)
`-seed <n>` `-gamemode survival`
`-ops <op1>,<op2>` (from the flag, not players.json) `-nats
tachyne-nats.tachyne.svc:4222` (dedicated pod; shared `nats.nats.svc` needs
auth) `-valkey tachyne-cache.tachyne.svc:6379`.

**Persistence**: PVC `data-tachyne-world-0` at `/var/world` holds
`{world,nether,end}.gob`, `players.json`, `inventories.json`,
`containers.json`, `mobs.json`, `spawns.json`, `advancements.json`,
`stats.json`, `recipebook.json`, `scoreboard.json`, `signs.json`, `books.json`,
`maps.json`, `banners.json`, `campfires.json`, `hives.json`,
`gatekeeper.json`, `settings.json`, `npc-mem-*.txt` and
`plugins/<name>/{config,data}.json` — the world
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
