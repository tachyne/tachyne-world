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

## 2026-08-02

### Added
- **Every biome grows vanilla's own tree mix — and fallen logs lie where
  trees once stood.** A mechanical audit of every biome's vegetation against
  vanilla found a dozen drifted pools, all corrected: plains lead a third of
  their trees as large oaks, meadows split between lone large oaks and tall
  super birches, jungles finally tower with mega jungle trees over their
  bushes, windswept hills are spruce-led, savannas mix a fifth oak, wild
  mangroves are mostly the tall kind, old-growth birch forests grow their
  taller birches, and snowy slopes go treeless as they should. And the
  fallen trees are in: mossy-stumped logs lying on the forest floor with
  mushrooms sprouting along them, at vanilla's rarity, in every biome that
  has them.
- **Huge mushrooms tower where they should.** Dark forests grow vanilla's
  huge red and brown mushrooms in the spots reserved for them — the mix of
  trees around them unchanged — and mushroom fields raise them as their only
  "trees", half red and half brown. Bone meal on a small mushroom has
  vanilla's 40% chance of growing the huge one on the spot, with vanilla's
  strict space rule: anything in the way, even a leaf-high ceiling, and the
  little mushroom just stays.
- **Mangroves stand on their stilts.** The last missing piece of vanilla's
  tree machinery is in: mangrove root placement. A mangrove's trunk now
  rises above where its propagule stood, with roots filling the gap and
  fanning out beneath it — turning muddy inside mud, waterlogging in water,
  and carrying the occasional moss carpet on top. A mangrove that cannot
  root — over a hole, against a wall — refuses to grow and the propagule
  simply tries again later, exactly as in vanilla.
- **Azaleas grow into azalea trees.** Bone meal an azalea or flowering azalea
  bush and it has vanilla's 45% chance of growing the azalea tree on the
  spot: a bending oak trunk under a canopy that mixes flowering patches
  through the plain leaves at one in four, standing on forced rooted dirt.
  If the tree doesn't fit, the bush survives the attempt.
- **Forests are carpeted in leaf litter, and they finally mix their trees.**
  Oak, birch, dark oak and large oak trees in forests and dark forests now
  scatter vanilla's leaf litter around their base — two passes of it, settling
  only on solid ground open to the sky through at most a canopy, never under
  the tree's own branches. And the forests roll vanilla's own species mix
  while they're at it: a regular forest is about a fifth birch with the odd
  large oak among its oaks, and a dark forest leads with dark oak but mixes
  all four — where vanilla would grow a huge mushroom or a fallen log, the
  spot is left open for now so every other tree's odds stay exactly right.
- **Trees prepare their own ground.** Every trunk placer now runs vanilla's
  ground rule: soil that is not already dirt-like gets dirt set beneath the
  trunk (a tree grown on stone stands on dirt, a mega spruce converts all
  four blocks under its 2×2 — and its podzol circles then claim them), while
  grass and podzol are left exactly as they are, matching vanilla. The
  converted block also joins the tree's own accounting, which anchors where
  cocoa pods may sit and where the podzol circles centre — the same
  bookkeeping vanilla keeps.
- **The pale garden wears its moss.** Wild pale oaks now grow the way vanilla
  grows them: a patch of pale moss laid into the ground at the tree's foot —
  topped with pale moss carpet that creeps up the trunk, short grass and tall
  grass — and strands of pale hanging moss trailing from the trunk and
  canopy. A sapling-grown pale oak stays bare, exactly as vanilla's does; the
  moss belongs to the wild trees.
- **Creaking hearts beat inside wild pale oaks again.** One pale oak in ten
  now grows with vanilla's creaking-heart rule: a log enclosed by logs on all
  six faces becomes a dormant heart, which only happens where the trunk's bend
  folds thick — so hearts are rare finds, exactly as in vanilla, and the pale
  garden's guardians spawn in newly explored chunks once more.
- **Mega spruces and pines podzol their ground.** The old-growth taigas now
  roll vanilla's own odds for their giant 2×2 trees, and each one converts the
  dirt and grass around its base into the familiar podzol circles — only ever
  replacing real soil, never stone or air.
- **Mangroves hang propagules.** Wild and planted mangroves alike now dangle
  young propagules beneath their canopies at vanilla's rate and spacing; they
  age on the branch and can be picked, closing the mangrove life cycle.

### Fixed
- **Seven leaf species were second-class.** An old predicate ended the leaf
  family at birch, so jungle, acacia, cherry, dark oak, pale oak, mangrove and
  azalea canopies were treated as solid blocks: dropped items would rest on
  top of them instead of falling through, and their freshly generated leaves
  started life marked for decay (kept alive only by the decay healer). All
  leaf species now behave alike.

- **Jungle trees wear their vines, swamps grow swamp oaks, cocoa grows wild.**
  The tree decorators vanilla dresses a placed tree with: jungle trunks and
  canopies hung with vines at vanilla's odds, one jungle tree in five carrying
  wild cocoa pods on its lower trunk, swamp oaks draped from the canopy, and
  mangroves trailing sparser vines. A sapling-grown jungle tree stays bare, as
  vanilla's does — the vines belong to the wild ones.
- **Leaves decay the vanilla way.** A leaf now tracks its distance from the
  nearest trunk, exactly as vanilla does, instead of scanning a box for any
  log. Fell a tree and the canopy rots from the cut outward in the familiar
  wave; a leaf bridge up to six long stays alive off a single log, while a
  leaf floating near a trunk with no connection to it dies. Stripped logs and
  wood hold a canopy up, so stripping your treehouse's trunk no longer risks
  its roof. Existing worlds are safe: old canopies carry stale data, and a
  leaf is only ever rotted after its distance is verified — stale-but-healthy
  leaves quietly heal instead.
- **Trees are vanilla's trees.** Every tree was a straight column with a
  rounded blob of leaves; now each species grows from a port of vanilla's own
  trunk and foliage placers, configured with vanilla's own numbers. Acacias
  lean and fork, cherries arc their branches out sideways, mangroves throw
  limbs as they climb, large oaks scatter foliage clusters and run a branch to
  each, mega spruces taper. The same code grows a planted sapling and a
  generated forest, so the two can never drift apart — and saplings follow the
  vanilla growers' odds: an oak sapling has its one-in-ten chance of a large
  oak, a 2×2 of spruce grows a mega spruce or pine, dark and pale oak need
  their four. Trees also measure the room they need before growing, as vanilla
  does — a sapling under your roof refuses politely instead of punching its
  canopy through the ceiling.
- **The creaking haunts the pale garden.** Pale oaks now grow around creaking
  hearts, and after dark a heart sends out the thing it has been keeping. It
  behaves as it should: it freezes solid the moment you look at it and only
  moves when you don't, your blows land on the heart rather than on it —
  bleeding resin onto the tree that tells you where to look — and the instant
  you break that heart, it comes apart. Come morning it does too.
- **Dark and pale oaks generate on the thick trunk they are supposed to have.**
  Growing four saplings gave you a proper 2×2 mega tree; the world generator
  gave the same species a single pole. Both build the same tree now.
- **The pale garden grows pale oak.** The biome generated, in the right places,
  carpeted in the wrong colour: it was decorating itself with dark oak, which
  is the one thing a pale garden is not. It now grows its own bone-white wood,
  over a floor of pale moss and closed eyeblossoms rather than borrowed dark
  forest grass and mushrooms.
- **Archaeology, and the desert wells to dig it out of.** A brush, two
  suspicious blocks and six loot tables existed with nothing in the world to
  point them at. Desert wells now generate in the desert — a vanilla feature
  that was missing outright — and each buries two caches of suspicious sand
  under its water. Brushing one works as it should: ten strokes on a cooldown,
  the dust clearing in stages, the find popping out of the face you were
  brushing, and the block left as plain sand. Walk away mid-dig and the sand
  settles back faster than you cleared it.

### Fixed
- **Armoured mobs burned as fast as naked ones.** The engine treated all
  environmental damage to a mob as unarmoured on the premise that fire, lava,
  falling and drowning all bypass armour in vanilla. Only half of that is true:
  falling and drowning do, but standing in lava or fire, on magma or in a sweet
  berry bush does not — so a zombie in a full set of diamond burned exactly as
  fast as one in nothing. Mob damage now runs through one path that decides
  from the damage type, as the player's does.
- **Evoker fangs went through a mob's armour but not a player's.** The player
  half of that spell was corrected last week; the mob half, in the same
  function, was not.
- **Netherite gear wore out in fire.** Vanilla marks it resistant to fire
  damage, which is most of the point of a netherite set near lava. It took
  durability like anything else.

## 2026-07-28

### Fixed
- **A shield blocked three things.** Arrows, a mob's bite and another player's
  swing — and nothing else in the game. Raising one against an explosion, a
  ghast's fireball, a wither skull, a thrown potion, a wind charge, llama spit,
  a bee sting or a mace smash did exactly nothing, though vanilla stops every
  one of them. Which hits a shield catches now comes from the damage type, so
  it also correctly declines to help against lava, fire, a cactus or a falling
  anvil. Blocking cancels what the blow carried, so a bite caught on a shield
  delivers no venom — while a bite merely soaked to nothing by armour still
  does. The shield also wears by what it stopped rather than a flat point per
  hit, shrugging off weak blows for free and paying for heavy ones.
- **Falling anvils ignored the helmet.** Vanilla batters the helmet in
  particular and loses a quarter of the blow's force doing it. Neither happened.
- **Armour did nothing against lava, fire, cactus, magma, berry bushes or
  lightning.** Vanilla decides what a hit does to you from the damage type's
  own tags; tachyne left that decision to each of the twenty-odd places that
  deal damage, and six of them never asked. Standing in lava in full diamond
  hurt exactly as much as standing in it naked. The mirror image was true too:
  the ender dragon's blows and a guardian's bite were softened by armour but
  never wore it down, so a set could outlast a whole End fight. Armour is now
  applied in one place, from the damage type, and absorbing a hit and wearing
  from it are the same decision — they cannot drift apart again.
- **Protection enchantments now stack where vanilla stacks them.** A ghast's
  fireball counts as both fire and a projectile, so Fire Protection and
  Projectile Protection should both guard against it; only one used to.
- **Resistance no longer blocks `/kill`, and starvation ignores it.** Damage
  that vanilla marks as bypassing effects, enchantments or resistance now does:
  Resistance V used to make a player unkillable by command, and the Warden's
  sonic boom was blunted by armour enchantments it should shrug off.
- **Hunger cost follows the damage type.** It was a per-call-site argument with
  a default of 0.1, so a few sources charged for hunger that vanilla does not
  and vice versa. Being pricked by a cactus or scorched by a campfire now costs
  what it should.

## 2026-07-27

### Fixed
- **Older clients could be disconnected by content newer than they are.** The
  translation layer can shift an id between versions but had no way to say
  "this does not exist on that client" — so a block, item or entity added after
  a player's version was sent with its id unchanged. Canonical ids run higher
  than an older client's registry (items reach 1504 against 1396 entries on
  1.21.5), so such an id could land past the end of the registry entirely,
  which a client cannot decode: not a wrong icon, a dropped connection. Items
  and blocks a client has never heard of now arrive as air, and entities as
  their nearest sensible stand-in — a happy ghast reads as a ghast rather than
  vanishing. 26.x players were never affected, since those versions are a
  superset; this only ever bit 1.21.5-1.21.9.
- **Horses are no longer clones.** Every horse had the same 22 health and the
  same speed, and nothing rolled a jump at all — which quietly removed the
  point of breeding them. Vanilla randomises three attributes per horse, each
  the sum of several small rolls so the middle is common and an exceptional
  animal is rare: health between 15 and 30, speed across a three-fold range,
  and jump strength between 0.4 and 1.0. Foals now land between their parents,
  so breeding two good horses tends toward a better one without ever promising
  it. Skeleton and zombie horses roll only their jump, and donkeys and mules
  roll nothing — being dependable is their job.
- **Mending and Efficiency did nothing, and Efficiency was worse than nothing.**
  Both sat in the treasure and fishing pools and were wired to no code at all.
  Mending — the most valuable enchantment in the game — now spends experience
  on damaged held and worn gear before any of it reaches your bar, two
  durability per point, picking one item at a time as vanilla does. Efficiency
  is now modelled by the anti-cheat instead of guessed at: the old blanket
  allowance assumed enchantments could roughly double mining speed, but
  Efficiency V adds twenty-six to the speed, which on a wooden pickaxe is a
  fourteen-fold speed-up — so a legitimately enchanted player was breaking
  blocks faster than the server permitted and having every break reverted.

### Added
- **Sniffer eggs hatch, and chorus plants fall.** An egg dug out of suspicious
  sand sat on the ground for ever, which made the whole archaeology-to-sniffer
  chain a dead end; it now cracks twice and opens into a snifflet, with moss
  underneath halving the wait. And a chorus plant that loses its footing comes
  down: cut the base of a chorus tree and the rest pops after it, segment by
  segment, instead of hanging in the air.
- **Projectiles do something to what they hit, and Flame and Infinity work at
  all.** An arrow through a candle did nothing, amethyst never chimed and a
  decorated pot shrugged off a direct hit; the reactions that did exist were
  each wired wherever the flight loop happened to notice them. There is now one
  place that decides: a burning projectile lights candles and candle cakes,
  amethyst rings at a random pitch, a decorated pot shatters and spills what it
  held, and a thrown trident still travelling shears pointed dripstone off the
  ceiling. Target blocks work in every dimension now rather than the overworld
  alone. Found on the way: **Flame and Infinity were in the enchantment pools
  and wired to nothing** — a Flame bow now really does set what it hits alight
  (and lights those candles), and Infinity really does keep the arrow.
- **The dragon fight has its shape back.** The ender dragon circled and swooped
  and nothing else: no breath, no fireball, no perching — so it could be beaten
  by standing still and swinging, and a bow was pointless because its head
  never came within reach. It now runs vanilla's phase machine. It circles, and
  at each lap decides whether to strafe you — closing to line up a fireball
  that bursts into a cloud of breath where it lands — or to come in and land on
  the exit portal, where it sits with its head in reach and breathes over the
  podium to drive you off before climbing away again. The odds of it landing
  rise as the crystals come down, which is what finally makes destroying them
  the objective rather than a chore. A perched dragon no longer deals contact
  damage, so the window it opens is a real one.
- **The evoker casts.** It spawned, joined raids and dropped its totem without
  ever doing the one thing an evoker does. Both spells are in: fangs erupt from
  the ground — a line walking out toward you at range, two rings around it up
  close, each fang biting a moment after it surfaces — and a flight of three
  vexes is conjured when there are not already vexes about, each with a limited
  life so an abandoned swarm clears itself. The fangs bite through armour, so a
  full set of diamond is no answer to one.

## 2026-07-26

### Added
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
