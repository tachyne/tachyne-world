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

## 2026-07-26

### Added
- **The evoker casts.** It spawned, joined raids and dropped its totem without
  ever doing the one thing an evoker does. Both spells are in: fangs erupt from
  the ground — a line walking out toward you at range, two rings around it up
  close, each fang biting a moment after it surfaces — and a flight of three
  vexes is conjured when there are not already vexes about, each with a limited
  life so an abandoned swarm clears itself. The fangs bite through armour, so a
  full set of diamond is no answer to one.
- **The world has a border.** Nothing implemented one: no wall, no damage, no
  warning, no command. There is now a real border with `/worldborder` —
  `get`, `set`, `add`, `center`, `damage amount|buffer` and
  `warning distance|time` — persisted with the world settings and shown to
  every client. Straying past it hurts, scaled by how far out you are and
  starting only beyond the damage buffer, and the death it causes says so. A
  border can also be set to move over a number of seconds; the engine stores
  where it started, where it is going and when it set off, and computes the
  rest from the clock, so a player joining mid-move sees the tail of the
  animation rather than a jump.
- **Phantoms are the price of not sleeping again, and villages have cats.**
  Phantoms were a flat one-in-thirty roll over any player at night, so sleeping
  changed nothing and they were just ambient noise. They now run off an
  insomnia clock: three days without a bed before they can appear at all, and
  steadily more likely after that, only under open sky at or above sea level.
  Climbing into a bed stops the clock — as in vanilla, getting in is what
  counts, so being woken early still buys the night off. Separately, cats now
  spawn around villages; nothing had ever spawned one, so the only cats in the
  world were summoned.
- **Cows can be milked, and llamas spit.** Two mobs were missing the thing
  everyone knows them for. A bucket on an adult cow, mooshroom or goat now
  fills with milk — which strips every status effect and, unlike food, can be
  drunk on a full stomach, so it is finally the answer to a witch's poison.
  A bowl on a mooshroom comes back as stew. And a provoked llama spits from
  twenty blocks rather than biting: the spit is the only damage a llama has
  ever been able to do, so its melee is gone.
- **Thorns bites back in PvP, and reaches the archer.** Thorns only ever
  retaliated against mobs, so a player in a full enchanted set was no more
  dangerous to attack than one in rags. It is the victim's armour that carries
  the enchantment, and it now fires at whoever landed the blow regardless of
  what they are — including down the flight path of an arrow to the archer who
  loosed it, which is how it has always worked in vanilla. A blocked blow deals
  no damage and so draws no retaliation, and the damage it deals is a
  continuous roll rather than one of five whole numbers.
- **Death messages say what happened.** Every death read "<name> died",
  whatever killed them — which loses the one thing a death message exists to
  carry. Deaths are now attributed: slain by a player or a named mob, shot by
  whoever loosed the arrow, fell from a high place, impaled on a stalagmite,
  tried to swim in lava, drowned, pricked to death, blew up, struck by
  lightning. The cause travels with the damage rather than being guessed at
  the end, so walking out of lava and dying of the burns still credits the
  lava.
- **Players can fight each other.** Melee aimed at another player fell straight
  through and did nothing — two people could swing at each other indefinitely
  without a scratch. PvP now runs the same swing the mobs get (weapon, attack
  cooldown, criticals, mace smash, Knockback, Fire Aspect) through the player
  damage pipeline, so armour, protection enchantments, shields and absorption
  all apply. Gated on the `pvp` gamerule, which is finally a real rule — and
  the rule covers bows too, so turning PvP off no longer stops fists while
  leaving arrows working.
- **Coral dies out of water.** Every coral block, plant and fan has a dead
  twin, and coral pulled from a reef and replanted on land stayed brilliantly
  alive forever — which made keeping it wet, and silk touch, pointless. It now
  bleaches a few seconds after the last water beside it goes.
- **Game rules use their real names, and nine more of them work.** Every rule
  was still spelled the pre-rename way — `doDaylightCycle` rather than
  `advance_time` — which meant nothing a player typed from a wiki or another
  server worked. Both spellings are accepted now, so nothing anyone has
  memorised breaks, and `/gamerule` lists the canonical ones. New and
  enforced: `spawn_phantoms`, `spawn_patrols`, `spawn_wardens`, `raids`,
  `tnt_explodes`, `water_source_conversion`, `lava_source_conversion`,
  `player_movement_check` and `elytra_movement_check`.
- **Eyeblossoms open at night**, and shut again at first light, the way the
  pale garden's clock is supposed to work.
- **A nether portal left standing breeds zombified piglins**, at vanilla's
  difficulty-scaled rate — so a portal in the Nether is a piglin farm again,
  and peaceful breeds none.
- **Dripstone lives.** Pointed dripstone was scenery: it never grew, never
  dripped and never hurt anyone. Stalactites hanging off dripstone stone now
  lengthen over time or raise a stalagmite from the floor beneath them, water
  or lava standing above one drips through and fills a cauldron under the tip
  (so a lava farm works), and landing on a stalagmite is the one fall that
  hurts MORE than the ground — it counts two and a half blocks further and
  doubles the damage, including from heights that would otherwise be safe.
- **Explosions carve real craters.** A blast was a sphere with a cutoff: every
  block inside the radius went, every block outside survived, and a wall of
  obsidian protected nothing beyond its own cell. Explosions now cast vanilla's
  rays, each worn down by what it passes through — so craters are ragged,
  obsidian stops a blast dead and shields what is behind it, and a blast punches
  further through soft ground than through stone. Two bugs surfaced doing it: an
  explosion in the Nether was blowing a hole in the OVERWORLD at the same
  coordinates (so was primed TNT), and both now stay where they happened.
- **Sponges work.** Drop one in water and it drinks up to 64 blocks around it,
  taking the kelp and seagrass with it, and turns wet.
- **Blocks need something to hold them now.** Support was a six-block list
  checked only in the cell directly above an edit, so mining a wall left its
  torches, ladders, signs and levers hanging in the air, and a rail or a
  flower could be placed in mid-air with nothing under it. Every block is now
  classified by what it needs — a floor, soil, tilled farmland, a wall behind
  it, a ceiling above, the face it grew on — and that one rule is applied both
  when you place a block and when anything next to it changes, cascading so a
  stack comes down together. Deliberately gentle about what counts as a hold:
  a torch on a fence post and a carpet on a slab stay exactly where they are.
- **Cake, and the composter.** Both were entirely absent: a cake could be placed
  and never eaten, and a composter was scenery. Cake now feeds you a slice at a
  time and disappears on the seventh, and a candle plants in an untouched one.
  A composter takes vanilla's full list of plant matter at vanilla's chances,
  composts a second after it fills, and pays out bone meal.
- **Comparators can read blocks that aren't containers.** Cake, composter,
  cauldron, beehive, respawn anchor, end portal frame, detector rail — and a
  jukebox now reads out WHICH disc is playing (each song has its own signal),
  not merely that one is. They all resolve through a single reading now, so
  the next block to gain one is a case rather than a hunt.
- **Firework rockets fly, and elytra travel finally works.** A rocket was an
  inert item: you could hold one while gliding and nothing happened, so an
  elytra could only ever go downhill. Rockets now launch, climb, and pop, and
  one used in the air drags you toward where you are looking — the boost the
  whole elytra endgame is built around.
- **Bottles o' enchanting throw.** They shatter where they land into 3 to 11
  experience, so stored levels are worth carrying again.
- **Goat horns sound.** All eight instruments, audible across 256 blocks, with
  vanilla's seven-second hold before you can blow it again. (Nothing drops one
  yet — that is the goat's ram, still to come.)
- **The spyglass scopes.** Raising one now registers as a scope and sounds the
  way it should, for both you and anyone watching.
- **Frogspawn can be placed.** It goes on the surface of water, which no other
  item does, and so had never worked: the client's own aim passes straight
  through water and the server had nothing to place against. It now finds the
  water surface itself.
- **End gateways.** Killing the dragon now raises a ring of twenty gateways
  around the main island. Step into one and it throws you a thousand blocks out
  along its own bearing, onto the first outer island in that direction, and
  leaves a gateway home beside where you land — so the outer End is somewhere
  you can actually reach and come back from, not just somewhere you can see.
- **The End has its outer islands.** Past the void ring around the main island
  there was nothing at all — the End was one disc of end stone and empty space
  forever. Now it opens out the way it should: scattered islands stretching
  outward without limit, thick in the middle and thin at the rim, with real
  void between them to glide across. This is the ground end cities and gateways
  need to stand on.
- **Sheep come in colours, and dye works on them.** Every sheep in the world was
  white — the fleece colour was never actually sent — so a flock was uniform and
  a dye did nothing to a live sheep. They now spawn with vanilla's spread
  (mostly white, the greys and browns, pink about one in six hundred), a dye
  recolours one, and the wool you shear or collect matches the fleece.
- **Name tags work.** A name tag from an anvil renames the mob you use it on,
  the name shows above it, and — the part that matters — a named mob never
  despawns, so a pet or a hard-won villager stays where you left it.
- **Magma blocks burn, berry bushes scratch and wither roses wither.** All
  three were decoration: you could stand on a magma block indefinitely, walk
  through a berry patch untouched and plant wither roses as a garden feature.
  They now work on mobs too, so a berry hedge or a magma floor is a real
  defence. Fire resistance and Frost Walker boots spare you the magma, a bush
  only catches you while you are moving through it, foxes and bees push through
  unharmed, and the undead ignore wither roses.
- **Beehives fill and can be harvested.** Bees working a hive fill it with
  honey, and at full you take it: shears cut three honeycomb, a glass bottle
  draws a honey bottle, and either empties the hive. Rob one without a campfire
  smoking underneath and the bees come after you — which is what the campfire
  under a hive has always been for. (Bees fill a hive by working near it for
  now; proper pollination waits on the bee's own behaviour.)
- **Vaults open.** Beat a trial spawner, take the key it drops, and the vault
  in the room lights up and pays you — once. Each player gets their own single
  claim from each vault, so a whole group can run the same chamber and everyone
  is rewarded, and coming back with a second key gets you nothing. Ominous
  vaults take the ominous key and pay from the better table.
- **Trial spawners run their fight.** Walk into a chamber room and the spawner
  lights up, throws waves at you — more of them, and more at once, the more of
  you there are — and when the last one falls it opens and pays out a reward to
  everyone who fought, then sleeps for half an hour. The block shows all of it:
  dark, lit, flaming, shutter open, spent. **Trial keys now exist**, which is
  what vaults open with.
- **Trial chambers have their spawners.** They generated the rooms, the
  corridors and the vaults but not a single trial spawner — the room that is
  supposed to be a fight was just an empty room with a reward in it. All four
  families are there now, and a chamber picks a theme: its melee spawners are
  all zombies, or all husks, or all spiders, and its two kinds of archer always
  match. Chunks regenerate from the seed, so existing chambers fill in too.
- **Conduits.** Build a prismarine frame around one underwater and it grants
  Conduit Power to anyone swimming in range — so you can breathe and see and
  mine down there — and a full frame hunts hostile mobs in the water around it.
- **Decorated pots hold an item.** Right-click to drop a stack in, right-click
  again to take it back, and breaking the pot spills what was inside.
- **Ender chests and shulker boxes.** An ender chest shows the same 27 slots
  wherever you open it, and they follow you between dimensions and across a
  logout — the block is only a door onto storage that belongs to you, so nobody
  else can see what is in yours. Shulker boxes keep what is inside them when
  broken, which is the whole point of the block: fill one, mine it, carry it,
  place it, and everything is still there.
- **Potions work on mobs.** Status effects were a player-only system, so a
  splash potion of Harming did nothing to a cave full of zombies, a tipped
  arrow of Slowness did not slow anything, and no mob could be poisoned,
  strengthened or healed. Effects now apply to any living thing, with vanilla's
  quirks intact: poison hurts but never kills, the undead ignore poison and
  regeneration entirely, and Healing and Harming are the wrong way round on
  them — a splash of Healing is a weapon against a zombie.
- **Armour a mob is wearing counts properly.** Mobs pick up dropped gear, and
  enchanted armour on one now protects it: the protection enchantments apply,
  and diamond and netherite gear bring their toughness, which was hardcoded to
  zero so it absorbed no better than leather.
- **Sixteen missing enchantments, including the whole protection family.** Fire
  Protection, Blast Protection, Projectile Protection and Feather Falling did
  nothing at all before — the specialised armour people actually build for was
  decoration. They work now, and each guards what it should: Fire Protection
  also shortens how long you burn, and Blast Protection braces you against the
  shove as well as the blast. Also in: Smite and Bane of Arthropods (which bite
  the undead and the creepy-crawlies respectively), Fire Aspect, Thorns,
  Respiration, Aqua Affinity, Depth Strider, Swift Sneak, Soul Speed, Frost
  Walker — which freezes the water you walk over, and the ice thaws behind you
  the way it should — and both curses: Binding keeps armour on, Vanishing
  destroys the item when you die instead of dropping it. Only Channeling is
  still missing; it needs a lightning bolt the engine cannot yet throw.
  Enchanting tables offer the new ones too, by armour slot: helmets can roll
  Respiration, boots Feather Falling or Frost Walker, leggings Swift Sneak.
- **Sixteen missing status effects.** Health Boost, Luck, Unluck, Saturation,
  Conduit Power, Dolphin's Grace, Invisibility, Glowing, Nausea, Darkness and
  Mining Fatigue all work now, and Luck actually shifts what you pull out of
  the water. The four trial-chamber ominous effects are real mechanics rather
  than placeholders: Wind Charged bursts a gust when you die, Weaving strings
  cobwebs where you fell, Oozing spills slimes, and Infested bursts silverfish
  out of you when you are hit. Only `/effect` reaches those four so far — the
  ominous bottle that grants them is still to come.

### Fixed
- **Vaults and decorated pots actually respond now.** Both shipped earlier today
  able to do everything except be clicked on — the block-side work was there and
  the interaction was not wired to it. Same for placed shulker boxes, fixed
  earlier. There is a test now that checks both ends of that wiring.
- **Structures stamp their blocks properly.** Anything with a property but no
  facing was being placed in its default state — so rails in a structure came
  out straight instead of curved or sloped, snow was always one layer deep, and
  farmland, leaves, candles, lanterns, brewing stands and a dozen others all
  lost whatever the builder had set. Forty-three kinds of block were affected.

### Changed
- **Haste and Mining Fatigue change how fast you swing.** Both set the attack
  speed, but nothing read it, so a beacon's Haste sped up mining and left
  combat untouched — and the elder guardian's curse was purely cosmetic in a
  fight.
- **Falling damage is reduced by the right things.** Armour never softened a
  fall in vanilla and does not here, but Feather Falling does — previously the
  protection enchantments were bundled into the armour calculation, so anything
  that skipped armour skipped them too. Resistance and enchantment protection
  also now apply in vanilla's order, which is not the same as applying their
  sum.
- **Raiding a village takes 30 seconds now.** Walking into a village with Bad
  Omen no longer drops the raid on your head the same instant: the omen turns
  into a Raid Omen and the horn sounds half a minute later, at the spot where
  it turned — the warning window vanilla has given you since 1.21.

## 2026-07-25

### Added
- **Attributes are now a real system**, with a public `plugin/attribute`
  package plugins can compile against: entity stats have base values and
  modifiers that stack the way vanilla's do, instead of being fixed numbers
  scattered through the engine. Health, movement speed, armour, armour
  toughness, attack damage, follow range and knockback resistance all run
  through it now, for players as well as mobs — so equipment, potions and
  enchantments finally have somewhere to change a stat.
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
- **Slimes and magma cubes get their size right.** Both now take their health,
  pace, damage and armour from their size the way vanilla does: a magma cube
  used to move at the same speed whether it was tiny or huge, carried no armour
  at all, and hit for two less than it should, and neither species' health
  ceiling followed its size. A tiny slime is harmless, as in vanilla, while a
  tiny magma cube still bites.
- **Knockback resistance is a fraction rather than all-or-nothing.** Ravagers,
  hoglins, zoglins and the nautilus family resist part of a shove instead of
  none of it, and the wither can be knocked back again — vanilla never made it
  immune.
- **Armour a mob is wearing still protects it after a restart.** The gear was
  saved but the protection it gave was not, so a helmeted zombie came back
  wearing a helmet that did nothing.
- **Baby zombies keep their speed.** They move at 1.5x like vanilla's, and stay
  that way through a restart or a change of behaviour — both of which used to
  quietly reset them to adult pace.
- **Mangrove propagules ripen and grow.** They age while hanging under
  mangrove leaves and, once planted, grow a mangrove tree — completing the set,
  so every tree species in the game can now be grown from what it drops.
- **Chorus plants grow in the End.** A flower climbs, branches sideways or
  dies off, leaving jointed chorus stems behind it — so chorus fruit is
  farmable rather than limited to what generated with the world.
- **Bamboo and mushrooms grow.** Bamboo grows from its tip up to sixteen tall,
  moving its leafy crown up the stalk as it goes, and mushrooms creep across
  dark ground — stopping once five already crowd the area, so a cave floor
  never turns solid with them.
- **Kelp and vines grow.** Kelp climbs through water, twisting vines climb,
  and weeping and cave vines hang downward — none of them did anything before.
  Cave vines occasionally grow a segment carrying glow berries.
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
