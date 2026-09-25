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

## 2026-09-25

### Added
- **/tellraw.** `/tellraw <targets> <message>` sends a JSON text message
  with its colours, styles, click and hover actions, as in vanilla. Score
  and selector parts are filled in for each reader. Bedrock players get the
  plain text.
- **/stopsound.** `/stopsound <targets> [<source>|*] [<sound>]` stops
  every sound, a whole category or one sound for the chosen players, as in
  vanilla. Bedrock players can have every sound stopped.
- **Middle-click pick block.** Middle-clicking a block or an entity now
  works as in vanilla, in survival too: it selects the matching stack in
  your hotbar, or swaps it in from the rest of your inventory. In creative
  you get a fresh one if you have none. Crops pick their seeds, wall torches
  and signs pick the standing item, a potted plant picks the plant, a mob
  picks its spawn egg and an item frame picks what it holds. The server had
  been ignoring the click since 1.21.4, when the game moved picking to the
  server.
- **/particle by name.** `/particle minecraft:flame ~ ~1 ~ 0.5 0.5 0.5 0.1
  20` works as in vanilla for every particle that takes no extra options
  (colours, blocks and items are not supported yet); it used to want a
  numeric id.
- **/playsound in vanilla's form.** `/playsound <sound> [source] [targets]
  [x y z] [volume] [pitch] [minVolume]` — the source picks which volume
  slider it plays on, and a far-off player hears it only if a minimum volume
  is given.
- **/locate biome.** `/locate biome <id>` finds the nearest biome of that
  kind — cave biomes included — within 6400 blocks, searching the way
  vanilla does.
- **/clear an item.** `/clear [targets] [item] [maxCount]` takes only that
  item, up to the count given, or with a count of 0 just says how many there
  are, as in vanilla; it used to empty the whole inventory.
- **/tp moves other players and mobs.** Operators can send players or mobs
  to another entity (`/tp bob alice`) or to a position, optionally with a
  rotation (`/tp @e[type=cow] 0 80 0 90 0`), as in vanilla. `/tp` is now for
  operators only, as it is in vanilla; before, any player could use it.
- **Two more team options.** `/team modify` takes `seeFriendlyInvisibles`
  and `deathMessageVisibility` — a team's deaths can be announced to
  everyone, only the team, everyone but the team, or nobody, as in vanilla.
- **More scoreboard criteria.** Objectives can track `food`, `air`,
  `armor`, `xp` and `level` as well as `health`, kept up to date by the
  game and read-only to `/scoreboard players set`, as in vanilla. The death
  counter uses vanilla's name, `deathCount` (objectives made with the old
  `deaths` carry over).
- **/spawnpoint takes targets and a position.** Operators can set any
  player's spawn point, at a given position and in whichever dimension they
  are in, as in vanilla; it was open to everyone and only ever set your own,
  where you stood, in the overworld.
- **/xp does everything vanilla's does.** `/xp` (and `/experience`) can
  add, set or query a player's experience in points or levels; a bare
  number means points, as in vanilla, and taking points away drops levels.
- **/difficulty on its own tells you the difficulty**, and setting the one
  already in force says so.
- **Beached shipwrecks.** Wrecks now also lie run aground on beaches, half
  buried in the sand with their chests, as in vanilla (upright or on their
  side, never upside down). `/locate structure shipwreck_beached` finds
  them, and dolphins lead you to them too. They skip any spot you have
  built in.
- **Ice spikes, boulders and blue ice.** The ice spikes biome finally has
  its spikes: packed-ice spires, the odd one raised high on a pillar, and
  packed ice patches in the snow. Old-growth taigas are strewn with mossy
  cobblestone boulders, and blue ice grows on the undersides of the frozen
  oceans' icebergs. None of them appears where you have built.
- **/rotate.** Operators can turn a player or mob to a rotation (`~` for
  relative), or make it face a position or another entity's feet or eyes.
- **/trigger.** Scoreboard objectives can use the `trigger` criteria, and
  an operator can `/scoreboard players enable <player> <objective>`; that
  player (no operator rights needed) can then `/trigger <objective>`, or
  `add`/`set` a value, once per enable — the vanilla way to let players
  press a button in a map or minigame.
- **More plants in the wild.** Newly generated land grows the rest of
  vanilla's surface plants: tall grass in the plains, savannas and cherry
  groves, large ferns in the taigas, sunflowers in the sunflower plains,
  lily pads on swamp water, leaf litter on the dark forest floor, bushes
  in the forests, plains, rivers and windswept hills, firefly bushes in the
  swamps and along shores, short and tall dry grass in the deserts and
  badlands, wildflowers in the birch forests and meadows, pale moss patches
  in the pale garden, and vines on the jungles' trees and cliffs. None of
  them grows on your floors or under your roofs.
- **Ravines.** Long, deep cuts through the ground now wander across the
  overworld as in vanilla, sometimes opening to the sky and flooding where
  they pass under the sea. Nothing is cut where you have built: a ravine
  that would reach a build is left out whole.
- **Lava lakes.** Pools of lava ringed with stone now form on the surface
  (rarely) and underground (often, from y=0 up), as in vanilla. They too
  keep clear of your builds.
- **Dirt, gravel and clay pockets underground.** Stone now holds pockets of
  dirt and gravel everywhere, and lush caves pockets of clay, as in
  vanilla — never in the stone around a build.

### Fixed
- **Tall grass drops seeds from either half.** Breaking the top half of
  tall grass or a large fern can now drop wheat seeds too, as in vanilla;
  a plant whose two halves come down together still rolls only once.
- **Loose scaffolding falls.** Scaffolding set down with nothing holding
  it (by a command or a structure) now drops like sand and settles on the
  ground as scaffolding, as in vanilla.
- **Grass and mycelium spread and die as in vanilla.** Grass now dies
  under water, a bottom slab, a lower stair or deep snow (a single snow
  layer is fine), no longer spreads onto dirt that is under water, and
  grass that spreads under snow comes up snowy.
- **Sponges dry out waterlogged blocks.** A sponge now drains waterlogged
  slabs, stairs, fences and the like (they stay, dry) and soaks up the
  water beyond them, as in vanilla.
- **Boats need room.** A boat can no longer be placed inside another boat,
  a player, a mob or a wall, as in vanilla.
- **Buckets scoop what you are looking at.** An empty bucket no longer
  reaches through grass, flowers or a torch to the water behind them, it
  aims from your real eye height (lower when you crouch), and it can
  scoop a bubble column, as in vanilla.
- **Spawn eggs make babies of more mobs.** Using a spawn egg on a
  villager, squid, glow squid, dolphin, zombie, husk, drowned, zombie
  villager, zombified piglin, piglin or zoglin now spawns a baby of it, as
  in vanilla. A baby villager takes its parent's or the biome's type and
  starts unemployed.
- **Empty maps work as in vanilla.** An empty map works from the offhand
  too, a last empty map turns into the new map right in your hand, a full
  inventory drops the new map instead of losing it, creative no longer
  uses up the empty map, and filling one plays the cartography sound and
  counts as a use in your statistics.
- **Fishing catches fly to you.** A catch now leaves the bobber as an item
  that arcs towards you, as in vanilla, instead of appearing straight in
  your inventory, and keeps every detail of what was caught.
- **Drops keep everything on them.** Gear a mob picked up, vault and
  trial-spawner rewards, archaeology finds and trade items that no longer
  fit your inventory now drop whole: names, dye, potions on tipped arrows
  and ominous bottles, and the desert well's suspicious stew effect are
  no longer stripped.
- **Named armour stands.** A name tag now names an armour stand, a renamed
  armour-stand item places a stand with that name (by hand or from a
  dispenser), the name is kept across restarts, and breaking the stand
  drops a named item. The armour it wore drops whole — dyed leather,
  custom names and banner patterns on a head are no longer lost.
- **Bucketed mobs keep their health and name.** A hurt fish, axolotl or
  tadpole scooped into a bucket comes back out just as hurt, and a named
  one's bucket carries the name and names the mob again when poured, as in
  vanilla.
- **Copper golems sound their age.** A weathered or oxidized copper golem
  now hurts, dies and steps with its own creakier voice, as in vanilla, and
  a golem turning into a statue plays its sound again (it had been silent).
- **End crystals go down anywhere.** An end crystal can be placed on
  obsidian or bedrock in any dimension, as in vanilla, not only in the End;
  only the cell above has to be clear, and any mob, item or crystal in the
  way stops it. Struck, it blows up where it stands.
- **Lightning cleans copper.** A bolt that strikes copper turns it back to
  fresh copper and scrapes the oxidation off copper blocks around it, with
  a spark on each, as in vanilla. Lightning advancements now count every
  player within 256 blocks of the bolt, not just those within 30.
- **Endermen drop what they carry.** Killing an enderman that is holding a
  block now drops that block, as in vanilla.
- **Creative players pick things up.** Walking over dropped items in
  creative now picks them up, as in vanilla; only spectators leave them.
- **The dragon's rewards are vanilla's.** Killing the Ender Dragon no
  longer drops an elytra (those come from End ships). The first kill gives
  12000 XP and the dragon egg; later kills give 500 XP and no new egg. Each
  kill opens one more End gateway instead of all twenty at once.
- **Falling blocks fall like vanilla's.** Sand, gravel, concrete powder,
  anvils, the dragon egg, suspicious blocks and loose stalactites now come
  down as falling blocks that pick up speed as they drop, a moment after
  losing their support, instead of stepping down one block a tick. One that
  lands where it cannot stay — on a torch, a flower, a slab, a bed or a few
  layers of snow — breaks into its item. Suspicious sand and gravel break
  whenever they fall. Concrete powder sets in the first water it drops into.
  An anvil hurts what it lands on by how far it fell and can come down
  chipped, or shatter if it was already damaged, and powder snow cushions
  it. A block caught mid-air by a restart lands afterwards instead of
  vanishing.
- **Mobs ride bubble columns, bounce on slime and stick in cobwebs.** A
  mob caught in a soul-sand updraft is carried to the surface and tossed
  clear of it, and a magma whirlpool drags down even a mob swimming for
  air; before, the columns only moved fish. A mob dropped onto a slime
  block bounces instead of taking damage, and walking on slime slows it. A
  mob caught in a cobweb over a drop sinks slowly through it, and one
  falling against a honey block's side slides down it. A mob's fall now
  counts only from the last thing that broke it — water, a cobweb, powder
  snow or a honey slide — and lands softer on hay, honey and beds, as a
  player's does. A player falling through a cobweb has the fall broken
  there too.
- **Dropped items bounce on slime and catch in cobwebs.** An item dropped
  on a slime block bounces, one in a cobweb creeps down through it, powder
  snow slows it, and the top of a soul-sand bubble column throws items
  clear of the water as in vanilla.
- **Mobs freeze in powder snow.** A mob left in powder snow now frosts
  over, slows down and, once frozen through, takes freeze damage — five
  times as much for blazes, striders and magma cubes — while strays, polar
  bears, snow golems and anything wearing leather are spared. Rabbits,
  foxes, endermites, silverfish and mobs in leather boots walk on top of
  the snow instead of sinking into it, a burning mob that blunders into it
  is put out and melts it, and so does a burning player.
- **Potion swirls.** Players and mobs under an effect now give off its
  coloured swirls, as in vanilla — faint for a beacon's or a conduit's,
  none for an effect given with hidden particles, and Oozing, Weaving,
  Infested, Wind Charged and the omens show their own particles. Nobody
  could see that anyone was under a potion before.
- **Redstone dust dots stay dots.** A dot made by clicking a lone cross of
  dust now powers only the block under it, as in vanilla — it used to light
  the lamp beside it like a cross — and turning a cross into a dot or back
  updates what is around it at once. Adventure players can no longer flip
  it.
- **Comparators read item frames, and read what is behind them outright.**
  A comparator reading through a block now picks up an item frame hung on
  the far side (one step per turn of the item), and a container or other
  readable block behind it sets the reading on its own, so an empty chest
  reads 0 even with power running into it. A comparator no longer reads a
  cart parked on an ordinary rail; on a pressed detector rail it reads a
  chest or hopper cart's contents, and 0 for any other cart.
- **Pressure plates, detector rails and tripwires keep vanilla's rhythm.**
  A pressed plate or detector rail re-checks once a second (half a second
  for weighted plates) and lets go on that beat, instead of exactly a second
  after the last thing left. Plates count what actually touches them —
  carts and boats too, never spectators. Dropped items, arrows, carts and
  boats now trip tripwire, a trip lasts at least half a second, hooks click
  as they power, attach and detach, and laying or cutting a string updates
  the hooks at both ends however far apart they are.
- **Fire behaves as in vanilla.** Fire now burns on its own clock, so
  activity nearby no longer makes it spread faster, and a fire on magma
  burns forever as it does on netherrack (and on bedrock in the End). Fire
  burns faster in jungles, swamps, mushroom fields, snowy slopes and dappled
  forests, it eats blocks even with mob griefing off, and rain only puts out
  fires it actually falls on — not ones in caves or under glass. Soul fire no
  longer burns out. Any fire that appears inside an obsidian frame lights
  the portal — a fire charge, a dispenser, lightning, a spreading blaze —
  but a frame in the End never lights. Lava no longer lights fires through
  a slab or glass lid.
- **TNT with tnt_explodes off stays put.** Powering, shooting or trying to
  light TNT no longer removes the block when TNT explosions are off; the
  flint and steel or fire charge is not used up, and you are told TNT
  explosions are disabled.
- **Clicks that should fall through to your block.** Clicking an iron door
  or trapdoor, an empty jukebox, or the side or top of a bookshelf or shelf
  with a block in hand now places the block, as in vanilla, instead of doing
  nothing. A chiseled bookshelf gives its book back whatever you are holding,
  and enchanted books have their own sounds going in and out.
- **Large chests count as chests.** Opening a double chest now counts
  toward your chest statistics and angers nearby piglins, as a single chest
  does, and its sound comes from the middle of the pair. An ender chest held
  shut by a block no longer counts as opened.
- **Bells light up raiders after the ring.** A rung bell now resonates a
  moment into its swing if a raider is within 32 blocks, and the raiders
  around it glow when the ringing ends, as in vanilla — pillager patrols
  too, not only raid members.
- **Ravagers flatten a proper swath.** A ravager now tramples every crop
  its body passes over, not one row at walking pace, and broken crops, lily
  pads and doors show their break.
- **Sculk hears more of the world.** Sensors now pick up a candle being
  snuffed, a bite of candle cake, a disc going into a jukebox, a book set on
  a lectern, a cauldron level dropping, an empty dispenser clicking,
  farmland or a path turning to dirt, a shelf powering, a dried ghast
  drinking, a sniffer egg cracking, and blocks broken by mobs and boats. A
  sensor's clicking now audibly stops.
- **Frogspawn, dried ghasts and frosted ice keep their own time.**
  Frogspawn no longer hatches early whenever something changes beside it,
  and it lasts on any water with nothing on top. A dried ghast drinks a
  step every 5000 ticks while waterlogged (its own water, not water beside
  it). Frost Walker ice melts at the right light, ages its neighbours as it
  goes, and a lone leftover block melts straight away.
- **Hoppers pick up items as vanilla's do.** A hopper takes items silently,
  from the whole area over its bowl, and a stack it can only partly take no
  longer counts as a pull; a solid block on top stops it collecting items.
- **Smaller fixes.** Copper doors weather as one piece, and a copper chest
  someone has open does not weather. Dry farmland under a fence gate stays
  tilled, and anything standing on farmland or a path that turns to dirt is
  lifted onto it. Sponges soak up water that flows next to them, and a wet
  sponge pushed into the Nether dries. A creaking heart's comparator
  follows its creaking live, and a jukebox's redstone signal stops when the
  song ends. Placing a book on a lectern resets it; a leftover moving-piston
  block from before a restart goes when clicked; an open shulker box opens
  for a second player whatever is in front of it; a sign stays yours while
  you are within reach; portal-spawned zombified piglins do not walk
  straight back through, and appear only with a player nearby; an
  eyeblossom poisons a bee for 25 ticks, not 40; and stepping into fire
  thaws a player freezing from powder snow.
- **Explosions and falls, as in vanilla.** An explosion or a wind charge
  that reaches a beehive sets the bees near it on a player nearby, whoever
  set it off. Ore blown up by TNT a player lit drops its experience, as if
  mined. Mobs landing on turtle eggs can crack them — zombies never do.
- **Ice and coral read the water as vanilla does.** Ice broken over air, a
  flower or a torch no longer leaves water behind — only over a solid block
  or a liquid. Coral beside a waterlogged block, seagrass or kelp stays
  alive instead of bleaching.
- **Redstone from jukeboxes and trapped chests.** A jukebox whose song
  ends now turns off the redstone it was powering, and a trapped chest in
  the Nether or the End gives a signal when opened, as in vanilla.
- **Fences join their gates.** A fence now connects to a fence gate set in
  line with it, as in vanilla, and no longer reaches out to glass panes,
  leaves, pumpkins or melons; wooden fences and the nether brick fence keep
  apart. Glass panes and iron bars join copper bars.
- **Placing blocks as in vanilla.** A second pink petal, wildflower or
  leaf litter clicked onto the first adds to it; a plant is never swapped
  for itself by clicking it with the same plant. Campfires face the way
  you look and go in unlit under water. Trapdoors hinge on the side you
  click, or face you when set on a floor or ceiling (they were turned the
  wrong way). Ladders go on the wall you look toward and never onto the
  front of another ladder. Concrete powder placed beside water is concrete
  at once, grass placed under snow is snowy, and a big dripleaf set on
  another keeps its facing. A door, trapdoor or gate placed next to power
  goes down open, without swinging open with a sound a moment later.
- **Signs, banners, heads and bells go where vanilla puts them.** Clicking
  a ceiling with a sign or banner now puts it on the wall you are looking
  toward, or on the floor below, instead of doing nothing; mob heads face
  the way you look rather than away from you. A hanging sign set on a floor
  under a low ceiling hangs from the ceiling, and one goes on a wall only
  where something holds it at the side. A bell clicked onto a wall that
  cannot hold it goes on the floor or ceiling instead, and only a hit on
  its ringing side rings it — counted in your statistics when you shot it.
  Adventure-mode players' arrows, and mobs' arrows with mob griefing off,
  no longer smash decorated pots.
- **What holds a block, as in vanilla.** Carpet stays on anything that is
  not air — a torch, a flower, even water — where it used to fall off.
  Amethyst buds and clusters point out of the face you set them on (up,
  down or sideways) and drop when that block goes. A fire whose floor goes,
  with nothing beside it to burn, goes out at once. Frogspawn needs still
  water under it (running water no longer holds it) and no longer minds
  water above it.
- **Cut plants grow on, and hanging moss holds together.** Cutting the top
  off a kelp stalk or snapping a vine now leaves a fresh tip that keeps
  growing, as in vanilla; before, the cut end never grew again. Bamboo
  growing out of a shoot turns the shoot into a stalk, and a stalk picks up
  the thicker look of older bamboo above it. A big dripleaf set on another
  turns the lower one into stem. Pale hanging moss hangs from more moss and
  marks its new end when cut — strands used to fall apart whenever anything
  nearby changed. Pale moss carpet lets go of a wall that is taken away, and
  an upper layer with nothing left to climb goes with it.
- **Ruined portals sit in the ground.** A ruined portal now settles, as in
  vanilla, until at least three of its four corners stand in solid ground,
  so one on a cliff edge sinks into the rock instead of hanging off the
  side. Only chunks with a portal change, and a portal never moves into a
  player's build.
- **Goat horns say which call they are.** A goat horn's tooltip now names
  its call — Ponder, Sing, Seek and the rest — as in vanilla; horns used to
  carry no instrument at all on the client.
- **Tie leads to a fence with anything in hand.** Clicking a fence while
  leading animals now ties them to it whatever you are holding, as in
  vanilla, not only with a lead in hand; with nothing on a lead the click
  does what the held item would.
- **Rowing makes a sound again.** Rowing a boat now plays the paddle
  splash (or the scrape on land) with each stroke, for you and everyone
  near, and other players see the boat's paddles move, as in vanilla. The
  server never heard which paddles were rowing, so boats moved in silence
  with still oars.
- **Raids re-centre instead of failing.** A raid whose centre stops being
  a village now moves to the nearest village section within two sections
  before it is declared lost, as in vanilla, so a raid at the edge of a
  village no longer ends in defeat just because its bell was out of reach.
- **Respawn beside your bed, not on it.** Respawning at a bed or respawn
  anchor now stands you up in a free spot next to it, as in vanilla,
  instead of on top of it. A bed or anchor walled in with nowhere to stand
  counts as obstructed, and you respawn at the world spawn.
- **Fiery blasts light soul fire on soul blocks.** A fiery explosion — a
  bed going off in the Nether, a ghast fireball — now lights blue soul fire
  on soul sand and soul soil, as in vanilla.
- **Wardens emerge where vanilla's do.** A summoned Warden now looks for
  ground up to five blocks around the shrieker and six up or down, so it
  can climb out on a ledge or floor nearby instead of failing to appear
  when the few spots right beside the shrieker are blocked.
- **Sculk hears mobs everywhere.** A sculk sensor or shrieker built in the
  Nether or the End now picks up mobs walking past, as it does in the
  overworld.
- **Nether lava runs fast.** Lava in the Nether now flows three times as
  fast as the overworld's and spreads as far as water (seven blocks), as in
  vanilla; it used to behave like overworld lava.
- **Turtles roam the sea.** A turtle in the water now keeps swimming,
  stretch after stretch, toward far-off points as in vanilla, instead of
  standing about; ashore it sets off on a stroll a little more often.
- **Foxes act like vanilla foxes.** A fox now walks in upright on a
  chicken or rabbit until it is close, crouches, and pounces from the
  crouch — and a pounce that comes down in snow leaves it head-first in the
  drift for a couple of seconds. By day it leaves the open sky for cover
  (at once in a thunderstorm), and it only sleeps somewhere sheltered — on
  grass or in good light — with nothing alive close by, not just no
  players. It picks ripe sweet berries and glow berries and keeps one in
  its mouth, sits down now and then to look about, wanders in toward a
  village at night, and watches players from 24 blocks. A fox defending a
  player it trusts no longer runs from other players, wolves or bears, or
  panics when hit, and a sleeping, sitting or crouched fox now looks that
  way to players who arrive later.
- **Piglins hunt hoglins, fight wither skeletons and celebrate.** A grown
  piglin in the wild now goes after a hoglin it sees, now and then, with the
  piglins around it joining in; the hoglin fights back, and when the piglins
  are outnumbered they back off. A bastion's own piglins and hoglins leave
  each other alone, as in vanilla. Piglins and brutes attack wither
  skeletons and the wither on sight, and a baby piglin runs from them. When
  a piglin's target dies it gathers where it fell for fifteen seconds, and
  after a hoglin hunt the piglins sometimes dance. Shooting a piglin now
  angers it, and the piglins near it, the way a blow does. Piglins also
  stroll at vanilla's slower idle pace.
- **Piglin brutes keep to their bastion.** A brute remembers where it
  spawned and, with nothing to fight, walks back there, strolls about it,
  and wanders over to the piglins and brutes near it, all at an unhurried
  pace, as in vanilla. It used to drift off across the Nether like any
  other monster. Piglins and brutes also open wooden doors in their way now.
- **Placing pointed dripstone.** Pointed dripstone now hangs from a ceiling
  or stands on a floor depending on where you look, as in vanilla, and a
  column keeps its proper taper — tip, frustum, middle, base — as pieces are
  added or taken away. Sneaking keeps a stalactite and a stalagmite from
  merging their tips.
- **Small dripleaf placement.** A small dripleaf now faces you when you
  plant it and keeps the water it's planted in, as in vanilla — it used to
  face away and dry out its cell.
- **Seagrass only in water.** Seagrass can be planted only into water, as
  in vanilla; it used to go down on dry land too.
- **Planted propagules are full size.** A mangrove propagule planted by
  hand goes down fully grown and standing, as in vanilla, ready to grow
  into a tree.
- **Shulker boxes face the way you place them.** A shulker box opens
  toward the face you place it against — up, down or sideways — as in
  vanilla.
- **Mushrooms need shade.** As in vanilla, a mushroom stays in bright light
  only on mycelium, podzol or nylium; anywhere else it needs a light level
  under 13 and a solid block beneath, or it pops off at its next update.
- **Mobs crack turtle eggs and light redstone ore.** A mob standing on
  turtle eggs can crack them, as in vanilla (turtles and bats never do,
  and it needs mob griefing), and any mob walking on redstone ore makes it
  glow — before, only players did either.
- **Sneak across magma.** Crouching on a magma block no longer burns you, as
  in vanilla.
- **Mobs trample farmland.** A big enough mob — a cow, a horse, a zombie —
  that drops onto farmland can turn it back to dirt, as in vanilla, when
  mob griefing is on. Chickens and other small mobs never do.
- **Fences, walls and panes reconnect whatever changes beside them.**
  Fences, walls, glass panes, iron bars and stairs now re-join (or let go of)
  their neighbours when an explosion, a piston or anything else changes the
  block next to them, as in vanilla — not only when a player builds or
  breaks. Tripwire now connects only to tripwire and hooks, not to any
  solid block beside it.
- **Wall bells stay up.** A bell hung on one wall was checked against the
  wrong side and fell the first time anything next to it changed. It now
  hangs on the wall it faces. A bell between two walls that loses one keeps
  hanging from the other, and a wall bell that gains a second wall is held
  by both, as in vanilla.
- **Coral dies when it dries out, however that happens.** Coral left
  without water by a sponge, a piston or receding water now bleaches, as in
  vanilla — before, only a player removing the water set it dying.
- **Comparators notice a statue or heart going.** Breaking a copper golem
  statue or a creaking heart that a comparator was reading now drops the
  comparator's signal straight away.
- **No half beds or doors.** When one half of a bed or a door (or a tall
  plant) is destroyed by something other than a player — an explosion, a
  piston — the other half now goes with it, as in vanilla, instead of
  being left behind.
- **Signal fires.** A campfire on a hay bale becomes a signal fire with its
  tall column of smoke, as in vanilla, and goes back to an ordinary campfire
  when the hay is taken away.
- **Crops need light.** Wheat, carrots, potatoes, beetroots, torchflowers
  and pitcher plants now pop off when their spot is too dark (a light
  level under 8), as they do in vanilla — a farm shut in without torches
  loses its crops.
- **Fireworks on the right volume slider, and thunder once.** Firework
  sounds were sent on the Music channel, so turning music down silenced
  them; they now play as ambient sounds, as in vanilla. Each lightning
  strike's thunder was also heard twice, because the game client plays
  it itself as well as the server sending it.
- **/summon puts mobs where you ask.** A summoned mob now appears at the
  exact coordinates given, rather than dropping to the ground below them,
  in whatever dimension you are in, and Nether mobs (piglins, blazes,
  ghasts, striders, hoglins, magma cubes) can be summoned with their proper
  setup anywhere.
- **/worldborder times in ticks.** As in vanilla, the time for a border
  move is in ticks, or seconds and days with `s` and `d` (`10s`, `1d`); a
  bare number used to be read as seconds. The reply now says whether the
  border is growing or shrinking.
- **Phantoms circle higher.** A phantom now circles 20 to 40 blocks above
  its target, picking a fresh height before each swoop, and never below sea
  level, as in vanilla.
- **Endermen blink away from harm.** Hurt by something that isn't a
  creature — fire, a cactus, a fall — an enderman teleports away nine
  times in ten, as in vanilla.
- **Ghasts drift up and down.** A ghast now floats to its chosen point in
  three dimensions instead of holding one height.
- **Horses rear.** Horses, donkeys, mules and skeleton and zombie horses
  now rear up now and then with a whinny, as in vanilla, and rear when
  angered or bucking an untamed rider — the engine never sent the rearing
  pose before. A tamed horse shows as tamed to everyone nearby.
- **Spectators don't start raids.** A spectator with Bad Omen can pass
  through a village without setting off a raid, as in vanilla.
- **Brewing stands bubble smoothly.** The brew now counts down every tick,
  as in vanilla, so the progress bar and bubbles move steadily instead of
  jumping once a second.
- **Players can be crammed.** A survival player squeezed in with as many
  mobs or players as the `max_entity_cramming` rule allows now takes
  cramming damage, as in vanilla — before, only mobs did.
- **The Warden's Darkness pulses.** As in vanilla, a Warden sends out
  Darkness every six seconds, lasting thirteen, to survival and adventure
  players within 20 blocks — the screen fades in and out rather than
  staying dark — and creative players are left alone.
- **Weaving and Oozing follow the rules.** A player who dies with Weaving
  leaves cobwebs even with `mob_griefing` off (only mobs need it), and the
  slimes Oozing lets out are capped by the `max_entity_cramming` rule, as in
  vanilla, instead of a fixed 24.
- **Maces against players.** A falling mace blow on another player now
  counts as a smash ("was smashed by"), and Breach cuts through a player's
  armour as it does a mob's.
- **Wind charges sting.** A wind charge that hits a player deals its one
  point of damage, as in vanilla, not just the shove.
- **Lightning rods don't stick on.** A lightning rod set down already
  powered (by a command, or copied with /clone) now switches off, as in
  vanilla, instead of powering its redstone for ever.
- **Hiding from the locator bar.** As in vanilla, a player who is
  invisible, wears a carved pumpkin or a mob head, or is in spectator mode
  no longer shows on other players' locator bars, and reappears when that
  ends. A player who changes dimension now leaves the bars of those left
  behind.
- **The Nether has its bedrock roof.** Above the caverns the Nether is now
  solid netherrack up to vanilla's bedrock roof at y=127, ragged on its
  underside, with open air over it; before, the top of the caverns gave
  onto an endless dark void. Anywhere you built at y=115 or higher, the
  void stays open over your build and two blocks around it.
- **Sandstone under the sand, and no sand over caves.** Deserts now have
  sandstone under their sand, reaching up to thirty blocks down, and
  beaches and warm oceans a few blocks of it, as in vanilla. Sand, red
  sand or gravel with a cave straight under it generates as sandstone,
  red sandstone or stone, so a cave roof no longer pours sand on you when
  you open it. Frozen ocean floors have vanilla's small bare-stone dips,
  and flooded badlands are floored with orange terracotta. Only
  untouched ground changes; anything you placed or dug stays as it is.
- **Sculk catalysts leave your builds alone.** A catalyst's bloom now turns
  only natural ground (stone, dirt, sand, gravel, and the rest vanilla
  lists) into sculk. Before, it could turn any solid block near a death
  into sculk, planks and bricks included.
- **Sculk spreads the way it does in vanilla.** When something dies near
  a sculk catalyst, its experience now creeps outward over the next few
  seconds instead of appearing as a patch all at once: veins run across
  the ground, each point of experience turns one block of natural ground
  into sculk, and charge that crosses older sculk can sprout sculk sensors
  and shriekers (a shrieker grown this way never calls a Warden). Players
  who die near a catalyst feed it too, and the catalyst blooms with its
  soul particles and sound. Charge still travelling when the server
  restarts is lost.
- **Snow layers need a proper floor.** A placed snow layer now follows
  vanilla's rule: it can't sit on ice, packed ice or a barrier, always sits
  on honey, soul sand or mud, and otherwise needs a full top face. Snow
  also settles on mud when it snows.
- **Soul fire.** Flint and steel, a fire charge, or fire spreading onto
  soul sand or soul soil now lights blue soul fire, as in vanilla; before,
  only a flaming arrow did. Soul fire goes out when the block under it is
  removed.
- **Summon the Wither on soul soil.** The Wither's T can be built from
  soul soil as well as soul sand, and, as in vanilla, the two spaces
  beside the bottom block must be empty.
- **Wool hides you from sculk.** As in vanilla, a wool block between a
  sound and a sculk sensor, shrieker or Warden now stops the vibration;
  placing or breaking wool (and wool carpets, slabs and stairs) makes none,
  and walking on wool or a wool carpet is silent. Wardens themselves make
  no vibrations for other listeners.
- **Wall hanging signs need a side to be placed.** A hanging sign on a
  wall is placed only where one of its two sides holds it (or another sign
  turned the same way), as in vanilla. Once up it stays, even if both
  sides are later broken — vanilla never drops it.
- **Wither roses wither the undead.** Zombies, skeletons and other undead
  standing in a wither rose now get Wither, as in vanilla; only wither
  skeletons and the Wither shrug it off. The other vanilla immunities are
  in too: spiders and nautiluses can't be poisoned, the parched can't be
  weakened, silverfish ignore Infested and slimes ignore Oozing.
- **Copper golem statues take axes and wax.** Honeycomb on a statue now
  waxes it and an axe scrapes a weathered one or takes the wax off, as in
  vanilla, instead of just turning the statue's pose.
- **Team-coloured sidebars.** `/scoreboard objectives setdisplay` now takes
  the sixteen `sidebar.team.<colour>` slots, so players on a team of that
  colour see their own sidebar in place of the shared one (Java edition).
- **Shearing a hive drops the honeycomb.** Shears on a full beehive or nest
  now pop the three honeycomb out of the hive, as vanilla does, instead of
  putting them straight into your inventory. Filling your last glass bottle
  leaves the honey bottle in the same hand. Both make a noise that sculk
  sensors hear, and count as using the item in your statistics.
- **End gateways come in pairs.** The first trip through a gateway in the
  ring now finds the near edge of the outer islands along its bearing,
  hangs a return gateway ten blocks over the island's highest point, and
  lands you on the ground beside it. The two stay linked: going back
  through the far gateway brings you out at the ring gateway you left by,
  not the middle of the main island, and later trips reuse the same pair.
  Where no island can be found, a small one is made, as in vanilla.
- **Nether portals keep time like vanilla.** The wait in a portal is now
  counted every tick instead of once a second, so the portal delay rules
  are exact. Stepping out drains the wait four times as fast as it built
  up, rather than wiping it. Creative players go through at once, as the
  creative delay's vanilla default of 0 asks.
- **Adventure mode can't rewrite signs.** Right-clicking a sign in
  adventure mode no longer opens the editor, or dyes, glows or waxes it.
- **/attribute moves a player's fall damage.** A player's falls now read
  the safe-fall-distance and fall-damage-multiplier attributes, as vanilla
  does, so changing either with /attribute or a plugin takes effect. Jump
  Boost still adds a block of safe fall per level, now through the same
  attribute.
- **Beacons work in the Nether.** A beacon's beam now goes through bedrock
  as it does in vanilla, so a beacon under the Nether's bedrock roof lights
  up and gives its effects; before, the roof switched every Nether beacon
  off. Tinted glass now stops the beam, as in vanilla.
- **The anvil can take a name off.** Clearing the name box on a renamed
  item now removes the custom name for one level, as in vanilla; a box of
  only spaces counts as empty. Names lose the characters chat refuses
  (colour-code signs, control characters), and a name is measured in
  characters rather than bytes, so accented names up to 50 letters go
  through.
- **Right-clicking a mob does it once.** The Java client sends a mob
  interaction for the main hand and, when that one works, again for the
  offhand; the server could not tell the two apart and ran the main-hand
  action twice — two dyes spent on one sheep, a second feed, a second trade
  click. The offhand's copy is now recognised and skipped.
- **/gamerule takes vanilla's ranges.** Every number rule was capped at
  1000. Each now accepts vanilla's range: random tick speed, sleeping
  percentage and the rest go as high as vanilla allows, snow accumulation
  stops at 8, and the fire spread radius takes -1 for "everywhere". Values
  out of range get vanilla's message.
- **Unbreaking saves each point of wear separately.** Unbreaking decided
  whether to spare a whole hit at once. As in vanilla it now rolls for every
  point of wear, so a hit costing two can cost one.
- **Levitation lifts mobs.** A mob hit by a shulker bullet was given
  Levitation but stayed on the ground; it now floats upward as in vanilla,
  and falls when the effect wears off.
- **Splash potions of Harming weaken with distance.** A Harming splash hit
  players at full strength however far from the splash they stood; as in
  vanilla it now falls off with distance, like Healing, and a glancing
  splash can do nothing at all. Undead mobs healed by Harming get vanilla's
  amount.
- **Your offhand works on blocks.** Right-clicking a block with something
  in your offhand used whatever was in your main hand instead, so a torch
  or a stack of blocks held beside a pickaxe could not be placed, and bone
  meal, buckets, flint and steel, hoes, shovels, axes, shears and spawn
  eggs did nothing from the offhand. As in vanilla, the offhand item now
  gets its turn when the main hand has nothing to do, and it is the one
  used up. It also feeds a composter, a campfire, a lectern, a decorated
  pot, a cake or a respawn anchor, but never opens doors or chests on its
  own, and a respawn anchor waits for the offhand's glowstone instead of
  setting your spawn.
- **Frogs swim at a frog's pace.** A frog swimming about with nowhere to
  be moved at its full walking pace; as in vanilla it now swims a little
  slower than it walks.
- **Frogs climb out of the water.** A frog in a pond now makes for the
  nearest bank every few seconds, as vanilla frogs do, instead of drifting
  about in the water.
- **Iron golems walk their villages.** An iron golem with nothing to fight
  stood where it was made. It now strolls about its village at its unhurried
  pace, often toward the villagers and their beds, workstations and bell, and
  one that has wandered out heads back toward the village.
- **Villagers show you their trades.** Stand near a villager holding
  something it trades for, and it turns to you and holds up what it would
  give in return, cycling through when there are several offers, as in
  vanilla.
- **Villagers work at their job sites.** A villager at its workstation in
  working hours now gets on with its job every so often, with its trade's
  work sound. A farmer at its composter bakes bread from its wheat, takes
  out the bone meal when the composter is full, and tips its spare seeds
  in, keeping ten for sowing.
- **Farmers farm again.** The villagers who harvested and replanted
  fields, asked for seeds and handed out spare wheat were the
  cartographers, not the farmers. Farmers with a composter now do their own
  job.
- **Axolotls play dead instead of fleeing.** A struck axolotl ran off in a
  panic, which vanilla axolotls never do. Now its only answer to a hit is
  the chance to play dead under water, and only a hit from something sets
  that off, not fire or a fall.
- **Angry bees calm down.** A bee you hit, or one from a hive you robbed,
  stayed angry for good. As in vanilla it now chases you for 20 to 39
  seconds and then goes back to its flowers, and a bee that has stung
  someone stops chasing at once. An angry bee also stings only the player
  it is after, not whoever happens to be closest.
- **Llamas can be ridden and tamed.** Climb onto a wild llama with an
  empty hand and it throws you off until it settles, as a horse does, only
  sooner; wheat and hay bales bring it round faster. A tamed llama carries
  you but can't be steered, and a tamed trader llama no longer leaves with
  its trader. A tamed horse, donkey or camel can also be mounted without a
  saddle now; as in vanilla, you just can't steer it.
- **Elder guardians stay home.** An elder guardian now keeps to the part
  of its monument where it first appeared, as in vanilla, and swims back
  there if it is lured or carried more than sixteen blocks away.
- **Drowned come up at night.** A drowned deep underwater now swims up
  toward the surface after dark, as in vanilla, which is how it reaches the
  shore to come after you; before, one on the seabed stayed there.
- **Forgiving the dead no longer tames zombified piglins and endermen.**
  With the forgive-dead-players rule on, a zombified piglin or enderman
  that had been after a player who died became a harmless wanderer for
  good. It now just calms down, as in vanilla, and will fight again if
  provoked.
- **Zombified piglins hold a grudge the vanilla way.** Hit one and it stays
  angry for as long as it can see you and 20 to 39 seconds after, instead
  of a fixed time; it no longer turns on other players it was never angry
  at. While it fights it calls the rest of the pack in again every few
  seconds, including latecomers, gives its angry grunt, and moves a little
  faster. Shooting one now rouses the pack too.
- **Silverfish finish their walk before burrowing.** A silverfish could
  vanish into a stone block partway through a stroll; as in vanilla, it now
  only burrows in when it has stopped.
- **Spear zombies charge villagers and golems too.** A zombie, husk,
  zombie villager or zombified piglin carrying a spear used to charge only
  players and just bite anything else. As in vanilla, it now lowers its
  spear and charges whatever it is hunting, and a villager it kills that
  way can rise as a zombie villager.
- **Wardens charge faster and wander slower.** A warden coming for you
  now moves at vanilla's fighting pace, a fifth faster than its walk, and
  when it has nobody to hunt it ambles about at half speed.
- **Wardens go to see what they heard, and mind being bumped.** A warden
  that hears something it is not yet angry about now walks over to where
  the sound came from, as in vanilla, instead of only standing and
  sniffing. Walking into a warden now angers it, a little more each second
  you stay in contact, and draws its attention to where you stand.
- **Ravagers speed up for a charge.** A ravager now eases up to a faster
  pace while it is after something and back down when it gives up, and
  pulls away slowly again after each bite, as in vanilla.
- **Pillager patrols stand their ground.** A patrol that spots you from
  afar now stops and watches you, as in vanilla, instead of opening fire at
  once; it attacks when you come within ten blocks or hit one of them, and
  the pillagers beside it join in. Pillagers now hold their crossbows at the
  ready while fighting, and vindicators raise their axes.
- **Skeletons raise their weapons when they fight.** A skeleton, stray,
  bogged or parched with a target now holds its bow up, a wither skeleton
  raises its sword, and an illusioner draws its bow, as vanilla shows them.
- **Skeletons move at vanilla's pace in a fight.** A wither skeleton, or a
  skeleton holding a sword, now rushes you a fifth faster than it walks. A
  skeleton circling you with its bow drifts at a quarter of its pace, not
  nearly its full speed, and an illusioner walks in at half speed until you
  are in bow range.
- **Raiders celebrate a raid they win.** When a raid is lost because the
  village is gone, its surviving raiders no longer march on; with nobody
  left to fight they cheer, raise their arms and jump about, as in vanilla,
  until the raid is over.
- **Every raid wave has a banner-carrying captain.** As in vanilla, the
  first raider of each wave that can lead wears the ominous banner, and
  everyone who comes into view later sees it too. A pillager captain still
  drops an ominous bottle; a vindicator captain drops only its banner.
- **Beds refuse you the way vanilla does.** A bed someone is already in,
  one more than three blocks away, or one with a solid block over it now
  turns you down with vanilla's message above the hotbar. "Respawn point
  set" appears only when your spawn actually moves, and creative players
  can sleep with monsters about.
- **Falling stalactites shatter.** A stalactite that fell stayed where it
  landed as a block, hanging from nothing. As in vanilla it now breaks into
  pointed dripstone with its crash. Its damage to whatever it lands on is
  also one block's worth lower, to match vanilla.
- **Comparators read a shelf only from behind.** A comparator beside or in
  front of a wooden shelf read its contents; as in vanilla, only one behind
  it does.
- **Berry bushes slow mobs down.** Mobs walking through a sweet berry bush
  are now slowed, as in vanilla (foxes and bees excepted), and are only
  scratched while moving, not while standing still. Sculk now hears berries
  ripening.
- **Melons and pumpkins grow on vanilla's grounds.** A stem put out its
  fruit only onto dirt, plain grass or farmland. It now grows onto any
  ground vanilla allows: podzol, mycelium, mud, moss, rooted and coarse
  dirt, and snowy grass too.
- **Clicking your own respawn anchor again is quiet.** It repeated
  "Respawn point set" and its sound every time; as in vanilla, an anchor
  that is already your respawn point now does nothing.
- **Sculk catalysts stop blooming, and other small block fixes.**
  - A sculk catalyst stayed in full bloom forever after its first mob death;
    it now blooms briefly and settles, as in vanilla.
  - A calibrated sculk sensor now stays active for half a second, not a
    second and a half.
  - Levers click with vanilla's quieter, lower sound.
  - Turtle eggs hatch in 26.3's pre-dawn window.
- **The End poem and credits.** The first time a Java player leaves the
  End through its exit portal, the End poem and credits now roll, as in
  vanilla, and then they return home. After that the portal takes them
  straight home. Bedrock players go straight home for now.
- **The End's arrival platform is obsidian again, and End portals take
  mobs and items.** Since the move to 26.3 the platform you land on in the
  End was built out of piston heads; it is obsidian again. Mobs and dropped
  items that fall into an End portal now travel too, as in vanilla: into
  the End they land on the platform, out of it they arrive at the world
  spawn. The dragon and the wither stay put.
- **Kelp and vines hold on as in vanilla.** Kelp no longer grows on magma
  blocks, and one kind of vine no longer holds up another: kelp, weeping,
  twisting and cave vines each stand only on their own kind or on solid
  ground.
- **Farmland reads water and rain as in vanilla.** Waterlogged blocks near
  farmland now keep it wet, as open water does. Rain no longer soaks
  farmland through a glass or leaf roof, and a greenhouse keeps its soil
  dry unless there is water nearby.
- **Fence gates swing away from you.** A gate opened from its back swung
  towards you; as in vanilla it now turns first, so it always opens away
  from the player.
- **Leaving the game closes what you had open.** Disconnecting with a chest,
  barrel, ender chest or shulker box open left its lid up for good. Leaving
  now closes it, with its close sound, as in vanilla.
- **Composters finish on time, however they got full.** A full composter
  could turn to bone meal at once when a block beside it changed, and one
  that arrived full some other way (moved by a piston, set by a command, or
  left full across a restart) never finished. It now always takes its
  second, then becomes ready, as in vanilla.
- **Frogspawn hatches after a server restart.** Frogspawn laid before a
  restart never hatched. It now hatches within the usual three to ten
  minutes after its area loads again.
- **Flower pots hand the plant back.** Taking a plant out of a pot dropped
  it on the ground; now it goes into your inventory, and is only dropped if
  there is no room, as in vanilla. Clicking a filled pot with another plant
  no longer swaps it out. Sculk hears plants going in and out of pots, but
  not a potted eyeblossom opening or closing, as in vanilla.
- **Lava, fire, campfires and cactus hurt as fast as in vanilla.** These
  hazards hit once a second, half vanilla's rate. They now hit twice a
  second at vanilla's damage per hit, for players and mobs alike. Lava
  cauldrons had been doing double damage to players; they now match lava.
- **Zombie horses follow red mushrooms.** A zombie horse followed players
  holding golden carrots and apples like a living horse. As in vanilla, it
  now follows a player holding a red mushroom instead.
- **Vexes attack what their evoker is fighting.** Vexes only ever went
  after players, so the vexes an evoker summoned against villagers or an
  iron golem ignored them. As in vanilla, they now go for their evoker's
  target, and only pick a player on their own if they can see them.
- **Shulkers need to see you.** A shulker used to open fire on a player
  behind a wall. As in vanilla, it now only picks a target it can see, and
  loses interest in one that stays out of sight for a few seconds.
- **Zoglins wander slowly and giants stand still.** An idle zoglin now
  ambles at vanilla's slower pace instead of charging about, and a giant no
  longer turns its head to watch players, since it has no behaviour at all.
- **Piglin brutes stand their ground.** Brutes backed away from soul fire
  and zombified piglins like ordinary piglins. As in vanilla, they no
  longer do.
- **Breeze wind charges fly at vanilla speed.** A breeze's wind charges
  flew twice as fast as they should. They now fly at vanilla speed from the
  breeze's middle, aimed a little lower on you, with vanilla's spread.
- **Witches throw their potions properly.** A witch's potions flew twice
  as fast as they should, so they sailed over players and were hard to
  dodge. They now fly at vanilla speed in a proper arc, a little slower up
  close, and are aimed at where you are going.
- **Slimes and magma cubes look for you at their own level.** As in
  vanilla, a slime or magma cube now only notices a player within four
  blocks above or below it, so one at the bottom of a cliff no longer
  locks on to you at the top. Once it is after you, it keeps chasing.
- **The creaking wakes when you look at it.** A creaking used to hunt
  anyone nearby like any other monster. As in vanilla, it now stays idle and
  ambles slowly until a player within 12 blocks looks at it; then it goes
  after that player, freezing whenever someone watches it, and goes back to
  sleep when nobody is left nearby. A creaking made with a spawn egg or a
  command no longer vanishes at once for having no heart.
- **Drowned throw tridents at villagers too.** A drowned holding a trident
  only ever threw it at players, so one chasing a villager, an iron golem
  or an axolotl never attacked it. It now throws at whatever it is
  hunting, with vanilla's aim spread, sound and damage (8, not 9).
- **Bogged and the wither shoot at vanilla speed.** A bogged now draws its
  bow as slowly as in vanilla (every three and a half seconds, two and a
  half on Hard) instead of at a skeleton's pace. The wither's middle head
  fires a skull every two seconds at targets within 20 blocks; it used to
  fire twice as often and from twice as far.
- **Water hurts endermen, blazes, striders and snow golems.** These mobs
  used to be unharmed by water and rain, and an enderman only fled from
  rain now and then. As in vanilla, water and rain now hurt them, and an
  enderman that gets wet teleports away almost at once.
- **Creepers unwind slowly.** Stepping away from a hissing creeper used to
  reset its fuse at once, so you could step back in for a fresh second and
  a half every time. As in vanilla, the fuse now winds back down gradually,
  and a creeper you return to picks up where it left off. The range counts
  height too, so a creeper right under you hisses. A creeper lit with flint
  and steel now always explodes, even with nobody nearby.
- **Monsters placed by the world now fight.** A warden called by a
  shrieker, the guardians of an ocean monument, the drowned in an ocean
  ruin, a creaking from its heart, silverfish bursting from infested stone
  and endermites from ender pearls all used to wander about harmlessly
  until the server restarted. They now attack from the moment they appear.
- **The warden fights like the real one.** A warden now attacks only
  someone it has grown angry at, so you can sneak past one that has only
  smelled you. Up close it hits hard every second or so; its sonic boom is
  for targets it cannot reach, comes ten seconds after its roar, and
  charges up with a warning sound before the blast. Hitting a warden makes
  it come for you straight away, without the roar.
- **Birthday Song counts in every dimension.** An allay dropping a cake on
  a note block earned the advancement only in the Overworld; it now counts
  in the Nether and the End as well.
- **More things count as crafted in the statistics.** Taking smelted
  items from a furnace, a result from a smithing table, and what a
  villager trades you now add to that item's "Times Crafted", as in
  vanilla.
- **Brushing suspicious gravel sounds like gravel.** It made the sand
  sound. The player brushing no longer hears the sound twice.
- **Riding statistics count the right mount.** Riding a nautilus counted
  as horse riding, and flying a happy ghast counted nothing; each now has
  its own statistic, as in vanilla, for everyone aboard. Riding distances
  include climbing and falling, not only ground covered.
- **The crafter makes maps and book copies, and hands back buckets.** A
  crafter now extends and clones maps and copies written books, as in
  vanilla. It also gives back what a recipe leaves over: a cake returns its
  three empty buckets, a honey recipe its glass bottles, a book copy the
  original book and a banner copy the patterned banner. What a crafter
  drops on the ground keeps everything it carries, so a firework rocket
  with stars or a copied map comes out whole.
- **Spawn eggs make babies and work on water.** Using a spawn egg on an
  animal of the same kind now gives a baby of it, as in vanilla (a lamb
  takes its parent's colour). Using a spawn egg while looking at water or
  lava puts the mob straight into it, so a squid or fish egg works on the
  sea. A spawn egg no longer makes the sound of a thrown egg.
- **Boats face the way you place them.** A boat put down on water always
  pointed south; as in vanilla it now faces the way you are looking, and a
  boat from a dispenser faces the way the dispenser shoots.
- **Strays, bogged and parched drop real tipped arrows.** The tipped
  arrows these skeletons can drop when a player kills them came out with
  no effect. They are now arrows of Slowness (stray), Poison (bogged) and
  Weakness (parched), as in vanilla, and `/loot` gives the same.
- **Charged creepers give one head, babies included.** A charged
  creeper's blast gave a head for every mob it killed; as in vanilla it
  now gives one head per creeper. A baby zombie caught in the blast drops
  its head like an adult.
- **Piglins barter dried ghasts.** 26.3 added the dried ghast to what a
  piglin can give for a gold ingot (about one barter in 47); the table
  had not caught up, so bartering never paid one out.
- **The offhand works for more items.** Ender pearls, snowballs, eggs,
  bottles o' enchanting, splash and lingering potions, eyes of ender, wind
  charges, firework rockets, goat horns and spyglasses can now be used
  from the offhand, and a bow, crossbow or trident held there draws,
  shoots and wears in that hand. Throwing from one hand now takes from
  that hand rather than the first matching stack in the inventory, an
  arrow held in the main hand is shot before the ones in the inventory,
  and an empty bucket or glass bottle held in the offhand fills in place.
- **Copper golems sort chests nearby, not across the whole area.** A copper
  golem now looks for chests within 32 blocks sideways and 8 up or down,
  as in vanilla. It used to look twice as far.
- **Bigger shoals of cod and tropical fish.** A shoal of cod or tropical
  fish now holds up to eight fish, as in vanilla. A salmon shoal still holds
  five, and a shoal leader now counts as one of them.
- **Fox kits follow their parents.** As in vanilla, a baby fox now trots
  after the nearest grown fox. A baby happy ghast now drifts right up to
  three blocks from an adult before it stops.
- **Angry animals calm down.** A wolf, polar bear, panda, dolphin or llama
  you hit used to stay hostile for good, going after any player it saw.
  As in vanilla, it now goes only after the player who hit it. A wolf or
  polar bear stays angry at that player for 20 to 39 seconds after losing
  sight of them, and a pack of wolves all go after the same attacker. A
  llama spits at its attacker once and then goes back to its business.
- **Polar bears hunt foxes.** As in vanilla, an adult polar bear now goes
  after a fox it can see, rears up and bites it. Cubs leave foxes alone.
- **Panicked animals calm down on time.** A cow, pig or chicken you hit
  used to run around for four seconds whatever happened. As in vanilla, it
  now panics for two seconds after the last hit and then finishes the dash
  it is on. Goats, camels, frogs, sniffers and the other animals that
  panic "by brain" run for five to six seconds, and hitting them again
  does not make it longer. An animal backing away from a wolf or a monster
  panics at once when hit, and animals that never panic in vanilla no
  longer panic when bitten.
- **Villagers panic like vanilla villagers.** A frightened villager used to
  run in a straight line from any zombie or illager it could sense, even
  through walls, and a villager you hit ran around like a startled
  chicken. Now a villager only takes fright at threats it can see. It runs
  to a spot away from a threat once it comes within six blocks, and
  otherwise scurries about nearby. A villager you hit runs from you, and
  calms down soon after you back off.
- **Wandering traders keep away from zombies and illagers.** As in vanilla,
  a wandering trader now walks away from zombies (husks, drowned and zombie
  villagers too), pillagers, vindicators, evokers, illusioners, vexes and
  zoglins that come near. A worried panda backs away from a monster that
  gets within four blocks.
- **Fed animals walk to their mate.** Two animals you fed used to wander
  around at a trot until they happened to meet. As in vanilla, each one
  now walks over to the nearest fed animal of its kind; cats, ocelots and
  rabbits walk there a little slower, and axolotls slowly. A fed animal with
  no partner nearby just idles and wanders as usual. Cats and ocelots also
  wander at their own slower pace, as do tadpoles and axolotls.
- **Horses eat, follow and rear as in vanilla.** Horses, donkeys and mules
  now eat carrots. Only a tamed horse falls in love on golden food; a wild
  one takes it to calm down. A wild horse holding out for its rider rears
  at a saddle, a chest or anything else that is not food, instead of
  letting you on. Zombie horses eat and follow red mushrooms, not golden
  carrots, and can be tamed by riding them like other horses. A skeleton
  horse that did not come from a trap ignores you, follows no food and
  sinks in water.
- **Squid and bats no longer panic.** A squid you hit jets away from you
  and then calms down, and a bat you hit keeps flying as it was; as in
  vanilla, neither runs around in a panic afterwards.
- **Piglins hold a grudge and call their friends.** A piglin you hit stayed
  angry for ten seconds; as in vanilla it now stays angry for thirty, and
  the adult piglins around it join the fight. A baby piglin you hit runs
  away and sends the adults after you.
- **Arrows stay stuck for a minute.** Arrows and tridents stuck in the
  ground vanished after ten seconds; as in vanilla they now stay for a
  minute, so you have time to pick them up. Arrows and thrown items in
  flight no longer run out of time before they land.

## 2026-09-24

### Added
- **/loot.** Operators can roll a loot table and send the result to
  players, into a container, onto the ground, or into chosen slots. The
  source can be a named table (the chest, barrel, dispenser, archaeology,
  equipment and gameplay tables), a mob's drops as if the operator had
  killed it (their Looting counts), or a block's drops as if mined, with an
  optional tool (Silk Touch and Fortune count). Containers only take what
  their slots accept. Fishing loot, and mobs as targets, are not supported
  yet.
- **/item.** Operators can put items straight into a chest, barrel,
  shulker box, furnace, hopper, dispenser, dropper, brewing stand or
  crafter, or into a player's hotbar, inventory, hands, armour, ender chest,
  cursor or crafting grid. An item can be named with a count, or copied from
  another container's or player's slots. replace fills the slots in order,
  fill repeats the items across every slot, and override also empties the
  slots left over, as in 26.3. Changing items with loot modifiers, using
  item components, and targeting mobs are not supported yet.
- **/version and /stop.** /version shows operators which game version the
  server speaks, laid out as vanilla lays it out. /stop saves everything and
  shuts the server down cleanly, the same way as stopping it from outside.
  On the cluster the server then starts again by itself, so there /stop
  works as a clean restart. Everyone online is disconnected while it
  happens.
- **/save-all, /save-off and /save-on.** Operators can save the world on
  demand and pause the automatic saves, with vanilla's messages. While
  saving is off, the blocks, containers and mobs stop being written, but
  player data is still saved, as in vanilla. /save-all saves everything
  even then, and the server still saves when it shuts down. Saving comes
  back on after a restart.
- **/bossbar.** Operators can make their own boss bars and show them to
  chosen players: add, remove, list, get, and set the name, colour, style,
  value, maximum, visibility and players, with vanilla's messages. The bars
  are saved with the world, so they survive a restart, and a player who
  was on a bar sees it again when they rejoin. A bar's name can be plain
  text or a styled text component with named colours, bold, italic and the
  like. Components that need translating, scores or hex colours are refused
  for now, not shown wrongly.
- **/clone.** Operators can copy a box of blocks somewhere else, up to
  32,768 blocks at a time, as vanilla does. Chests, barrels, shulker boxes,
  furnaces, hoppers, dispensers, brewing stands and the other containers
  keep what is inside them, and signs, banners, lecterns, jukeboxes,
  shelves, decorated pots and campfires keep theirs too. It supports
  replace, masked (skip air) and filtered (only one block, or a block tag),
  and normal, force (allow the copy to overlap its source) and move (empty
  the source without dropping anything). `strict` stops attached blocks
  from popping off afterwards, and `from`/`to` copy between dimensions.
- **Online mode for Java players.** The Java gateway
  can now log players in the way vanilla does with online mode on. The
  connection is encrypted, and Mojang's session service confirms the
  player owns the account. The player then joins as that account's real
  UUID, with the name spelled as the account spells it and their skin
  attached. A spoofed name gets "Failed to verify username!". When Mojang
  is unreachable, everyone is refused, as vanilla does. Other players see
  the skin too. It is set with `TACHYNE_ONLINE_MODE=on`, and it is on for
  the cluster.
- **Player data is keyed by account, not by name.** Inventories, game
  modes, advancements, stats, recipe books and spawn points are now saved
  under each player's UUID, as vanilla saves them. A player who renames
  their Minecraft account keeps everything. What was saved under a name
  moves to the UUID that player next joins with, Java and Bedrock alike.
  Each original file is kept as `<file>.pre-uuid`. Commands that name an
  offline player still find them, through a name cache (`usercache.json`,
  as vanilla keeps one). For the move to online mode, a `uuidmap.json`
  beside the world moves each listed player's data, and the pets they own,
  to their real account.
- **Bedrock players have a stable identity.** A Bedrock player's UUID now
  comes from their Xbox account, the way Floodgate builds it. Their name
  shows to Java players with a `.` in front (spaces become `_`), so it can
  never clash with a Java player's name. Their saved data and their pets
  follow them to the new identity on their next join. A Java player who
  already joined with the same bare name keeps their own data.
- **Operators from tachyne-access.** A player granted the `op` role in
  tachyne-access is now an operator in game, along with the `-ops` list.
  Once online mode is on, that role is tied to the player's own account.
- **Parrots ride on your shoulder.** A tamed parrot that is not sitting and
  has been around for five seconds lands on its owner's shoulder when it
  touches them. The left shoulder fills first, then the right. It chatters
  there and copies the calls of nearby mobs. It stays on through a relog or
  a portal, and every client sees it. It hops off, owned and named as
  before, when you jump or fall, go into water or powder snow, sleep, get
  hurt, die, switch to spectator or use riptide.
- **`/effect` works as in 26.3.** `/effect give` takes `infinite` in place of
  seconds and a hideParticles flag. An infinite effect never runs down,
  survives a relog, and still heals or hurts on schedule. It comes back when
  a stronger potion wears off. Entity selectors reach mobs. `/effect clear`
  can remove one named effect. The feedback now matches vanilla, including
  "Unable to apply this effect" when a stronger effect is already running
  or the target is immune.
- **Happy ghasts follow you.** A happy ghast drifts after anyone holding a
  snowball or a harness, rising or sinking to their height and stopping
  three blocks off. A ghastling keeps near the nearest player. Both panic
  when hurt, as vanilla's do.
- **Thirteen more commands.**
  - `/advancement` grants and revokes.
  - `/attribute` reads and sets attribute values, bases and modifiers.
  - `/recipe` gives and takes recipe-book entries.
  - `/tag` adds entity tags, and selectors now take `tag=`.
  - `/ride` mounts and dismounts.
  - `/damage` deals damage by type.
  - `/spreadplayers` scatters players.
  - `/forceload` keeps chunks loaded and ticking.
  - `/setworldspawn` and `/defaultgamemode` persist across restarts.
  - `/random`, `/swing` and `/teammsg` (`/tm`) complete the batch.
  - A player's game mode is now recorded on their first join, so changing
    the default only affects new players, as in vanilla.
- **Potent sulfur and geysers (26.3).** Potent sulfur under water bubbles
  and makes swimmers and mobs near it nauseous. Over a magma block it
  becomes a geyser that sleeps and erupts on vanilla's timing; over lava it
  erupts without stopping. Each eruption launches mobs, dropped items and
  primed TNT up the column, taller for deeper water, with the eruption
  sounds and animation. The sulfur caves' pools are gas vents; a geyser is
  something you build.
- **`/setblock`, `/fill`, `/enchant`, `/seed` and `/me`.** Operators can set a single
  block or fill a box (up to 32,768 blocks), naming the block as vanilla
  does, e.g. `oak_stairs[facing=north]`. Both take vanilla's modes: destroy
  (drops what was there), keep (only fills air), hollow, outline, and
  replace with an optional filter. `/seed` shows the world seed and `/me`
  sends an emote. `/enchant` adds an enchantment to the item a player is
  holding, if the item supports it and nothing on it conflicts.
- **Llama caravans.** Put a lead on a llama and the free llamas within nine
  blocks fall in behind it, each two blocks behind the one ahead, as in
  vanilla. A llama that falls more than 26 blocks behind speeds up, and
  leaves the line if it still can't catch up. The caravan breaks up when
  nobody at the front is on a lead any more. A wandering trader's llamas
  now spit at whoever hurts the trader, and any llama spits at a wild wolf
  within ten blocks. Camels stroll at their vanilla
  pace.
- **The camel husk (26.3).** One naturally spawned husk in ten, given room
  for a camel, rides out on a camel husk:
  - the husk sits in front and drives, with an iron spear, and its spear
    charges run at four times the pace;
  - a parched rides in the back seat and shoots; if the husk dies, it moves
    up and takes the reins.

  The camel husk otherwise behaves like a camel: it sits, dashes, carries a
  saddled player and makes its own camel-husk sounds. It heals on rabbit's
  feet but never breeds. It cannot be led on a lead and never panics while
  a mob is riding it. Like a monster, it despawns unless a player has
  interacted with it. Other changes that came with it:
  - a mob riding another mob moves at its mount's pace, as in vanilla, so a
    chicken jockey is as quick as its chicken;
  - zombie horses and zombie nautiluses count as monsters for spawning, as
    in 26.3;
  - pigs, striders and camels play their own saddle sounds.
- **The sulfur cube.** 26.3's block-swallowing cube is in the game. Empty,
  it hops about like a slime and never attacks: size 2 grown, size 1 as a
  baby (a slime ball helps it grow), 4 health per size, and it dies into
  two babies. Hand it a block — or throw one near it — and it swallows it
  and turns into a ball. The block's archetype decides how it bounces,
  slides, drags and floats and how far a hit or a push sends it, and blows
  knock it about instead of hurting it. A swallowed TNT lights from flint
  and steel, a fire charge, fire, a burning arrow or a redstone signal
  (short from a blast) and goes off 6 seconds later with a power-3
  explosion. A swallowed magma block burns whatever touches it. Shears pop
  the block back out, and an empty bucket scoops the cube, block and all.
  Giving one TNT, or letting it take TNT you threw, earns Uh Oh. It spawns
  in the sulfur caves' pool, but that biome is not generated yet, so for
  now it comes from spawn eggs, `/summon` and buckets. 26.2 clients see it
  too (26.2 already knew the mob), and Bedrock shows its block.
- **Boats seat two, and pick up mobs.** An empty boat takes aboard a
  villager, goat or other mob that bumps into it, the vanilla way to move
  villagers about. You can climb in beside it. Mobs as wide as a boat
  (horses, iron golems, spiders…) and fish, squid and the like stay out,
  and a boat you are steering picks up nothing. Whoever boarded first sits
  in front and steers. Riding a boat with a goat earns Whatever Floats Your
  Goat!
- **Others see you swing and mine.** Other players now see your arm swing,
  with either hand: at air, at a block you're mining or placing against,
  at a mob. Before this, it moved only on a hit against a player. The
  cracks spreading across a block now show to everyone watching, stage by
  stage at vanilla's pace, and clear when you stop. The pace takes in the
  tool and which blocks it is made for, Efficiency, Haste, Mining Fatigue,
  being underwater without Aqua Affinity and being off the ground.
- **Spears work.** Before this, a spear hit like a bare fist.
  - **The jab.** A left-click hits everything on a short line in front of
    you, from 2 to 4.5 blocks out (6.5 in creative). It does the spear's
    attack damage: 1 for wood and gold, 2 for stone and copper, 3 for iron,
    4 for diamond and 5 for netherite, plus Sharpness and similar. It needs
    a full charge, and it knocks back what it hits.
  - **The charge.** Hold use to lower the spear. After a short delay
    (0.4 to 0.75 s, depending on the material), whatever you run into is hit
    for 1 plus your closing speed in blocks per second times the spear's
    multiplier (0.7 to 1.2). Moving fast enough also knocks the target back,
    and faster still pulls a rider off its mount. The time you can hold each
    of these effects for runs out one after another. A charge on horseback
    is what the spear is built for, and striking five mobs in one charge
    earns the advancement for it.
  - **Mobs use them too.** Zombies, husks and zombie villagers can spawn
    with an iron spear (one armed zombie in six). Zombified piglins carry a
    golden sword, or one time in twenty a golden spear. A mob with a spear
    closes in, lowers it and charges, then wheels off and comes round again.
  - **Lunge** can be enchanted onto spears, from the table, books, trades
    and loot. Each level jolts you forward 0.46 blocks on a jab, for four
    points of exhaustion and a point of durability, when you have the
    hunger to spare and aren't riding, gliding or swimming.
  - **Still to come.** Other clients don't see the stab animation yet.
- **Strongholds are whole strongholds.** The portal room no longer sits
  alone underground. It is at the far end of vanilla's maze: a spiral
  staircase down, then corridors behind wooden doors, iron doors and
  grates, prison cells, turns, fountain and pillar rooms, galleries,
  staircases, five-way crossings and libraries. The walls are weathered
  stone bricks, some cracked, mossy or infested with silverfish. Chest
  corridors, galleries and libraries hold chests with the vanilla
  stronghold loot. The portal room has its lava pools, its barred windows
  and a silverfish spawner on the stairs up to the frames.
- **Mineshafts are vanilla's mineshafts.** Each one starts from a domed
  room and spreads out as corridors, one- and two-storey crossings and
  staircases, the way vanilla lays them out. Corridors have timber supports,
  torches, cobwebs and rail lines. Plank bridges cross ravines, with log
  pillars or chains holding them up. Cave spider nests have a spawner
  buried in webs, and chest minecarts carry the abandoned-mineshaft loot. A
  mineshaft can now start in any chunk, as often as in vanilla. In the
  badlands they are built from dark oak and sit higher up, often breaking
  out into the canyons. `/locate structure mineshaft_mesa` finds them.
- **Sulfur caves generate.** 26.3's new cave biome now appears where
  vanilla puts it: 26 to 115 blocks under flat land (the highest erosion)
  between the coast and inland, where the weirdness runs furthest
  negative. It is rare, about one column in 170. Its rock is banded with
  sulfur and cinnabar, and on it grow sulfur spike clusters and single
  spikes, pools of water rimmed in sulfur with wet potent sulfur on their
  beds, and now and then a rooted sulfur spring: one of vanilla's ten
  spring templates, stamped on the first flat, open ground above the
  cave, with tuff scattered around it and sulfur roots running back down.
  The world reports the biome underground, so the natural spawner uses
  its pool (sulfur cubes will spawn there once the mob exists), and its
  Adventuring Time criterion can now be met. Chunks over the new biome
  regenerate with it underground; what players have built or dug there
  stays.

### Fixed
- **Redstone-driven doors, trapdoors and gates sound and vibrate as in
  vanilla.** Opened or shut by a redstone signal they played too quietly,
  at one fixed pitch, and sculk sensors never heard them. They now play at
  full volume with vanilla's slight pitch variation, and sensors pick them
  up.
- **The dragon egg lands on something when it teleports.** Clicking the
  dragon egg could send it to a spot in mid-air. As in vanilla, it now only
  moves to a free spot with a block beneath it.
- **Ice melts at vanilla light levels, and stays water in the End.** Ice
  needed one more level of block light than vanilla to melt. Ice melted in
  the End vanished as if it were the Nether; now it leaves water, and only
  Nether ice evaporates.
- **The newest music discs give their vanilla comparator signal.** A
  comparator reading a jukebox playing Bounce, Lava Chicken or Tears gave
  the wrong strength; they now give 8, 9 and 10.
- **Comparators read a double chest as one chest.** A comparator next to
  a large chest read only the half it touched, and still read a chest whose
  lid was blocked. As in vanilla it now reads both halves together, and
  reads nothing while a solid block or a sitting cat keeps either half shut.
- **Cactus flowers.** A cactus now grows a cactus flower on top now and
  then, more often on a full three-tall column, as in vanilla. The flower
  stays put on the cactus, where it used to pop off because it looked for
  soil beneath it.
- **Cactus and campfires hurt mobs, and cactus destroys items.** Only
  players took damage from cactus and lit campfires. Now mobs pressed
  against a cactus or standing in a lit campfire are hurt too (fire-immune
  mobs walk over campfires unharmed), and a dropped item that lands on or
  against a cactus is destroyed, as in vanilla.
- **Containers sound once, however many people open them.** Each extra
  player opening a chest, barrel or ender chest replayed its open sound, and
  each one leaving played the close. As in vanilla, the sound (and the
  vibration sculk hears) now comes only when the first player opens it and
  the last one closes it. Barrels no longer play their close sound twice,
  and furnaces, dispensers and droppers no longer set off sculk sensors when
  opened, since they don't in vanilla.
- **Cake is quiet to sculk when you are full.** Trying to eat cake while
  full, or putting a candle on it, set off sculk sensors as if you had
  eaten. Now only a bite actually taken is heard, and eating the last slice
  also counts as the block being destroyed, as in vanilla.
- **Sculk hears more of what changes around it.** Ringing a bell, a big
  dripleaf tipping or springing back, laying food on a campfire (and the
  food finishing), filling or knocking a decorated pot, and adding or taking
  a book from a chiseled bookshelf are all block changes a sculk sensor now
  picks up, as in vanilla. A comparator reading a decorated pot now updates
  as the pot fills.
- **Repeater, comparator and daylight sensor clicks behave as in
  vanilla.** A comparator clicks lower going to subtract mode and lower
  still going back. A repeater changes its delay silently. An inverted
  daylight sensor updates its signal at once. Players in adventure mode can
  no longer change any of the three.
- **Food goes on an unlit campfire.** As in vanilla, you can lay food on a
  campfire that is out; it waits there and cooks once the fire is lit.
- **Carpets and candles no longer kill cactus.** A cactus broke when any
  block touched its side. As in vanilla, only a solid block does, so a
  carpet, candle, pot or lantern beside a cactus leaves it standing.
- **Soul fire burns harder than fire.** Standing in soul fire now does
  twice the damage of ordinary fire, as in vanilla.
- **Cake is as filling as in vanilla.** A slice of cake, or the first
  slice of a candle cake, gave a quarter of vanilla's saturation.
- **Weighted pressure plates release at vanilla speed.** Light and heavy
  weighted plates held their signal for a second after the last thing left,
  like the other plates; in vanilla they let go after half a second.
- **Beetroot grows at vanilla speed.** It grew as fast as wheat; in
  vanilla it grows at two thirds of that rate.
- **Opening a shulker box trips observers and lifts what is on it.** As in
  vanilla, a shulker box's lid now updates the blocks around it when it
  starts and stops moving, so an observer watching the box pulses when
  someone opens or closes it. The rising lid also pushes mobs and dropped
  items sitting on it out of the way.
- **Piglins only attack the player they are after.** A piglin chasing
  someone could hit a player in gold armour who was standing next to it.
  Now it only swings at its own target. If you anger piglins, by hitting
  one or opening a chest they guard, they come for you even in gold
  armour. Before, they forgot about you as soon as they saw the gold.
  When their anger runs out, they go back to leaving gold-wearers alone.
- **The Scale attribute changes an entity's size in the world.** A mob
  or player resized with /attribute already looked bigger or smaller,
  but the server still treated them as normal size. Now a scaled mob
  takes up its new size: blocks can't be placed inside it, and it is
  reached, pushed and hit at its new size. A scaled player's eyes and
  body move with them, so reach, explosions, wind charges and block
  placement use their real size. A shrunk player can walk under a
  one-block gap without being pulled back. Shulkers stop growing at three
  times their size, happy ghasts never grow bigger, and the ender dragon
  keeps its size, as in vanilla.
- **The Gravity attribute works.** Changing an entity's gravity with
  /attribute used to do nothing. Now a mob with no gravity stays in the
  air when the ground under it is dug out. The arcs of leaping wolves,
  jumping goats and breezes, and mobs thrown up by a geyser, rise and fall
  by their own gravity. A player whose gravity is lowered is no longer
  pulled to the ground for floating. Slow Falling now slows those mob arcs
  on the way down, and a mob under Slow Falling or Levitation takes no
  fall damage, as in vanilla.
- **The Flying Speed attribute works.** Bees, parrots, allays, ghasts,
  happy ghasts and the Wither now fly faster or slower when their flying
  speed is changed with /attribute. Reading it with /attribute now shows
  each species' own speed, not a generic one. At their normal speed they
  fly as before.
- **Zombie reinforcements follow vanilla's rules.** A zombie's chance to
  call for backup on Hard is now its Zombie Reinforcements attribute, so
  /attribute changes it. Zombie leaders now appear only as often as the
  local difficulty allows. They also get their extra health and can break
  doors. A zombie called in as backup rolls its own chance instead of
  copying its caller's. A zombie that keeps calling for backup becomes
  less likely to do it again with each zombie it brings.
- **The Block Break Speed attribute works.** Setting a player's block
  break speed with /attribute did nothing. It now multiplies how fast they
  mine, as in vanilla, and the cracks other players see keep pace.
- **Thrown tridents are no longer lost when they hit something.** A
  trident without Loyalty vanished when it hit a mob, a player or a
  vehicle, and one lying in the ground vanished after ten seconds. Now,
  as in vanilla, it bounces off what it hit and drops, and it stays there
  until you pick it up.
- **Flint and steel wears out lighting TNT.** Lighting TNT with flint
  and steel didn't use up any durability. It now uses one point, as
  lighting anything else does.
- **Breaking the dragon's healing crystal hurts the dragon.** The ender
  dragon healed as long as any end crystal was left anywhere. Now, as in
  vanilla, it heals only from the nearest crystal within reach.
  Destroying that crystal while it is healing costs the dragon 10 health.
- **Kills count for the player who started the fight.** A mob you hurt
  that died moments later from a fall, fire, lava or another mob gave you
  nothing. Now, as in vanilla, a mob that dies within five seconds of
  your last hit is your kill. It drops experience and player-only loot,
  and it counts for your kill statistics and advancements. A tamed
  wolf's bite counts for its owner. Mobs killed only by a dispenser's
  arrows no longer drop as player kills. Killing another player with TNT,
  an arrow or Thorns now counts as a player kill, as punching them to
  death already did.
- **Crossbow shots scatter like vanilla's.** Crossbow bolts flew in a
  perfectly straight line. Now they have the small random spread vanilla
  gives them. Multishot's side bolts stay 10 degrees either side of your
  aim even when you shoot steeply up or down, and each bolt plays its own
  shot sound at vanilla's pitch.
- **Dispensed projectiles hit mobs.** Snowballs, eggs and fire charges
  fired from a dispenser flew straight through mobs. They now hit them as
  in vanilla: a snowball stings a blaze, and a fire charge sets a zombie
  alight.
- **Bottles o' enchanting splash, and potions break with one sound.** A
  thrown bottle o' enchanting broke with no splash at all. It now bursts
  in green, as in vanilla. Splash and lingering potions sent their
  breaking sound twice in different ways. Now the sound is sent once, the
  way current clients expect.
- **Fireballs, wither skulls and shulker bullets no longer vanish
  after ten seconds.** Every projectile disappeared ten seconds after it
  was fired. Now, as in vanilla, a fireball, wither skull or wind charge
  flies until it hits something or leaves the loaded world, even if
  whoever fired it has died. A wind charge that climbs far above the
  build limit bursts up there. A shulker bullet keeps homing until it
  hits, and disappears when the difficulty is set to peaceful.
- **A breeze's wind charge is its own kind of projectile.** A breeze
  shot an ordinary wind charge. It now shoots the breeze wind charge, as
  in vanilla, with its own burst sound. A player can still bat it back,
  its burst is still the wider one, and it still leaves other breezes
  alone. Because it is not a player's wind charge, it now also hurts a
  wither that is below half health.
- **Endermen teleport the way they do in vanilla.** Every enderman
  teleport landed on the top of the ground, so endermen in the sun
  teleported from one sunny spot to another and gathered in the open. Cave
  endermen were pulled up to the surface, and some landed half inside
  blocks. Now an enderman lands within 32 blocks above or below where it
  was, on solid ground, with room for its whole body. In sunlight it can
  land in shade or a cave and wander off from there. Hitting one no longer
  makes it teleport away, as in vanilla; arrows still never land.
- **You can't place a block inside a mob.** A block could go where a mob,
  another player, a boat or a minecart was standing, which left the mob
  standing inside the block. As in vanilla, a block that would overlap
  anything standing there, including yourself, is not placed.
- **A lightning rod moved by a piston still switches off.** A rod pushed
  while a strike was still powering it stayed powered for good. It now
  switches off eight ticks after it lands, as in vanilla.
- **Water and lava wash away exactly what they do in vanilla.** Flowing
  water and lava used to break any block without a solid shape. They now
  follow vanilla's list: pressure plates and banners hold water back;
  buttons, levers, chorus plants, end rods and snow of any depth are washed
  away. Waterlogged blocks, kelp and seagrass take the water in and stay.
- **Pistons no longer lose blocks next to water.** While a piston slides a
  block, the cell it is moving through is solid in vanilla, and water
  cannot flow into it. Ours let water flow in and replace it, so a block
  pushed or pulled beside flowing water vanished and left water behind.
- **Kills you cause indirectly now count as yours.** A mob killed by TNT
  you lit (with flint and steel, a fire charge, a flaming arrow, or in a
  chain from your own blast), by a TNT minecart you set off, by an end
  crystal you broke, by your Thorns armour, or by the blast of a ghast
  fireball you knocked back now counts toward your kill statistics,
  scoreboards and advancements such as Monster Hunter. The blast also names
  you in death messages. A ghast's own fireball knocked back into it now
  kills it outright, as in vanilla, which is what Return to Sender needs.
  A fire charge used on TNT lights it, and an end crystal now goes off
  with a real explosion.
- **A vehicle that was just hit looks hit to players who arrive.** Someone
  joining or coming into a dimension while a boat or minecart was still
  rocking from a blow, or a boat was on fire, saw it sitting still and
  unharmed. They now see the same wobble and flames as everyone else.
- **TNT minecarts go off the way they should.** A flaming arrow now blows
  one up on the spot, and fire, lava or any explosion that reaches it lights
  its fuse instead of knocking it back into an item, however little the
  blast hurt it. A creeper's blast leaves it alone when mob griefing is off,
  and with TNT explosions switched off a cart no longer lights at all.
- **Boats and minecarts take damage from more than fists.** Arrows and
  other projectiles, explosions, lava and fire now hurt them the way they do
  in vanilla: the vehicle rocks, and enough damage breaks it into its item.
  Lava breaks one almost at once, fire burns a boat down in moments and
  leaves it smouldering, and a snowball only rocks it. A creeper's blast
  spares vehicles when mob griefing is off, and a rider's own arrow never
  hits the vehicle they are sitting in.
- **Piglins fight with their golden spears.** The one piglin in ten that
  carries a golden spear now uses it as vanilla does. It closes in, lowers
  the spear and charges, then wheels away for another pass. It used to swing
  the spear like a sword. It still leaves anyone wearing gold alone.
- **The `send_command_feedback` and `log_admin_commands` gamerules work.**
  With `send_command_feedback` off, a command no longer tells you it worked.
  Errors still show. While it is on, the other operators online see what an
  operator changed, as a gray italic "[Name: message]" line. With
  `log_admin_commands` on, the server log records the same line. Questions
  such as `/time query` and `/locate` answer only whoever asked.
- **Sculk, bees, villager doors and creaking hearts work in the Nether and
  the End.** A sculk sensor, shrieker or catalyst built there now hears and
  answers that dimension's own vibrations, and a shrieker's Warden rises
  beside it. It no longer reacts to what happens at the same spot in the
  overworld. Bees live in their own world's hives, which keep their bees
  across a restart. They come and go at any hour where there is no night.
  Villagers open and shut the doors of the world they are in. A creaking
  heart a player builds now ticks wherever it stands, including one laid
  on its side. In the Nether and the End it sleeps, as in vanilla.
- **Projectiles break with their own effects.** Snowballs and eggs now
  break into bits of themselves, where every projectile used to show the
  same generic puff of smoke. Fireballs, wither skulls, ender pearls and
  llama spit no longer puff at all. A shulker bullet that hits a wall now
  gives a small burst and a thud. A dragon fireball now bursts into its
  purple cloud with its sound.
- **Breezes handle projectiles like vanilla's.** A breeze now turns
  arrows and other projectiles back at half speed instead of being hit by
  them. Wind charges still hit it. A breeze's wind charge now hits other
  mobs in its way, but never hurts another breeze. It also bursts with its
  full, wider blast, even after a player hits it back.
- **Thrown things carry your motion and a little wobble.** Snowballs,
  eggs, ender pearls, bottles o' enchanting, thrown potions, wind charges,
  tridents and bow shots now add the thrower's own movement, as in
  vanilla. Upward or downward movement only counts while off the ground.
  Each throw also has vanilla's small random scatter instead of flying a
  perfect line. Dispensers now shoot from vanilla's spot at vanilla's
  strength and spread. Potions and bottles fly faster and straighter than
  snowballs and arrows. Fire and wind charges leave a full block out.
- **Shulker bullets fly and break like vanilla's.** A shulker's bullet
  now zig-zags toward its target one direction at a time, turning when it
  lines up with the target or meets a wall, instead of curving smoothly
  straight at it. Hitting a bullet now destroys it.
- **Llama spit flies like vanilla's.** A llama now spits from its mouth
  with vanilla's aim scatter instead of a perfect shot every time. Spit
  that touches water, grass or any other block now vanishes where it is.
- **Blaze fireballs start fires.** A small fireball that hits a block now
  lights a fire in the empty space in front of the face it hit, as in
  vanilla. A blaze's fireball only does this with mobGriefing on. One from
  a dispenser, or one a player has hit back, always does.
- **Fireballs burn the way vanilla's do.** A ghast's fireball no longer
  sets the player it hits on fire; it only hurts. A blaze's fireball, or
  a fire charge from a dispenser, sets the target alight and hurts it. A
  blow that fails leaves the target's old fire as it was. Fire Resistance
  now also stops fireball damage. Burning arrows and fireballs now set
  mobs on fire as well as players.
- **Lingering potions fly as lingering potions.** A thrown, dispensed or
  trial-spawner lingering potion now appears as its own kind of thrown
  potion rather than as a splash potion, as it does in vanilla.
- **Dropped items work their way out of blocks.** An item lying where a
  block was then placed stayed buried inside it. As in vanilla, it now
  slides out of the nearest open side, or rises out of the top if it is
  walled in. Items floating up under a ceiling also stop at the ceiling
  instead of sinking into it.
- **Mobs no longer walk into tall walls.** A wall more than eight blocks high
  looked like flat ground to a walking mob, so a sheep could wander into a
  player's wall and turn black inside it. Mobs now step only where their
  whole body fits.
- **A piston pushes every mob its block touches.** A block moved by a piston
  used to push only a mob whose centre was in its path, so a wide mob could
  be left half inside the block. Now any mob or player the block overlaps is
  moved clear of it, as in vanilla.
- **Sheep graze on every plant vanilla lets them.** They ate only short
  grass or a grass block; ferns and short and tall dry grass now feed them
  too. With mob griefing off, the plant they eat stays, as in vanilla.
- **Endermen no longer pile up.** An enderman carrying a block never
  despawns, in vanilla as here. But vanilla's idle clock keeps running
  while it carries, so once it sets the block down far from everyone it
  can despawn straight away. Ours restarted that clock, and the enderman
  picked up the next block first, so carriers never left and gathered
  around the spawn area. The ones already there will thin out as they set
  their blocks down.
- **Evoker fangs can be seen.** An evoker's fangs bit, but Java players
  never saw them rise. The client only draws a fang once the server says
  its attack has started, and that signal was never sent. It now goes out
  at the vanilla moment, with the snap played by the client as in vanilla.
- **Mob blasts respect mobGriefing, and fireballs burst on walls.** With
  mobGriefing off, a ghast's fireball, a wither skull and the wither's
  spawn blast still broke blocks. Now they keep every block and only hurt
  what stands in the blast. A ghast's fireball also lodged in a wall like
  an arrow instead of exploding, and blaze fireballs stuck there for ten
  seconds. Every fireball and skull now ends where it hits, and a blaze's
  fireball primes TNT or lights a campfire it strikes.
- **Wither skulls wither by difficulty.** A wither skull gave a weak,
  ten-second Wither on every difficulty. It now gives Wither II, for ten
  seconds on normal and forty on hard, and none on easy, as in vanilla.
- **Thrown things fly as far as they do in vanilla.** Every thrown
  projectile fell as fast as an arrow, so snowballs, eggs and ender pearls
  landed well short. Each now falls at its own vanilla rate, and a bottle
  o' enchanting drops faster. Splash potions, lingering potions and bottles
  o' enchanting leave the hand tilted upward, at vanilla's speeds. Llama
  spit falls a little faster, and a shulker bullet whose target is gone now
  drops to the ground.
- **Dispensed TNT no longer deletes the block in front.** A dispenser firing
  TNT turned whatever block stood in front of it into air. It now drops a
  lit charge there and leaves the block alone, as in vanilla. With TNT
  explosions switched off, the dispenser keeps its TNT.
- **A parched's kill makes a creeper drop a music disc.** Since 26.3 the
  parched counts as a skeleton, so a creeper it shoots dead drops a disc,
  as one killed by any other skeleton does. Wolves now hunt the parched too.
- **Piglins spawn as vanilla's do.** Every piglin used to be grown and carry
  a golden sword. Now a fifth are babies that hold nothing and fight no one.
  Half the grown ones carry a crossbow and shoot from range, and some of the
  rest carry a golden spear; some wear gold armour. A bastion's piglins hold
  the crossbow or sword their spot in the bastion gives them.
- **Boats and minecarts take several punches to break.** They broke on the
  first hit. As in vanilla, each blow now rocks the vehicle and adds to its
  damage, which drains away over time, so a bare fist needs a quick run of
  punches. A creative player still breaks one at once, and gets no item.
- **"Surge Protector" needs a villager close to the bolt.** A villager up
  to 30 blocks away in any direction counted, and so did one in another
  dimension. As in vanilla, the villager must now be within 15 blocks
  across, not struck by the bolt, and in the same world.
- **"Two by Two" needs real frogs, sniffers and turtles.** Breeding any
  animal ticked off the frog, sniffer and turtle entries, because those
  three lay eggs or spawn instead of having babies. Each now needs its own
  pair to breed.
- **"Good as New" needs the wolf armour fully mended.** Any scute given
  to a wolf in worn armour granted it. As in vanilla, it now counts only
  when the scute leaves the armour with no damage.
- **"Oh Shiny" and "Star Trader" check the player.** Distracting a piglin
  with gold now counts only when you wear no gold armour, as in vanilla.
  Star Trader now needs you to trade at the build limit (y 319 or higher),
  not anywhere.
- **Kill advancements check how the kill was made.** Sniper Duel now
  needs a skeleton shot from at least 50 blocks away. Return to Sender
  needs a ghast killed by its own fireball, and Uneasy Alliance a ghast
  killed in the Overworld. Blowback needs a breeze killed by a breeze's
  wind charge batted back at it. Voluntary Exile needs a raider wearing the
  ominous banner, not just any kill. Before, any kill of the right mob,
  or for Voluntary Exile any kill at all, granted them. A mob killed by a
  player's wind charge now counts as that player's kill.
- **"Careful Restoration" needs four sherds.** Crafting a decorated pot
  from one pottery sherd and three bricks granted the advancement for a
  pot made only of sherds. As in vanilla, each sherd the advancement asks
  for now has to be a different item in the grid.
- **Mob drops follow vanilla's odds.** A polar bear now drops cod three
  times as often as salmon, and a witch drops sticks twice as often as
  each of her other drops. Every choice in a mob's drop table was being
  drawn evenly.
- **You see other players crouch, sprint and swim.** Other players stood
  upright while crouching and never showed a swim. Their clients draw
  those poses from a set of flags the server never set. It now sets them,
  so crouching, sprinting and swimming show as they do in vanilla.
- **Eyes of ender lead to the stronghold's entrance.** Since the full
  stronghold mazes landed, an eye of ender flew toward the portal room,
  which can be a hundred blocks inside the maze. As in vanilla, it now
  leads to where the stronghold starts, and `/locate stronghold` reports
  the same place.
- **Adventure mode is survival without building, as in vanilla.** Many
  survival rules applied only to survival mode, so adventure players were
  never hurt, never hunted by monsters, never got hungry, didn't use up
  arrows, food or other items, and couldn't pick things up. They now play
  exactly like survival players, except that they cannot break or place
  blocks.
- **Observers no longer stay stuck on.** An observer that a piston moved
  mid-pulse, or that was saved mid-pulse over a restart, came back powered
  with nothing to switch it off. As in vanilla, it now switches off when
  it has no pulse to finish. The restart check now covers redstone in the
  Nether and the End too.
- **Items keep everything when moved in a menu.** Moving an item in an
  inventory or container window kept only its damage, enchantments, name
  and a few other details. A potion became water, a shulker box or bundle
  lost its contents, dyed armour lost its colour, and a lodestone compass
  forgot its target. Swapping two different potions could also swap their
  types. A moved or thrown item now keeps its whole stack. The same holds
  for what you drop on death, what you throw with Q, what a closed crafting
  grid or enchanting table hands back, and gear a mob picks up or drops.
- **Burning animals run for water again, and fish flee.** Since an earlier
  change today, a burning animal ignored the water it had found, and a
  fish or squid panicking in open water froze in place. A burning animal
  now heads for water within five blocks first, as vanilla's panic does. A
  panicking fish swims to another spot in the water, and a panicking flier
  flies to one.
- **Shift-clicking with a block in hand places it.** Sneaking and
  right-clicking a door, chest or lever with a block in hand places the
  block against it, as vanilla does. Sneaking empty-handed still opens a
  door, where before sneaking always stopped you using it.
- **Nether portals stay lit in the Nether.** A portal block on the Nether
  side checked its obsidian frame in the overworld, at the same
  coordinates, and went out whenever a block beside it updated. It now
  checks its own frame.
- **No snow under roofs.** Snow and ice now form only on the top block of
  a column, as vanilla's weather does. A floor under a roof stays clear
  however high the roof is. Before, a roof more than six blocks up counted
  as open sky, so snow piled up indoors. A spot lit to block light 10 or
  more never collects snow, and snow settles only where a snow layer could
  stand.
- **No more floating dripstone in regenerated caves.** When the cave
  generator changed, dripstone and cave vines that had grown in the old
  caves were kept, while the rock around them regenerated. Some were left
  hanging in open air. Now, when a chunk comes into range, any such growth
  that has lost its support breaks, and a hanging one falls, as vanilla
  does when support goes.
- **Waves only on the coast.** The optional beach waves treated any water
  at sea level as the ocean, so river banks, lakes and swamp edges waved
  too. A wave now starts only where the water is ocean or the shore is a
  beach.
- **You see your own skin.** A joining player was never sent their own
  tab-list entry. Their client had no skin data for them and drew a
  default skin, and they were missing from their own tab list. Every
  player now gets their own entry when they join, with their skin, as
  vanilla sends it.
- **No more "chat messages can't be verified".** With online mode on, the
  gateway tells clients and the server list that the server enforces
  secure chat, as a vanilla online server does. The warning toast no
  longer appears when you join. Chat still arrives as server messages, so
  every client shows it, whatever its secure-chat setting.
- **Hoppers keep vanilla time.** Each hopper now ticks once per game tick
  with its own eight-tick cooldown. An idle hopper takes an item the tick
  after one arrives. It pushes before it pulls, and one fed by another
  hopper waits its eight ticks before passing the item on. Before, every
  block update next to a hopper started another transfer loop, so a busy
  hopper could run several times faster than vanilla's.
- **Tamed pets walk after you.** A wolf, cat or parrot following its owner
  used to stall in the idle cycle. It only caught up by teleporting once it
  was more than twelve blocks behind. It now walks or flies after you as
  vanilla's do. Pets reloaded after a restart also follow again, instead of
  wandering off like wild animals.
- **Parrots copy only the mobs they know.** A parrot picks the mob to imitate
  from those whose call it has, as vanilla's does. It no longer goes quiet
  when a cow or a pig happens to be the nearest mob.
- **Nautiluses, zombie nautiluses and sniffers behave as in 26.3.** A
  nautilus follows its food, courts its mate, and charges whoever hurt it
  or a pufferfish now and then. It then waits out a cooldown before it
  charges again. The zombie nautilus is an animal, not a monster. It no
  longer hunts players, and it stays in peaceful. A tamed nautilus no
  longer sends a sit flag, which on 26.x clients landed on another field.
  Sniffers now scent, sniff, rise and look happy between digs, as in
  vanilla. Two sniffers that mate lay an egg.
- **Full shulker boxes keep their contents when broken.** Breaking one by
  hand dropped an empty box and scattered everything that was inside it. A
  blast did the same. The box now comes back with its contents, however it
  was removed, including when a piston breaks it. An earlier version of
  this fix lost the contents entirely on the piston path.
- **Decorated pots keep their sherds.** A pot broken by hand without a
  tool, or blown up, drops with the sherds on its faces. Before, the pot's
  record of its sherds was cleared just before the drop was made, so it
  came out plain.
- **Banners keep their patterns however they break.** A banner blown up,
  knocked off its wall or broken by a piston drops with its pattern, as one
  broken by hand does.
- **Bookshelves count through grass and snow.** As in 26.3, a bookshelf
  powers an enchanting table even with short grass, snow, water or anything
  else from `#replaceable` in the gap between them. A torch still blocks
  it.
- **Picking a trade fills the payment slots.** Choosing an offer in the
  villager screen moves the items it costs from your inventory into the
  trade slots, and puts back whatever was there before, as in vanilla.
- **Ender chests follow chest rules.** A solid block on top keeps an ender
  chest shut, and opening one near piglins angers them, as opening a chest
  does.
- **Droppers keep what items carry.** A dropper feeding a chest or
  throwing an item out passes on its potion, name, enchantments and
  contents. Before, only the bare item got through (a thrown item kept its
  damage and enchantments). The container insert is also silent now, as in
  vanilla.
- **Lava fuel gives the bucket back.** A lava bucket burnt in a furnace
  leaves its empty bucket in the fuel slot. Before, the bucket was lost.
- **`/gamerule <rule>` shows the current value**, as vanilla's does, and
  `limited_crafting` can be set with `/gamerule`.
- **Dispensed spectral arrows glow.** A spectral arrow fired from a
  dispenser flies as a spectral arrow and makes what it hits glow, like one
  shot from a bow.
- **Enchantment maths as in vanilla.**
  - Sharpness, Smite and Bane of Arthropods now scale with how charged the
    swing is, not with the squared curve the base damage uses.
  - A crit, the strong-hit sound and a sweep need a swing more than 90%
    charged.
  - Breach cuts through armour on every mace hit, not only smash attacks.
  - A sword sweep reaches what vanilla's does (beside the target and within
    three blocks of you), and never reaches into another dimension.
  - Mending repairs only what you are holding or wearing, including the
    offhand, and the repaired durability now shows straight away.
- **More things work outside the overworld.** Trapped chests give a
  redstone signal in the Nether and the End, endermen there pick up and
  put down blocks, copper golems can be built there, and a zombie can call
  reinforcements in any dimension.
- **Lightning stays in the overworld.** An overworld bolt struck and set
  fire to players and mobs standing at the same coordinates in the Nether
  or the End.
- **The overworld's weather stays in the overworld.** When it rained in
  the overworld, a Nether farm was watered, a Nether fire was put out and a
  Nether cauldron filled, if the overworld column at the same coordinates
  saw the sky. Nether farmland and dried ghasts also looked for water in the
  overworld. All of these now read their own dimension.
- **Allays in any dimension.** An allay delivers to a note block it heard in
  the Nether or the End as well as the overworld. It only brings items to
  its player while that player is in survival or creative and within 64
  blocks, as vanilla's does.
- **Potions can be drunk.** Using a drinkable potion took the path for
  food, which turned it away, so no potion could ever be drunk; they only
  worked when splashed. They now take their 32 ticks and leave the bottle.
  Adventure players eat and drink as survival players do. Creative players
  can eat on a full hunger bar, and keep the food or potion as vanilla's
  do.
- **Frogs croak.** An idle frog on land stops now and then and croaks for
  three seconds, throat puffing, as vanilla's do.
- **Dolphins leap and race boats.** A dolphin swimming along the surface
  of open water now and then jumps clear of it and splashes back down, and
  a dolphin near a boat you are rowing swims alongside and races ahead of
  it, as vanilla's do.
  Leaping mobs (spiders, wolves, cats and foxes) now feel air drag, so
  their pounces are vanilla's length instead of nearly twice it.
- **Buckets drain waterlogged blocks.** An empty bucket used on a
  waterlogged slab, stair or fence takes the water and leaves the block
  dry.
- **A bucketed axolotl keeps its colour.** The bucket now remembers the
  axolotl's variant and a baby's age, so a blue axolotl comes back out blue.
- **Eggs hatch properly.**
  - Brown and blue eggs can be thrown (before, only white ones could).
  - Chicks hatch where the egg broke, in any dimension, and hatch warm or
    cold to match the egg.
  - One hatching in thirty-two gives four chicks.
  - Eggs hatch when they hit a creature and when fired from a dispenser.
- **Bundles empty when you hold them.** Using a bundle tips its contents
  out one at a time, as in vanilla. Before, holding a bundle did nothing.
- **Tools wear at vanilla's rates.**
  - A sword, mace or trident loses two points per block it breaks; a
    pickaxe, axe, shovel or hoe loses one.
  - Hitting something costs a pickaxe, axe, shovel or hoe two, and a sword,
    spear, mace or trident one.
  - Shears wear on every block they cut, grass included.
  - Blocks that break instantly cost nothing, and items that aren't tools
    (a flint and steel, a bow) no longer wear from digging or hitting.
  Before, every item lost one point either way.
- **Efficiency digs faster on your screen too.** In 1.21 and later the
  game client works out digging speed from an attribute the server sends,
  and tachyne never sent Efficiency's share of it. So an Efficiency
  pickaxe dug at a plain pickaxe's pace for the player. The held item's
  Efficiency and Sweeping Edge now reach the client, and update whenever
  you switch items. So does the held weapon's attack speed, which drives
  the attack-cooldown indicator: it showed a bare hand's instant recharge
  for every weapon. Copper tools also have their own attack cooldowns now
  instead of a fist's, and a trident swung in melee hits for nine, not one.
- **More blocks behave as in vanilla.**
  - A comparator reads a candle cake as a whole cake.
  - Ravagers trample pitcher crops.
  - Breaking a clutch of turtle eggs takes one egg at a time.
  - TNT, creepers and withers let a hive's bees out, and a fire lit beside
    a hive empties it.
  - A chorus flower drops itself when shot or broken by a player.
  - Boats break the lily pads they run into.
  - Copper chests pair whatever their oxidation, and the two halves
    weather, wax and scrape together.
  - A head can be placed on top of a note block.
  - A powered dragon head moves its jaw and a piglin head flaps its ears.
- **Decorated pots shatter.** A pot broken with a tool, or by an arrow or
  a trident, cracks and drops its sherds, with a brick for each plain side,
  as in vanilla. Break it by hand or with Silk Touch to keep the pot whole.
- **Blocks place the way vanilla places them.**
  - A crafter faces the way you look.
  - Bamboo goes down as a shoot, or extends a stalk.
  - A dirt path placed under a solid block comes out as dirt.
  - Kelp needs a water source, and kelp or vines placed on the same plant
    extend it.
  - Glow berries plant cave vines.
  - Huge mushroom blocks join up with their neighbours.
  - Pale moss carpet climbs the walls beside it, and is no longer treated
    as a wall.
  - A cave vine keeps its berries on the segment that had them, instead of
    fruiting all the way down as it grows.
- **Blocks react to their neighbours.**
  - Grass, podzol and mycelium turn snowy when snow lands on them, and back
    when it goes.
  - A fence gate between walls lowers to fit them.
  - A pumpkin or melon stem lets go when its fruit is picked.
  - Huge mushroom blocks close the faces that touch their own kind.
  - A piston head passes neighbour updates to its piston.
- **Villagers level up on the trade.** As in 26.3, a villager takes its new
  tier, its new offers and its level-up sparkle the moment the promoting
  trade is made, with no two-second pause.
- **Water Breathing protects mobs.** A mob with Water Breathing or Conduit
  Power no longer drowns.
- **Fire Resistance works as in vanilla.** You still burn, and others see
  the flames, but the fire does no damage. Before, drinking the potion put
  the fire out at once.
- **Absorption ends with its hearts.** Once the golden hearts are used up,
  the Absorption effect goes too, instead of sitting on the effect list for
  the rest of its time.
- **A stronger potion no longer wipes out a longer one.** Drink Speed II
  while Speed I has a minute left, and Speed I comes back with the rest of
  its time once Speed II wears off. Before, the weaker effect was lost.
- **Fall deaths say how you fell.** Death messages for falls now come from
  vanilla's combat log. A long fall reads "fell from a high place", and one
  off a ladder, vines or scaffolding says so. If something knocked you off,
  it reads "was doomed to fall by Zombie", or "fell too far and was finished
  by …" when the same attacker finished you. Every fall used to read "hit
  the ground too hard".
- **Tempting range is an attribute.** Animals follow food from ten blocks,
  happy ghasts from sixteen and sulfur cubes from eight, and effects and
  plugins that change the range now apply.
- **Shulkers and slimes.** A shulker now picks targets within four blocks
  above or below it, as vanilla's does, not from any height inside a
  twenty-block circle. A slime with nothing to chase keeps one heading for
  two to five seconds instead of changing direction every hop.
- **Witches and blazes hold their ground.** A witch walks up to throwing
  range and stands there to throw. A blaze that can see you hovers where it
  is and fires. Neither backs off and circles like a skeleton any more.
- **Vexes fly like vexes.** A vex now drifts between random spots near
  where it was summoned, then darts at its target, charging and crying out,
  and strikes when it touches them. It passes through walls, as in vanilla.
  Before, it chased like any other flyer, at a walking pace.
- **Four monsters fight on vanilla's timing.**
  - Magma cubes wait four times as long between hops and jump higher the
    bigger they are.
  - An illusioner fires once a second on every difficulty.
  - A creaking strikes every two seconds.
  - A zoglin goes for whatever living thing is closest, a pet included, and
    sticks with it.
- **Armadillos, tadpoles and axolotls panic and chase as in 26.3.** An
  armadillo now runs from fire, lava and the other environmental hazards,
  unrolling first; a blow still just rolls it up. Tadpoles flee at vanilla's
  pace, and an axolotl chasing prey on land slows to its land speed.
- **Big dripleaf stays up.** A big dripleaf's leaf used to pop off its own
  stem, because the stem has no collision. It now follows vanilla's rule:
  the leaf stands on a stem, another leaf, or the plant's ground (clay, moss,
  dirt and the other #supports_big_dripleaf blocks). A stem likewise no
  longer takes root on stone.
- **Hanging signs chain.** Clicking a hanging sign while holding another
  one now places the new sign against it instead of opening the editor. It
  works on a ceiling sign's underside and on any face of a wall sign except
  its two text faces. The new sign hangs from the one above.
- **Redstone keeps vanilla's time.** A block change now reaches its
  neighbours in the same tick, and components wait on scheduled ticks run in
  vanilla's order, instead of every step waiting an extra tick. A lamp at the
  end of dust lights the moment the lever is flipped (it used to take 2
  ticks), a line of repeaters runs 2 ticks a stage instead of 3, torches
  switch after 2 ticks instead of 1, and observers pulse 2 ticks after the
  change they see. Dispensers fire after 4 ticks instead of 5, crafters now
  wait their 4 ticks, a comparator's mode click takes effect at once, and
  pistons move at the end of the tick, their blocks landing 2 ticks later.
  Clocks run at their vanilla period, and a torch clock that is too fast
  burns its torch out as it does in vanilla.
  A powered note block sounds at the end of the tick, a hopper switches off
  the moment power reaches it, and a crafter animates for six ticks, and
  only when a craft succeeds.
- **Items keep everything they carry when picked up.** Picking up a potion,
  a shulker box, a bundle, dyed leather, a firework rocket, a goat horn, a
  decorated pot, a suspicious stew or a silk-touched hive used to strip it
  back to the plain item, and a box or bundle lost what was inside. This
  included items dropped on death. Every place that decides whether two
  stacks merge now uses vanilla's single rule, "same item, same data":
  inventories, hoppers, dispensers, pots, crafting results and items lying
  on the ground.
- **The Nether and the End act in their own dimension.** Several actions
  there used to happen at the same coordinates in the overworld instead:
  - redstone-lit, dispensed and flint-and-steel TNT;
  - everything a dispenser throws or drops;
  - spawn eggs;
  - note blocks and bells;
  - arrows from skeletons.
  Redstone now hears about blocks removed in the Nether and End. Nether
  portals breed zombified piglins in the overworld, as in vanilla, and no
  longer inside the Nether.
- **Mob combat closer to vanilla.**
  - Armed mobs hit one point too hard. A weapon now adds only its own
    bonus, so a vindicator hits for 13, not 14.
  - Blazes hit for 6 in melee, not 3.
  - Witches no longer hunt villagers and iron golems, and ravagers leave
    baby villagers alone.
  - The parched now shoots its Weakness arrows, on its slower 70-tick draw,
    and no longer burns in daylight.
  - Hitting an ocelot, a snow golem, a zombie horse or an armadillo no
    longer sends it running.
  - Ranged mobs now attack the creatures they hunt, not only players:
    - pillagers and illusioners shoot villagers and iron golems, and
      evokers bite them with fangs;
    - skeletons shoot baby turtles;
    - guardians beam squid;
    - the wither's centre head fires at whatever it targets.
  - A mob's arrow or wither skull now hits any mob in its path, so a
    skeleton can kill a creeper for its music disc again.

- **Blocks.**
  - Bone meal grows a sweet berry bush or a cave vine; picking happens only
    when there are berries.
  - Leaves you place are persistent and never decay.
  - A sign placed in water stays waterlogged.
  - Cobwebs slow mobs to a quarter (spiders pass through).
- **More hunting targets.**
  - Skeletons and spiders go after iron golems; spiders only in the dark.
  - Wither skeletons fight piglins.
  - Guardians hunt axolotls.
- **Advancements that could not be earned:**
  - Very Very Frightening (a channeled bolt on a villager);
  - Birthday Song (an allay drops a cake on a note block);
  - You've Got a Friend in Me (you pick up an allay's delivery);
  - War Pigs (opening a bastion's loot chests counts, as vanilla's first
    open of an unlooted container does).
  An allay now also hands over everything the item carries, not five
  fields of it.
- **Shears take gear off a mob**, as in 26.x:
  - saddles from pigs, striders, horses and other mounts;
  - horse, wolf and nautilus armour;
  - llama carpets and happy-ghast harnesses.
  The body slot comes off before the saddle. A mob carrying someone keeps
  its gear, a wolf answers only to its owner, and Curse of Binding holds a
  piece on outside creative. The piece drops with everything it carries,
  and taking a wolf's armour off earns Snip Snap.
- **26.3 brewing stand bars.** A 26.3 client is sent the brew and fuel
  totals its brewing stand measures its bars against. A 26.2 client, whose
  menu has no such slots, never sees them.
- **Crossbows fire rockets.** A firework rocket held in the off hand
  loads, as vanilla allows (not one from the backpack). It flies straight
  at the aim, and goes off on the first mob or player in its path, or on a
  block if it carries stars. The blast does 5 + 2 per star within 5
  blocks. Each rocket costs the crossbow 3 durability. Every projectile now
  costs its own wear, so a multishot arrow volley takes 3, as in vanilla.
- **Bows and crossbows shoot any arrow.**
  - Tipped and spectral arrows fire from a bow or crossbow as themselves,
    drawn from the off hand first, then the inventory.
  - A spectral arrow makes what it hits glow.
  - A tipped arrow gives mobs an eighth of the potion's time, as it does
    players.
  - Picking an arrow back up returns the same kind.
  - Infinity spares plain arrows only.
- **Smaller fixes.**
  - Fish buckets can't be eaten.
  - A boat placed from the off hand is used up, instead of coming back
    free.
  - A brush wears once per block brushed clean, not once per stroke.
  - The dragon and the Wither can be named, and a name tag keeps any mob
    for good.
  - /gamemode with a selector is remembered for each player it reached, not
    saved under "@a".
  - /time takes vanilla's forms (set, add, query, with d/s/t units) and,
    like /say, needs op.
- **No more stalls when flying over new ground.** Moss patches in caves
  were planted a block above their floor, floating, and fell to thousands
  of dropped items as soon as anything beside them changed. The pile of
  items then made each server tick slow. Patches now sit on their floor.
  Nearby dropped items merge far faster, and a hot block-state lookup that
  villagers' pathing leaned on is much quicker, which removes the
  multi-second spikes.
- **No more snap-backs in fast flight.** The movement check now uses
  vanilla's rule: a single move may carry you up to 10 blocks. It replaces
  a stricter speed budget of the engine's own, which hitched fast creative
  flight every few seconds whenever the server ran a slow tick.
- **Panicking animals run like vanilla's**, to one random spot nearby after
  another, preferring grass, rather than straight away from whoever hit
  them. Goats panic when struck; skeleton horses never do. Only dolphins
  and axolotls walk back to the water; stranded squid and fish flop where
  they lie.
- **The tab list shows each player's game mode.** Everyone was listed as
  creative. A spectator now looks like one to everyone else, and to their
  own client, which reads it from the same entry.
- **Two players at one furnace** both see it cook. The first player's
  window used to freeze when the second opened it.
- **Daylight burns by 26.3's list.** Zombie horses and zombie nautili burn in
  the sun; the parched does not.
- **Raids can be lost.** A raid whose village is gone after a wave has come
  ends in defeat ("Raid - Defeat" on the bar for thirty seconds). No raid
  outlasts 48,000 ticks, and the bar reads "Raiders Remaining" when two or
  fewer are left.

### Changed
- **Villager and wandering-trader trades come from 26.3's trade data.**
  26.3 turned the trade tables into data — each offer a villager trade, each
  career level a trade set naming its offers and how many to draw — and the
  engine now reads that data out of the vanilla jar instead of a transcribed
  table. What changed for players: a master librarian sells red and yellow
  candles, and the name tag moved to the wandering trader, which also sells
  poplar logs and saplings, golden dandelions, shelf mushrooms and sulfur
  spikes; the farmer's cake costs three emeralds; the novice weaponsmith's
  enchanted iron sword can be bought twelve times and reacts harder to
  demand; the leatherworker's master dyed helmet pays five trade XP, not
  thirty; the farmer's stew is one offer hiding one of six effects rather
  than six offers; dyed leather takes one to three dyes as 26.3 rolls them;
  and for cooked fish and flint the raw fish or gravel is now the price that
  demand and reputation move, the emerald the fixed extra. The fisherman's
  biome boats are five offers, each for its own villager types. Villagers
  keep the trades they already have.
- **Advancements come from 26.3's data.** The tree gains Uh Oh (it stays
  locked until sulfur cubes arrive). Adventuring Time now asks for the
  dappled forest, and for the sulfur caves, which can't be earned until they
  generate. Goat-boat rides count in poplar boats, and glow ink counts on
  poplar signs. Piglins now love golden dandelions, so tossing one earns Oh
  Shiny. Biome advancements could not be earned before: Adventuring Time,
  Hot Tourist Destinations and Sound of Music never matched the world's
  biome names, and cave and Nether biomes were not checked. Now the biome
  at your feet counts, in every dimension. Placing a chiseled bookshelf no
  longer earns The Power of Books without a comparator reading it.

## 2026-09-23

### Added
- **Poplar trees and the dappled forest.** Poplars grow in red, orange and
  yellow: tall trunks with a ring of short branches and a narrow rhombus
  canopy braced by log spokes, with shelf mushrooms low on the trunk.
  Poplar saplings grow one of the three colours. The dappled forest, 26.3's
  new biome, takes vanilla's coldest, driest plains: poplars, the odd spruce
  and fallen poplars on grass with coarse-dirt patches, red shrubs and
  brown mushrooms. Farm animals there take their cold look. A 26.2 client
  sees it as forest, a Bedrock client as forest too.
- **Explorer maps, as 26.3 has them.** Each is its own item (buried treasure
  map, ocean explorer map, and fourteen more) from shipwrecks, ocean ruins,
  camps and cartographers. They are maps in every way: held, framed,
  cloned and locked. A 26.2 player sees a filled map, the same map.
- **Abandoned camps.** A tent and a campsite in the style of eighteen
  biomes, with a campfire, seats, camp loot and their trees, set onto
  the ground the way vanilla's terrain adaptation does.
- **26.3's new building blocks.** Wool and concrete stairs and slabs in all
  sixteen colours, the poplar wood set (logs, planks, stairs, slabs, fences,
  gates, doors, trapdoors, buttons, pressure plates, signs, hanging signs, a
  shelf, poplar boats and chest boats) and poplar leaves in red, orange and
  yellow. They craft, cut on the stonecutter, drop and burn as vanilla's do,
  and burn in a furnace. Poplar saplings can be planted but don't grow yet;
  poplar trees arrive with 26.3's worldgen.

### Fixed
- **Sprinting and sneaking on 26.x clients.** 1.21.6 dropped the two sneak
  actions from the client's movement command, so every later action arrived
  two places off: starting to sprint read as "leave bed" and stopping read
  as nothing. Sprinting was never seen, so there were no sprint jumps, no
  faster hunger and no sprint knockback. Sneaking now comes from the
  client's input packet the way vanilla reads it, and sprinting is carried
  on every move.
- **Eating from the offhand works, and no longer crashes the server.**
  Letting go of food held in the offhand could crash the whole world, and
  offhand food was never actually eaten. Food, potions, milk and ominous
  bottles now work from either hand, as in vanilla.
- **Water in a waterlogged block flows out, as vanilla's does.** A
  waterlogged stair, slab, fence or other block holds a water source, and
  now pours it out of every side its shape leaves open. A stair pours out
  of the open side of its step but not through its back, and a slab
  spills sideways but not downward. It feeds the water next to it too.
  Waterlogged blocks used to hold their water and do nothing with it.
- **Creative players keep their hotbar when they rejoin.** The server kept
  a creative player's inventory but sent it back on joining only in
  survival, so a creative player came back to an empty hotbar, and the
  first thing they changed overwrote what the server had kept.
- **Observers, pistons, dispensers, droppers and barrels face up or down as
  vanilla's do.** They turn to face up or down once the player looks more
  than 45° up or down (sooner when looking diagonally), from whichever way
  the look points most. They used to wait until 60°.
- **Trees stop vanishing near where people play.** The tree guard also
  counted what the world does by itself as a build: grass under a trunk
  turning to dirt, and fire or lava left in a burning tree's cells. Nearly
  every tree anyone had lived near, and every tree that caught fire, was
  dropped when its chunk regenerated. That left bee nests, vines, fire and
  lava hanging in the air. Only a player's own blocks move a tree aside now.
- **Fire and flowing water or lava no longer freeze after a restart.** A
  restart used to drop every pending block update, so a fire caught
  mid-burn or lava caught mid-flow stopped where it was. They now pick up
  again when a player comes near.
- **No more Nether mobs above the ceiling.** Where the Nether's roof is open to
  the void above, natural spawning could still put a mob one block above
  the ceiling.
- **Chopped trees stand again.** Since the tree guard landed earlier today, a
  tree a player had chopped part of was not generated at all, so the rest
  of its trunk and all its leaves disappeared. Near spawn that took many
  oaks, birches and jungle trees, and the vines that had grown on the
  jungle trees. A chopped tree now stands as the player left it, as in
  vanilla. A generated tree still makes way for a player's build.
- **The game no longer stalls to generate land nobody is near.** Growth,
  natural spawning, held maps and scheduled block updates (flowing water,
  falling sand) now run only in loaded chunks, as vanilla's do. A chunk
  nobody has loaded is left alone, and an update waiting there runs once
  it loads. Before, any of these could generate a whole chunk in the
  middle of a tick, stalling the game for everyone.
- **26.3 players are no longer disconnected by particles.** 26.3 changed
  how a particle packet carries its count, and the gateway still sent it
  the 26.2 way, so the first particle the server sent (a potion's swirl, a
  villager's hearts) dropped a 26.3 client. 26.2 players were unaffected.
- **The world no longer freezes when two stalactites let go together.** When
  the ceiling under two side-by-side stalactites was removed, the check for
  what had lost its support passed the two back and forth for ever. The game
  stalled, the pod was restarted, and every player was disconnected. Both
  now fall as they should.
- **Villages sit on the land.** Houses no longer stand in ponds with the
  water above their floors, hang off slopes or cut into hillsides. Each
  house is placed at the surface where it meets its street, water
  included, as vanilla places them; streets follow the ground block by
  block; and the ground is built up under a house that overhangs, through
  water too, as vanilla's dirt pillars. Every village regenerates this
  way, settled ones included.
- **Structures have no more one-block holes.** The connection points
  between a structure's pieces are now filled with the block vanilla puts
  there (a bastion wall's blackstone, a trial chamber's tuff bricks, a
  street's path) instead of being left empty.
- **Springs run.** Water and lava springs in cave walls start flowing when
  their chunk loads, as vanilla's do, instead of hanging in the air as a
  single still block.
- **26.2 players see 26.3's items.** An item a 26.2 client doesn't have
  used to be an empty slot. Poplar and the wool and concrete stairs and
  slabs now show as the item of their stand-in block, and explorer maps as
  filled maps.
- **Trees no longer grow inside player builds.** tachyne keeps a player's
  builds and generates the land around them afresh, so whenever
  generation changed, a tree could grow where someone had built: up
  through a castle's floor, with its canopy in the rooms. A generated tree
  whose trunk would stand on a player's floor or pass through a wall or
  roof, or whose canopy runs into a build, is now left out, in every
  chunk it would reach. A block or two in a canopy doesn't count.
- **26.2 players see 26.3's blocks as something of the same shape.** A block
  a 26.2 client doesn't have used to reach it under its 26.3 number, which on
  26.2 is some other block: a white wool slab showed as fire. Each one is now
  shown as a block of the same shape and about the same colour: poplar as
  birch, and a wool or concrete slab as a stone or wood slab of its colour.
  Bedrock players, who saw them as the "update!" block, get the same
  stand-ins. Recipes that need an item a 26.2 client doesn't have are
  left out of its recipe book, where they would have disconnected it.
- **A coal smelts 8 items in a blast furnace or smoker, not 16.** Both burn
  fuel for half as long as a furnace while cooking twice as fast, so a fuel
  smelts the same count in all three. The halving had been taken out.
- **Pistons pop what vanilla's pop.** Leaves, buttons, beds, candles,
  pumpkins, melons, flower pots and amethyst buds break when pushed instead
  of sliding along. The list was kept by hand and had missed every block
  that inherits this from a family of blocks; the game's own answer is used
  now.
- **Bamboo rafts can be placed again.** The raft item was looked up under a
  name that doesn't exist.
- **Azalea leaves decay** like every other leaf once their tree is gone.
- **Shelves burn, and waterlogged blocks don't.** Fire's odds for each block
  now come from the game itself, per state.
- **Water inside waterlogged blocks shows on 26.x.** The newer clients are
  told how much fluid each chunk section holds, and the count only knew
  water and lava blocks — so a section whose only water sat in waterlogged
  stairs, slabs, kelp or seagrass was sent as dry.
- **Shulker boxes show up again on 26.2 and 26.3.** The newer clients
  numbering block entities one lower than 1.21.11 was handled from the wrong
  place: shulker boxes arrived without the block entity they are drawn
  through, and beds were sent one that called them shulker boxes.
- **Maps paint blocks the way vanilla's do.** Each block had one map
  colour, where vanilla colours some by state: an upright log shows its end
  grain and a fallen one its bark, a bed's head is wool and its foot the
  dye, ripe wheat turns yellow, waterlogged barriers read as water, and
  signs take the upright log's colour. 46 blocks now match.
- **Wood, copper, wool, glass and 380 other blocks sound like themselves.**
  Each block's sound type — its footsteps and the sound of placing it — was
  read out of the server's source, which only sees a block that names its
  sound on its own line. Every block that copies another's properties, or is
  built by a helper or a colour set, fell through to stone: wooden stairs and
  slabs, fences and doors, cut copper, wool and carpet, glass panes, candles,
  lanterns and more. The sounds now come from the game itself, for every
  block.
- **Firework stars are the colours of their dyes.** Each dye's firework
  colour was taken from the wrong column of vanilla's dye table — its text
  colour — so white stars burst pure white instead of vanilla's soft grey,
  and every other colour was off the same way (orange, magenta, …). They use
  the firework colours now.
- **Bedrock players on a current client can join again.** The Bedrock
  gateway spoke only Bedrock 1.26.30, and Bedrock updates itself — through
  1.26.40, .44, .45 and now 1.26.50 — so every up-to-date phone, console and
  PC was turned away at login. It speaks 1.26.50 now. Its block and item
  tables moved with it: the older mappings named 511 block states that 1.26.50
  no longer has, which would have drawn the wrong blocks, and the generator now
  refuses to write any block or item the target Bedrock version does not know.
- **What lies on the ground survives a restart whole.** A dropped item was
  saved as a hand-kept list of its fields, and the list was short: dyed
  leather lost its colour, a firework rocket its flight and bursts, a
  decorated pot its four faces. Drops now save the whole stack, packed exactly
  as a chest slot is.
- **A freshly renamed item keeps its name through a restart.** Names are
  stored once in a shared table, and the last save before a restart wrote
  that table before item frames, armour stands, hoppers, shelves, lecterns
  and mob gear had added theirs — so an item renamed and put in any of them in
  the last half-minute came back nameless.

### Changed
- **Villagers find their beds, workstations and bells as vanilla's do.**
  Every second or two a villager without one looks at the free ones within
  48 blocks and claims the closest it can actually walk to, upstairs and
  indoors included. It lets go of one it has been unable to reach for a
  minute (a bell after ten seconds). A bell placed later gets used, and a
  broken one is let go. They used to look only 16 blocks around, never
  checked they could get there, and were handed the village's first bell
  for life. They now plan their way block by block, so they take the
  stairs to a bed upstairs and the door into a house.
- **Village golems, cats and sieges follow vanilla.** Golems are asked for
  when villagers gossip or panic, and land on solid ground rather than in
  walls. Cats come only to villages with more than four lived-in homes,
  and to swamp huts. Zombie sieges start at midnight around a player
  standing in a village, one zombie every few ticks.
- **Villages bring their own creatures.** A village is populated from what
  its pieces carry, as vanilla places a structure's entities: the town
  centre's iron golem, the cats, the animals in the pens, the desert's
  camels, a zombie village's zombie villagers, and one villager per house
  slot. Each creature appears when its part of the village first comes into
  view, and only once, across restarts too. Villagers spawn with neither a
  bed nor a job, then claim them the way vanilla's villagers do. Villages
  populated before this get their animals now, and a golem or cats only if
  they have none.
- **Villagers stay in their village.** A wandering villager walks back
  toward the nearest village, measured from the beds, workstations and bells
  villagers hold, as vanilla's do. A villager without a home no longer heads
  for the world's origin.
- **The world now speaks 26.3.** Every block, item and entity the engine
  keeps is numbered as Minecraft 26.3 numbers it, and the world's saves were
  carried across on the first start — block edits, inventories, ender chests,
  mobs and their gear and trades, statistics — each checked by name to be
  exactly what it was. Loot, recipes and fuel now read 26.3's data (the
  new blocks above came with them); worldgen and advancements are still
  1.21.11's while the rest of 26.3's reworked data arrives piece by piece. A
  26.2 client sees 26.3-only things as their nearest 26.2 equivalent: a
  poplar boat as an oak one, poplar planks as birch.
- **Java 1.21.5–1.21.8 are no longer served.** Java 26.2 and 26.3 (and
  Bedrock) are; an older client is told at login which versions to use.
- **The game's facts come from the game.** The generated tables that
  describe game content — block states and their light, collision and harvest
  tools, items with their stack sizes, durability and food, entity and
  particle ids, and the id translation between client versions — are now read
  from the vanilla server itself: its data reports, plus a small extractor
  (`scripts/extract`) that loads the server and asks it per block and per
  item. They used to come from community datasets that stop at 26.1 and
  spell some things differently. Each table regenerates identically for
  today's content, with no network access. This is the groundwork for moving
  the engine's content to 26.3: the version is now one setting for every
  generator, a block's sound, note-block instrument and map colour are
  asked of the game too, and the engine already passes its whole test suite
  on 26.3's numbering — with today's gameplay data, so the move changes
  what the ids are, not what the game does.
- **Saves are ready to change content version.** Moving to a newer canonical
  version renumbers items, blocks, entity types and statistics, and the saves
  hold those numbers. One migration now carries every one of them across on
  the first boot after a bump: every stack wherever it sits (ender chests,
  bundles, shelves, jukeboxes, pot sherds, armour stands, mob gear and
  villager trades as well as inventories and chests), mobs' types and held
  items, an enderman's block, and every player's statistics. It finds them by
  type rather than by a hand-kept list, a test refuses any new saved number
  nobody has classified, and it writes nothing unless every id has a home —
  keeping a copy of each file it rewrites.

## 2026-09-22

### Added
- **The server list shows who is playing.** The player count on the multiplayer
  screen read 0 no matter how many people were in game. No gateway could
  answer it honestly — each one sees only the clients on its own protocol
  range, and your client pings whichever gateway matches your version — so the
  world is asked instead, and it answers with the same roster `/list` reads.
  The hover card lists names again too. Both editions: the Bedrock list had the
  same blind spot.
- **The recipe book's three cooker tabs stop being empty.** The green book only
  ever listed crafting recipes, so opening a furnace, blast furnace or smoker
  showed a book with nothing in it. All 236 cooker recipes are book entries
  now, each carrying the cook time and the experience it banks, filed under the
  same tab vanilla files it under — a smelted ore on the furnace tab, the same
  ore blasted on the blast-furnace one. They unlock on the ingredient like
  every other recipe, and clicking one with a cooker open loads the ingredient
  into its input slot (and refuses when the entry belongs to a different
  cooker).
- **Trial chambers arm their traps and stock their pots.** Their dispensers
  were placed empty, so the arrow and fire-charge traps clicked and did
  nothing; their corridor pots held air, which cost you one of the two ways a
  trial key enters the world; and three of their barrels were empty too. All
  three now carry what vanilla puts in them.
- **Powder snow goes in and out of a bucket.** It could be neither poured nor
  scooped, so the block that freezes you — and the reason leather boots exist
  — was unobtainable and unplaceable.
- **Five more `/locate` names**: ruined portals already generate in vanilla's
  desert, jungle, mountain, ocean and swamp flavours, and each can be found by
  its own name now.
- **Wolves have voices and salmon have sizes.** Every wolf sounded the same:
  vanilla registers seven wolf sound variants and gives one to each wolf at
  birth, so a pack is a chorus. Their idle noise is three sounds rather than
  one as well — a growl when angry, a pant, and a whine when a tamed wolf is
  hurt. Salmon come small, medium and large now, at vanilla's odds, and the
  size scales the fish.
- **Mobs and dropped items go through nether portals.** Only players could
  travel, so anything that pushes mobs or their drops through a portal simply
  did not work. Everything else now goes through the instant it touches a
  portal — no standing and waiting, which is a player's rule — and cannot come
  straight back for fifteen seconds. It travels to the portal a player's own
  trip paired it with; an unpaired portal still carries nobody, because the
  far side only gets built for a player.
- **A glass bottle can be filled from the dragon's breath.** Nothing in the
  game could produce the stuff, which quietly made every lingering potion
  unobtainable — the recipes, the clouds and all the rest of it were already
  built and had no way to start. A bottle used beside a cloud the dragon left
  now fills with it, and the cloud shrinks for it.
- **Sand, clay and gravel patches in the beds under water.** The real game
  lays flat disks of them in every river, lake and shallow sea; this world had
  none. Clay is the reason they matter, and clay is not fixed yet: every
  underwater floor here is gravel or sand, and a clay patch only replaces
  dirt, so clay stays a cave-only block for now.
- **An axe wakes a copper golem statue.** A golem that oxidises through
  freezes into a statue, and the way back is an axe — but only while the
  statue is still bright copper. Here it stayed a block forever.

### Changed
- **What you build in the Nether and the End finally ticks.** Redstone,
  fluids, falling blocks and fire were all inert outside the overworld — not
  because the simulation could not run there, but because a player's edit
  never woke it. It does now, in its own dimension: a repeater in the Nether
  is a repeater in the Nether, and the state the simulation keeps beside the
  world (a pending flip, an observer's last look, a pressed plate, a fire's
  age) is no longer shared with whatever sits at the same coordinates at home.
- **Two things vanilla does not generate are gone**: the small broken
  stone-brick shells scattered over habitable land, and wild nether wart on
  soul sand. Already-generated ground keeps what it has.
- **Animals have to meet before they breed.** Two fed at opposite ends of an
  eight-block pen made a baby without ever walking toward each other. They now
  head for the nearest courting partner and only breed after sixty ticks
  within three blocks — and a panda needs bamboo in reach as well as the food.
- **Iron golems ask the village to have slept.** A golem used to be the
  automatic reward for five villagers sharing a bell, however they lived. It
  now takes five villagers who have been to bed within the last day and who
  are standing together — which in practice is the midday gathering at the
  meeting point, exactly where vanilla's golems come from. A village with no
  beds grows none.
- **Horses have to be tamed.** Anyone could put a saddle on any horse and ride
  away. Now it is the ritual it should be: climb on a wild one bareback, get
  thrown, climb on again until it settles — and feeding it apples or carrots
  along the way makes that shorter. A horse will not take a saddle until it is
  tamed.

### Fixed
- **Leaves and grass drop at vanilla's rates, and leaves drop their own
  sapling.** Short grass broken by hand dropped seeds 23% of the time instead
  of 12.5%, and leaves gave saplings and sticks about twice as often as they
  should: when a loot roll came up empty it was mistaken for a block with no
  loot table, and a fallback rolled it again. Decaying leaves always dropped
  an oak sapling and could drop an apple whatever the tree, so a birch or
  spruce forest could not reseed itself. Every drop that involves no tool —
  leaf decay, pistons, a block losing its support, banners and pots — now
  rolls the block's own vanilla loot table, and wall torches, signs, banners
  and heads use the standing block's table as vanilla has them do.
- **Mixed materials craft.** A crafting table from two oak and two spruce
  planks, sticks from an oak plank over a spruce one, a furnace ringed with
  cobblestone and cobbled deepslate — none of these crafted, because every
  recipe slot matched one exact item and the recipe list held a separate copy
  of each recipe per material (twelve crafting tables, one per wood). Recipes
  now come from the game's own recipe files, where a slot takes an ingredient
  — any planks, any of the stone-like blocks — so they can be mixed as in
  vanilla, and clicking a recipe in the book fills the grid from whatever mix
  you are carrying. The book shows one crafting table rather than twelve, as
  vanilla's does, and every recipe you had already unlocked carries over.
- **End portals and end gateways are visible.** Neither has a block model —
  the starfield you see is drawn entirely by the block's block entity — and
  the table deciding which blocks carry one was a list of name patterns that
  missed them, so a stronghold portal you filled with eyes, or the gateway
  that opens after the dragon, showed as nothing at all. Weathered and waxed
  copper golem statues were missing theirs too. The table is now taken from
  the game rather than guessed.
- **An open fence gate lets you through, and deep snow holds you up.** Whether
  a block stops an entity is a per-state question — a gate collides when shut
  and not when open, snow collides from two layers up — but the table behind
  it held one answer per block, taken from the default state. So every open
  fence gate still blocked the way (192 states, every wood type), a drift of
  snow was walked through rather than over, and a tilted big dripleaf, a
  pitcher crop and some wall shapes were solid when they should not be.
- **Farmland knows what counts as a lid.** Vanilla turns tilled soil back to
  dirt when something solid sits on it, and 2,809 block states were on the
  wrong side of that test — a closed fence gate, a sign, a cobweb, a turtle
  egg, a conduit or a shulker box did not squash a field, while a ladder did.
  Vanilla marks 173 blocks solid outright regardless of their shape; the table
  behind this knew about two of them, and had shulker boxes backwards.
- **Light passes through what it should and stops at what it should.** A
  double slab let light straight through as if it were glass, while
  waterlogged fences, stairs and walls dimmed nothing at all. How much a block
  dims light is a per-state property in vanilla — a double slab is solid where
  its halves are not — but the table behind it held one value per block, so
  10,394 of the 29,671 block states carried the wrong one, 125 of them letting
  light through a solid block. Every state was re-checked against vanilla and
  all of them now match.
- **Lit blocks give off light again.** A lit furnace, a powered redstone lamp,
  lit redstone ore, a charged respawn anchor, a campfire, glow berries — none
  of them lit anything, while an *unlit* redstone torch lit the room it was
  in. Light is a per-state property in vanilla (a candle gives 3, 6, 9 or 12
  by how many are lit), but the table behind it only had one value per block,
  taken from that block's default state: unlit for most, lit for a redstone
  torch. Sixty blocks were affected. The table is per-state now, and every one
  of the 29,671 block states was checked against vanilla — all of them match.
- **New blocks on a newer client stop arriving as something else.** A 26.3
  player putting poplar planks in their hotbar had the server store redstone
  ore, and placed redstone ore. Poplar is 26.3 content and the world engine's
  registry is 1.21.11, which has no poplar — but an item travels as a number,
  and 26.3 renumbered nearly every one, so poplar planks (72 there) landed on
  whatever 72 means here. The engine already refused to *send* content an older
  client lacks; it had no matching guard for *receiving* content a newer one
  has. An item the engine does not know now leaves the slot empty instead of
  becoming a different block — 153 items on 26.3, 32 on 26.2. Reported in game
  by LegionZA.
- **A block you place is heard by everyone.** Placement was silent to every
  player but the one placing it — your own client makes the noise when it
  predicts the placement, so you would never have noticed, while anyone
  watching saw blocks appear without a sound. Every block now speaks its own
  voice when it goes down, read off the block that actually landed, so a slab
  that stacked or a block that waterlogged sounds like what it became.
- **A hoglin's bite throws you again.** Being tossed is the whole point of a
  hoglin, and the throw was silently doing nothing. Ravagers and wardens also
  hit with their proper weight now rather than shoving like a zombie.
- **Magma cubes were twice as generous with cream, and small ones dropped
  slimeballs.** A small magma cube should leave nothing at all, and a big one
  gives up its cream about one kill in four.
- **Guardians drop the rare fish.** Both guardian tables end on a small chance
  of a fish, and that pool had never been rolled. Looting also stopped
  multiplying the things it should not: a sheep's fleece, an elder guardian's
  sponge and its tide template.
- **A mace kill says it was a mace.** The death message read like any other
  hit.
- **Flying an elytra into a cliff costs something.** It cost nothing at all
  before.
- **Mobs no longer all fall like a zombie.** A fox lands from five blocks
  unhurt and a horse from six, then takes half of what is left.
- **`/bug #15 <…>` adds to report 15** instead of filing a new one. The note
  form was `/bug re #15`, and the `re` is easy to miss — a follow-up became a
  fresh report with the `#15` still in its text, burying the thread it was
  about. A bare leading number still files a report.
- **A species sweep for a backlog that cannot despawn.** `-cull-species` is
  one-time maintenance: name a species and its wild members are dropped from
  the saved mobs at boot, keeping anything tamed, named or carrying gear.
- **Nothing spawns naturally outside the world border.** The border was read for the damage it does you and nothing else, so shrinking it hurt you at the
  wall while mobs carried on appearing past it.
- **A current carries mobs, not just the things you drop in it.** A river moved
  dropped items and left every mob standing still.
- **A water bucket fills the block you click.** Pour it on a slab, stair or
  fence and it waterlogs that block instead of putting water down beside it.
- **Mounts walk over what they should.** Step height was left at the default
  for every mob; a camel's 1.5 is why it strolls across a fence a horse has to
  jump, and since the attribute is what the riding client reads, it is the
  difference you feel in the saddle.
- **Tall mobs need room to stand where they spawn.** The spawn test asked for
  two blocks of headroom whatever the mob was, which is right for a zombie and
  wrong for an enderman at 2.9 blocks — so every two-high cave pocket was an
  enderman spawn site vanilla would have refused. Since an enderman holding a
  block never despawns, each of those became permanent; the live world had
  collected 198 of them.
- **You can sit your cat with something in your hand.** It insisted on an
  empty one; vanilla only excludes a dye and its food.
- **A pet runs after you instead of blinking.** The follow goal acts on a
  ten-tick clock and the engine was re-deciding five times as often, so a cat
  that fell behind teleported every time rather than being seen to run.
- **Walking out of the End sends you home**, to your own bed or charged
  respawn anchor if it still stands, instead of dropping you at the world
  origin. (It reads the anchor without spending a charge — a walk out is not
  a death.)
- **A grindstone stays on the wall** when the wall behind it goes, and a
  parrot sets off after its owner from five blocks rather than ten.
- **An anvil lies across your look**, not facing you, the way vanilla places
  it; a calibrated sculk sensor's amethyst face was a full 180 degrees out and
  read its side signal from the wrong block.
- **Nothing spawns on top of the Nether** any more — a column whose roof was
  exposed could put a mob above the ceiling, standing in the void.
- **Hit one zombified piglin and the pack comes for you.** They used to get
  angry and then stand there: nothing ever told them who had done it.
- **A creeper you duck away from stands down.** Its fuse watched only the
  distance, so a wall made no difference and it went off anyway.
- **Slimes and magma cubes float.** They were walking along the bottom of
  ponds. So was the creaking.
- **A mob in armour is worth more experience**, as it should be — a skeleton
  in full iron paid the same as a bare one.
- **Six mobs get the attack damage they are meant to have**, and the giant
  loses the hostile behaviour it was never supposed to have: vanilla's giant
  has no goals at all and simply stands where it is put. The illusioner also
  stops speaking with a pillager's voice.
- **A composter works with hoppers** — one above feeds it, one below empties
  it, which is the whole composter farm.
- **A conduit works for creative players**, and an end gateway's cooldown
  belongs to the gateway rather than to each player.
- **A guardian shows its spikes, and a freezing skeleton shivers.** The
  guardian's spines never moved, however faithfully they reflected damage, and
  a skeleton turning into a stray in powder snow gave no sign until the
  crackle at the end.
- **The dragon has eight hitboxes, not one.** Only the head takes a blow
  whole; everything else is worth a quarter, which is the shape of the fight.
  Before this the dragon was a single box that arrows could barely find.
- **A note block plays through the game's own signal.** It sent a sound and a
  particle where the real game sends an event the client plays the note from —
  so the block never bobbed.
- **A pale garden turns in a wave.** Eyeblossoms opened one at a time, each on
  its own clock, so a garden took minutes to change with nothing tying the
  flowers together. Each flower that turns now wakes the ones near it, sooner
  the closer they are, and they wake their own neighbours in turn. The flower
  that started it plays the long sound and the wave plays the short one, which
  is how the real game does it. A flower in a pot keeps the same hours too —
  it never switched at all before.
- **Walking fast no longer carries you through a hazard.** Block contact —
  magma, a cactus, a berry bush, a wither rose — was sampled once a second for
  everyone, and a sprint could cross a hazard between two samples without ever
  being checked. Players are checked every tick now.
- **The wither fires blue skulls, and its skulls explode.** A wither skull
  never detonated at all, and the blue skull did not exist — so the boss had
  one shot where it should have two. A side head that has been idle too long
  always fires blue and the middle head does so once in a thousand shots, and
  a blue skull goes through obsidian that shrugs off everything else. Bedrock
  still stands.
- **A shulker box opens as a shulker box.** It opened as a plain chest, which
  is not only a matter of the picture: the shulker screen is where the game
  decides a shulker box cannot go inside a shulker box. With the wrong screen
  it could, by hand or by hopper, and a whole base could be packed inside a
  base.
- **Spawners and end gateways say what they are doing.** A spawner sends the
  signal that restarts the little turning mob each time it fires, and a
  gateway the one that draws its beam when it takes someone. Neither did.
- **An elytra cannot be flown from a seat.** Riding anything, or being under
  Levitation, ends a glide in the real game; here it carried on.
- **A glass bottle fills by looking at water, not by clicking it.** It wanted
  a clicked block, which is not how the real game does it: the bottle follows
  where you are looking, out to arm's reach, and will not fill from the
  flowing edge of a stream.
- **A boat can be put on the water you are looking at.** Boats only went down
  on a clicked block, so aiming at open water — the one thing everybody does
  with a boat — did nothing at all.
- **The world no longer hitches twice a minute.** Every thirty seconds the
  server pauses to save, and it was doing the whole job on the thread that
  runs the game: with nobody online at all, a save cost two and a half ticks'
  worth of time. A profile of the running server put a third of it in
  formatting the save files and a fifth in checking whether they had changed —
  none of which needs the game to stand still. The game now takes only the
  snapshot and hands the writing to a worker. Saving on shutdown still waits
  for the disk, because that one has to.

## 2026-09-20

### Added
- **Villagers are worth levelling up again.** Four professions — armorer,
  weaponsmith, toolsmith and fletcher — reached master tier with nothing to
  sell but raw materials, because the one trade type that puts enchanted gear
  in a villager's window had never been implemented. A master weaponsmith now
  sells an enchanted diamond sword, an armorer the enchanted diamond armour,
  a toolsmith the enchanted tools, a fletcher the enchanted bow and crossbow
  and a fisherman the enchanted rod. Each is enchanted when the villager
  unlocks the tier, from the same set the real game draws on, and priced by
  how good the roll was.
- **Cartographers sell explorer maps.** The ocean monument, the woodland
  mansion and the trial chamber had no map to them, which in the real game is
  the only way to find one without swimming until you trip over it. A
  cartographer now sells maps to all three, and — at journeyman — maps to the
  villages and swamp huts and jungle temples of the biomes it did not grow up
  in. The map is drawn the moment the trade appears, centred on a real
  structure near the villager, and costs emeralds plus a compass.
- **The rest of the trade book.** Every kind of offer the real game gives a
  villager now exists: a leatherworker dyes the leather it sells (one dye,
  sometimes two, sometimes three, blended); a farmer sells the six suspicious
  stews; a fletcher's master trade tips arrows with a potion drawn from
  everything a brewing stand can make; a fisherman buys the boat its home
  biome calls for; and the trades that ask for an item beside the emeralds —
  cod for cooked cod, gravel for flint — work as trades rather than as gaps
  in the list.
- **The wandering trader stocks what it should.** Its enchanted iron pickaxe
  and its bottle of Invisibility were missing, and three of the things it
  buys could be sold to it only once, for double experience, instead of twice
  for single. It also no longer shows an experience bar it never fills, and
  it will not wander off while you have its window open.
- **A pig with a carrot on a stick actually goes.** Steering a pig or a
  strider worked, but the stick never did anything when you used it: the
  mount now sprints for a rolled few seconds, easing in and back out, at the
  cost of seven points off the stick — and a worn-out stick becomes the
  fishing rod it was made from.
- **An elytra wears out.** Gliding cost nothing at all. A wing now takes a
  point of damage for every second in the air and gives up at its last point
  — unusable rather than destroyed, exactly as the real game leaves it — and
  the flight ends there instead of carrying on for free.
- **Powder snow turns a skeleton into a stray.** Stand one in the snow for
  seven seconds and it starts to change; fifteen seconds later it is a stray,
  keeping its gear, its name and its refusal to despawn. Pulling it out stops
  it.
- **Guardians bite back.** Melee a guardian while its spikes are out and you
  take two points for it, elder or not — and, as in the real game, an archer
  never pays that price.
- **Experience comes out in orbs, and they pile up.** Every award used to be
  one orb worth the lot. It is now paid out in the same denominations the real
  game uses, and orbs lying together merge into one that pays out over several
  touches — so a farm no longer leaves hundreds of motes lying around.

### Changed
- **A powered door moves both halves.** A lever or a pressure plate beside one
  half of an iron door opened that half and left the other standing shut — the
  door was open and closed at once. Both halves now answer the signal, the way
  the real game reads it, and the sound is the door's own instead of an iron
  door's for everything.
- **A piston no longer stamps a redstone signal into the ground.** A sculk
  sensor pushed while it was listening, or a target still registering a shot,
  arrived in its new cell still powered — permanently, because the tick that
  would have cleared it stayed behind.
- **Adventure and spectator players cannot change the world.** Both could mine
  and build here. Neither can now; using a block — a door, a button, a lever —
  still works in adventure, as it should.
- **A blocked container stays shut.** A chest with a solid block over it, or
  a cat sitting on the lid, opened anyway; so did a shulker box with no room
  for its lid, and a large chest with one half covered. All of them now
  refuse, and a barrel still opens under a block — which is the whole reason
  to build one.
- **Shift-clicking a crafting result crafts the lot.** It handed over exactly
  one result per click, so a stack of logs took sixty-four clicks to become
  planks. It now repeats until the grid runs out or your inventory fills.
- **Covered farmland goes back to dirt.** Placing a chest or a slab on a
  tilled row left it tilled forever, still growing wheat under the floor. A
  carpet or a passing piston still leaves it alone.
- **The pale garden keeps the right hours.** Eyeblossoms opened and closed on
  a hand-made window that was wrong at both ends, and the creaking woke on the
  same wrong clock. Both now switch on the real game's times, and an
  eyeblossom taken to the Nether keeps whichever face it went in with.
- **Composters and note blocks can be heard.** Neither emitted the vibration
  the real game does, so a sculk sensor or a warden was deaf to a composter
  being filled or a note block played by redstone. A muffled note block still
  makes no sound and no vibration.
- **A converted mob keeps its name.** Every conversion — a zombie drowning, a
  pig struck by lightning, a villager hit by a witch's curse — quietly threw
  away the name tag you spent on it and put the mob back on the despawn
  clock.
- **Death messages say what killed you.** Starving, falling out of the world
  and withering away all read "<name> died", along with ten other ways to go
  that had no message at all. Every one now reads what the real game reads,
  word for word, including the forms it uses when somebody is to blame — "was
  slain by X using Excalibur", "walked into a cactus while trying to escape
  Zombie" — and a creeper is credited for blowing you up instead of you simply
  blowing up. Landing on a stalagmite is also its own kind of harm now rather
  than a fall with a different name, so it goes through armour and a shield
  the way it should.
- **Potions last exactly as long as they should.** Durations were written out
  in whole seconds where the real game counts ticks, so a strong potion of
  Poison ran 21 seconds instead of 21.6 and strong Regeneration 22 instead of
  22.5. The whole table is taken from the game now, tick for tick, and the
  length carries through a tipped arrow's eighth and a splash potion's falloff
  the same way.
- **Every injury has its own sound.** Drowning, burning, freezing and a sweet
  berry bush each make the noise the real game makes for them; before, one
  sound covered everything.
- **Difficulty changes what hurts you, and by how much.** The difficulty used
  to multiply a hostile mob's bite and nothing else, so an explosion, an
  arrow, a fall or a lava bath did the same damage on Peaceful as on Hard,
  while a zombie punching a villager was scaled when it should not have been.
  Damage now scales where the real game scales it — as it reaches a player,
  according to what kind of damage it is. Easy no longer simply halves
  everything either: a small hit comes through whole and a big one is softened,
  which is the real formula. On Peaceful, the four things that always scale —
  explosions, a bad respawn point, a warden's boom — do nothing at all.
- **Villagers level up like villagers.** A trade that promotes one no longer
  promotes it on the spot: the tier lands a couple of seconds later, with the
  glow the real game shows, and one trade is worth one tier however much
  experience it paid. The two trades a tier unlocks are drawn at random rather
  than in table order, and a librarian's enchanted book is one of the two
  instead of a third offer nobody else gets.
- **Restocking follows the day, not the bed.** A villager whose bed was broken
  — or that never claimed one — stopped restocking for good after its first
  two. Its trading day now turns over on the clock, catching up the restocks it
  missed, and the day's budget survives a restart instead of resetting to full.
- **Firework rockets go off with a bang.** A rocket packed with stars did
  nothing to anyone, which is most of the reason to load one into a crossbow.
  It now deals the real game's damage — five plus two a star, at full strength
  to whoever it is boosting and softened by distance for anything else within
  five blocks — and a wall between you and the burst still saves you. A plain
  rocket with no star in it stays perfectly safe to fly with.
- **The server stops writing itself out when nothing is happening.** An idle
  world was rewriting four player files from scratch every thirty seconds,
  with nobody online to have changed them — enough to cost a visible hitch
  twice a minute. It skips them when the server is empty now, and any save
  whose contents would come out identical to the file already there no longer
  touches the disk at all. One of those files was also being written twice in
  the same pass.
- **You can sleep through a thunderstorm.** A bed refused you by the clock,
  so a storm at noon was no help. The real game does not check the clock — it
  checks how dark the sky is, which a thunderstorm darkens past the threshold
  and ordinary rain does not quite. Same nights as before, to the tick; storms
  are new.
- **An enderman only takes a block it can see.** The real game makes one
  check its line to a block before lifting it; ours did not, so an enderman
  could reach a block buried in a hillside beside it or on the far side of a
  wall. That matters more than it sounds: an enderman holding a block never
  despawns and stops counting towards the monster limit, so every extra
  pick-up is one more permanent enderman and one more slot for the spawner to
  fill. (Legion reported endermen crowding up, which is what sent me looking.)
- **Spawn eggs work.** Right-clicking a block with one did nothing at all —
  eggs only worked on a monster spawner or fired out of a dispenser — so in
  creative there was no way to place a mob. The mob now appears on the face
  you clicked, standing inside a flower or a tuft of grass if that is what you
  clicked, and it stays put rather than despawning the way a wild one does.
  (Reported in game by Legion.)
- **A mace's Wind Burst is a real gust.** It had been a bare upward shove on
  whoever swung. Now the smash sets off the same burst a wind charge makes,
  centred on the attacker — it throws back everything within three and a half
  blocks and flips the levers and buttons it washes over, which is what the
  enchantment is in the real game.
- **Raids arm themselves as the waves go on.** Every pillager and vindicator
  in a raid carried plain gear, whatever the omen that called it. From the
  fourth wave a pillager's crossbow may come with Quick Charge — and draws
  faster for it — and a vindicator's axe comes sharpened, harder still after
  the fifth. How likely that is rises with the Raid Omen level, so a bad omen
  carried in at full strength now brings a genuinely worse raid.
- **Riptide is a weapon, not just a ride.** Launching yourself with a trident
  flung you across the water and did nothing to whatever you flew through. The
  first living thing you pass through now takes the real game's eight damage,
  and that ends the spin — one strike, not a drill.
- **Four enchantments do the rest of what they promise.** Bane of Arthropods
  slows what it bites to a crawl, which is half of what it is for. Impaling
  bites sea life when the trident is in your hand, not only when it is in the
  air. Soul Speed cancels soul sand's drag outright rather than partly paying
  it back. Looting makes a mob a little likelier to drop what it was wearing,
  a percent a level. Frost Walker works wherever there is water to walk
  on, instead of only in the overworld — and stops working while you ride. And
  a bookshelf only counts towards an enchanting table when the gap between
  them is genuinely empty, where a torch or a tuft of grass used to do.
- **Five more game rules do something.** A nether portal can be switched off
  entirely, the time you must stand in one before it takes you is settable
  (separately for creative), arrows and tridents can be stopped from breaking
  what they hit, and the sounds the real game plays to a whole dimension — a
  wither waking up, an end portal opening — now carry to everyone in it, from
  the right direction, rather than only to whoever was standing close.
- **Fire follows the real game's rule, not an invented one.** The engine kept
  a `fire_ticks` switch of its own making; the real game replaced that switch
  in 1.21.9 with `fire_spread_radius_around_player`, a distance. Fire now
  spreads and burns out only within that distance of somebody — 128 blocks by
  default — so a forest nobody is standing in does not quietly burn down while
  the server is empty. Setting it to 0 is the old "off", and -1 is "anywhere".
- **A villager with nothing to sell says so.** Right-clicking one whose stock
  is gone opened an empty trade window; it shakes its head now, and a sleeping
  villager is left alone.
- **Prices react per trade, the way the real game's do.** Armour, bells,
  shields, saddles, explorer maps, dyed leather and most enchanted gear
  respond four times harder to heavy use and to your standing with the
  village than everything else does — exactly the trades a player grinds.
  Every offer used to react at the gentler rate.
- **Potions look like what they are.** Every brew rendered the same default
  purple and its tooltip said nothing about its effects, so a potion of
  Healing and a potion of Poison were indistinguishable in the hand. Potions
  and tipped arrows now carry their effects to the client, which tints the
  liquid from them exactly as the real game does — Swiftness cyan, Harming
  red, Turtle Master its muddy violet — and lists each effect with its
  duration. A thrown potion now bursts in its own colour too, instead of a
  water splash. Two smaller stacks-on-the-wire gaps close with it: a
  suspicious stew now carries the effect it hides (which creative mode is
  meant to reveal) and a repaired item carries the anvil's prior-work
  penalty.
- **A full shulker box shows what is in it.** Picking a loaded box up gave
  you an item that looked exactly like an empty one — the contents were
  kept, and came back when the box was placed, but nothing said so until
  you put it down again. The box now carries its contents to the client, so
  the first few appear under its name in the tooltip the way they should.
- **Decorated pots can be made, and they wear their sherds.** The pot had no
  crafting recipe at all — the block existed, the sherds existed, and there
  was no way to put one together. Four sherds or bricks in a diamond now make
  a pot that wears them on its four sides, in the order the grid reads; the
  faces show on the item, on the placed block, survive a restart, and come
  back on the pot when it is mined.
- **Answering a bug report no longer means filing another one.** `/bug re
  <what you want to add>` adds to the last report you filed, and `/bug re #3
  <…>` adds to that one, so a question and its answer stay in one thread.
  `/bug list` shows the last five with who filed each and where it stands —
  open, answered if somebody wrote back, and how much the reporter added
  since. Both asked for in game by LegionZA.
- **A compass points at spawn.** The server never told clients where the
  world's spawn point was, so they fell back to their own assumption of the
  world origin and every compass in the world pointed at 0,0 — wherever
  spawn actually was.
- **Spawners show what they spawn.** A mob spawner is a cage with a little
  version of its mob turning inside it — that is how you know a dungeon is a
  skeleton dungeon before anything walks out of it, and how a fortress
  throne reads as a blaze spawner. Every spawner in the world was an empty
  box, because nothing ever told the client what was in it. Now it does,
  including after a spawn egg changes one.
- **Fireworks have stars.** A rocket was three gunpowder-and-paper and
  nothing else: it always flew the same short hop and always burst as
  nothing. The gunpowder now sets the flight duration it was always meant
  to, and firework stars are craftable — gunpowder and dyes give a burst its
  colours, a fire charge, feather, gold nugget or any mob head give it its
  shape, glowstone dust makes it twinkle and a diamond gives it a trail, and
  a second pass with more dyes sets the colours it fades to. Up to seven
  stars go into one rocket, and the rocket carries every one of them to the
  client — on the item AND on the rocket in flight, which is what actually
  paints the burst in the sky, so what goes up is what you built.
- **A raid captain's bottle keeps its own level.** The bottle stores its Bad
  Omen level in the same place a potion stores its brew, so it now goes out
  with its own component instead of being mistaken for whichever potion
  happens to share that number.
- **Reports get answered.** A reply can be sent to a player in game, and if
  they are not online it waits and arrives the moment they next join —
  which is the usual case, since whoever answers a report is rarely at a
  keyboard at the same time as whoever filed it. An answer tied to a report
  is recorded against it, so the list shows what has been dealt with.
- **`/bug` files a report with the world attached.** Typing
  `/bug <what went wrong>` records not just the message but the fifteen
  blocks around you — every block and the properties that differ from its
  default, plus where you were standing, which way you were looking, what
  you were holding and what was nearby. A description like "pistons next to
  my dust line do nothing" is guesswork to act on; the same report with the
  build attached can be rebuilt exactly and turned into a test. Reports are
  kept beside the world and served on the internal health endpoint.
- **Village children play tag.** Baby villagers ran the adults' daily
  schedule, which had them standing at workstations they cannot use. They
  now play the game vanilla gives them: a child picks one of the other
  children it can see and runs after it, a child being chased drops its own
  quarry and bolts somewhere else in the village, and whoever is already
  being chased is the one the rest pile onto — up to five at a time. They
  still run from a zombie first, and still go to bed at dusk.
- **Tab completion past the command name.** The command tree the client is
  sent described every command as a name followed by one opaque run of
  text, so pressing tab after `/gamemode ` offered nothing and every
  argument you typed looked equally valid. The commands worth describing
  now carry their real grammar: `/gamemode` lists the four modes,
  `/gamerule` lists every rule and whether it takes true/false or a number,
  `/weather`, `/difficulty`, `/time`, `/whitelist` and `/effect` list their
  sub-commands, and the arguments that take a player, an item, a count or a
  position are typed as such — so tab fills in player names and item ids,
  and the input turns red before you press enter rather than after. Only
  the argument types that mean the same thing on every version tachyne
  serves are used, so a 1.21.5 client and a 26.3 client read the same tree.
- **The Warden's four set pieces.** It simply appeared, chased, and
  blinked out of existence. Now a warden a shrieker calls rises out of the
  ground over six and a half seconds before it does anything; it casts
  about sniffing when somebody is near but nothing has provoked it, and
  gets angry at whatever that turns up within six blocks; it rears up and
  roars for four seconds when it fixes on you, and only comes for you when
  the roar ends; and left alone for a minute it spends five seconds
  burrowing back into the ground instead of vanishing. Each one roots it to
  the spot for the length of the animation, exactly as long as vanilla's.
- **Target selectors and relative coordinates.** Commands took a player
  name and absolute numbers and nothing else, which made half of them
  awkward and some useless. `@s`, `@p`, `@a`, `@r` and `@e` now stand
  anywhere a name did — with the predicates people actually type,
  `type=`, `distance=`, `limit=`, `sort=` and `name=`, including a
  negated `type=!` — so `/kill @e[type=zombie,distance=..16]` clears the
  monsters around you without touching anybody's pets, and
  `/gamemode spectator @a` works. Coordinates take vanilla's `~` for
  relative and `^` for local, measured along the way you are looking, so
  `/tp ~ ~10 ~` lifts you ten blocks and `/summon creeper ^ ^ ^5` puts
  one five blocks in front of your face. `/tp <player>` finally takes you
  to someone instead of only to numbers.

### Fixed
- **Kelp, vines and dirt paths obey the rules they should.** A handful of
  blocks had no survival rule at all, so they simply hung in the air when
  whatever held them was taken away: kelp and twisting vines growing up,
  cave vines and weeping vines hanging down, and frogspawn floating with no
  water under it. Each now needs what the real game asks for. A dirt path
  with a block set on top turns back into dirt, as it should, rather than
  staying a path forever — a fence gate on top still doesn't count. Bells
  and big dripleaf stems learned their rules too: a bell hung between two
  walls falls if either one goes, and a stem needs both the ground below and
  the rest of the plant above.
- **Chickens lay the egg they should.** A chicken from a warm biome lays a
  brown egg and one from a cold biome a blue egg; every chicken here had
  been laying the plain white one, even though the server already knew which
  kind of chicken it was.
- **Breaking a powered block lets go of its neighbours.** Mining a lectern,
  a sculk sensor, a target, a block of redstone, a shelf, a trapped chest or
  a playing jukebox left whatever it was powering still switched on, because
  the list of blocks that announce their own removal had drifted away from
  the list of blocks that can give power. The two are pinned together by a
  test now, so a new source cannot be added to one and forgotten in the
  other. Same family as the lever bug LegionZA reported.
- **A jukebox powers redstone while it plays.** Vanilla jukeboxes are a
  redstone source in their own right — a full signal for as long as the disc
  runs, which is what note-block contraptions and disc-triggered doors are
  built on. Only the comparator reading of *which* disc was in it worked
  here. A wind charge also flips levers and presses buttons in the Nether
  and the End now, not just the overworld; that restriction outlived the
  reason for it.
- **A kick tells you why.** Being kicked put a line in chat and then dropped
  the connection, so what you actually read was "connection lost". The
  reason now appears on the disconnect screen, where the real game puts it.
- **`/title` works.** The big words across the middle of the screen were the
  one part of the player-facing display the server could not drive at all.
  `/title <who> title|subtitle|actionbar <text>`, `times <fade in> <stay>
  <fade out>`, and `clear`/`reset` — vanilla's own shape, with tab
  completion.
- **The dragon, the wither and a raid each get their own boss bar.** All
  three were drawn the same purple bar, so the only way to tell which fight
  you were in was to look at what was hitting you. The dragon's is pink now,
  with the boss music and the fog it brings; the wither's is purple and
  darkens the sky; a raid's is red and notched into ten for its waves.
- **Falling in makes a splash, and landing hard makes a thud.** Entering
  water was silent for players and mobs alike — the game registered it (a
  sculk sensor could hear it) but nothing played. So was every landing that
  hurt, however far the drop. Both are back, with the real game's own rules:
  the splash is as loud as the speed you go in at, a player gets their own
  heavier splash for a proper dive, and a landing past four points of damage
  gets the big thud rather than the small one.
- **It stays light under water and under trees.** Light was being charged
  twice over for anything you can see through: each block of water or leaves
  took two levels off instead of one, so a pond or a canopy went dark at half
  its real depth — and since hostile mobs spawn in the dark, they were
  spawning where they should not. Both now cost exactly what the real game
  charges.
- **Hitting a mob shoves it as hard as it should.** Knockback was landing at
  half strength: the game measures it in blocks per tick, mobs here take a
  step every second tick, and that number had been stored as the per-step
  one. A hit now moves a mob about a block, the way it does in the real game.
  Two smaller things went with it — the mob keeps half its own momentum
  rather than having it thrown away, so something walking into your swing is
  turned rather than reset, and the little upward hop is vanilla's exact
  ceiling instead of a shade under it. Reported in game by LegionZA.
- **A broken lever no longer leaves the line it powered switched on.** A
  lever mounted on a block powers that block, and dust on the block's far
  side reads the power straight through it — so the dust a lever actually
  drives can sit two cells away from the lever. Breaking the lever told only
  the six cells touching it to look again, so that dust kept its signal
  forever and whatever it fed — a lamp, a piston — stayed on with nothing
  powering it. Removing or placing any signal source now re-checks
  everything the block it hangs on was driving. Lines already stuck that way
  are cleared too: dust keeps its power in its own block, so a bad signal
  survives a restart, and the server now asks every powered dust cell and
  every piston in the world to work itself out once at startup. Reported in
  game by LegionZA.
- **Lichen stops sprouting where two pieces meet.** The connector that
  joins a fence to its neighbour matched any block with north, east, south
  and west switches — which is also how glow lichen, vines, sculk veins,
  resin clumps, chorus plants, fire and the mushroom blocks name their
  faces. So placing one lichen beside another "connected" the two, and each
  grew a vertical face with nothing behind it. A real connector has no up
  switch, which is what tells them apart.
- **Lichen and vines already in the world get repaired.** Fixing the
  placement only helped blocks put down afterwards; the ones already
  standing kept every face they had been given, including the ones facing
  open air. A sweep at startup now trims each multiface block to the faces
  something actually holds and removes any left holding nothing — which is
  the update vanilla would have run on the next neighbour change. It does
  not guess: a face with a block behind it stays, wherever it came from.
  Naturally generated lichen and vines were never affected, because
  worldgen builds them from the vanilla default rather than the state the
  engine was using.
- **Pistons work in the Nether and the End — and stop rewriting the
  overworld.** The entire piston path read and wrote the overworld whatever
  dimension the piston stood in: the structure resolver looked up overworld
  blocks to decide what to push, the moving cells and the blocks they land
  as were written to the overworld, the drops fell there, and the record of
  what each cell was carrying was filed by position with no dimension at
  all — so a piston in the Nether quietly rewrote whatever was at the same
  coordinates in the overworld, and two pistons at matching coordinates in
  different dimensions shared one animation.
- **Particles reach the dimension they happened in.** Every particle
  effect the engine sends — crits, splashes, a ravager's roar, a mace
  smash, an exploding crystal, bee nectar, squid ink, a furnace minecart's
  smoke — was addressed to the overworld whatever dimension it actually
  happened in. Since viewers are filtered by dimension, that meant nobody
  in the Nether or the End ever saw one, while a player standing at the
  matching overworld coordinates got a burst out of nowhere. A wind burst
  even carried an explicit "only in the overworld" guard to suppress the
  wrong particle rather than send the right one; it sends the right one
  now.
- **Squid ink and minecart smoke were the wrong particles.** Particle ids
  shift as new versions insert particles ahead of old ones, and the table
  that renumbers them for each client was maintained by hand — four the
  engine emits were not in it at all, so squid ink, glow squid ink, smoke
  and a furnace minecart's large smoke came out as whatever particle
  happened to hold that id on the client. That is every client but the
  oldest. The table is now generated from the vanilla registries and covers
  all 114 particles, so the next one added is right without anyone having
  to notice.
- **A decorated pot puffs dust when you put something in it.** Seven motes
  off the rim, as vanilla does — the particle that told you the pot took
  the item was missing, leaving only the sound.
- **Glow lichen, vines and sculk veins stop growing a face into thin air.**
  A newly placed multiface block started from a state that already had
  every one of its six faces switched on, and placement only ever added
  one more — so a patch put on a floor came out as a full cube of lichen,
  with the sides that had nothing behind them hanging in the air. Vanilla's
  default has no face set at all, and now so does ours. The same wrong
  default was behind bone-mealing glow lichen across a wall, vines
  spreading, and the resin a wounded creaking heart bleeds onto its tree.
- **Pillager patrols actually patrol.** A patrol spawned and then ambled
  on the ordinary wander goal, so it never crossed the country and never
  arrived anywhere. The captain now picks a point up to five hundred
  blocks away and leads the patrol toward it in ten-block legs, handing
  each leg to the pillagers with it — which is what makes a patrol turn up
  at a village, or on the road behind you. A patrol that loses its last
  companion stops being one, and a patrol no longer despawns while you are
  within a hundred and twenty-eight blocks of it.
- **A raider that strays comes back to its raid.** A wave only advances
  once every raider in it is dead, so a pillager that spawned wide of the
  village, or wandered off after losing sight of you, could stall the raid
  until it timed out. Raiders with nothing to fight now walk back to the
  raid they belong to, and gather any idle illager, ravager or witch they
  pass within sixteen blocks into it on the way.
- **Blocks stop flickering when you place or break them.** Since 1.19 a
  client applies your dig or placement immediately, tags it with a
  sequence number, and then shows its own guess for that position -
  ignoring the server - until the server acknowledges the sequence. The
  gateway was answering that acknowledgement itself, the moment the packet
  arrived and before the world had seen it, so the block snapped back to
  what the client last knew and only then flicked to what the world
  actually decided. The sequence now travels to the world and is
  acknowledged after the resulting changes have been sent, which is the
  order vanilla uses.
- **The offhand works again.** The frame that says which hand you used was
  being decoded as an empty one on the way into the engine, so every use
  was treated as the main hand however the client had sent it - a shield
  in the offhand never raised. Reported in the same session as the
  placement bug above.
- **Trial-chamber mobs wear the armour the loot table describes.** Two
  loot functions were dropped when the tables were baked: the one that
  sets an armour trim and the one that sets more than a single
  enchantment. An ominous trial spawner's zombies and skeletons therefore
  came out in plain, unenchanted chainmail. They now arrive trimmed in
  copper and wearing Protection, Fire Protection and Projectile
  Protection IV together, exactly as the chamber intends.
- **A torch sticks to the wall you highlighted.** Placement picked the
  attachment face from the way you were looking and ignored the face you
  had actually aimed at, so a torch put against a dirt wall could jump to
  the floor or to the block beside it, and a lever would only take a wall
  if you stood square to it. The highlighted face now leads the search, as
  it does in vanilla, for every block that attaches to a surface — torches,
  levers, buttons, signs, ladders, tripwire hooks, vines and glow lichen.
  Only a placement that replaced what it landed on (into tall grass, into
  snow) still goes purely on your look direction.
- **Redstone dust links up the moment it is laid.** A dust's connection
  shape was only recomputed when its power level changed, so a fresh line
  of unpowered dust stayed a row of unconnected dots until something
  switched it on, and dust that gained or lost a neighbour kept the old
  shape. Neighbour changes now re-link the dust straight away, powered or
  not.
- **Amethyst clusters drop four shards to a pickaxe.** The drop table asks
  for a tool from a tag, and tags were not resolved, so a fully grown
  cluster paid two shards whatever you mined it with. Mining one with any
  pickaxe now gives the full four (and Fortune applies), while breaking it
  by hand still gives two.
- **Smelting pays what the recipe says.** Every furnace result banked the
  same experience — seven tenths, or a third for food — so a stack of
  ancient debris was worth no more than a stack of cactus. The cook
  tables now carry each recipe's own experience, and a furnace hands over
  exactly that: two for netherite scrap, one for green dye and gold, seven
  tenths for iron, a third for a steak.
- **Seven advancement criteria come off the blocked list.** They were
  marked unobservable when the mechanics behind them did not exist, and
  the note outlived the code: the trident's channeling, sliding down a
  honey block, an allay putting an item on a note block, picking up what
  you threw, riding what you saddled, and sneaking past a sculk sensor
  all have working fire sites. Piglins picking up gold they love now fire
  theirs too, so distracting one counts. Only the wolf-shearing and spear
  criteria remain unreachable.
- **The coat advancements need the coat.** Taming a single cat completed
  all eleven criteria of the cat catalogue, taming one wolf finished the
  whole pack, and leashing one frog finished all three variants — the
  generator that bakes the advancement table never read the variant out
  of the predicate's components, so every criterion matched any animal.
  Each criterion now names its coat, and the tame, breed and leash hooks
  pass the animal's own.
- **The dragon's wings throw you.** Contact with it dealt a flat eight
  wherever it touched you. Vanilla has two: the body and head deal ten,
  and the wings deal five with a hard sideways shove — the hit that
  sends you off the island. A perched dragon still deals nothing, which
  is what makes the head safe to attack.
- **The F key works.** Swapping the held item with the off-hand did
  nothing — the action arrived and was dropped. It swaps the two stacks
  whole now, enchantments, damage and all, and shows on every client.
- **Potion effects survive a relog.** They lived only in memory, so
  logging out threw away whatever you were carrying — a brewed potion, a
  beacon's gift, a conduit's — and coming back gave you nothing. Active
  effects now save with the player, amplifier, remaining time and the
  ambient flag included, and are pushed to the client on the way in.
  Effects also tick in every game mode, as vanilla's do: night vision
  runs out in creative rather than lasting forever, while the periodic
  damage and healing stay a survival matter.
- **The trial-chamber effects work on mobs too.** Wind Charged, Weaving,
  Oozing and Infested only ever did anything to players. In vanilla they
  belong to any living thing: a mob that dies wind-charged leaves the
  gust, one that dies weaving strings cobwebs where it fell, one that
  dies oozing splits into slimes, and an infested one bursts silverfish
  whenever it is hurt. An ominous trial's mobs now do all of that.
- **The sword sweep reaches other players.** It clipped mobs standing
  beside your target and passed straight through players, which made
  Sweeping Edge worth nothing in a fight between people. It now hits
  every living thing beside the target, players included, when the PvP
  gamerule allows it.
- **Tipped arrows are an eighth as long, and netherite shrugs off a
  hit.** A tipped arrow handed over the full bottle's duration — eight
  minutes of poison from a single shot; vanilla gives an eighth of it,
  and now so does this. And netherite armour finally carries the
  knockback resistance that is the whole reason to wear it: a tenth per
  piece, four tenths for the set, eating its share of every shove.
- **The mason sells stone again.** A stonecutter villager was offering
  iron and chainmail armour, a shield, a bell, a clock and a name tag —
  another profession's stock entirely. The generator that bakes the trade
  table ran the last profession's section to the end of the file and
  swallowed the trade-rebalance tables that follow it, so the mason
  inherited the experimental armorer's and librarian's offers. It now
  sells what it should: clay and bricks, stone and chiseled stone bricks,
  granite, andesite, diorite and their polished forms, dripstone, quartz,
  every terracotta colour glazed and plain, and quartz blocks and pillars.
  (The same fix restored every "villager buys X" offer the generator had
  been dropping for want of a cast in the reference's formatting.)
- **Hit an iron golem and it hits back.** A golem only ever went for
  players the village had a grudge against, so one you attacked yourself
  simply took it — and, because it is not a hostile mob, the engine had
  it panic and run. It now does what vanilla's does: whoever strikes it
  becomes its target for half a minute, reputation or no reputation.
- **Soul torches keep piglins off, and piglins avoid their own undead.**
  Both of vanilla's avoid rules were missing. A piglin or brute now backs
  away from soul fire, soul torches, soul lanterns and soul campfires
  within eight blocks — which is what those blocks are for, and what
  bastion paths are built out of — and gives a zombified piglin six
  blocks of room, keeping its distance for five to seven seconds after
  it sees one.
- **The Warden holds a grudge.** It chased whoever happened to be
  nearest. It now keeps vanilla's anger tally, one score per suspect: a
  disturbance it hears within sixteen blocks adds thirty-five, an arrow
  ten, a blow a hundred, and it goes for whoever it is angriest at — so
  the player who keeps still is not the one it comes for. The tally ebbs
  a point a second and everything it notices resets its burrow clock, so
  staying quiet really does send it back into the ground. Its sonic boom
  now throws you the way vanilla's does, hard along the beam and a little
  upward, instead of an ordinary melee shove.
- **Auto-crafters make what a crafting table makes.** The crafter ran a
  reduced recipe matcher, so it could not tip arrows, dye armour, make
  firework rockets or transmute a shulker box — recipes a crafting table
  has always handled, and which vanilla's crafter handles too because
  they are ordinary recipes to it. It runs the same resolver now. The map
  recipes stay out: they mint a new map when a player takes them, and a
  crafter has no player.
- **Two statistics start moving, and a broken tool snaps.** The
  Statistics screen's "dropped" and "broken" columns never counted
  anything. Dropping a stack counts it now, and a tool that wears out
  counts once — and makes the snapping sound everyone nearby expects to
  hear, which the engine never played.
- **Impaling bites the sea, not the rain.** A trident enchanted with
  Impaling hit anything standing in water or rain harder — the pre-1.17
  rule. It now bites what vanilla marks sensitive to it: turtles,
  axolotls, guardians, the fish, dolphins, squid, tadpoles and nautiluses,
  wet or dry. A rained-on zombie takes the plain damage.
- **Mob heads and turtle scutes exist.** A charged creeper's blast now
  leaves behind the head of whatever it killed — a creeper, skeleton,
  wither skeleton, zombie or piglin — which is the only way to any of
  them in survival, and the trick the trap is built for. And a turtle
  that grows up drops a scute, the one source of them in the game: sea
  turtle helmets were unobtainable without it.
- **Phantoms circle and swoop.** One simply hovered over you and bit. It
  now flies vanilla's attack: a circle of five to fifteen blocks around a
  point ten to thirty above its target, held for eight to twelve seconds,
  then a dive to your level with the swoop cry, and back up to circle
  again. Losing sight of you ends the dive.
- **Stranded animals head back to their element.** A dolphin or a fish
  left on the sand, and a strider walked off its lava, stayed where they
  were and died there. Each now makes for what it needs — water within
  twelve blocks, lava within eight — which is why a strider you lead out
  of a lava lake turns straight back to it.
- **Turtles head for the water, and hatchlings run for it.** A turtle
  caught on land simply milled about. It makes for water within
  twenty-four blocks now, and a hatchling does it at twice the pace,
  which is what gets a clutch off the beach and away from whatever is
  waiting there. A grown turtle that has drifted more than sixty-four
  blocks from the beach it was born on turns and swims home now and
  then, as vanilla's does.
- **Villagers stand still to trade, and take the bed you built them.** A
  villager kept walking its schedule while its trade screen was open, so
  the shop wandered off mid-deal; it stands and faces its customer now,
  as vanilla's trading sink does. And a villager with no bed — one born
  without one, or one whose bed was broken — looks for a free bed within
  sixteen blocks and claims it, so adding a house to a village houses
  somebody instead of leaving them standing in the square. A bed that is
  broken is given up again, and two villagers never share one.
- **Fish shoal, and dart away from you.** Cod, salmon and tropical fish
  swam as a scatter of singletons and let you swim right up to them. They
  now form schools the way vanilla's do — one fish leads, up to four
  follow it about, and a straggler more than eleven blocks behind breaks
  off and looks for another — and every fish, pufferfish and tadpole
  included, darts away from a player who comes within eight blocks.
- **Animals panic the way each of them does.** Everything but a chicken
  bolted at twice its walking speed, and only ever from a blow. Each
  species now runs at its own goal's pace — a cow and a rabbit sprint, a
  sheep or a pig trot, a llama and a turtle barely hurry, a wandering
  trader actually slows down — and the things vanilla panics them over
  do it: fire, lava, a cactus, a hot floor, freezing, lightning. An
  animal that catches fire makes for water within five blocks rather
  than running anywhere. Wolves and adult polar bears keep their nerve
  when something hits them and run only from the environment (a cub
  still bolts from anything), and goats, armadillos and zombie horses —
  which have no panic goal at all — now stand their ground; a goat that
  is hit no longer turns on you, it rams when it chooses to.
- **The wither fights the room.** Its two side heads never did anything
  of their own: everything it fired went at the player. As in vanilla,
  each head now picks its own victim — any living thing in a twenty-block
  box that is not undead — shoots it on its own clock, and, with nothing
  to shoot at, lobs a skull at a random point nearby, which is what
  hollows the arena out. The wither's main target can be a mob too, so
  one loose in a village fights everything it meets; and a blow arms the
  block-smashing it does twenty ticks later, bedrock and the rest of the
  wither-immune blocks excepted.
- **Illagers behave like raiders.** A pillager charged its crossbow at a
  dead run; vanilla lets it walk at full pace only with an empty one, so
  it now closes at half speed while the bolt is on the string. An evoker
  wandered off mid-spell — it stands to cast now, as its caster goal
  says, and looks for a fight at its own twelve-block range rather than
  sixteen. A vindicator was the one illager that did not keep away from
  a creaking; it does now. And a vindicator named Johnny does what he
  does in vanilla: attacks every living thing in reach, his own kind
  excepted.
- **Skeletons fight at vanilla's distances; ghasts stop chasing.** A
  skeleton backed away from anything closer than five blocks and stood
  still past ten — vanilla's bow goal closes while you are outside
  fifteen, then circles you, drifting out inside seven and a half and
  back in past thirteen. One holding a sword rather than a bow now walks
  in and swings, as the goal it runs depends on what is in its hand, and
  every skeleton kind — wither skeletons included — keeps six blocks
  from a wolf. Ghasts had been given chase, which is not something a
  ghast does: it drifts to a random point within sixteen blocks, turns
  to face what it is shooting at and lobs a fireball from wherever it
  happens to be, and it only targets someone within four blocks of its
  own height.
- **Zombies work a village at night; drowned keep to the water by day.**
  A zombie with nobody to chase milled about wherever it happened to be,
  and a drowned behaved like any other zombie: it would leave the sea at
  noon and chase you up a beach. Both now follow vanilla. A zombie, husk,
  drowned or zombie villager standing in a village after dark walks
  through it, building to building, which is how a horde ends up at the
  doors by morning — and while it is really chasing something it raises
  its arms, the flag every client renders and the engine never sent. A
  drowned only comes for you in daylight if you are in the water with it;
  caught ashore by the sun it heads back to the water, and after dark it
  comes out onto the beach. A drowned with a trident now closes to ten
  blocks and throws from there instead of backing away and circling like
  a skeleton. Hatchling turtles are prey only out of the water, as
  vanilla's selector says.
- **Frogs lay spawn, and pets keep vanilla's distances.** Breeding two
  frogs produced a baby frog out of nowhere. As in vanilla, the pair now
  leaves one of them carrying a clutch: it walks to the bank, lays
  frogspawn on the water beside it, and three to ten minutes later the
  clutch bursts into two to five tadpoles that grow into frogs of the
  local kind. Spawn placed by hand hatches the same way, and draining
  the water under it destroys it. Tamed animals follow at vanilla's own
  distances too: a wolf comes to your heel, a cat stops five blocks off,
  a parrot right at your feet, and any pet left more than twelve blocks
  behind catches up with a blink that lands beside you rather than
  underfoot.
- **Light, not the clock: spiders, daylight detectors, endermen and the
  Warden's warning.** Four mechanics that were reading the wrong thing. A
  spider went neutral by the world clock, so one in a pitch-dark cave
  ignored you at noon and one standing in a lit base hunted you at
  midnight; it now reads the light where it stands, as vanilla's does —
  neutral at light twelve and up, and a spider already chasing gives up
  now and then once it is in the light. The daylight detector followed a
  hand-drawn day curve that ignored the sky above it; it now reads the
  sky light reaching the block, less the time-and-weather darkening, bent
  by the sun's real eased angle, so a roof, a tree or a thunderstorm
  brings it down and an inverted one reads the night properly. Endermen
  hold a staring contest the way vanilla's do: one you have in your
  crosshair stops where it is and stares back, blinks away if you close
  to within four blocks, blinks towards you if you back off past sixteen
  and stop looking, and one caught in the open sun disappears. And the
  warning level that summons a Warden moved to where vanilla keeps it —
  on the player, shared with whoever is nearby, raised at most once every
  ten seconds and fading after ten minutes of quiet — so shriekers count
  together instead of each keeping its own tally, every shrieker response
  now drops Darkness on the room, and the ones before the fourth play the
  growl from below that is supposed to warn you.
- **Decorated pots, brewing stands, banners and the crafting bench keep
  what they are given.** Four benches-and-blocks fixes that each lost
  something. A decorated pot swallowed an entire stack per click, handed
  it straight back to an empty hand, and forgot everything on restart; it
  now takes one item at a time, holds up to a stack of them, rocks and
  refuses anything that does not belong, gives its contents back only
  when it is broken, saves them across restarts, and a hopper can fill or
  empty it. A brewing stand ran an invisible clock: no bubbles, no fuel
  gauge, no bottles on the model, and a restart threw the brew and the
  blaze powder away. It now follows vanilla's own tick — powder swallowed
  on sight for twenty charges, one charge spent to start a twenty-second
  brew, the brew abandoned if the ingredient is swapped under it — and
  the bottle arms, the bubble bar and the fuel gauge all show it, saved
  with the world. A patterned banner dropped plain when broken and now
  comes back with its layers. And the five items that leave a container
  behind — three buckets and two bottles — do: a cake gives back its
  milk buckets instead of eating them. Trading now pays the player the
  experience it should, three to six a trade and five more when the trade
  levels the villager up.
- **Anvils repair with materials, grindstones behave, enchanting offers
  hold.** Three benches that looked right and quietly were not. An anvil
  could only ever combine two of the same item, so a half-worn diamond
  pickaxe could not be mended with diamonds — the ordinary way anyone
  repairs gear. It now takes the item's own material, a quarter of the
  bar back per ingot, gem or plank, one level each and only as many as it
  needs out of the stack; two enchanted books merge into one, which is
  how a library is built; and a rename on its own is capped at
  thirty-nine levels and no longer pushes the item further up the
  prior-work ladder. The grindstone stripped curses along with everything
  else — a curse surviving the grindstone is the whole point of a curse —
  dropped the second item's durability instead of merging it with a five
  per cent bonus, and paid a flat amount of experience. Now curses from
  both inputs stay, durability combines, and the experience handed back
  is what the enchantments it removed were worth to begin with. At the
  enchanting table, a bookshelf only counts when the cell halfway to it is
  open, so a wall between shelf and table no longer powers it, and the
  three offers now come from a seed the player keeps: they hold across
  closing the table and logging out, and are spent only when something is
  enchanted, instead of reshuffling on every open.
- **Wild sugar cane, pumpkins, melons, springs and the stone variants.**
  Five families of feature the generator never placed, and their absence
  reached well past the scenery. With no wild cane there was no paper, so
  no books and no bookshelves; with no wild pumpkins there was no carved
  pumpkin, so no snow or iron golem unless a village grew one. Cane now
  grows two to four tall wherever air meets water on sand or dirt, one
  chunk in six; pumpkins scatter one chunk in three hundred on the grass;
  melons do the same in the jungles. Water and lava springs seep out of
  cave walls — twenty-five and twenty attempts a chunk, the lava biased
  hard toward the bottom, each needing stone above, below and on four of
  its five other sides with exactly one way out — and they are source
  blocks, as vanilla's are, so a cave spring is a water supply rather
  than a trickle that dries up. Granite, diorite and andesite blobs fill
  out the stone between sixty-four and one-twenty-eight and again down at
  the bottom, with tuff below zero.
- **An entity's updates follow the viewers holding it.** Everything the
  server says about a creature, item or orb — its movement, metadata,
  equipment, swings, status flashes, passengers — now goes to exactly the
  players whose clients are holding that entity, as vanilla sends to its
  tracking players, instead of to whoever happened to be within a fixed
  radius. The two could disagree, and the moment they did a viewer who
  could see something stopped hearing about it. Tracking ranges are the
  entity registry's own, clamped by the viewer's render distance and
  measured horizontally as vanilla measures them: four chunks for arrows
  and thrown things, six for dropped items and experience orbs, eight for
  most monsters, ten for most animals and vehicles, sixteen for an end
  crystal. A spawn now carries everything the two-second re-assert carries,
  so an entity that comes into view is right immediately rather than a
  sweep later.
- **Every player is told what they can actually see.** The server now keeps,
  per player, the set of entities their client is holding, and each pass
  spawns what has come into view and removes what has left it — vanilla's
  tracked-entity model. Creatures, dropped items and experience orbs used to
  be announced once, to whoever happened to be near at the time, and never
  retracted: walk away from a cow and it froze on your screen where it
  stood, since nothing was ever sent about it again. Now it is dropped when
  it leaves your view and spawned afresh, in full — attributes, equipment,
  variant, passengers — when it returns. Joining and changing dimension go
  through the same path, which also fixes experience orbs surviving a trip
  to the Nether on the client's side of things.
- **Ghost creatures no longer linger.** A mob removed while every player
  was far away told nobody it had gone: the removal frame was culled to the
  six-chunk interest radius, and the two ways a creature leaves the world
  both happen further out than that — a despawn once the nearest player is
  past 128 blocks, and a chunk's mobs unloading five seconds after the
  chunk leaves the view. Since nothing else ever tells a client to forget an
  entity, every client that had seen the creature kept it: standing there,
  never moving, impossible to hit, because the server no longer had its id.
  Removals now reach every player in the dimension. Additions stay culled
  to the interest radius, as they should be — it is only the goodbye that
  has to travel.
- **Mobs look around, call for help, amble at their own pace, keep to the
  dark and hunt more than players.** Five defaults that vanilla gives
  almost every mob were missing, which left eighty-odd species short of
  their behaviour. Mobs now turn their heads: a cow watches you from six
  blocks, a zombie from eight, a pillager from fifteen, and between glances
  they look about at random — the head turns, not the body, as vanilla's
  look control has it. A blow on a zombie, silverfish, blaze, vex, pillager,
  wolf, bee or panda rouses its neighbours within follow range, and a
  husk's cry carries to zombies and drowned (though never to a zombified
  piglin, the one vanilla excludes by name). An idle amble runs at the
  stroll goal's own speed, so a ravager lumbers at four tenths of its pace
  and a horse at seven. Monsters pick their wandering by how dark it is,
  drifting into the shade rather than out into the torchlight. And the
  target classes below the player are in: zombies and raiders go for
  villagers and iron golems, a drowned for axolotls, an enderman for
  endermites, a guardian for squid, a fox for chickens and fish, a polar
  bear for foxes, and the skeleton and zombie families for baby turtles
  caught on land.
- **The offhand works, and status effects show their icons.** The attach
  frame for a right-click carried no hand, so the gateway's was thrown
  away and everything in the offhand was dead — above all a shield, which
  is where a shield normally lives. The hand rides the frame now: a
  shield raises from either hand, wears from what it stops in the hand
  that held it, and offhand food, buckets and throwables work. Status
  effects never drew their HUD icon either, because the renderer
  hard-coded "show particles" for every one; the frame carries
  MobEffectInstance's own visibility now, so every effect draws its icon,
  a beacon's and a conduit's read as ambient, and an infinite duration can
  be expressed.
- **Damage has vanilla's invulnerability window.** Every source of damage
  landed in full, every time it was applied: standing in fire, in a cactus
  or in a crowd of zombies stacked hits tick after tick, and two blows in
  the same tick both told. Living things now carry vanilla's cooldown —
  for ten ticks after a blow lands, a smaller one does nothing and a
  bigger one lands only its excess over the last, then the window resets
  to twenty ticks. It sits where vanilla has it: after a shield and the
  helmet's share, after a mob's own reductions (an armadillo's roll-up),
  and before armour, resistance and a wolf's barding.
- **The Hunger effect drains twenty times slower.** It applied a whole
  second's exhaustion every tick, so a husk's bite emptied the food bar in
  seconds; it is vanilla's 0.005 per tick per level again.
- **Unbreaking protects armour by the armour formula.** A piece was spared
  wear with the tool's odds (a half, two thirds, three quarters by level)
  instead of vanilla's armour branch — a fifth, 27%, 30% — so enchanted
  armour lasted about twice as long as it should.

## 2026-09-19

### Added
- **Minecraft Java 26.3 clients are served.** 26.3 (released 2026-09-15,
  protocol 777) joins through the 26.2 gateway, which now accepts 776–777,
  with the ingress routing it there. It is a real translation step, not a
  re-use of 26.2's ids: three clientbound packets and one configuration
  packet were inserted (a hundred-odd ids shift), the arm swing became a
  payload-free "punch", most block-state, item, entity, particle and item
  component ids renumbered, and the login and respawn spawn info, entity
  moves, position syncs, particles, animations, sign updates and teleport
  confirms changed shape. The shared library carries the step (packet ids
  from the server's own packet report, the 26.3 tag set and its three newly
  synced registries, body rewriters for every changed layout); a server
  list ping from a served version is now answered with that version.
- **A full vanilla-parity audit, and a scorecard.** Every unit of vanilla's
  server-side surface — block behaviour hooks, block entities and menus,
  items and their components, the entity roster, every mob's AI, recipes,
  loot, advancements, statistics, tags, game rules, enchantments, effects,
  attributes, damage types, brewing, villagers, world systems, worldgen,
  player mechanics, commands, chat and the protocol — was enumerated
  mechanically and graded against the engine as it is today. The summary
  lives in `docs/PARITY.md` (rewritten from the 2026-07 plan into the
  scorecard); the per-unit ledgers stay outside the repo. Headline: of
  about 2,300 gradeable units, roughly half match one-for-one, a third
  exist with a deviation, and a sixth are absent — with most of the
  deviations traceable to a dozen cross-cutting defects listed there.

- **More of vanilla's vibrations and cues.** Sculk sensors now hear mobs
  walking (throttled like footsteps; nothing on the wing), a boat, minecart
  or armour stand set down, a furnace opened, and a player mounting or
  leaving a boat or cart, and a player stepping into water (a splash,
  never a sneaking one). A villager inside a raid sweats now and then, and
  reeling a hooked mob in shows the rod's tug, both as vanilla's cues.
- **The nautilus is a mount.** Tame one with a pufferfish (one try in
  three, as vanilla's), saddle it, and ride it under water, where it dashes
  on the jump key with a forty-tick cooldown and its rider breathes on the
  new Breath of the Nautilus effect the mount grants and refreshes. A
  sneak-click opens its armour screen (the five nautilus armours), fish
  and fish buckets feed and breed it, and it speaks under water in its own
  voice and on land in the other, the young in theirs.
- **Guardians drop what vanilla's drop.** A guardian pays 0–2 prismarine
  shards and then cod (cooked if it was burning), prismarine crystals or
  nothing at vanilla's odds; the elder adds a wet sponge for a player's
  kill and a tide armour trim template one time in five. Before, crystals
  and the template never dropped and the sponge came with every death.
- **The special crafting recipes.** Six of vanilla's dynamic recipes now
  work in the crafting grid: two damaged tools or armour pieces combine
  into one (both remainders plus five percent, curses kept, every other
  enchantment lost), a lingering potion ringed by eight arrows tips them,
  a shulker box or bundle and a dye recolour it with its contents, a
  patterned banner and a blank one make a copy (the patterned one stays),
  paper and one to three gunpowder make three rockets, and a written book
  with book-and-quills makes copies a generation up (the original stays,
  a copy of a copy is final). Armour dyeing, suspicious stew and the map
  recipes already worked.
- **universal_anger.** With the rule on, a provoked neutral mob holds its
  grudge against everyone nearby rather than the one who struck it, and a
  death buys no forgiveness, as vanilla's rule does.
- **The mount inventory closes when it should**, like every other menu:
  when the horse, donkey or llama dies or is left out of reach.
- **Four block clicks.** A ripe sweet berry bush picks by hand (two or
  three berries, back to age one, with vanilla's pick sound), a berried
  cave vine gives up its glow berry, a copper golem statue cycles standing,
  sitting, running and star on a click unless an axe is held (which
  scrapes or unwaxes it as before), and a lone piece of redstone dust
  toggles between its cross and a dot, the dot staying put until something
  connects to it, as vanilla's does.
- **Return to Sender.** A swing at a ghast's fireball or a wind charge no
  longer passes through it: the projectile turns along your look as your
  own shot, with vanilla's no-damage swing sound. A ghast killed by its
  returned fireball counts as your kill by a fireball, which is what its
  Tears music disc and the Return to Sender advancement ask for. Loot
  tables can now ask what struck the killing blow (the damage type's tags,
  the projectile, who was behind it), so the ghast's and turtle's vanilla
  tables run as written. A raid captain's ominous bottle now drops however
  the captain dies, not only from a melee blow, and an ordinary pillager
  drops nothing but that, as vanilla's does (it dropped arrows, and every
  pillager an ominous bottle).
- **Fireballs fly as vanilla's fly.** A ghast's or blaze's fireball, a
  wither skull and the dragon's fireball are self-propelled now: they leave
  slowly, push along their flight every tick and settle toward vanilla's
  top speed on a level line, never dropping like an arrow (a ghast's shot
  reaches you at range instead of falling short), and slow in water; a
  wind charge coasts. Every projectile also moves exactly one velocity a
  tick now — the path sampling had been advancing them half again as far,
  so arrows and everything thrown flew fifty percent faster than their
  speed said — and the path is sampled every half block, so a fast arrow
  cannot pass through a player between samples.
- **Shield decoration.** A plain shield and a banner in the crafting grid
  make the shield carry the banner's layers over the banner's colour, as
  vanilla's recipe does; the shield keeps its own wear, name and
  enchantments, a decorated shield takes no second banner, and the base
  colour rides the wire (translated for every Java version), the ground and
  the save. Bedrock clients see the plain shield.
- **Dispensers sound their launch.** A projectile leaving a dispenser
  (arrows, eggs, snowballs, fire charges, bottles, potions, wind charges)
  plays vanilla's launch sound instead of the plain dispense click.

### Fixed
- **Chest lids open and close for everyone.** Chests, trapped chests,
  ender chests and shulker boxes never sent their opener count, so other
  players saw a lid that never moved (and the open sound reached only the
  opener). Every open and close now sends the block's lid event with the
  number of players inside it, and the open and close sounds — chest,
  ender chest, shulker box, barrel — play to everyone near the block at
  vanilla's volume and pitch.
- **Menus keep vanilla's slot rules on the server.** A modified client
  could put anything anywhere; the choosy slots now refuse what they do
  not take (a furnace's fuel slot wants fuel, the enchanting table lapis,
  a beacon its payment, the armour slots their piece, a horse its saddle
  and barding, a loom banners, dyes and patterns, a smithing table a
  template, result slots nothing) and no placement may exceed the item's
  stack cap. Shift-clicking a stonecutter, loom, smithing table, anvil,
  grindstone or trade result now quick-moves it into the inventory
  (repeating while the inputs last for the stonecutter, loom and smithing
  table) instead of onto the cursor. Hoppers, droppers and comparators
  use each item's own stack cap: eggs stack to 16 and tools to one, not a
  flat 64.
- **Mobs chatter at vanilla's cadence.** Every mob rolled one flat
  one-in-twelve chance a second to vocalise. The roll is vanilla's now: a
  per-tick counter that starts at minus the species' interval after each
  call and grows likelier as it climbs — 80 ticks for most mobs, 120 for
  animals, fish, golems and cats, 160 for guardians, 200 for turtles, 400
  for the horse family, 900 for ocelots — and babies squeak half an
  octave up.
- **Dust carries a signal end to end in one tick.** Redstone dust used to
  pass power one block per tick, so a ten-block line added ten ticks and
  every contraption's timing was off. A dust whose power changes now
  re-evaluates every dust within two blocks at once and the change runs
  the whole line within the tick, on and off, as vanilla's dust update
  does; the components around still respond on the next tick.
- **Lectern clocks and lightning rods drive redstone.** A page turn on a
  lectern pulses its power for two ticks and a comparator behind it reads
  the open page (1 on the first page through 15 on the last, 0 empty); a
  lightning rod struck by a bolt powers for eight ticks. Neither emitted
  anything before.
- **Redstone timing: repeaters keep short pulses, lamps hold four ticks,
  plates and detector rails hold twenty.** A pulse shorter than a
  repeater's delay was dropped (the pending flip was cancelled); vanilla's
  diode tick still fires, so the output comes out one delay long. A
  redstone lamp now goes dark a scheduled four ticks after its power
  leaves (and stays lit if it returns first) instead of at once. Pressure
  plates and detector rails release twenty ticks after the last thing
  stood on them, as vanilla's pressed time has it.
- **Every button and pressure plate works, doors sound like doors, iron
  doors need redstone.** Only the stone and oak button and the stone, oak
  and weighted plates were live; the other twelve buttons and eleven
  plates were inert blocks. All fourteen buttons and sixteen plates now
  work with vanilla's set-type sounds and press lengths (wooden buttons
  30 ticks, stone 20; an arrow presses a wooden button and holds it),
  vanilla's sensitivities (stone plates feel living things, wooden and
  weighted plates feel dropped items and arrows too) and the weighted
  plates' real formula (a heavy plate needs 150 things for full power).
  Doors, trapdoors and fence gates play their open and close sounds
  (wooden, cherry, bamboo, nether wood, copper, iron) to everyone but the
  player who worked them, and iron doors and trapdoors no longer open by
  hand. A hopper takes an item that lands in its own cell even with a
  chest above it.
- **Explosions drop what vanilla's drop, and burning arrows light things.**
  TNT-mined stone and ore dropped nothing: the blast applied the player's
  correct-tool rule and an old drop roller. Every block an explosion
  breaks now rolls its real loot table with an empty tool — stone drops
  cobblestone, diamond ore its diamond — with explosion decay's one-in-
  radius survival per item where the blast decays (TNT's does not by
  default, as vanilla's). A flaming arrow primes the TNT it strikes and
  lights a campfire, as it already lit candles.
- **Mobs have footsteps.** Every walking mob was silent underfoot. They
  now play vanilla's step sounds at vanilla's cadence (one every 1.67
  blocks walked): the block's own sound type at step volume — stone,
  wood, grass, sand, snow, wool and the hundred-odd others, from the full
  per-block table — with a carpet, snow layer or the sprouts and roots a
  mob wades through playing over a muffled copy of the block beneath, and
  powder snow, lily pads and petals playing alone. Fifty species carry
  their own footfall (cows, pigs, sheep, wolves, the zombie and skeleton
  families, piglins, a warden's thud, a camel's sand step, a strider's
  lava step, a baby turtle's shamble); fliers and fish make none, and a
  mob moving through water splashes instead.
- **Eleven species found their voices, mobs look from vanilla's eye
  height, and baby monsters drop their loot.** Husks, drowned (with their
  underwater voice), strays, endermen, witches, blazes, slimes and magma
  cubes (small ones squeak), zombified piglins, iron golems and villagers
  were silent when hurt or killed. Every mob's eyes sat at 0.85 of its
  height; the vanilla per-type table is in (an enderman looks from 2.55,
  a villager from 1.62, a baby from half its adult's), which moves line
  of sight, drowning and every ranged mob's aim. Babies of the monster
  classes — baby zombies, piglins, zoglins — drop loot and pay experience
  as vanilla's do; baby animals still drop nothing, a baby hoglin pays
  experience only.
- **Chest loot keeps its potions, names, horns, stews, ominous levels
  and treasure maps.** Six loot functions were dropped when the chest
  tables were baked, so an ancient city's strong regeneration was a water
  bottle, an outpost's goat horn had no voice, a shipwreck's suspicious
  stew was plain, a trial chamber's ominous bottle was level one and a
  shipwreck's treasure map was a blank map. All six are in the tables
  now: the shipwreck map chest hands out a filled map centred on the
  nearest buried treasure with a red cross on it (the mark is kept with
  the map), and the ominous trial spawner's tipped arrows and lingering
  potions carry their effects.
- **No more four-enchantment cap.** A stack carried at most four
  enchantments, so an endgame sword (sharpness, unbreaking, mending,
  looting, fire aspect, knockback, sweeping edge) could not exist; the
  anvil silently dropped the fifth. Stacks now hold eight, persisted
  alongside the old columns so existing inventories load unchanged.
- **Every block change checks what leaned on it.** Only a player's edits
  used to drop the torches, crops, rails and plants that lost their floor,
  wall or ceiling; the engine's own changes — water washing a cell out,
  a piston, fire, sand falling away, farmland trampling back to dirt, a
  growing tree — left them floating. The support sweep now runs from the
  engine's block setter for every change, as vanilla's setBlock notifies
  its neighbours.
- **Dropped items move as vanilla's do.** Items had no horizontal motion:
  a block's drop appeared at rest in its cell, a toss landed a block and a
  half ahead on the spot, and a stream could not carry anything. Items now
  run vanilla's ItemEntity physics — gravity, air drag, ground friction
  (ice slides, slime grips), walls that stop them, ledges they tumble
  off, a floor that drops them when it is mined out — and flowing water
  pushes them downstream with vanilla's current (the flow vector of the
  cell they are in, at the water scale). A block's drops pop out with
  vanilla's quarter-block offset and hop, so neighbouring blocks' drops
  land close enough to merge into a stack, and a toss leaves from the
  eyes along the look and arcs a block or two out.
- **A felled trunk now rots its canopy.** The leaf-distance recompute
  wrote each leaf's new distance with a setter that never told its
  neighbours, so the wave stopped one leaf in and the canopy stood forever
  after the last log was mined. Every rewrite now schedules the six
  neighbours (vanilla's updateShape tick), so the recompute crosses the
  whole canopy and the random tick rots it from the far edge in. A
  player's block edits in the Nether and the End now schedule their
  neighbours in that dimension too.
- **Redstone dust and cocoa beans can be placed.** The item-to-block table
  pairs items with same-named blocks, and neither item is named after its
  block; both now resolve (dust onto any sturdy top or hopper, cocoa onto
  the jungle log it faces).
- **Torches go on walls, lanterns hang, vines never float.** The torch
  family (torch, soul, redstone, copper) and the coral fans place as
  vanilla's StandingAndWallBlockItem does — by the player's look order:
  down onto a floor for the standing block, otherwise against the first
  wall the look order reaches, facing away from it. Lanterns pick hanging
  or standing the same way (a ceiling holds a hanging lantern; the copper
  lanterns hold too). Vines, glow lichen, sculk veins and resin clumps
  take the first look-order face something actually holds — a second
  vine placed onto a vine joins it with a new face instead of replacing
  it, and a face nothing holds is never placed, so no more half-in-air
  vines. Cocoa faces its log. Levers, buttons and grindstones pick floor,
  ceiling or wall the same way (a lever on the floor faces the way you
  face). A hopper's spout points into the block it was placed against,
  or down from a floor. And the same item stacks into its own block as
  vanilla's does: a slab clicked on its open half (or placed into a cell
  already holding one) becomes a double slab, candles, sea pickles and
  turtle eggs count up to four (not while sneaking), snow piles a layer
  when clicked from the top.
- **Flowing water washes out what vanilla's does.** Water and lava used
  to stop at any block that was not air or grass, so a crop, torch, dust
  line or carpet dammed a stream. Flowing fluid now enters any cell whose
  block does not block motion (vanilla's canHoldAnyFluid, with its
  exceptions: doors, signs, ladders, sugar cane, portals; waterloggable
  blocks stand, since the engine keeps no flowing water inside them),
  and water drops the washed block's loot as it goes — a flooded wheat
  field pays its seeds. The replaceable set is vanilla's too: dead bushes,
  seagrass, vines, glow lichen, resin clumps, roots, sprouts, leaf litter,
  hanging roots and single-layer snow are overwritten by any placed block,
  falling block or fluid.
- **Redstone, fire, rails, plates, tripwires, dispensers and comparators
  work in every dimension.** The block simulation read and wrote the
  overworld whatever dimension the block was in: a lever in the Nether did
  nothing there, and a scheduled update at Nether coordinates could rewrite
  overworld blocks at the same position. The simulation now runs in the
  block's own dimension (the scheduled update's, the clicking player's, the
  cart's, the dispenser cell's, the removed block's), with lecterns usable
  outside the overworld too.
- **Hordes of cows near spawn.** Every restart used to seed three small
  "herds" of cows around the origin for something to see on join, and since
  mobs persist, each rollout added nine to fifteen more — the cattle crowd
  Wesley found on the live world. The boot seeding is gone, along with the
  non-vanilla sampler spawner and its periodic animal top-up: vanilla's
  NaturalSpawner is the only source of natural mobs now. A one-time
  `-cull-spawn-cows` pass removes the accumulated wild cows within 160
  blocks of the origin from the saved mobs (tamed and named cows stay).
- **Three drops by vanilla's conditions.** A creeper a skeleton, stray,
  wither skeleton or bogged shoots dead drops one of the twelve music
  discs; a burning sheep's mutton comes cooked; a turtle struck by
  lightning leaves a bowl.
- **Tall grass, large ferns, snow layers and chorus flowers drop right.**
  Their loot tables were outside the generated set, so they fell back to
  dropping their own item. Now shears cut a two-tall plant into two of its
  small kind and a bare hand finds wheat seeds one time in eight (from the
  half that breaks), snow layers give a snowball a layer (the layers
  themselves to shears or Silk Touch), and a chorus flower drops nothing,
  as vanilla's do.

### Changed
- **Natural spawning is vanilla's, biome by biome.** Species, weights and
  pack sizes come from vanilla's own biome data for every biome, the cave
  biomes down a column included (the hand-written family pools remain only
  as a fallback for a biome the data does not name); the chunk-generation
  packs use each biome's own creature probability (badlands sparser, snowy
  plains too); the glow squid is its own capped category; the per-player
  local cap keeps one player's crowd from filling another's range; each
  species' spawn-cluster limit applies (fish and wolves in eights, the
  horse family in sixes, ghasts and pillagers alone); after a pack's first
  animal each further one is born a baby one time in twenty (every rabbit
  after the first; none for wolves, foxes, axolotls or parrots); creatures
  may spawn at any distance inside the spawn ring; the jungle's ocelot
  spawns by its own two-in-three rule though it sits in the monster pool;
  the axolotl category's despawn distance is vanilla's 128, not 64; and
  the census behind the caps skips what vanilla's skips — named, tamed,
  leashed, riding and gear-carrying mobs do not count against them.
- **The Nether and the End spawn as vanilla's do.** The Nether's old pass
  (a species rolled on a ring around each player under a flat cap of
  fourteen) and the End's absence of natural spawns are replaced by the
  same NaturalSpawner as the overworld's, dimension by dimension: each
  nether biome's own pools, weights and packs, the monster cap scaled by
  the chunks around players, the per-player local cap, one attempt per
  chunk per tick at any height, vanilla's per-species rules (a ghast one
  try in twenty, no piglin, hoglin or zombified piglin on a nether wart
  block, magma cubes and blazes in any light, skeletons and endermen only
  in the dark), striders placed in lava with air above and laid as packs
  when a nether chunk first loads, the fortress's garrison pool inside its
  pieces, the soul sand valley's and warped forest's spawn costs (each
  costed mob is a charge that keeps the next one at a distance), and the
  End's endermen in packs of four.
- **Despawning follows vanilla's per-species rules.** Animals never
  despawn, but a wild cat or ocelot does once it has been alive two
  minutes; a jockey's chicken goes with its rider; nautiluses, zombie
  horses and hoglins always; golems, allays, wardens, villagers and traders
  never; a zombie villager only while it is not being cured; a raider never
  inside its raid and a patrol captain only beyond 128 blocks. A leashed
  mob, one riding another, a bucketed fish or axolotl, a tamed nautilus and
  an enderman holding a block are kept whatever the distance.

## 2026-09-18

### Added
- **Item cooldowns shown on the client.** The cooldowns the world keeps — a
  shield disabled by an axe, a goat horn blown, chorus fruit eaten — now
  reach the client as vanilla's cooldown packet, so the sweep is drawn
  over the item (Bedrock included for the groups it animates). A new
  attach frame in the shared library, forwarded by every gateway.
- **Wind charges throw from the hand.** A right-click with a wind charge
  throws it from the eyes along your look, as vanilla's does — the gust
  hurts what it strikes a point and bursts (swinging doors, pressing
  buttons), and the burst throws everyone near it away from the centre by
  vanilla's explosion knockback: burst one at your feet and it launches
  you straight up (the wind charge jump). The breeze's gust reaches three
  blocks, a thrown one 1.2, as vanilla's do. One charge spent, half a
  second before the next. Ender pearls gate on their second of cooldown
  the same way.
- **Jungle temple traps fire.** The temple's two dispensers are loaded from
  vanilla's jungle_temple_dispenser table (arrows) the first time anything
  touches them, so the tripwire fires an arrow instead of clicking on an
  empty dispenser. `/locate structure nether_fossil` finds nether fossils.
- **Villages peopled from their pieces, nitwits included.** A village's
  villagers are now the ones vanilla's jigsaw places (the villagers pool
  hung off each house): most unemployed, one in twelve a nitwit — green
  robe, no trades, never a workstation, a head shake when you try — and
  one in twelve a child. Each claims the nearest free bed. Before, tachyne
  spawned one adult per bed, so villages had rather more villagers than
  vanilla's; a village already populated keeps the villagers it has.
- **Primed TNT moves.** Lit TNT hops up in a random direction, falls
  under gravity, drags in the air and settles on the ground with its fuse
  burning, as vanilla's does, and a blast throws any primed charge in
  reach — so a TNT cannon fires. Its blast goes off at vanilla's height
  on the charge. It sat where it was lit before.
- **Thirteen more game rules.** `/gamerule` now takes freeze_damage,
  spread_vines, spawn_monsters, spawner_blocks_work (dungeon and trial
  spawners), forgive_dead_players (a neutral mob angry at you calms when
  you die), ender_pearls_vanish_on_death (your pearls in flight go with
  you), entity_drops (boats, minecarts, armour stands and item frames
  leave nothing when off), the three explosion drop-decay rules by what
  set the blast off — and, as vanilla's defaults have it, a TNT blast now
  drops every block it breaks while a creeper's or a bed's drops one in
  `power` — max_entity_cramming and respawn_radius (a death with no bed
  puts you down at a random spot within ten blocks of the spawn), plus
  spawn_wandering_traders, and max_snow_accumulation_height (raised above
  vanilla's one, snowfall piles layers up to it). All with vanilla's
  defaults; the old camelCase spellings are accepted.
- **Menus close when they should.** A container's menu now closes from the
  server the moment it stops being valid, as vanilla's does every tick:
  the block broken out from under it, or the player walked out of reach
  (the interaction range plus four blocks) or into another dimension, and
  a trade screen when its villager dies or is left behind. What the
  cursor and crafting slots held comes back. A new attach frame in the
  shared library, forwarded by every gateway (Bedrock included). Menus
  stayed open over a broken chest or from across the map before.
- **Vanilla's entity cues.** Eleven of the one-byte cues vanilla broadcasts
  for clients to animate now fire where vanilla fires them: the death
  cloud when a corpse goes, the bubbles of a drowning breath (players and
  mobs), portal particles at an enderman's or a chorus-fruit teleport, a
  witch's ambient sparkle, a fox's crumbs, the sniffer's digging sound,
  the creaking's shudder, the ravager's roar, a firework's burst from its
  own item, the sniffer's dig, and the happy burst after a trade. Villager
  hearts and happiness now use the villager's own ids (its hearts were
  sent on the animals' id, which draws nothing on a villager).
- **Sculk hears what vanilla's hears.** Sculk sensors (and through them
  shriekers and the warden) heard only footsteps, deaths and blocks placed
  or broken. They now hear the rest of vanilla's vibrations at vanilla's
  frequencies: doors, trapdoors and gates swinging, buttons, levers and
  pressure plates, containers opened and closed, projectiles loosed and
  landing, a landing after a fall (never a sneaking one), eating and
  drinking, explosions and a TNT fuse, note blocks, every hit taken,
  teleports, buckets emptied and filled, lightning, shearing, mounting
  and dismounting, the goat horn, cake, bone meal and an elytra glide.
  A door swung no longer counts as a block placed, and nothing in the
  Nether or End reaches an overworld sensor.
- **Anvils wear out.** One use in eight chips an anvil a stage (anvil,
  chipped, damaged) and a damaged one breaks under the next, as vanilla's
  do; creative use never wears it. The anvil's use and break sounds now
  ride vanilla's level events.
- **Vanilla's world effects.** Fifteen effects vanilla asks the client to
  draw by number now fire where vanilla fires them: the bone meal burst
  with its sound, the smoke and flames of a spawner, a trial spawner's
  spawn burst and item ejection, a vault opening, closing and paying
  out, a turtle egg cracking, the chorus flower's growth and death sounds,
  a bee's crop growth, the dig-out of a brushed block with the block's own
  sound and particles, the dragon's fireball shot, and the puff out of a
  dispenser's face. Some replace hand-made particle bursts; most were
  missing.
- **Villagers yield a workstation.** An unemployed villager on its way to
  a workstation gives it up to a neighbour who already holds that trade
  but lost its own block, and that villager walks there instead, as
  vanilla's do.
- **The outer End.** Chunks carry their biome ring; the highlands grow
  chorus forests and hide return gateways; the small islands' biome floats
  end-stone islands, all on vanilla's placements.
- **Ruined portals by biome.** Buried in the desert, overgrown in the
  jungle, in the mountains' rock, on the sea floor, in the swamps, or half
  underground elsewhere; aged by mossiness, gold pilfered, lava to magma
  or netherrack, on a spread of netherrack with drips beneath, half of them
  mirrored, as vanilla's.
- **Mushrooms, underwater magma, open water in the frozen oceans.** Mushroom
  patches in the nether and under cover on the overworld's surface, magma
  in the underwater caves, and the frozen oceans' temperature modifier
  (patches of open water in the frozen ocean, mostly open water with
  icebergs in the deep frozen ocean).
- **Villager jobs.** Villagers are born unemployed and take the nearest free
  workstation, gaining its profession and trades; a workstation serves one
  villager; a lost workstation costs the job, and the profession too if it
  never traded, as vanilla's do.

### Changed
- **Explosions hurt the way vanilla's do.** What a blast does to whoever
  stands in it now follows vanilla's model: everything within twice the
  power is a candidate, the blast's view of it (rays from a grid over its
  body to the centre, through blocks) scales the damage and the shove — a
  wall shields, a corner half-shields — and the damage is vanilla's curve
  (a TNT blast at your feet is 57, at the edge 1) with the shove from the
  eyes in three dimensions, scaled by Blast Protection. Mobs are shoved
  too. Before, damage fell off linearly to a per-cause cap and nothing
  shielded. TNT, creepers (charged ones at twice the power), the wither,
  fireballs, beds and anchors and TNT carts all use it.

### Fixed
- **Silent mobs speak.** Cave spiders had no voice at all, pufferfish no
  hurt or death sound, the creaking no hurt sound, and turtles, axolotls,
  allays, breezes and sniffers no idle call — their vanilla sound ids do
  not follow their names. Each now uses vanilla's: the spider's voice for
  the cave spider, the pufferfish's own, the creaking's sway, and the idle
  calls that depend on state (a turtle only on land, an axolotl by water
  or air, an allay by whether it holds something, a breeze by ground or
  air, the sniffer's idle). Baby turtles hurt and die in their own voice.
- **Cauldrons on high ground fill in the rain.** The precipitation scan
  started at the ground's own height on any ground above sea level plus
  four, so a cauldron (or the snow already lying) on it was never seen.
- **Ender pearls throw again.** A player's pearl right-click was posted to
  the world and never handled — since the public release nothing flew.
  It is dispatched now, and a test holds every posted event to a handler.


## 2026-09-17

### Added
- **Jungle temples, swamp huts, nether fossils.** The mossy pyramid with
  its tripwire arrow traps, lever puzzle and two chests; the witch's hut on
  stilts with the witch and her black cat at home; and the soul sand
  valley's bone-block skeletons, all as vanilla places them.
- **Lush caves.** Cave vines with glow berries, moss patches on floors and
  ceilings with azaleas, carpet and grass, clay patches and pools with
  dripleaves, spore blossoms, rooted azalea trees and wall vines, on
  vanilla's placements; glow lichen grows in every cave.
- **Dripstone caves.** Dripstone clusters, large dripstone with its wind
  lean, and pointed dripstone spikes, on vanilla's placements.

## 2026-09-16

### Added
- **The nether's biomes.** Chunks carry their biome, and vanilla's surface
  rules dress the caverns: nylium floors in the crimson and warped forests,
  soul sand and soul soil in the valleys, basalt and blackstone in the
  deltas, gravel and a soul sand layer in the wastes. The forests grow
  huge fungi (one in seventeen a giant), roots, fungi, sprouts, weeping
  vine roofs and twisting vines on vanilla's per-layer placement.
- **The nether's ores and features.** Vanilla's nether ores — gold, quartz,
  gravel, blackstone, magma, soul sand, and ancient debris buried away
  from air — plus glowstone blobs, fire and soul fire, basalt pillars,
  lava deltas, basalt columns and blobs, and lava springs. Nether mobs
  spawn by their biome's lists.
- **Snow, ice, icebergs, fossils, silverfish.** Cold water freezes and cold
  ground takes snow layers (vanilla's freeze-top-layer pass, with each
  biome's temperature and the altitude drop); frozen oceans raise icebergs;
  deserts and swamps hide fossils with coal or diamond in their bones; the
  mountains carry infested stone.
- **Surface rules.** Floors follow vanilla's overworld surface rules: packed
  ice and ice on the frozen peaks, powder snow pockets on the slopes and
  grove, calcite in the stony peaks, gravel on the stony shore, bare stone
  and coarse dirt where the surface noise says in the windswept biomes and
  old-growth taigas, terracotta badlands surfaces above y=74, swamp
  puddles, and grass under the snow in the snowy plains and taiga.
- **Bone meal, complete.** Every vanilla target answers: tall flowers,
  grass and ferns, flower beds, sea pickles, seagrass, kelp and the vines,
  dripleaves, rooted dirt, bamboo, propagules, the sniffer's crops, stems,
  huge fungi on nylium, nether vegetation, netherrack beside nylium, moss
  patches, glow lichen, pale moss, bushes and dry grass, and bone meal on
  water seeds seagrass (corals in a warm ocean).
- **Villages grow.** Fed villagers (twelve food points in belly and
  pockets) court within eight blocks and, given a vacant bed within
  forty-eight, bear a child that claims it; parents eat, digest and wait
  five minutes. Villagers chatting share surplus food, a farmer's spare
  wheat, and what a neighbour's trade asks for, as vanilla's do.

## 2026-09-13

### Added
- **Farmers farm.** Villagers carry vanilla's eight-slot pockets and pick
  up the seeds, crops and bread they want; at work a farmer harvests ripe
  crops, sows seed from its pockets on bare farmland, and feeds growing
  crops bone meal when it has some, on vanilla's timings (mob griefing off
  stops it).
- **Zombies stamp turtle eggs.** A zombie, husk or drowned that finds a
  clutch within twenty-four blocks goes for it ahead of anything else,
  stands on it stamping, and after sixty ticks the eggs are gone, as
  vanilla's does (mob griefing off protects them).
- **Axes disable shields.** A melee blow from an axe caught on a shield
  puts it down for five seconds — it cannot be raised and blocks nothing
  until the cooldown is out — as vanilla's does.
- **Leaping.** Spiders, wolves, cats, ocelots and foxes spring at a
  target two to four blocks away — a jump along the line to it that
  lands them on you — as vanilla's do.
- **Mobs float.** The land mobs vanilla floats — animals, villagers,
  creepers, spiders, the illagers — bob at the surface of deep water with
  their eyes clear and swim across it instead of drowning on the bottom;
  the undead, piglins, hoglins and golems sink and walk the bed, as
  vanilla's do.
- **Mob looting by vanilla's rules.** A looting monster weighs what it
  walks over as vanilla does: armour must beat what it wears on points,
  toughness, then enchantments and wear (never over a bound piece), a
  weapon must beat what it holds with the species' preferred weapon first
  (a skeleton keeps its bow over any sword, a drowned its trident), and an
  empty hand takes anything, the whole stack. Replaced gear drops at
  vanilla's odds, picked-up gear always drops on death with its
  enchantments, and mob griefing off stops the looting.
- **Line of sight.** Hostiles hunt by sight: a monster acquires a player
  only when it can see them and gives one up after three seconds out of
  sight (fifteen once that player hurt it), as vanilla's target goals do.
  Every ranged mob now needs to see its target before
  it shoots, as vanilla's do: a skeleton, pillager, witch, drowned, llama,
  snow golem, wither, ghast or blaze holds fire while a block is between,
  and a guardian's beam neither locks onto nor stays on a player behind a
  wall. Before, all of them shot straight through stone.
- **Dolphins play.** A dolphin that sees an item floating within eight
  blocks calls out, swims over, takes it in its mouth and tosses it ahead
  of itself, then goes after it again, as vanilla's do.
- **Water animals out of water.** A fish, squid or tadpole on land lasts
  its fifteen seconds of air and then takes drowning damage, an axolotl
  its five minutes before drying out, and a dolphin dries out after two
  minutes (rain keeps it wet). Under water a dolphin has vanilla's four
  minutes of air, not the fifteen seconds every non-breather had, and
  surfaces to breathe when it runs low.
- **Wololo.** An evoker with nobody to fight casts on a blue sheep within
  sixteen blocks (mob griefing on): forty ticks under the casting arms,
  then the sheep turns red, as vanilla's evokers have always done.
- **Lamb colours.** A lamb wears the dye its parents' dyes would craft
  together — red and yellow give an orange lamb, blue and red a purple one,
  white and black a gray one — and otherwise one parent's colour, as
  vanilla's sheep breed; before, every lamb rolled a fresh spawn colour.
- **Dolphin's Grace.** A dolphin that sees a swimming player within ten
  blocks keeps them company — racing to within two and a half blocks at
  four times its pace — and gives them Dolphin's Grace for five seconds,
  renewed one tick in six while they keep swimming, until they stop or
  draw off past sixteen, as vanilla's does.

### Fixed
- Iron golems no longer attack creepers, which vanilla's golems leave
  alone (they would punch one into exploding beside the village).

- **Fox trust.** A cub born of two fed foxes trusts whoever fed each
  parent (kept across restarts); a fox never runs from a player it
  trusts, and goes for whatever last hurt one of them within sixteen
  blocks, as vanilla's does.

- **Raid witches heal.** A witch in a raid looks now and then for a hurt
  fellow raider within reach and throws it a healing potion (four health
  or less) or one of regeneration, leaving the players alone for ten
  seconds, as vanilla's does.

- **Drawn bows.** Skeletons, strays, bogged and illusioners draw their
  bows for the twenty ticks before each shot and a loading pillager's
  hand is busy — the living-entity flag every client animates the pull
  from, as in vanilla.

- **Hoglin packs; camels on a lead.** A hoglin's bite brings every adult
  hoglin within sixteen blocks onto the same player, and a sat camel led
  more than six blocks stands up, as in vanilla.

- **Pillager crossbows.** With a target within eight blocks a pillager
  stops, draws its crossbow for twenty-five ticks (the charging flag the
  client animates from), holds the loaded bolt for twenty to forty
  ticks, fires and draws again, as vanilla's does, in place of the
  skeleton's bow cadence.

- **Villagers flee.** A villager with one of the hostiles it fears within
  that hostile's range — zombies, husks, drowned, zombie villagers and
  vexes at eight, vindicators and zoglins at ten, evokers, illusioners
  and ravagers at twelve, pillagers at fifteen — runs from it at one and
  a half times its pace to six blocks off, and from whatever hurt it for
  five seconds after the blow, as vanilla's do.

- **Ghast charges; blaze volleys.** A ghast with a target within
  sixty-four charges for twenty ticks — the warning cry at ten, the red
  eyes and open mouth while charged — fires and rests forty; a blaze
  within two blocks bites every twenty ticks, otherwise flares up (its
  charged flag), waits sixty, fires three small fireballs six ticks apart
  on a spread that widens with distance, and rests a hundred — vanilla's
  cadences in place of the old steady shots.

- **Goats long-jump.** Every thirty to sixty seconds an idle goat looks,
  twenty tries, for a spot up to five blocks out and five up or down that
  it cannot simply walk to — a gap or a ledge in the way — crouches for
  forty ticks and jumps it on a steep arc with its long-jump bleat,
  landing with a hoof-step and a fresh cooldown, as vanilla's do.

- **Zoglins and endermen fight mobs.** A zoglin goes for any living
  thing within its follow range that is not a zoglin or a creeper —
  biting for its hoglin damage every forty ticks (fifteen for a baby)
  and tossing what it bites — and an enderman goes for endermites within
  sixty-four blocks, as in vanilla.

- **Cats and ocelots hunt.** A wild cat goes for rabbits and turtle
  hatchlings on land, an ocelot for chickens and hatchlings — looked for
  one tick in ten, chased and bitten every twenty ticks, given up past
  fifteen blocks — as vanilla's do.

- **Wolves hunt.** A wild wolf goes for sheep, rabbits and foxes (looked
  for one tick in ten) and turtle hatchlings on land, every wolf goes for
  skeletons, and a tamed wolf goes for whatever hurt its owner or
  whatever its owner hits — never a creeper, a ghast or a pet of the
  same owner — chasing at full pace and biting every twenty ticks, as
  vanilla's do.

- **Helmets against the sun; phantoms fear cats.** An undead wearing
  anything on its head no longer burns in daylight: the helmet takes the
  sun, a point or none each tick, and burns away before the wearer
  catches fire. A phantom about to swoop checks every twenty ticks for a
  cat within sixteen blocks; the cats hiss and the swoop is called off,
  as in vanilla.

- **Tadpoles grow up; vexes charge and fade.** A tadpole counts its age
  and at twenty-four thousand ticks becomes a frog of the biome's kind;
  a slime ball fed to it takes a tenth of the time left off (the age
  survives a restart). A summoned vex past its life takes a point of
  magic damage every twenty ticks rather than vanishing, and its charging
  flag is raised while it goes for a target, as in vanilla.

- **Bats hang; parrots mimic.** A flying bat under a solid block settles
  one tick in a hundred and hangs there upside down until the block goes
  or a player comes within four blocks, dropping with the takeoff
  flutter; and one tick in four hundred, half the time, a parrot picks a
  mob within twenty and squawks its call if it is one of the thirty-six
  monsters parrots know — as vanilla's do.

- **The iron golem's poppy.** One tick in eight thousand, a golem with a
  villager within six blocks holds out a poppy for four hundred ticks,
  standing still and facing them, then puts it away; its punches now show
  the arm swing on every client, as in vanilla.

- **Sheep graze; striders shiver.** A sheep standing on a grass block or
  in short grass lowers its head one tick in a thousand (fifty for a
  lamb), eats for forty ticks — the grass block turning to dirt or the
  short grass gone — and grows its wool back (a lamb grows a little
  too); wool no longer regrows on a timer. A strider off lava shivers,
  goes purple and walks a third slower, as vanilla's does; both are
  synced for every client.

### Changed
- A sheared sheep's wool regrows only by grazing, as in vanilla (it used
  to come back on a forty-second timer).

- **Shulker shells.** A shulker starts closed — twenty points of armour,
  arrows glancing off — peeks out now and then, opens wide and fires a
  homing bullet every one to five and a half seconds at anyone within
  twenty blocks, closing when they leave; hurt below half health it
  teleports one time in four up to eight blocks onto a floor; and struck
  by one of its own bullets it teleports and, unless shulkers already
  crowd the spot, leaves a new one of its colour where it stood — as in
  vanilla. The shell's opening is synced for every client.

- **Squid ink and endermites.** A squid hurt by something squirts a cloud
  of thirty ink particles and jets away from its attacker at up to three
  blocks a second while they are within ten; a glow squid also goes dark
  for a hundred ticks (synced for every client). One ender pearl in
  twenty lands an endermite, which lives two minutes unless something
  keeps it — as in vanilla.

- **Polar bears guard their cubs.** An adult bear with a cub within eight
  blocks turns on any player within ten; a hit bear rouses every adult
  bear within sixteen (a hit cub only rouses the others and does not
  fight); and a bear about to bite rears up on its hind legs with a
  warning growl, dropping as the bite lands or the target draws off, as
  vanilla's do. (26.2 clients see the rearing once the shared library's
  next pin lands.)

- **Silverfish nests.** A hurt silverfish looks, twenty ticks later,
  through the infested blocks within five up or down and ten across and
  breaks them open one after another (each freeing its silverfish),
  stopping after any with a coin toss; an idle one, one tick in ten,
  burrows into stone, cobblestone, stone bricks (mossy, cracked or
  chiselled too) or deepslate beside it, leaving the block infested — as
  vanilla's do, under the mob-griefing rule.

- **Axolotl behaviour.** Axolotls hunt fish, squid and tadpoles within
  eight blocks (and rest for two minutes after a hunt), always fight
  drowned and guardians, play dead for ten seconds (regenerating) when a
  blow lands under water by vanilla's roll, and grant a player who kills
  their target within twenty blocks regeneration and relief from mining
  fatigue, as vanilla's do; the play-dead pose reaches every client.

- **The wandering trader.** Every twenty minutes the world rolls for one
  (a quarter chance, rising by a quarter each miss to three quarters,
  reset on a spawn) and one time in ten it appears within forty-eight
  blocks of a random player with two leashed trader llamas, nine trades
  drawn from vanilla's three pools, forty minutes to live, drinking
  invisibility at night and milk at dawn. The spawner's clock and chance
  and the trader's time left survive a restart; a new `doTraderSpawning`
  rule switches it off.

- **Illusioner spells and casting arms.** An illusioner with a target
  casts its mirror spell (invisible for a minute — the client draws the
  four illusions — every three hundred and forty ticks) and, on hard,
  blinds its target for twenty seconds every hundred and eighty, never
  the same target twice, shooting its bow between spells; spellcasters'
  arms (evoker fangs and vex summons included) now animate from the
  synced spell id, as in vanilla.

- **Zombification and hoglin AI.** Piglins, brutes and hoglins outside
  the Nether turn after three hundred ticks (the client shows the
  shudder) into a zombified piglin or a zoglin, nauseous for ten seconds,
  unless flagged immune; the timer survives a restart. Hoglins bite for
  half their damage plus a roll and throw the victim up and away, wait
  forty ticks between bites (fifteen for a piglet), are pacified by warped
  fungus, nether portals and respawn anchors within eight blocks and walk
  off, keep eight blocks from adult piglins, retreat at 1.3 for five to
  twenty seconds when outnumbered, and a hurt piglet runs — as vanilla's.

- **Ravager stun and roar.** A ravager's bite pauses it for ten ticks; a
  bite caught on a shield stuns it for forty half the time — after which
  it roars, dealing six to everything within four blocks and hurling
  every mob — or hurls the shield-bearer instead; while stunned, roaring
  or biting it cannot move, and leaves in its path are trampled, as in
  vanilla.

- **Witches brew properly.** A witch drinks water breathing under water,
  fire resistance when burning, healing when hurt and swiftness when her
  target is far (thirty-two ticks a bottle at a quarter less speed, no
  throwing meanwhile), and throws slowness at a distant target, poison at
  a healthy one, weakness now and then within three blocks and harming
  otherwise; the splash lands on everyone within four blocks, weaker with
  distance, as vanilla's does.

- **Breeze AI.** The breeze fights as vanilla's does: it slides to a spot
  behind you (or away, if you are within four blocks), draws breath and
  long-jumps there on a forty-to-eighty-degree arc, lands, and in the
  hundred ticks after a landing inhales for fifteen and fires a wind
  charge — shooting from where it stands when it cannot jump. Its poses
  (inhaling, jumping, shooting) drive the client's animation.

- **The dragon respawn ceremony.** Four end crystals set around the exit
  portal no longer bring the dragon straight back: the portal closes, the
  crystals' beams point at the sky, the dragon growls, the beams sweep
  the obsidian pillars one by one as each goes up in a blast, then return
  to the centre and the crystals detonate — and the dragon is back over
  the island with a crystal on every pillar, as in vanilla. Destroying a
  ceremony crystal aborts it.

- **Dropped items float.** An item under water rises to the surface and
  floats there, an upward bubble column lifts it clear of the water and it
  drops back in to be lifted again (so item elevators work), and a
  whirlpool holds it at the bottom, with vanilla's per-tick figures.

- **Camels sit.** An idle camel that has held its pose for twenty
  seconds folds its legs now and then and gets up the same way later; a
  rider pushing forward, a hit or water under it stands it up. The
  client animates the fold and the rise from the synced pose-change
  tick, and the pose survives a restart, as vanilla's does.

- **Cats lie on beds.** A tamed cat whose owner goes to sleep within ten
  blocks walks to the foot of the bed, watches them for a moment in the
  relax pose, then lies there until they wake — one cat per bed — and a
  tamed cat with nothing to do lies on any bed within eight blocks for a
  while, as vanilla's do. Both poses reach every client.

- **Goat horns and turtle eggs render.** A goat's screaming variant and
  the horns it still has, and a turtle's carried egg and digging pose
  while it lays, now reach every client (26.2 included, its metadata
  shift for both species landed in the shared library).

- **Mobs avoid players at vanilla's figures.** Rabbits (eight blocks),
  foxes, wild cats and ocelots (sixteen) and evokers (eight) keep clear of
  survival and adventure players at vanilla's walk and sprint paces,
  picking a spot on the far side rather than bolting blindly; a tamed cat
  is not shy, nor one following your fish.

- **Mobs avoid the mobs vanilla's avoid.** A skeleton backs off from a
  wolf, a creeper from a cat or ocelot, a spider from an armadillo that
  has not rolled up, a rabbit from a wolf or any monster, a fox from a
  wild wolf or a polar bear, a dolphin from a guardian, the illagers from
  a creaking, and a wild wolf from a llama it judges too strong — each
  at vanilla's range and pace, picking a spot up to sixteen blocks away
  on the far side and sprinting while the threat is within seven.

- **Pandas sit and eat.** A grown panda that spots bamboo or a cake lying
  within eight blocks trots over, picks it up, sits down with it and
  chews — the eat counter and munching sound the client animates from —
  until the food is gone; a bored or hurt panda drops it and waits ten
  seconds to a few minutes before trying again, as vanilla's does.

- **Cats sit on chests, beds and furnaces.** A tamed cat left to itself
  looks every ten to twenty seconds for a chest nobody has open, a lit
  furnace or the foot of a bed within eight blocks, walks onto it and
  settles into the sitting pose for a minute to three, getting up when
  the chest is opened or the furnace goes out, as vanilla's do.

- **Wolves beg.** A wolf that sees a player within eight blocks holding
  a bone or any of its food tilts its head and watches them for two to
  four seconds, as vanilla's does.

- **Skeletons flee the sun.** A skeleton, stray or bogged burning in
  daylight with no target and no helmet looks for cover — a spot within
  ten blocks the sky cannot see and that is dim — and walks there, as
  vanilla's do.

- **Zombies break doors.** On hard, a zombie that spawned with the knack
  (one in ten, scaled by the regional difficulty) and is stopped by a
  closed wooden door on its way to a player beats on it — the thud and
  swing about once a second, the crack overlay stepping through its ten
  stages — and after twelve seconds knocks it off its hinges, dropping
  the door. Iron and copper doors hold; mob griefing off stops it. The
  crack overlay is a new attach frame (block-break progress) rendered on
  every client.

- **Camel dash.** Release the jump key on a saddled camel and it dashes:
  the server plays the dash, raises the camel's dash flag for the
  animation and runs vanilla's fifty-five-tick cooldown, while the lunge
  itself is the riding client's own physics, as in vanilla. The camel's
  metadata now shifts correctly for 26.2 clients (gateways repinned).

- **Hand tools on blocks.** Vanilla's remaining item-on-block behaviours:
  shears cap a kelp, cave-vine, weeping- or twisting-vine head at full
  age so it stops growing; a water bottle turns dirt, coarse dirt or
  rooted dirt to mud and hands the bottle back; a spawn egg used on a
  monster spawner makes it spawn that mob from then on (kept across
  restarts); a firework rocket lit against a block launches from the
  click; and an end crystal placed on
  obsidian or bedrock in the End stands as a crystal — set one two blocks
  out on each side of the exit portal after the dragon is beaten and the
  fight begins again.

- **Banner markers on maps.** Use a filled map on a banner inside its
  area and a marker of the banner's colour is pinned there; use it again
  to take it off. The marker goes when the banner is broken or recoloured,
  rides with a locked copy, and is kept across restarts.

- **Lightning burns; two more comparator readings.** A bolt now sets the
  player or mob it strikes alight for eight seconds on top of its five of
  damage, as vanilla's does. A comparator reads a decorated pot's single
  slot like any container's, and a creaking heart's distance to its
  creaking — fifteen at the heart, fading to one thirty-two blocks out,
  nothing while it is dormant or uprooted — and a copper golem statue's
  pose, one for standing through four for the star.

- **What comes out of a broken block.** Mining a monster spawner pays
  vanilla's fifteen to forty-three experience and the sculk sensors,
  shrieker and catalyst five each; an infested stone block sets its
  silverfish on you unless Silk Touch keeps it whole.

- **Pufferfish puff and sting.** A pufferfish inflates in two stages at
  a player or a mob that comes within two blocks (the calm sea life does
  not count), deflates in two stages once left alone, and while puffed
  stings whatever touches it — one damage plus its stage, poison for
  three seconds a stage, and the sting flash for a player — as vanilla's
  does.

- **Soul sand and honey slow mobs.** A walking mob on soul sand or a
  honey block moves at vanilla's four tenths of its speed; it already did
  for players, whose movement is the client's.

- **Turtles lay eggs.** Breeding two turtles gives one of them an egg
  instead of a hatchling; she carries it back to the beach she came from,
  digs for ten seconds on sand and lays a clutch of one to four. The eggs
  crack twice and hatch on random ticks, mostly in the hour before dawn,
  into hatchlings whose home is the nest; a turtle carrying an egg cannot
  be courted again until she has laid it.

- **Snow golems do their job.** A snow golem leaves a trail of snow
  layers where it walks, throws a snowball every second at the nearest
  hostile within ten blocks — harmless to everything but a blaze, which
  takes three — and melts, a heart every two ticks, in deserts, savannas,
  badlands and the Nether, as vanilla's does. A snowball a player throws
  now hurts a blaze too.

- **Rabbits raid gardens.** A hungry rabbit hops to a grown carrot on
  farmland within sixteen blocks and takes a bite — one growth stage off,
  the whole plant if it was at its first — then is full for a while;
  mobGriefing off keeps them out, as vanilla's RaidGardenGoal has it.

- **Goats ram.** Off its cooldown a goat picks the nearest player or
  animal, walks to a spot four to seven blocks straight out from it,
  lowers its head for a second and charges at three times its walk;
  whatever it hits takes its bite and a shove scaled by its speed, and a
  charge into a log, stone, packed ice or a coal, iron, copper or emerald
  ore snaps a horn off as a goat horn with a random call. One goat in
  fifty is a screaming goat (its own sounds, its own horns, rams every
  few seconds) and one in ten is born with a horn already gone; both
  survive a restart.

## 2026-09-11

### Added
- **Attributes reach the client.** Vanilla syncs a living entity's
  attributes to whoever sees it (on first sight, and when they change);
  tachyne never did, so a horse's rolled speed and jump stopped at the
  server and the client rode every horse alike. A new attach frame
  carries each entity's syncable attributes by canonical name with their
  modifiers, the Java renderer emits update_attributes with the registry
  ids remapped per client version (Bedrock gets its movement and jump
  attributes), and the engine sends them on pairing and once a second
  when something changed. Horses now keep their rolled speed and jump
  across a restart as well.

- **Animals eat their whole vanilla food list.** Breeding used one item per
  species; it now takes everything on vanilla's per-species food tag — a
  pig carrots, potatoes or beetroot, a rabbit a dandelion or golden carrot,
  a fox glow berries, a chicken any seed, a cat or ocelot salmon as well as
  cod (for taming too, and any seed tames a parrot), a wolf every meat and
  fish and a bowl of rabbit stew, a horse golden apples, an axolotl a
  bucket of tropical fish (the water stays in the bucket). Feeding a baby
  grows it a tenth faster, a hurt tamed wolf or cat heals on its food (and
  taming a wolf raises it to vanilla's forty health), the horse family has
  its own table — wheat, sugar, apples and hay heal a hurt horse and grow a
  foal, hay is a llama's love food, a cactus a camel's — and a mount is
  never ridden with its food in hand.

- **Animals follow held food.** Hold a species' food — or the carrot or
  warped fungus on a stick that steers it — and every cow, pig, sheep,
  chicken, rabbit, horse, donkey, mule, llama, camel, goat, frog, bee,
  turtle, panda, armadillo, sniffer, strider, tadpole or axolotl within
  ten blocks trots after you at vanilla's per-species pace (a camel at
  two and a half times its walk, an axolotl slow ashore), stops short
  beside you, looks at you, and loses interest for five seconds once the
  food is put away. Panic still wins, and a baby follows the food before
  its parent, as vanilla's goal order has it. Wild cats and ocelots creep
  up on fish the slow way vanilla has them do it, and within six blocks
  the slightest step or turn sends them off again.

- **Big dripleaves tip.** Stand on a big dripleaf and it turns unstable,
  tips part way half a second later, folds fully half a second after
  that and drops you through, then springs back five seconds on, with
  vanilla's tilt sounds. A redstone signal on any side pins it flat and
  straightens a tilted one, and any projectile folds it at once.

- **Bubble columns.** Source water resting on soul sand becomes an
  updraft a second later and over a magma block a whirlpool, climbing
  through every source-water cell above; mine the block underneath and
  the whole column falls back to plain water. The column is a water
  source to everything else — it spreads, floats, douses and wets as
  water does — and you do not drown inside one. The lift and drag on a
  player are the client's own physics once the block is there; a swimming
  mob is carried up an updraft and pulled down a whirlpool by the server.

- **Sea floors grow things.** Oceans, rivers and swamps were bare gravel
  and sand under the water. They now carry vanilla's sea-floor
  vegetation at vanilla's per-biome densities: seagrass (short in the
  shallow oceans, mostly tall in the deep ones, along rivers and through
  swamps and mangrove swamps), kelp forests where vanilla's low-frequency noise places them,
  each stalk one to ten tall and stopping short of the surface, and
  clusters of sea pickles in one warm-ocean chunk in sixteen, and coral
  reefs in warm oceans — vanilla's tree, claw and mushroom generators in
  all five colours, with coral plants and fans on top, wall fans on the
  sides and the odd sea pickle, at vanilla's noise-placed density; frozen
  oceans stay bare. A seagrass or kelp cell counts as water to mobs,
  spawning, drowning and swimming, as its fluid does in vanilla. Chunks already cached regenerate with the plants
  (generator version 13); player edits are kept as always.

- **Block contact odds and ends.** A burning player or mob standing in a
  water (or powder snow) cauldron is put out and the cauldron loses a
  level; a lava cauldron ignites and hurts whoever stands in it; a ravager
  tramples the crops it walks through; and a falling block — sand, gravel,
  an anvil — sinks through water and lava to the floor, destroying
  frogspawn on the way, instead of coming to rest on the surface.

- **Mobs' weapons are enchanted too.** Spawn gear enchanted armour but
  never the weapon; the main hand now rolls at vanilla's quarter of the
  regional odds, and the enchantments do their work — Sharpness on a
  zombie's sword, Fire Aspect setting you alight, Knockback shoving
  harder, Power, Punch and Flame on a skeleton's bow — survive a restart,
  and go with the weapon when it drops (armour drops keep theirs now as
  well).

- **Halloween heads.** On the 31st of October (the server's local date)
  a quarter of bare-headed zombies and skeletons spawn wearing a carved
  pumpkin, one in ten of those a jack o'lantern, and the head never
  drops — vanilla's one calendar rule.

- **Panda personalities.** The genes now change how a panda behaves, not
  only how it looks: a lazy one lies on its back now and then and gets
  up in its own time, a worried one sits out thunderstorms and keeps
  eight blocks from players, a playful one and every cub tumble along
  their facing — off a ledge for certain, otherwise at vanilla's odds —
  and a weak cub sneezes twelve times as often. Each state rides the
  panda's flags byte, so every client plays the animation, sneeze
  wind-up included.

- **Bedrock: the crafter's menu.** Opening a crafter on Bedrock now shows
  Bedrock's own crafter screen instead of closing again: the grid, the
  result preview, the disabled-slot overlay and the trigger state, with
  slot toggles reaching the world. The overlay and trigger travel as the
  block entity's data on Bedrock, rebuilt from the container properties
  Java uses.

- **Anvils fall, and hurt.** Anvils, suspicious sand and gravel, and the
  dragon egg now fall like sand when nothing holds them up. A falling anvil
  hurts whatever it lands on — two hearts' worth per block fallen after the
  first, up to twenty hearts, sparing creative and spectator players — and
  wears with the fall: a chance of five percent plus five per block chips
  it, then damages it, and a damaged anvil breaks outright.

- **The dragon egg blinks away.** Hit it or use it and the egg teleports
  to an empty cell nearby — up to fifteen blocks sideways and seven up or
  down, nearer cells likelier, inside the world border — as vanilla's
  does. Punching redstone ore now lights it up too, the way using it did.

- **Foods do what vanilla's do.** Every entry of vanilla's consumables
  table now applies: raw chicken (three in ten) and rotten flesh (eight in
  ten) bring thirty seconds of hunger, a spider eye poisons for five
  seconds and a poisonous potato does six times in ten, a pufferfish
  poisons for a minute, starves and sickens, a honey bottle lifts poison,
  and a chorus fruit blinks you up to eight blocks to nearby ground with
  the fall forgiven and a second's cooldown. Only the golden apples had
  their effects before.

- **Suspicious stew.** A bowl, a red and a brown mushroom and one flower
  craft a stew carrying the flower's effect — vanilla's table, from the
  dandelion's moment of saturation to the wither rose's wither and the
  eyeblossoms' blindness and nausea — and a brown mooshroom fed a flower
  gives that stew from its next bowl. The effect is a secret, as in
  vanilla, and it survives restarts on the stack.

- **The recovery compass points home.** The place a player last died is
  remembered with their record, sent with the login and respawn spawn
  info as vanilla sends it, and kept across restarts and dimension
  changes, so a recovery compass points at it on Java and on Bedrock.

- **The rest of the mob interactions.** Shears turn a mooshroom into a
  cow and shed five mushrooms of its colour, take a snow golem's pumpkin
  off (its head shows) and a bogged's two mushrooms once; an iron ingot
  mends a hurt iron golem by twenty-five with the repair clank; flint and
  steel or a fire charge lights a creeper's fuse; and a cookie poisons and
  kills a parrot — every mob interaction vanilla's mobs override now has
  its counterpart.

- **Hostiles spawn dressed.** Vanilla's spawn-time equipment: with the
  regional difficulty (hard worlds, and worlds past their first days) a
  zombie, husk, skeleton, stray, bogged, wither skeleton or piglin spawns
  in armour of a rolled tier from leather to diamond, each piece with a
  chance of enchantment, zombies sometimes with an iron sword or shovel,
  skeletons and their kin with bows, wither skeletons with stone swords,
  and drowned with a trident or a fishing rod at vanilla's odds. What a
  mob spawned wearing drops rarely, as in vanilla; gear rides along when
  a zombie drowns. Lightning now also swaps a mooshroom's colour and
  kills a turtle, as vanilla's does.

- **Armour goes on from the hand.** Using a piece of armour or an elytra
  held in the hand puts it on, as vanilla does: an empty slot takes it,
  a worn piece comes back to the hand (or into the inventory when the
  hand held a stack), a piece under the Curse of Binding stays put, and
  the material's equip sound plays.

- **Jockeys.** A baby zombie has vanilla's one-in-twenty chance of riding
  a chicken — one already nearby, or one spawned for it — and the chicken
  then carries it wherever its hunt goes, lays no eggs, despawns like a
  monster and pays ten experience; one spider in a hundred spawns with a
  skeleton on its back that shoots from the saddle. A ridden pair comes
  back mounted after a restart, and a reloaded mob never re-rolls the
  gear, riders or effects it spawned with.

- **Skeleton traps, strider riders, spider effects.** A skeleton horse
  born of a lightning strike now waits as a trap and springs on the first
  player within ten blocks: a flash, and four skeleton horses with
  helmeted, persistent skeleton riders shooting from the saddle. One
  strider in thirty spawns saddled with a zombified piglin on its back,
  else one in ten with a calf riding it. On hard, a spider may spawn with
  vanilla's lasting speed, strength, regeneration or invisibility.

- **Panda genes.** Pandas carry vanilla's main and hidden genes, rolled
  at vanilla's odds in the wild and inherited one from each parent with
  the one-in-32 mutation; the look follows the main gene unless it is a
  recessive brown or weak that the hidden gene does not match. Weak
  pandas have ten health and aggressive ones bite back. Both genes reach
  every client, Bedrock included.

### Fixed
- **A dropped suspicious stew kept its secret.** A suspicious stew on the
  floor — tossed, or ejected by a crafter — lost its hidden flower and
  landed as a plain one; the dropped item now carries it, the crafter
  crafts the stew the same way its preview shows it, and a stew on the
  floor survives a restart with its flower.
- **Iron golems had ten hearts, not fifty.** The golem is not on the
  species roster and its health fell through to the cow's ten; it now
  carries vanilla's hundred, so a village's golem is the wall it should
  be (golems already in the world keep their old maximum until they are
  replaced).
- **Blocks have a last word when removed.** Vanilla lets a block that is
  being replaced act on its neighbours once more, and the engine did not:
  breaking a powered lever, button, torch or plate left what it powered
  through a wall switched on; breaking a chest left a comparator reading
  it through a solid block on its old value; breaking a piston head left
  the extended base standing, and breaking the base left its head floating.
  Every block write now runs that hook, so the far side of the wall goes
  dark, the comparator drops to zero, the head takes its base with it (the
  piston drops as an item), and a base gone from behind a head takes the
  head.

## 2026-09-09

### Added
- **Ocean ruins.** The 48 vanilla underwater-ruin templates now generate
  on the sea floor, placed as vanilla places them: one site per twenty
  chunks of ocean, sandstone pieces in warm and lukewarm water and
  stone-brick pieces elsewhere (a cracked and a mossy copy of the same
  piece laid over the brick one at falling integrity, which is what gives
  a cold ruin its mottled walls), a large ruin three times in ten that
  usually brings a cluster of four to eight small ruins round it, the
  vanilla decay leaving gaps in the masonry, and a piece standing on the
  ocean floor sinking to the lowest ground under it on a slope. Each piece
  keeps its loot chest (the big and small underwater-ruin tables), seeds
  the drowned its template marks when a player first arrives (persistent,
  and a cleared site stays cleared across restarts), and turns up to five
  of its sand or gravel blocks suspicious, brushing from the real warm and
  cold ocean-ruin archaeology tables. A fed dolphin now swims for
  whichever of the nearest shipwreck or ocean ruin is closer.
- **Trail ruins.** The buried jigsaw structure generates from the real
  templates in taigas, old-growth birch forest and jungle: the tower start
  piece fifteen blocks under the surface with halls, roads, buildings and
  decor attached, the pool processors turning some gravel to dirt and
  coarse dirt and some mud bricks to packed mud, and vanilla's capped
  archaeology rules choosing each piece's suspicious gravel — six common
  and three rare finds in a house, two common in a road or tower top —
  which brush from the real trail-ruins common and rare tables. Every
  archaeology loot table vanilla ships now has a structure seeding it.
- **Desert temples are vanilla's.** The hand-built pyramid is replaced by
  a block-for-block port of the vanilla piece: the stepped sandstone shell
  with its two corner towers, the orange and blue terracotta motifs, the
  pillared hall, the treasure well under the centre with its four chests
  (each facing the pressure plate over the TNT), and the sand-filled
  cellar under the east side reached by a broken stair, where five to
  seven of the sand blocks and one in the collapsed roof are suspicious
  and brush from the desert-pyramid archaeology table. The temple faces
  a random direction and settles on the lowest ground under its
  footprint, as vanilla's does.
- **`/locate structure`.** Operators can ask for the nearest site of any
  generated structure — villages, temples, igloos, outposts, mansions,
  monuments, ancient cities, trial chambers, strongholds, shipwrecks,
  buried treasure, ocean ruins (warm or cold), trail ruins, ruined
  portals, mineshafts, and the Nether's fortresses, bastions and portals
  and the End's cities — within a hundred chunks, answered in vanilla's
  words with the distance.
- **Pistons animate.** A piston no longer teleports what it moves: each
  cell a block is heading for holds a moving_piston for two ticks carrying
  the block, the piston's facing and its direction, and the client draws
  the block sliding into place before the real block lands — the head
  sliding out on extension, the base drawing it back on retraction, and a
  sticky piston catching a block still on its way. Java clients see
  vanilla's animation; Bedrock lays the carried block down at once.
- **Bedrock: warm and cold farm animals.** Pigs, cows and chickens now
  show their warm and cold looks on Bedrock too. Bedrock keeps the
  variant as an entity property rather than entity data, so the gateway
  declares the climate property for the three types at spawn and sends
  each animal's value with it, the way Geyser does.

### Fixed
- **Crafter menu slot order.** The crafter window listed its result
  preview right after the grid, where vanilla's menu puts the first
  inventory slot; the result now sits last (slot 45) as in vanilla, so
  Java clients see the inventory in the right slots and clicks land on
  the items they show.

## 2026-09-08

### Added
- **Shelves.** The twelve wooden shelves of 1.21.9 work on 1.21.11 and
  26.2 clients: three display slots on the front face, a click swapping
  the whole held stack in or out of the column you point at, the stacks
  shown standing on the shelf and kept across restarts, a comparator
  reading a bit per filled slot, and the broken shelf dropping what it
  held. Power one with redstone and it links with powered shelves beside
  it that face the same way, up to three in a row, and a click on any of
  the row then swaps its nine slots with your hotbar in one go — the
  rightmost shelf taking the last three hotbar slots, as vanilla lays it
  out. Clients before 1.21.9 have no shelf and are sent none of this.

- **Dolphins lead to treasure.** Feed a dolphin a fish and it takes a
  bearing on the nearest shipwreck within fifty chunks and swims for it,
  giving the errand up once within four blocks (or at once if no wreck is
  in reach); a calf just eats. Vanilla's dolphins aim at shipwrecks and
  ocean ruins; the engine's oceans hold wrecks.
- **Foxes behave like foxes.** By day, sheltered from the sky and with
  nobody about, a fox lies down and sleeps (and wakes when someone walks
  up); it hunts chickens, rabbits, baby turtles on land and schooling
  fish, creeping in with the crouch before it bites; and it picks up
  whatever is lying within eight blocks, carrying it in its mouth — food
  it eats after half a minute, anything else it keeps and drops when it
  dies. Foxes were skittish wanderers before.
- **Armadillos roll up.** A threat within seven blocks — an undead mob,
  whoever last hurt it, a player sprinting or riding — makes an armadillo
  roll up (ten ticks), stay rolled for as long as the danger keeps being
  seen (checked every four seconds, remembered for four), then unroll
  over thirty ticks. Rolled up it holds still, cannot breed, and a blow
  loses a point and halves; a blow from anything living rolls it up on
  its own. A grown one sheds a scute every five to ten minutes. The
  state reaches 1.21.5 and 26.2 clients alike (the rolled-up model and
  animations are the client's), which needed the common library to learn
  the armadillo-state serializer's number on 26.2.

### Fixed
- **Block entities were mis-typed for every client but 1.21.11.** The
  block-entity type numbers in chunk data and block-entity updates follow
  1.21.11's registry; 1.21.5–1.21.8 clients received every type after
  the shelf (suspicious sand and gravel, decorated pots, crafters, trial
  spawners, vaults) one too high, and 26.2 clients — where beds lost their
  block entity — received everything from conduits on one too high
  (campfires, beehives, sculk blocks, chiseled bookshelves, pots, vaults
  and more). The translation chain now renumbers both, drops the entries a
  client has no type for, and swallows updates for them.

## 2026-09-07

### Changed
- **Pistons move what vanilla moves.** A piston now resolves the
  structure in front of it the way vanilla's does: the line ahead, plus
  everything a slime or honey block in that line is stuck to (slime sticks
  to anything but honey, honey to anything but slime), branching sideways
  and back, up to twelve blocks. Blocks carry their vanilla push
  reactions — glazed terracotta moves only away from the piston, torches,
  plants, redstone and the like break when pushed and are never pulled,
  chests, signs, banners and every other block with a block entity stop
  the piston, obsidian, crying obsidian, respawn anchors and reinforced
  deepslate never move — and a sticky piston pulls a slime chain back
  with the blocks stuck to it. A player or mob standing where a block
  arrives is carried a block along (lifted without fall damage). Before
  this, a piston pushed a plain straight column and slime blocks were
  just heavy blocks. Movement is still instant: there is no moving_piston
  animation yet.

### Added
- **Babies follow a parent.** Calves, lambs, piglets, chicks, foals,
  baby llamas, pandas, polar bear cubs, bees, striders, goats, axolotls,
  armadillos, camels and hoglins now trail the nearest adult of their
  kind, vanilla's way: an adult within eight blocks, followed once it is
  more than three blocks off (five for the brain-driven species) at the
  species' own speed, given up past sixteen. Before, a newborn wandered
  off on its own the moment it was bred.
- **The dispenser table is complete.** Every item vanilla gives a
  dispenser behaviour now has one here: bottles o' enchanting and
  firework rockets fly out, a mob bucket pours its water and its fish
  (or axolotl, or tadpole) and leaves an empty bucket, a chest straps
  onto a tamed donkey, mule or llama in front, a carved pumpkin is placed
  facing the dispenser and builds a snow, iron or copper golem where the
  body is ready, a shulker box is placed with its contents intact
  (opening along the dispense direction, or upward over solid ground),
  glowstone charges a respawn anchor ahead, and a brush combs an
  armadillo for a scute, wearing by sixteen. Brushing an armadillo by
  hand works too — it did not before.

### Fixed
- **Hoppers and droppers respect a container's faces.** A brewing stand
  now takes its ingredient from a hopper above, bottles into empty bottle
  slots and blaze powder as fuel from a hopper at its side, and gives a
  hopper below its bottles but never its fuel, vanilla's slot rules;
  before, a hopper poured anything into the first bottle slot. A furnace
  additionally accepts an empty bucket into its fuel slot and lets a
  hopper below take one back (the lava-bucket loop), and a dropper firing
  into a furnace or stand obeys the same faces.
- **Totem of undying.** A totem in either hand answers a killing blow
  the vanilla way: it is spent, health is set to one, every effect is
  cleared and Regeneration II (45 s), Absorption II (5 s) and Fire
  Resistance (40 s) take over, with the totem animation for everyone
  watching. A `/kill` still kills. Before this the totem was a drop with
  no effect.
- **Barrels are containers.** A placed barrel opens as a 27-slot
  container titled "Barrel", its lid shows open while someone has it
  open, hoppers and droppers feed and drain it, and it places facing the
  way you look, like a dispenser. Before, a barrel was a solid block that
  did nothing.
- "Very Very Frightening" (a channeling trident's bolt on a villager) and
  "Birthday Song" (an allay dropping onto a note block) now fire.
- **Piglins barter.** A gold ingot dropped in front of an adult piglin,
  or held out to it, goes into its off hand for six seconds of admiring,
  after which it throws something from vanilla's bartering table at the
  nearest player — ender pearls, quartz, obsidian, crying obsidian, fire
  charges, soul speed books and boots, fire resistance potions and the
  rest at their weights. Other gold it loves it simply keeps (and drops
  when it dies), a player wearing any piece of gold armour is left
  alone, and hitting a piglin ends its admiring and puts it off bartering
  for twenty seconds. None of this existed before.
- **Sniffers dig.** A grown sniffer picks a scent — reachable diggable
  ground (dirt, grass, podzol, coarse or rooted dirt, moss, mud) within
  ten to eighteen blocks it has not dug before — walks over, digs with
  its nose down for eight or nine seconds, and two seconds in drops a
  torchflower seed or a pitcher pod, then leaves digging alone for eight
  minutes and never returns to its last twenty sites. Until now the
  archaeology chain ended with a hatched sniffer that did nothing.
- **Three small mob habits.** Frogs hunt small slimes and small magma
  cubes and take them with their tongue — a slime eaten this way leaves
  nothing, a magma cube leaves the froglight of the frog's variant (ochre,
  pearlescent or verdant). Panda cubs sneeze now and then, one sneeze in
  seven hundred leaving a slime ball. A tamed cat that is not sitting
  turns up beside its owner at sunrise after a full night's sleep and,
  seven times in ten, drops a morning gift from vanilla's table.
- **Two more counters.** "Damage dealt (absorbed)" and "(resisted)" now
  count from the attacker's side against players and mobs, and a
  chiseled bookshelf's last-touched slot (its comparator reading) now
  survives a restart.
- **Honey blocks slide.** Falling against a honey block's side slows to
  a slide (the client already did the slowing): the fall resets every
  tick so the landing costs nothing, the slide sound plays now and then,
  and "Sticky Situation" is awarded.
- **Allays work.** Hand an allay an item and it becomes your helper:
  it flies to matching drops within thirty-two blocks, gathers them into
  one stack and throws them at you once within three blocks — or at a
  note block it heard played in the last thirty seconds — then keeps
  within a few blocks of you between errands. A jukebox playing nearby
  sets it dancing, and a dancing allay given an amethyst shard splits in
  two (five minutes' cooldown each). An empty hand takes its item back
  along with whatever it collected. It remembers its player and its
  stack across restarts. Allays were decorative before.
- **Pet collars.** A tamed wolf or cat wears a red collar from the
  moment it is tamed, and its owner recolours it with any dye (spent,
  unless it is the colour already there). The colour is synced to every
  client — Bedrock included — and kept across restarts. Collars were
  never shown before.
- **Wolf armour.** A tamed wolf's owner straps armadillo-scute armour
  onto it (not onto a pup), and while it is worn every blow that does not
  bypass wolf armour goes into the armour's durability instead of the
  wolf — cracking audibly at vanilla's thresholds and breaking when
  spent. Shears take it off and drop it, an armadillo scute repairs an
  eighth of it while the wolf sits, it dyes in the crafting grid like
  leather, drops when the wolf dies, and survives a restart. The item
  was a plain drop before.
- **Leather armour takes dye.** A leather helmet, chestplate, leggings,
  boots or horse armour (and wolf armour) crafted with one or more dyes
  takes vanilla's blended colour — the dyes' colours averaged with the
  piece's own and rescaled to their brightness — shows it on every
  client, keeps it across restarts and drops, and loses it again in a
  water cauldron for one level. The engine had no item colours at all
  before.
- **Hoppers reach a chest nobody has opened.** A freshly placed chest,
  barrel or shulker box counts as a container from the moment it is
  placed — a hopper or dropper feeding it, or a comparator reading it, no
  longer waits for a player to open it first.
- **Statistics and advancement triggers filled in.** Swimming distance,
  raid wins, target hits, filling a cauldron (separately from using one),
  washing a dyed shulker box back to plain in a cauldron, and opening a
  barrel now count. The triggers behind "Sneak 100" (a sneaking player
  near a sculk sensor makes no vibration), "Feels like home" (riding a
  strider on lava), "Postmortal" (the totem), "The Healing Power of
  Friendship"-style thrown-item pickups, starting to ride, and tool wear
  now fire.

## 2026-09-06

### Fixed
- **Survival loop, vanilla's small rules.** Peaceful difficulty now heals a
  point of health and saturation every second and a point of food every
  half second, as vanilla's peaceful regeneration does (it drained no food
  before but also never fed you). Exhaustion is capped at vanilla's 40, so
  a long sprint cannot bank more than ten food points of debt, and moving
  through or on water costs a hundredth of a point per block as it does
  on Java. Golden apples, enchanted golden apples, chorus fruit, honey
  bottles and suspicious stew can be eaten at full hunger (vanilla's
  can_always_eat), `/effect give … hunger` works, and a bee that has stung
  stands down for its last minute instead of stinging again.
- **Blocks you stand in and on.** Powder snow freezes: standing in it counts
  vanilla's 140 frozen ticks (the frost creeps over the screen), a fully
  frozen player takes a point of freeze damage every two seconds and
  "froze to death" if they stay, any piece of leather armour keeps the
  cold out, and the open air thaws two ticks per tick; a fall into powder
  snow costs nothing. Hay bales soften a fall to a fifth. Walking on turtle
  eggs cracks one in a hundred steps and a fall onto them one in three,
  sneaking spares them. Redstone ore glows when stepped on and fades on a
  random tick. Landing on a bed halves the fall, honey softens it to a
  fifth, and a slime block catches it whole unless you are sneaking.
- **Candles light and go out.** Flint and steel or a fire charge lights an
  unlit candle, candle cake or campfire in place (a waterlogged one never
  catches, and a fire charge is spent, a flint worn), a dispenser's flint
  does the same to the block in front of it, an empty hand snuffs a
  burning candle, and clicking the cake under a candle eats the first
  slice and hands the candle back. The fire charge now works in hand at
  all: it lights a fire against a block as vanilla's does.
- **Wind charges trigger what they hit.** A wind charge's gust — thrown by
  a player, a breeze, a dispenser or an ominous trial — now bursts where
  it lands and, as vanilla's trigger explosion does, swings the wooden
  doors, trapdoors and fence gates its blast reaches (both halves of a
  door; never iron, never one a redstone signal holds), presses buttons,
  flips levers, rings bells, snuffs candles and sends a hive's bees after
  the thrower. Before, a wind charge only shoved the mob it struck, and
  one from a dispenser flew like an arrow and stuck in the wall. The blast
  ray-cast that TNT and creepers use now serves the gust too, so a wind
  charge reaches exactly the blocks a vanilla one would.
- **Three small block interactions.** Shears carve a pumpkin where it
  stands — the face you click becomes the face, four seeds pop out of it,
  the shears wear a point — where before shears did nothing to a pumpkin.
  A right-click lights a dark redstone ore, as stepping on it does. A
  comparator beside a chiseled bookshelf reads the slot last put into or
  taken from (one to six), vanilla's rule, instead of nothing. And a wet
  sponge placed in the Nether dries out on the spot, the vanilla way to
  reuse one.

### Added
- **Mobs in all their coats.** Wolves, cats, horses, llamas, parrots,
  rabbits, foxes, mooshrooms and the warm/cold/temperate pigs, cows and
  chickens now roll their vanilla variants at spawn — the biome-tag
  rules, the pack that shares one coat, the full-moon black cat, the
  horse herd's colour with markings per animal, the llama's strength —
  inherit them by vanilla's breeding odds, keep them across restarts,
  and show them on every client. The Bedrock gateway renders them with
  Geyser's numbering. Getting there fixed a numbering slip from earlier
  today: the villager-data serializer is 19 on 1.21.5 (18 on 26.2), so
  1.21.5 clients now see villager clothes correctly.
- **Bedrock: crafting.** Bedrock players can craft. The world's recipe
  book is sent to the client as Bedrock crafting data (so its crafting
  screen recognises patterns and lists recipes), the player's 2x2 grid
  and the crafting table's 3x3 render in Bedrock's own crafting UI, and a
  craft — whether laid out by hand or picked from the recipe book, once or
  many times — is turned into the same result-slot clicks a Java client
  sends, so the world itself matches the recipe, consumes the grid and
  hands over the result. The recipe book's auto-craft goes through the
  world's own place-recipe step first, filling the grid from the
  inventory the way the Java book does. Previously every craft request
  from a Bedrock client was refused. The anvil and the grindstone open
  too: their inputs sit where Bedrock keeps them, the result is whatever
  the world previewed, and an anvil rename reaches the world before the
  result is taken. Villagers trade with Bedrock players: the world's
  offers (with their demand and reputation pricing, uses and tier) are
  rendered as Bedrock's trade screen, and picking an offer selects it in
  the world before the goods change hands. The enchanting table works
  too: the world's three rolled rows (cost, hinted enchantment and
  level) become Bedrock's enchant options, and picking one presses the
  same button a Java client does, so the world pays the levels and
  lapis and enchants the item. The stonecutter too: its recipe table
  rides in the crafting data, and a cut picks the world's row before
  taking the result. The smithing table upgrades diamond gear to
  netherite (the upgrade table rides as Bedrock transform recipes) and
  applies armor trims (Bedrock is told every pattern and material and
  the one tag-written trim recipe, and takes the world's trimmed
  preview), and the loom applies a
  pattern: Bedrock's pick becomes the row in the list the world offers
  for the banner and pattern item in hand. The beacon takes its payment
  too: the chosen effects are shown off the beacon's block entity, and
  paying sends the world the same effect choice a Java client does.
  Creative mode has an inventory on Bedrock at last: every item with a
  Bedrock counterpart is listed (blocks under the construction tab), and
  taking, dropping or destroying items becomes the same creative slot
  set a Java client sends — the world still checks the player's mode.
  Bedrock players can also drop items now, from a window or the held
  stack, where before every drop was refused. Fall damage on Bedrock
  now keys off the client's own ground test (its vertical-collision
  input flag) instead of a height-stability guess; sixty-odd more
  particles (flames, smoke, splashes, hearts, notes, portal swirls,
  enchanting glyphs…) reach Bedrock as the named particles Geyser's
  table maps them to; and Bedrock players see each other's real skins
  in the player list instead of a grey placeholder. Riding works on
  Bedrock: the player is seated on the boat or minecart (the world's
  passenger list becomes Bedrock actor links), the ride carries the
  loaded area, and the full key state steers a server-driven vehicle
  the way it does from Java — before, a Bedrock rider sent only sneak
  and the client was never told it was aboard. The cartography table
  opens on Bedrock as well, taking the world's preview like the anvil.
  Books work on Bedrock: a written book opens with its pages, title and
  author, a book and quill can be written and signed, and a lectern
  shows its book and turns pages. Signs too: every sign in a loaded
  chunk shows its text (colour and glow included), placing or editing
  one opens Bedrock's sign editor, and the text written there reaches
  the world. Banners show their patterns on the right base colour,
  campfires show what is cooking, and bells swing when rung. Potion and
  other status effects show on Bedrock, creative mode can fly (and a
  game-mode change mid-session takes effect), it rains and thunders,
  other players swing their arms, picked-up items fly to the player,
  the difficulty and a server-chosen hotbar slot follow, the dragon's
  boss bar shows, and a ridden vehicle snaps back where the world
  puts it. Filled maps draw on Bedrock (vanilla's colours, the player
  and banner markers, explorer-map icons), and scoreboards show in the
  sidebar, list and below-name slots with team colours and prefixes.
  The mount screen opens on Bedrock too: saddle and armour on a horse,
  saddle or carpet and the chest on a donkey, mule or llama. Typing a
  slash on Bedrock now offers the server's command names.
- **Bedrock: advancement toasts and boat woods.** Completing an
  advancement now pops Bedrock's toast ("Advancement Made!", "Goal
  Reached!" or "Challenge Complete!" over the advancement's English
  title) the way it does on Java; the gateway keeps the player's criteria
  from the streamed progress so the toast fires exactly once, and never
  for the join-time snapshot. Boats and chest boats render in their wood
  instead of all as oak. The gateway's table generator now also emits the
  Java entity names and the advancement strings from the vanilla
  language file.
- **Bedrock: the Nether and the End.** Bedrock players can now follow a
  portal. The gateway takes the client through Bedrock's dimension-change
  screen the way Geyser does (parked at the world origin with empty
  columns around it so the screen can finish), then the world's landing
  teleport places the player and the new dimension's chunks stream in.
  Nether and End chunks render at the same absolute height as on Java,
  clipped to Bedrock's shorter 0..127 and 0..255 ranges. Rendered
  entities are dropped on the way through and the world re-adds the new
  dimension's. Previously a Bedrock player stayed put in the overworld
  while the world moved them.
- **Bedrock: mobs look like themselves.** The Bedrock gateway now renders
  every mob look the engine syncs as entity metadata, reading each index
  by the mob it belongs to (Java reuses the same index for a sheep's
  fleece, a pet's sitting flag, a creeper's charge and a frog's variant)
  and writing the Bedrock actor data the Geyser project established for
  each: sheep colour and shearing, pets sitting/tamed/angry, bee stings
  and anger, creeper charge and lit fuse, slime and magma cube size,
  spiders climbing, drowning and curing zombies shaking, villager and
  zombie villager profession, biome and trade tier in Bedrock's
  numbering, frog and axolotl variants (wild and cyan swap places),
  the enderman's carried block, the guardian beam's target, name tags,
  primed TNT fuse, and the shared sprinting, invisible, gliding,
  swimming and sleeping states.
- **Bedrock: container windows.** A Bedrock player opening a chest, barrel,
  shulker box, hopper, dispenser, dropper, furnace, blast furnace, smoker
  or brewing stand now gets the real container screen at the block they
  used, with the container's slots laid out where Bedrock keeps them
  (the brewing stand orders its ingredient and bottles differently from
  Java) above their own inventory. Every move in that screen is
  translated through a per-window slot map into the same window-click
  the Java gateways send, so items move in the shared world, and the
  engine's per-slot updates render back into the open screen; the
  furnace's burn and cook bars and the brewing stand's fuel gauge are
  relayed as container data. Closing the screen tells the world to
  release the container. Menus Bedrock has no counterpart for (crafting
  tables, anvils, enchanting, trading…) are closed straight back instead
  of leaving the world waiting on a screen that never opens.
- **Four mechanics the advancement tree was waiting on.** Each had its trigger
  wired last week and nothing in the world that could fire it; each now works
  the vanilla way, with its advancement.
  - *Copper waxing and scraping, and axe stripping.* Honeycomb on any copper
    block, stair, slab, door, trapdoor, bar, grate, bulb, chest, lantern,
    chain, lightning rod or golem statue waxes it (one comb used, vanilla's
    particle burst); an axe strips a log, wood, stem, hyphae or bamboo block
    to its stripped form, scrapes one oxidation stage off copper, or takes
    the wax back off — in that order, with vanilla's sounds and particles, a
    point of durability each, and vanilla's rule that an axe held with a
    shield in the other hand blocks rather than strips unless you sneak. A
    double copper chest changes as one: wax, scrape or oxidise either half
    and the partner follows, and only the left half ages on its own. *Wax
    On* and *Wax Off* are obtainable.
  - *Mob buckets.* A water bucket on a cod, salmon, pufferfish, tropical
    fish, axolotl or tadpole scoops it up (its own pickup sound, the lead
    drops); pouring the bucket places the water — or boils it off in the
    Nether — and releases the mob, which never despawns again. *The Cutest
    Predator*, *Fishy Business*'s bucket half and *Tadpole in a Bucket* are
    obtainable.
  - *Lodestone compasses.* A compass used on a lodestone locks onto it with
    the lock sound and becomes a lodestone compass whose needle follows that
    block from anywhere in its dimension; a stack yields one lodestone
    compass and spends one plain one. The target is a real item component,
    so it survives chests, drops, restarts and both Java client generations
    (the shared protocol library learned to renumber it per version), and a
    compass in a player's inventory forgets a lodestone that has been
    removed within a second, the needle spinning as in vanilla. *Country
    Lode, Take Me Home* is obtainable.
  - *Pumpkin-built golems.* A carved pumpkin or jack o'lantern placed on two
    snow blocks builds a snow golem; on the iron-block T (arms along either
    axis, air at the shoulders and beside the feet) it builds an iron golem
    — the blocks break away with their particles and the golem stands where
    the foot was. Every player within five blocks earns *Hired Help*.

- **Six species that never spawned now do.** Mooshrooms, turtles,
  armadillos, camels, frogs and axolotls were in the roster but in no spawn
  pool. They now spawn where vanilla puts them, at vanilla's weights and pack
  sizes: mooshrooms alone on mushroom fields, turtles alone on beaches (never
  above sea level plus three), armadillos across the savannas and badlands,
  the rare camel in a desert, frogs in swamps and alone in mangrove swamps,
  and axolotls in lush-cave water over clay — as vanilla's own "axolotls"
  category with its cap of five. Each species keeps vanilla's spawnable-on
  rule (mycelium, sand, terracotta and coarse dirt, mud and mangrove roots,
  clay) rather than the generic grass rule. Frogs come in vanilla's cold,
  warm and temperate variants by spawn biome, axolotls in their five colours
  (one in 1200 blue; a bred one takes a parent's colour), persisted with the
  mob and translated for 26.2 clients — a fix in the shared protocol library
  and both Java gateways, since a frog's variant field renumbers there.

- **The vanilla enchantment engine.** The enchanting table, loot chests, the
  fishing treasure book and the anvil now run vanilla's own selection
  (EnchantmentHelper's cost roll, enchantability bonus and spread, weighted
  pick, and the halving follow-up picks) over every enchantment's real data
  from the 1.21.11 jar: weight, level cap, min/max cost windows, anvil cost,
  supported and primary item sets, exclusive sets, and the table / treasure
  / loot / tradeable tags. In play: a table row's clue is one enchantment of
  a whole selection, a gold sword rolls higher than a diamond one, an axe
  never offers Smite but accepts a Smite book, treasure enchantments never
  come from a table, and the fifteen enchantments no roller ever produced —
  the crossbow and trident sets, Knockback, Sweeping Edge, Density, Breach,
  Wind Burst, Soul Speed and both curses — are obtainable where vanilla
  places them (Swift Sneak in ancient cities, Soul Speed in bastions, Wind
  Burst in ominous vaults, the rest at the table or in loot). The anvil
  applies only enchantments the item supports and its current ones allow,
  drops the rest for a level each, and charges vanilla's per-enchantment
  anvil cost (halved for a book). Librarians sell an enchanted book at
  every tier from one to four the vanilla way — a random tradeable
  enchantment at a random level, priced 2 + rand(5 + 10·level) + 3·level
  emeralds (doubled for a treasure enchantment, capped at 64) plus one
  book, the first trade offer with two costs. A stack now carries up to
  four enchantments (it held two), which is every table and loot roll.

- **Redstone follow-ups.** A trapped chest is now a signal source: its
  strength is how many players have it open, it powers the block beneath
  it strongly (a trapped chest over a block over a lamp is the classic
  alarm), and opening or closing either half of a large one re-evaluates
  the wiring. Redstone torches burn out the vanilla way — eight flips at
  one torch inside sixty ticks leave it dark with the fizz and smoke, and
  it tries again 160 ticks later — so a torch clock or an inverter loop
  no longer runs forever. Dust sits on exactly the tops vanilla accepts:
  full blocks, top and double slabs, upside-down stairs, closed top
  trapdoors and hoppers; a bottom slab or an upright stair drops it.

- **Mobs and potions.** A lingering cloud doses every mob standing in it,
  not only players — a lingering Harming thrown into a horde works — and a
  hostile that picks up a dropped weapon or armour piece becomes persistent
  the way vanilla's does (Mob.pickUpItem sets persistence), so a looting
  zombie no longer despawns while you walk back for it; the flag rides the
  mob store across restarts.

- **Brewing is the whole vanilla table.** The stand brewed six potions from
  an awkward base and nothing else; it now runs vanilla's PotionBrewing
  recipes: every potion with its long (redstone) and strong (glowstone)
  forms, the fermented-spider-eye corruptions (swiftness or leaping to
  slowness, night vision to invisibility, healing or poison to harming,
  water to weakness), mundane and thick, and the container steps —
  gunpowder makes any potion splash, dragon's breath makes a splash potion
  linger. Each bottle brews on its own against the ingredient, and
  durations and amplifiers are vanilla's (Potions.java), so a Potion of
  the Turtle Master is Slowness IV with Resistance III for twenty seconds.

- **Ruined portals in the Nether.** They were overworld-only. The Nether
  now has its own, on the cavern floors above the lava sea: the same
  thirteen templates with vanilla's blackstone processor (the stone-brick
  masonry becomes polished blackstone), aged crying obsidian, and the
  ruined-portal chest loot.

- **Statistics that count.** Only thirty of vanilla's seventy-seven custom
  counters ever moved. The statistics screen now tracks the movement family
  by how you moved (walking, sprinting, crouching, jumping, falling,
  climbing, swimming, walking on and under water, flying, gliding, and
  riding a horse, pig, strider, happy ghast, boat or minecart), the damage
  family (dealt, taken, resisted by armour and magic, absorbed, blocked by a
  shield), every "interacted with" and "inspected" block menu (anvil,
  grindstone, crafting table, brewing stand, dispenser, dropper, hopper,
  ender chest), talking to a villager, triggering a raid, washing a banner,
  potting a flower, and the clocks — total world time, time since death,
  time sneaking.

- **Vines and scaffolding obey their rules.** Both had no survival logic
  at all. A placed vine, glow lichen, sculk vein or resin clump now attaches
  through the face toward the block it was placed on (a placed vine used to
  come out faceless), keeps each face only while that neighbour offers a
  full face — a vine's side face also hangs from the vine above it — and
  drops when its last face goes. Scaffolding carries vanilla's distance
  from support: zero on solid ground, one more per scaffold out or up from
  a neighbour, and the seventh out cannot stand; distance and the bottom
  marker are set at placement and recomputed when a neighbour changes.
  Vines also spread the vanilla way on random ticks — along a wall, round
  a corner, up it or hanging down — until five of them crowd a spot. Sugar cane and cactus follow
  vanilla's placement rules too: cane needs water beside the block it
  stands on, a cactus refuses a solid or lava neighbour and grows only on
  sand or another cactus, and bamboo roots only in sand, dirt, gravel or
  bamboo. Chorus stems and flowers keep vanilla's survival rules too, and
  a stem re-wires its connections when a neighbour changes.

- **Bells ring.** A bell could be placed and hung but never rung. Strike it
  on a proper side (along a floor bell's axis, across a wall bell's, any
  side of a ceiling bell — never from above or below, nor high on the
  block), power it, or hit it with a projectile, and it swings on every
  client with the bell sound at vanilla's volume; the strike counts toward
  the bell-ringing statistic. This rode a new shared frame for block
  events, so the Java gateways were updated with it. A rung bell makes
  every raider within 48 blocks glow for three seconds and resonates when
  it finds one; villagers running for their beds are not yet modelled.

- **Guardian beams.** A guardian or elder guardian now locks onto its
  target the vanilla way — the beam is drawn on every client from the
  synced attack target, charges for the attack duration (four seconds,
  three for an elder) while the target stays in reach, then lands its two
  hits and lets go. Before, the hits landed with no beam and no warning.
- **Villagers hear bells.** A villager within 32 blocks of a rung bell
  goes to its bed and stays there for fifteen seconds (vanilla's hide
  package), so ringing the village bell empties the streets.

- **Chest boats and the chest raft.** All ten woods' chest boats place,
  carry a 27-slot chest that a sneaking click opens in the ordinary chest
  window, spill their cargo when broken, and keep it across restarts.
- **Boats and minecarts survive restarts.** A parked vehicle used to vanish
  with the pod; it is now saved with the world's containers (type by name,
  position, heading, cargo) and put back on boot.
- **Vehicles in every dimension.** Boats and minecarts could only be placed
  in the overworld — a boat item on a Nether lake or a cart on an End rail
  did nothing, and a dispenser there swallowed the item. A vehicle now
  belongs to the dimension it was placed in (by hand or by dispenser), is
  saved and restored there, and is shown only to the players in that
  dimension instead of appearing as a phantom at the same coordinates in
  every world. Detector rails still switch only in the overworld, where
  redstone is simulated.
- **Minecarts roll.** A cart used to move only under a rider, and then
  only because the rider's client said so — which is not how a minecart
  works any more: since 1.21.2 a minecart has no controlling passenger, so
  the server moves it and the rider's client only sends its movement keys.
  The engine now rolls every cart itself with vanilla's classic behaviour:
  gravity, the cart pinned to the rail line through its cell, sloped rails
  that speed it up downhill and slow it uphill, powered rails that push a
  rolling cart to the 8 m/s cap and brake it to a stop when unpowered, a
  resting cart on a live powered rail setting off away from a solid block
  (the classic station), a live activator rail throwing the rider off, an
  empty cart bleeding speed four times faster than a ridden one, and a
  cart off the rails that falls, lands and skids to a halt. A rider's
  forward key gets a resting cart going and the rails take it from there.
  To make that work the player-input frame now carries every movement key
  (it only carried sneak), a passenger's move packets count as camera-only
  as in vanilla, and the gateway follows the ridden vehicle so a long ride
  keeps streaming chunks (boats and mounts benefit too — their riders'
  chunk window used to freeze at the point of boarding).
- **The special minecarts.** Chest, hopper, TNT and furnace carts were
  items that placed nothing. They now place on rails (by hand or
  dispenser), roll on the cart physics and do their jobs the vanilla way:
  the chest cart opens its 27 slots on a click, spills them when broken
  and rolls more freely the emptier it is; the hopper cart sucks up items
  lying on the track or draws from a container above it, hands its cargo
  to hopper blocks beneath, reads on a comparator over a detector rail,
  and a live activator rail switches it off; the TNT cart is lit by a
  live activator rail, a nearby blast or a punch while it is moving, goes
  off when it runs into a block at speed or drops three blocks, blows
  with vanilla's speed-scaled power and leaves the rails and their bed
  standing; the furnace cart takes coal and charcoal (a coal is three
  minutes of push, at most sixteen), sets off away from whoever fed it,
  follows the track at half the usual cap, shows its fire while it burns
  and coasts to a stop when the fuel is spent. None of them can be
  ridden. All of it survives a restart.
- **Carts scoop up mobs.** A plain cart rolling at speed takes aboard the
  mob in its path — not a player, an iron golem or a boss — and carries
  it until the cart breaks or blows up (vanilla's pickup rule); a cart
  with a passenger has no seat for a player. A carried mob stands where
  the cart was after a restart rather than aboard it.
- **Bastion remnants.** The Nether's first big structure: bastions are
  assembled from the real vanilla jigsaw pools (167 templates in the four
  vanilla flavours — housing units, hoglin stables, treasure rooms and
  bridges) at vanilla's start height, in every nether biome but the basalt
  deltas. The structure pipeline learned vanilla's rule processors on the
  way, so the pools' own degradation lists crack the polished blackstone
  bricks, crumble the gilded blackstone and eat the ramparts exactly as
  vanilla's do, position-seeded so a chunk edge never changes a roll.
  Chests fill from their own tables (bastion_treasure, bastion_bridge,
  bastion_hoglin_stable, bastion_other), and the templates' mob pieces
  seed the garrison — piglins, piglin brutes and hoglins — when a player
  first arrives; a cleared bastion stays cleared across restarts.
- **End cities.** The outer islands' highlands grow End cities: a port of
  vanilla's own piece generator (the house tower branching into towers,
  fat towers and bridges to depth eight, each batch discarded when it
  collides with an earlier one, one ship per city sailing off a bridge)
  over the twenty real end_city templates, with vanilla's rule that a
  floor piece keeps the world's blocks where its template has air. The
  treasure chests fill from end_city_treasure, the shulker sentries stand
  where the templates' markers put them, and the ship's item frame holds
  the elytra — all seeded when a player first arrives, and a looted city
  stays looted across restarts.
- **Nether fortresses.** A port of vanilla's fortress piece generator: the
  start crossing grows bridges, crossings, stairs rooms, room crossings
  and monster thrones on the bridge table, and the castle entrance leads
  into corridors, crossings, left and right turns, corridor stairs, T
  balconies and nether-wart stalk rooms on the castle table — with
  vanilla's placement caps, five tries per slot, the 112-block reach, the
  end-filler fallback, the random pending-piece order and the 48–70 height
  band. The corridor chests fill from nether_bridge, the thrones' blaze
  spawners run on the dungeon-spawner cadence, and inside the fortress the
  nether's spawn pass rolls the fortress's own table (blazes, wither
  skeletons, zombified piglins, skeletons, magma cubes) on its floors.
  Structures whose pieces reach past their siting cell's border (bastions,
  End cities, fortresses) now stamp, seed and route their chests from the
  neighbouring cells as well, so a bridge is never cut at a cell edge.

### Fixed
- **Villagers wear their profession.** Every villager rendered as an
  unemployed plains villager, whatever it traded: the VillagerData
  metadata (the type of the biome it was born in, its profession and its
  trade tier) was never sent. It is now, at spawn, on join, on a dimension
  change and whenever a villager is promoted — and the shared protocol
  library learned the serializer so 26.2 clients get it renumbered and
  index-shifted correctly.

### Added
- **Zombie villagers, both ways.** Zombies, husks and drowned now hunt the
  nearest villager within sixteen blocks when no player is near (vanilla
  puts that target goal right below the player one), and villagers run
  from a zombie within eight. A killing bite turns the villager on Normal
  (half the time) and Hard (always) — Easy just kills — into a zombie
  villager that keeps its profession, tier, trades, XP and gossip, with
  vanilla's infection level event. A golden apple fed to a weakened zombie
  villager starts the cure exactly as vanilla does: the apple is spent,
  Weakness gives way to Strength, the cure sound and shaking play from the
  entity event, three to five minutes tick down (each iron bar or bed
  within four blocks may hurry it along), and the villager returns with
  everything it had, queasy for ten seconds, owing its curer vanilla's
  major-positive gratitude (a steep discount) and the *Zombie Doctor*
  advancement. A cure in progress survives a restart.
- **Raids, finished.** The follow-ups the raids row listed: vanilla's
  per-difficulty bonus spawns (extra pillagers and vindicators, a witch on
  the witch waves, a ravager on the bonus wave), the Raid Omen level — a
  level above one brings the bonus wave after the last regular one and a
  stronger Hero of the Village — villagers ringing the village bell while
  the raid is on (which lights every raider up), grateful villagers
  walking up to a Hero of the Village and throwing gifts from their
  profession's own gift tables (thirty seconds to five and a half minutes
  apart), and raids that survive a restart: the raid and its raiders are
  saved with the mobs and pick up mid-wave.
- **Ominous trials.** The omen chain vanilla 1.21 built around the trial
  chambers: a slain raid captain now drops an ominous bottle (level I–V,
  rolled as vanilla's pillager loot rolls it) instead of cursing its
  killer; drinking the bottle gives Bad Omen for a hundred minutes at the
  bottle's level; a trial spawner that sees a player with Bad Omen turns it
  into Trial Omen (fifteen minutes a level, with the level event at the
  player's eyes) and goes ominous — its current mobs vanish in a puff, the
  round restarts on the ominous config (breezes in fours), the block shows
  the ominous flame, and each reward ejection pays from one table drawn
  by weight (ominous: keys three in ten, consumables seven; normal spawners
  now also draw one table per ejection at vanilla's even odds instead of
  paying both). The ominous state persists and wears off when the cooldown
  ends. Ominous zombies, husks and archers come armed from vanilla's
  trial-chamber equipment tables (trimmed, enchanted chainmail at even odds
  per piece, enchanted swords and bows) as gear that never drops, and
  while the fight is on the spawner conjures an ominous item spawner above
  a player every 160 ticks — the floating item vanilla shows — which drops
  its load straight down 60–120 ticks later: a lingering potion (wind
  charging, oozing, weaving, infested, strength, swiftness, slow falling),
  a plain, poison or slowness arrow, or a fire or wind charge.
- **Bedrock: health, hunger and XP.** The Bedrock gateway relayed none of
  the survival state — a Bedrock player sat at a static twenty hearts
  whatever happened. It now turns the world's health and XP frames into
  Bedrock attribute updates (health, hunger, saturation, experience bar
  and level), and the death screen and its respawn button work — the
  death frame becomes the death message plus Bedrock's respawn packet, the
  client's respawn answer reaches the world, and the respawn teleport
  carries the ready-to-spawn packet ahead of the move. Mobs flinch and
  keel over (hurt and death actor events), dropped items show as item
  actors carrying their stack, the common sounds — mob voices, hits,
  bows, chests, doors, explosions, the anvil, bells, thunder — play as
  level sound events, entities show what they hold and wear, burning,
  sneaking and baby entities read as such, and block-break chips,
  bone-meal sparkles and the crit, explosion, poof and bubble bursts play
  as level events.
- **Channeling, and what lightning does.** The last enchantment without an
  effect: a Channeling trident that hits a mob or player under open sky in
  a thunderstorm, or a lightning rod, calls a bolt down on the spot with
  the trident's thunder. Bolts now do what vanilla's do to what they hit:
  a creeper charges (its aura synced, its blast doubled, the charge saved
  with the mob), a pig becomes a zombified piglin and a villager a witch.
- Fixed: an ender dragon summoned by an op was saved with the mobs and came
  back as a stray after a restart; every dragon is an event, not a resident.
- **The gossip system.** Villager reputation is now vanilla's
  GossipContainer rather than a per-session trade counter: each villager
  holds the five gossip types about each player at vanilla's weights,
  caps and daily decay — trading (+2 a trade), the cure (+20 major, +25
  minor positive), a blow (+25 minor negative) and a murder, which every
  villager within sight holds against the killer (+25 major negative,
  −125 reputation). Gossip spreads between villagers standing together
  (ten weighted entries a chat, minus the transfer decay, once per twenty
  minutes each), rides along through infection and cure, is saved with
  the villager, sets the trade discount, and turns the village's iron
  golem on a survival player whose reputation with a villager within ten
  blocks has fallen to −100 (DefendVillageTargetGoal).

### Changed
- Jump Boost raises the safe fall distance by one block per level, as its
  attribute modifier does in vanilla; the three-block grace was fixed.
- Copper oxidation on a double copper chest now ages both halves together
  (vanilla's connected-half rule) instead of leaving a mismatched pair.
- Desert rabbits now spawn at vanilla's weight (12, from the 1.21.11 biome
  report) rather than 4.

## 2026-09-05

### Added
- **Redstone power travels through solid blocks the way it does in vanilla.**
  The engine only ever saw sources a consumer touched directly, so the first
  things a player builds — a lever on the far side of a wall, dust ending in a
  block with a lamp beyond it, a torch under a block — did nothing. Power now
  follows vanilla's own model: a source powers its neighbours *weakly*, some
  sources drive one block *strongly* (a lever or button into the block it
  hangs from, a torch into the block above it, a pressure plate, detector rail,
  lectern or sculk sensor into the block beneath, a repeater, comparator or
  observer out of its front, and dust into the block it points at), and a
  solid block receiving strong power passes it on to everything it touches.
  Which blocks conduct is decided per block state by vanilla's rule — a full
  collision cube, from the game's own collision data — with vanilla's
  exemptions (glass, ice, glowstone, sea lanterns, beacons, redstone blocks,
  pistons, TNT, leaves, copper grates and bulbs never conduct; soul sand and
  mud always do). Dust also connects the way vanilla connects it — recomputed
  from its surroundings every time it is asked, a lone connection extending
  into a straight line — so a line of dust ending beside a lamp lights it.
  Two behaviours that were wrong before and are now vanilla's: a redstone block
  beside a torch's support block no longer switches the torch off (it has no
  strong signal), and a torch or lever beside a comparator is no longer a side
  input (only dust, a redstone block or a diode is).

### Fixed
- **Most of the advancement tree could not be earned.** Sixty-nine of the 125
  advancements were unobtainable: one hundred and eight criteria used
  triggers the engine never fired, and one blocked criterion blocks its whole
  advancement. The engine now observes them — an item used on a block (a key
  in a vault, glowstone into an anchor, a disc in a jukebox, glow ink on a
  sign, a bottle at a smoked hive), a hand on a mob, a recipe taken from the
  crafting or smithing table, a crafter's own output, archaeology finds, a
  wither built, effects gained (the *all effects* and *all potions* sets
  included), levitation, falls from world height and after a wind charge,
  lightning beside a villager, Nether fast travel, stepping into an end
  gateway, a target block's bullseye, a kill beside a sculk catalyst, a
  spyglass trained on a ghast or dragon or parrot, arrows and tridents and
  mace smashes landing, a shield turning a projectile, a crossbow's kills
  (two phantoms with one bolt, five different mobs with one), a nest cut
  with Silk Touch, and where you stand: a stronghold, a trial chamber, powder
  snow in leather boots. The advancement table is generated with each
  criterion's real conditions now instead of a flag, so the tree tells the
  truth about what is earnable. Nine criteria remain unobtainable until their
  mechanics exist — channeling, honey-block sliding, wolf armour, piglin
  bartering, allays, a goat in a boat, spears, and sneaking past sculk — and a
  handful of others wait on mechanics not yet built (player copper waxing,
  mob buckets, a built iron golem, dragon respawn, lodestones, the Nether and
  End structures).
- **Potions, renamed items, anvil repair costs and goat horns did not survive a
  restart.** The in-memory stack carried all four, but the saved row never
  did — so every rollout turned potions back into water bottles, stripped
  anvil names, reset the prior-work cost and made every goat horn play
  *ponder*. Player inventories, containers, mob gear and items on the ground
  were all affected. The saved row carries them now (older saves still load;
  the new columns simply read as empty), custom names go through an interned
  name table saved alongside the containers, and the same four — plus a
  bundle's contents, which were lost the moment the bundle hit the floor —
  now ride a dropped item too, and are shown on it while it lies there.

## 2026-08-21

### Fixed
- **Fire reaching TNT could freeze the whole server.** The fire simulation
  handed the primed charge to the hub through the same queue the gateways use
  — from the hub's own goroutine. The hub is that queue's only reader, so with
  the queue full at that moment (a burst of player actions plus a fire reaching
  a TNT stack is exactly when a tick is already long) the hand-off could never
  complete: the tick loop stopped for good while the process stayed up and kept
  passing its health check. It is now a direct call, and the hub-side queue
  path fails loudly rather than deadlocking quietly.
- **A corrupt save file could be silently replaced by an empty one.** Sixteen
  on-disk stores — inventories, containers, mobs, players, advancements, stats,
  the whitelist and ban list, the game rules — loaded with the decode error
  ignored, so a truncated or damaged file came back as an empty store, the
  server started cleanly, and the next 30-second save wrote that emptiness
  over the only good copy. A file that fails to decode is now moved aside with
  a timestamp, logged loudly, and the store starts empty over preserved bytes;
  a file that cannot be read stops startup. Every store write is also atomic
  and synced to disk now — four stores (whitelist/bans, game rules, plugin
  data, migration markers) were plain overwrites that a crash could truncate,
  and none of them, nor the world file, synced before the rename.
- **Ten cows ticked forever on an empty server.** With nobody online the mob
  update still ran every tick for the boot herds, which (until the earlier fix
  today) walked the world generating terrain for no one. Vanilla ticks
  entities only in loaded chunks, and chunks load around players; the engine
  now does the same.
- **Neighbour searches walked every mob.** Seven places asked "who is within a
  few blocks of here" by scanning the whole mob list — the herd cohesion for
  every herd animal every tick among them, which made herds quadratic in the
  mob count. A per-tick spatial grid answers the question from a handful of
  cells, with exact positions checked, so the result is unchanged and the cost
  no longer grows with the size of the world's population.
- **Gear attributes were recomputed twenty times a second per player.** Armour
  points and enchantment attributes follow equipment changes now, as they do
  in vanilla, instead of being rebuilt every tick.
- **Three small per-player records outlived the player** (the sculk step
  throttle and last-position pair), one set per join, forever.

### Changed
- **The world reports its own health.** A new opt-in `-health` listener serves
  `/healthz` (503 once the tick loop has stalled for five seconds — a wedged
  hub still accepts connections, so the old TCP check could not tell), a
  `/debug/vars` page with players, mobs, cache sizes, block edits, tick timing
  percentiles and which chunk cache and plugin bus the pod actually connected
  to, and `/debug/pprof`. Any tick over 100 ms is logged with what it was
  carrying. The cluster's liveness probe now points at `/healthz`.
- **A shared chunk cache or plugin bus that is down at startup is retried**,
  every 30 seconds, and swapped in when it answers — instead of falling back
  for the life of the process. The local directory cache serves in front
  meanwhile and stays warm afterwards.
- **Cache budgets follow the memory limit.** The generator and light caches
  were fixed at ~530 MiB combined regardless of the pod's limit; they now take
  half of whatever `GOMEMLIMIT` allows, and keep the old sizes only when no
  limit is set. The directory chunk cache is bounded too (512 MiB, oldest
  files evicted) — it had no eviction at all.
- **Random ticks read blocks through a pinned chunk**, one lookup per chunk
  instead of two locks and three lookups per block, on the hottest read path
  in the engine (~5,800 reads a tick per player).
- Continuous integration now runs the race detector on every push and pull
  request; the image build no longer re-runs the test suite it already gated.
- The Bedrock gateway checks every packet write, so a client that drops
  mid-session ends that session at once instead of when its read side happens
  to fail.

### Fixed (earlier today)
- **An empty server slowly ate its own memory.** A world with nobody on it grew
  by about 15 MiB an hour, indefinitely — enough to exhaust the pod in a couple
  of days of sitting idle. Three things had to line up. Each world boots three
  cow herds so there is life near spawn; those are created directly rather than
  through the chunk bookkeeping, so nothing ever unloads them — and the unload
  pass is reached only from the natural spawner, which stops early when there
  are no players, so on an empty server it never runs at all. Meanwhile a
  herd's roaming goal drifted by a random walk with nothing pulling it back,
  and a random walk does not stay put: it wanders off without limit, and the
  cows steer after it. So ten cows spent every tick walking into terrain no
  player had ever visited, generating it and filing it in the chunk cache,
  forever. A herd's goal now stays within 64 blocks of where the herd was
  rooted — sized against the cache budget rather than by feel, so the whole
  boot population can only ever account for a small fraction of it.

## 2026-08-15

### Fixed
- **Every projectile dealt arrow damage.** The damage type decides more than it
  sounds: whether armour absorbs a hit, which protection enchantment applies,
  what the death message says, what it costs in hunger. So a ghast's fireball, a
  llama's spit and a wither skull were all absorbed and enchanted against
  exactly like an arrow — and the `fireball` type, which armour does *not*
  protect against the way it does arrows, was never dealt at all. Each
  projectile now carries its own: tridents, thrown snowballs and eggs, both
  fireballs, wither skulls, shulker bullets, spit, wind charges and ender
  pearls. Two of them depend on who threw it, as vanilla's damage sources do —
  an ownerless fireball is *unattributed*, and a wither skull with no living
  owner deals plain magic, and less of it.
- **A mace smash sent no shockwave in PvP.** The wave reached mobs only, so
  smashing another player moved nobody standing nearby. It was also centred on
  the attacker rather than on whatever was struck — the two only coincide when
  you land squarely on your target — and it was missing the flat upward
  component that makes the wave pop people into the air rather than slide them
  along the ground. Vanilla's exemptions are in place: the attacker, the entity
  struck, spectators, creative players, and your own tamed pets ride it out.
- **A warden dug away and left its loot behind.** Burrowing off after losing
  interest went through the death path, so waiting a minute near one paid out
  its sculk catalyst and experience. Vanilla removes it as *discarded* — not a
  death, nothing dropped. Its two clocks were wrong in opposite directions
  while we were there: it dug away after 600 ticks against vanilla's 1200, and
  its sonic boom recharged in 60 ticks where vanilla's takes 100 (a 60-tick
  attack and then a 40-tick cooldown), which made it markedly more punishing
  than the real thing.
- **A zombie drowned fifteen seconds too fast, and without warning.** Vanilla
  gives it two phases: thirty seconds with its eyes under water starts the
  conversion, and only then does a fifteen-second countdown run during which the
  zombie visibly shakes. We turned it the moment the first timer elapsed, so
  there was never the shudder that tells you what is about to surface. Once
  started the countdown finishes wherever the zombie is — dragging a shaking one
  onto dry land no longer saves it.
- **Spiders climb.** A spider walking into a wall now goes up it, which is
  vanilla's whole rule for climbing: you are climbing exactly when you bumped
  into something. The client is told, so it renders the spider clinging.
- **Drowned swim.** They used to trudge along the seabed like any other zombie;
  in water they now swim, which is what lets one come up at you rather than
  pacing about beneath you. They still walk out onto land — the only mob here
  that does both.
- **A wither could not feed itself.** Its skull heals it 5 when the skull KILLS
  what it hits — not when it merely wounds — and that clause was missing, so a
  wither could not claw health back by killing whatever else was in the fight.
  It made the boss meaningfully easier than vanilla's.
- **The hunger bar emptied on Peaceful.** Vanilla burns saturation on every
  difficulty but only takes from the food bar when the difficulty is not
  peaceful; ours drained it and then simply never starved you.
- **The Hunger effect did nothing.** A husk's bite grants it, and nothing
  consumed it — it should add exhaustion every tick, scaled by its level.
- **A bee's sting was an ordinary bite**, rather than the sting damage type
  vanilla gives it, which is what the death message reads from.
- **The ender dragon healed itself across a restart.** Bosses are not written
  to the mob file, so a fight interrupted halfway resumed against a dragon at
  full health. Its remaining health now rides the world settings alongside the
  flag that says the fight was won.
- **A scoreboard could not count kills.** `playerKillCount` was incremented
  nowhere in the engine, and `totalKillCount` was kept for mobs and arrows but
  not for killing a player, so an objective tracking either read zero however
  the fight went. The statistic was being kept correctly all along — it is the
  scoreboard criteria that were missed.

## 2026-08-11

### Added
- **Bundles.** All seventeen of them. A pouch holds one stack's worth of
  anything, by vanilla's own rule: each item costs one over its stack size, so
  sixty-four dirt, sixteen ender pearls or a single lava bucket each fill it
  exactly, and a mixture shares the space between them. Left-click a stack with
  a pouch in hand to take it in, right-click an empty slot to tip one back out,
  and scroll to choose which stack that is. The contents show in the tooltip,
  travel with the pouch when it is dropped or chested, and survive a restart. A
  pouch can go inside another at vanilla's one-sixteenth surcharge, which is
  what stops them nesting forever.
- **Leads.** Tie a mob to yourself with a lead and it follows you about; tie it
  to a fence and it stays there until you come back for it. The rope is real —
  vanilla draws it between two entity ids, so a fence knot is an actual entity
  that appears when the first lead is tied and vanishes with the last. Clicking
  a knot empty-handed collects everything on it back onto your own lead, which
  is how a pen full of animals gets moved. The physics are vanilla's: slack
  inside six blocks, a spring beyond that, and a snap at twelve that drops the
  lead as an item. Hostile mobs and most water life refuse a lead, exactly as
  they do in vanilla — dolphins and axolotls accept one, squid and turtles do
  not. A lead tied to a fence survives a restart; one held by a player does not,
  because it is already cut the moment that player disconnects.
  Java only for now: the Bedrock gateway does not draw the rope yet, though
  tying and untying work there because the engine itself is versionless.

### Fixed
- **A bee whose hive filled up while it was out wandered off with its
  nectar.** Fullness was consulted on every pass, so the moment a hive reached
  three occupants it stopped being a destination and its bees drifted away.
  Vanilla checks it in exactly two places: when a homeless bee ADOPTS a hive,
  and again when one ARRIVES at the door — and a bee that arrives to find it
  full simply drops that hive and goes to find another. In between it flies
  home regardless; whether the hive is busy is not something a bee knows from
  half a field away. A full hive is also not blacklisted now: being busy is not
  the same as being unreachable.
- **A homeless bee searched for somewhere to live every second**, local block
  scan and all. Vanilla allows one search every 200 ticks.
- **A bee could be homeless for ever.** When every hive with room had been
  blacklisted, nothing ever cleared the list, so a bee that once failed to
  reach the only hive around never went home again even after the way was
  clear. Vanilla clears the blacklist and takes the nearest.
- **The trip deadline was twenty minutes rather than two.** The counter ticks
  once a second with the rest of the bee clocks, but it was being compared
  against a limit converted for a different pass. Shipped in the previous
  release; a bee stuck out of reach of its hive took ten times too long to give
  up on it and look elsewhere.

## 2026-08-05

### Added
- **Mobs no longer stand inside one another.** Vanilla shoves every pair of
  overlapping living entities apart on every tick; tachyne had no mob-vs-mob
  separation anywhere, so two bees, or a herd of cows, or forty zombies could
  share a single point indefinitely. Each species now carries its real vanilla
  hitbox — babies at half size, slimes and magma cubes scaled by theirs — and
  the crowd is resolved with vanilla's own arithmetic, quirks intact: the shove
  weakens as a pair converges, and a pair sharing a spot to within a hundredth
  of a block gets none at all. The mobs vanilla exempts are exempt here too
  (bats, the ridden, a watched creaking). Packing a pen past the cramming limit
  now crushes what is in it, which is what makes one work.
- **Fliers can find their way.** A flying A* over air cells, ported from
  vanilla's `FlyNodeEvaluator` — the 26-neighbour expansion (every axis, edge
  and corner move) with vanilla's rule that a diagonal is only available when
  the orthogonal moves composing it are, so nothing squeezes through the gap
  between two blocks that merely touch. The engine's existing pathfinder is
  two-dimensional by construction: it walks x/z and asks the world where the
  floor is, which is no use to something with no floor.

### Fixed
- **A bee somewhere with no flowers never went home at all.** Going home was
  gated on "has nectar, or it is night, or it is raining" — but vanilla's
  `wantsToEnterHive` has a fourth reason: a bee that has been out 3600 ticks
  empty-handed gives up and returns anyway. Without it, a bee that could not
  find a flower satisfied none of the conditions and simply foraged for ever.
  The five vanilla refusals were missing too: a bee mid-pollination, dying of
  its sting, angry at someone, barred after a sedated robbery, or whose hive
  has fire beside it does not go in. (Fire — not a campfire. A campfire under
  a hive sedates it, which is what makes honey farms safe, and reading that as
  fire would have locked every bee out of its own hive.)
- **Bees picked up pollen and then hovered where they stood.** Two faults
  stacked. The altitude spring that keeps a flier at its cruising height ran
  every tick against the errand's attempt to climb, and the two cancelled — so
  a bee under a nest in a tree bobbed in place indefinitely, pollen on, going
  nowhere. And the errand steered in a straight line with no way around a
  trunk or a canopy. A bee now flies a real route, and the route's own
  waypoints set the altitude, so climbing to a nest is simply part of
  following it.
- **A bee would try an unreachable hive for ever.** Vanilla gives a trip a
  deadline and blacklists a hive it cannot route to from close up, keeping the
  last three; drops a hive left more than 48 blocks behind entirely; and, past
  a soft leash of 24 blocks, biases its idle wandering back toward home
  instead of drifting. All three were missing. The near/far split is vanilla's
  too: a real route is only computed within 16 blocks — further out the bee
  simply heads the right way — which is what keeps the search cheap enough to
  run on every bee.

## 2026-08-04 (evening)

### Fixed
- **Sleepers lay in the wrong half of the bed.** A bed is two blocks, and
  vanilla anchors everything about sleeping — the pose, the respawn claim, the
  position — to the HEAD half, stepping there first if you clicked the foot.
  tachyne used whichever end you clicked, and because the client draws a
  sleeping body extending from the anchor down the bed, clicking the foot laid
  the sleeper backwards with their legs hanging off the end into thin air.
  Sleeping villagers had the same bug, anchored to whichever cell worldgen
  recorded for their bed.
- **Beds never looked slept in.** Vanilla sets the block's `occupied` property
  while someone is in it, which is what rumples the blanket; it was never set
  or cleared.
- **Waking up left you standing in the bed.** Vanilla gets you out of it —
  a fixed ring of ten cells around the bed tried in an order that starts on
  whichever side you are already facing, the bed's own cells only as a last
  resort — and turns you to face the bed you just left.
- **Sleeping height was the mattress height** (0.5625) rather than vanilla's
  `setPosToBed` (0.6875), so a sleeper sat sunk into the bed.
- **A monster two floors up stopped you sleeping.** The check was a sphere of
  radius 8; vanilla's box is ±8 across but only ±5 up and down.

## 2026-08-04 (later)

### Fixed
- **A chest in the Nether and a chest in the Overworld shared one inventory.**
  Every block-entity store on the engine — chests, furnaces, hoppers,
  dispensers, droppers, crafters, brewing stands, jukeboxes, beacons, lecterns,
  chiseled bookshelves, campfires and banners — was keyed by x/y/z with no
  dimension, so the same coordinates in two worlds named a single container.
  Opening the Nether chest showed you the Overworld chest's contents, and
  whichever you touched last was the one that got saved. Opening a container
  also read the block state from the Overworld regardless of where you were
  standing, which is why a Nether double chest could pair against an Overworld
  neighbour. Everything is now keyed by dimension, and `containers.json` and
  `campfires.json` write `dim:x,y,z` keys — files written before this load as
  the Overworld records they have always been.
- **Beacons, banners, lecterns and jukebox-stop did nothing outside the
  Overworld.** Block-entity registration sat behind an early return that
  skipped every non-Overworld edit, so a Nether beacon never opened or ticked,
  a Nether banner rendered plain, a Nether lectern was inert, and music started
  in the Nether but could never be stopped — it played forever. Registration
  now runs in every dimension; block *simulation* (falling blocks, fluids,
  redstone, sculk) is still Overworld-only, which is the part that early return
  was actually for.
- **Mining a block in the Nether or the End dropped its item into the
  Overworld**, along with any ore experience — the same defect fixed for death
  drops yesterday, in the block-loot path.

## 2026-08-04

### Added
- **Respawn anchors.** The Nether's bed now works: glowstone charges it up to
  four times, a charged anchor claims your respawn point, and respawning there
  spends one charge and leaves you in the Nether instead of dragging you back
  to the overworld.

### Fixed
- **A bed in the Nether or the End silently stole your respawn point.** It
  neither exploded — vanilla's whole point — nor let you sleep, but it did
  record the spot as home. The claim was then validated against the *overworld*
  block at the same coordinates, because the stored respawn point had no
  dimension, so it usually evaporated without a word. Beds now detonate
  wherever they do not work, respawn points carry the dimension they were
  claimed in, and existing spawn files load as the overworld points they
  always were.
- **Trial-chamber progress was lost on every restart.** Vault claims and trial
  spawner cooldowns lived only in memory, so a restart re-armed every spent
  spawner and let every player claim every vault a second time. Both now
  persist — claims by UUID (capped at vanilla's 128 per vault, oldest first),
  cooldowns as ticks remaining, since a restarted world's clock begins again
  at zero.

## 2026-08-03 (later)

### Fixed
- **Flying mobs' wings didn't move.** Every mob's movement told the client it
  was standing on the ground — including bees, parrots, phantoms, ghasts and
  every other free-flying mob. Clients animate wings only while they believe
  an entity is airborne, so a hovering bee drifted past with its wings frozen
  mid-beat. Flying mobs now report themselves airborne, as they always were.
- **Breaking a block beside sugar cane, cactus or bamboo destroyed the farm.**
  Those plants stand on their own kind, but the support system only accepted
  soil beneath them, so every segment above the base counted as unsupported —
  and because the support sweep runs on each nearby block edit, mining next to
  a farm wiped the stack above its bottom block. They now stand on themselves,
  as vanilla has them.
- **Boats spawned the wrong entity entirely.** The boat table still held
  pre-retarget ids, so an oak boat arrived as a marker, a spruce boat as a
  sniffer, a dark oak boat as a creaking, a cherry boat as a cave spider and a
  mangrove boat as a llama. Boat types are now looked up by name in the
  canonical registry, and a test pins every wood so a future version bump
  can't repeat it.
- **Two composter inputs never composted.** Short and tall dry grass were
  listed under their internal Java field names rather than their real item
  ids, so the table silently dropped them.
- **`/worldborder` was missing from tab-completion** though the command works.
- **Dying in the Nether or the End scattered your things into the Overworld.**
  Both the inventory drop and the experience orb defaulted to the overworld
  instead of the dimension you died in, so a Nether death posted your gear and
  levels into a world you weren't standing in — unreachable, and gone.

### Changed
- **The feature matrix got stricter about itself.** A systematic audit of
  every documented claim against the source moved six areas from complete to
  in-progress, each with the specific reason: beacons, banners and lecterns
  are registered only in the Overworld (and a jukebox started in the Nether
  never stops), trial-chamber vault claims don't survive a restart, and 30 of
  vanilla's 77 statistics counters are wired. Several rows also understated
  the engine — the recipe book is 1,678 recipes, not ~1,570, and mount
  inventories do persist. Nothing about the software changed; the description
  of it did.
- **A pollen-carrying bee could disconnect 26.2 players — and it wasn't just
  bees.** 26.2 added a synced field to every ageable animal, shifting the
  indices of each species' own appearance data up by one; the translation
  layer didn't know, so the bee-look metadata shipped earlier today landed on
  the wrong field and kicked any 26.2 client that saw a pollen carrier. The
  per-version translation now shifts ageable-mob metadata for 26.2 clients —
  bees, sheep wool, and pet sit/tame flags, the last two of which were the
  same disconnect waiting to happen since they shipped.
- **Saddles now render the way modern clients expect — on every mount.**
  Saddles have been an equipment slot since 1.21.5, but pigs and striders
  were still sent legacy metadata aimed at fields that no longer exist —
  saddling a pig could disconnect nearby players outright, and a strider's
  saddle bit landed on its "suffocating" shiver flag. All mounts now carry
  the saddle in the real equipment slot, as horses already did.
- **Taming an ocelot could disconnect nearby players.** Ocelots aren't
  tamable in vanilla — they trust; the tamed-pet flags were being written
  onto the trust field with the wrong value type. Ocelots now get their
  proper trust flag.
- **Bees no longer emit a sound that doesn't exist.** Vanilla bees have no
  server-side ambient voice (the buzz is the client's own loop), so the
  engine no longer sends one.

### Added
- **Bees look and act the part, to the last detail.** A pollen-laden bee now
  visibly wears its dusted coat and drips falling nectar as it flies, an angry
  bee shows its red eyes and a spent one its lost stinger — the synced
  appearance state vanilla clients render, re-asserted for late joiners like
  every other one-shot look. On the way home a pollen carrier boosts the
  crops it crosses (wheat and friends, melon and pumpkin stems, sweet berry
  bushes, cave vines — up to vanilla's ten per trip, with the green burst
  over each). Dispensers work a full hive exactly as vanilla does: shears cut
  three honeycomb, a glass bottle draws a honey bottle, and either releases
  the bees calm — a machine has nobody to blame. And hives move house:
  broken with Silk Touch, the bees and honey level travel aboard the dropped
  item — through inventories, chests, hoppers and a restart — and step back
  in when it is placed; broken bare, the occupants spill out angry at the
  breaker, and a bee nest then drops nothing at all, exactly vanilla's loot
  rule.

### Fixed
- **Bonemeal's green burst showed the wrong particle on newer clients.** The
  per-version particle translation only covered the ids the engine emitted at
  the time it was written, so 1.21.9 and 26.x clients rendered the
  happy-villager burst as an unrelated particle. The table now carries it
  (and the new falling-nectar drip) across every served version.
- **Items that carry contents no longer lose them in transit.** A shulker
  box's identity now survives being tossed from an inventory, spilled from a
  broken chest, a donkey or a lectern, and a server restart while lying on
  the ground — paths that previously dropped the link and orphaned the
  contents. Carried hives ride the same, now-watertight rails.

## 2026-08-02

### Added
- **Bees live real lives.** A bee forages the nearest flower, hovers it for
  its nectar, and carries it home; the hive takes it in, and when the bee
  emerges from its stay the honey level rises — the proximity stand-in is
  gone, honey comes only from delivered nectar now. Bees head home at
  nightfall and in the rain, hive occupants survive a server restart, a
  robbed hive throws its occupants out angry unless campfire smoke keeps
  them calm, a broken hive spills its bees where it stood, holding out any
  flower courts a pair into breeding — and a sting is the last thing a bee
  does: one hit, sixty seconds, gone.
- **Bee nests hang in the trees, with bees to match.** Wild trees carry
  vanilla's bee nests at vanilla's odds — one plains oak in twenty, one
  forest tree in five hundred, every large oak in a meadow — hung off the
  trunk at the canopy's base, facing south, with two or three bees hatched
  beside each fresh nest as the world generates. And vanilla's gardener rule
  is in: a sapling grown within two blocks of a flower comes up carrying a
  nest of its own. Harvesting was already live (shears for honeycomb, a
  bottle for honey, campfire smoke to calm the swarm) — now the wild has
  hives to find.
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
