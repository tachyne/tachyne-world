package worldgen

// GenVersion stamps persisted generated chunks. The generator is a pure
// function of (seed, GenVersion): whenever a change ALTERS GENERATION OUTPUT
// (terrain shape, caves, features, ores, decoration), bump this so cached
// chunks from the old generator are ignored and regenerate — serving stale
// cache after a terrain change would desync the world from fresh generation.
// Pure speedups that keep output byte-identical must NOT bump it.
//
// v10: the 1.21.11 re-target changed EVERY block-state id (generation output is
// now 1.21.11-numbered), so pre-retarget cached chunks (1.21.5 ids) must be
// invalidated — otherwise previously-visited chunks serve stale ids that the
// gateway then mis-remaps (snow rendered as acacia_hanging_sign, etc.).
//
// v11: earth-mode climate lapse fixed to real metres (Signal Hill was snowy —
// the 1/70-blocks rate is vscale× too strong under vertical compression);
// biomes and snow cover change on every earth column above sea level.
//
// v12: the capetown DEM crop grew from the bare peninsula to greater Cape Town
// (same grid name, new data) — every earth chunk regenerates.
//
// v13: sea floors grow seagrass, kelp forests and sea pickle clusters
// (seafloor.go) — every water column below sea level decorates differently.
//
// v14: mangrove swamps grow seagrass too (seagrass_swamp is in their feature
// list as well).
//
// v15: the canonical content version moved to 26.3, which renumbers block
// states — every cached chunk holds the old ids.
//
// v16: generated trees no longer grow into player builds (treeguard.go).
// Generation now reads the edit overlay for that one decision, so a cached
// chunk reflects the edits of when it was made — a tree already generated
// stays, as in vanilla; only a fresh generation leaves it out. The same
// release adds 26.3's dappled forest (cold, driest plains become forest) and
// the poplar trees.
//
// v17: villages meet the ground as vanilla's do — pieces projected to the
// surface counting water, houses placed at the street jigsaw's surface,
// streets terrain_matching, dirt beards under rigid pieces — and every
// jigsaw block becomes its final_state instead of a hole.
//
// v18: a chopped tree grows again with its chopped logs left out, rather
// than not at all — the tree guard counts only a player block in a log cell,
// not the air a chopped log leaves.
//
// v19: nor the fire, lava, water or plants the world leaves in a tree's
// cells — a tree that caught fire was dropped whole on regeneration — nor
// the soil under a trunk turning from grass to dirt and back, which dropped
// nearly every tree anyone had lived near.
//
// v20: a vegetation patch (moss floors, lush-cave clay pools) plants on its
// ground, not a block above it: every moss patch's carpets, grass and azaleas
// used to float, and fell to items the first time anything beside them
// changed.
//
// v21: strongholds are vanilla's whole maze of pieces around the portal
// room, and mineshafts vanilla's rooms, corridors, crossings and stairs,
// started from any chunk (the old 256-block grid is gone), with the dark oak
// mesa variant in the badlands.
//
// v22: 26.3's sulfur caves generate — a cave biome under flat, far-weird
// land, its rock banded with sulfur and cinnabar, with sulfur spikes,
// sulfur pools and rooted sulfur springs. The underground of every chunk in
// that climate changes (and dripstone/lush caves there give way to it).
//
// v23: vanilla's missing overworld features and the Nether roof, all behind
// the build guard (buildguard.go), which from this version reads the edits
// as they stood when the version first booted (world/guardsnap.go):
//   - the vegetal step's surface patches — tall grass, large ferns,
//     sunflowers, lily pads, leaf litter, bushes, firefly bushes, dry grass,
//     wildflowers, standalone pale moss and the jungles' vines — none on a
//     player's floor or under a player's roof;
//   - ravines (the canyon carver), lava lakes on the surface and below,
//     dirt, gravel and (lush caves) clay pockets — each left out whole
//     where a build lies in its box;
//   - sandstone deep under desert and beach sand, sandstone or stone over a
//     cave's ceiling, frozen-ocean floor holes, orange flooded badlands;
//     ice spikes and patches, old-growth taiga boulders, blue ice under the
//     icebergs, beached shipwrecks;
//   - the Nether's bedrock roof at y=127 over netherrack from 121, left open
//     over any build at y>=115 and two columns round it.
//
// Nearly every chunk generates differently.
//
// v24: ruined portals settle as vanilla's findSuitableY does — down from
// their placement height until three of the four bottom corners stand in
// solid ground — so one on a cliff edge sinks into the cliff instead of
// hanging off it. A portal whose settled box holds a build stays put.
// Only chunks with a portal change.
//
// v25: the sulfur caves' rooted springs and pools join the build guard. They
// came with v22, before the guard, and a spring stamped under a player's
// build left its ring hanging over the ground they had dug and built in. A
// spring or pool with a build or a dug-out cell in its box is now left out
// whole (every draw still made); only chunks near such a build change.
//
// v26: four of vanilla's 26.3 placements, the new ones behind the build guard:
//   - spring_lava_frozen: lava out of the snow, powder snow and packed ice
//     of the frozen peaks, groves, jagged peaks and snowy slopes (its own
//     draws); and no spring in the deep dark, asked at the spring's cell;
//   - the desert's (every chunk), the badlands' (one in five) and the
//     swamp's (one in three) own sugar cane patches, and the sparse
//     jungle's melons (one in sixty-four), from their own stream — the plain
//     cane patch leaves those biomes, and the dappled forest, to them, and
//     pumpkins leave the dappled forest; the plain patches keep their draws,
//     so no other feature in the chunk moves;
//   - disk_grass on the mangrove swamp's mud, and no sand or gravel disks
//     in either swamp;
//   - igloos face one of four rotations — except an igloo a player has
//     touched, which keeps its old layout.
//   - seagrass as 26.3's eight placements: each a patch of attempts about
//     one column of the chunk, its own tall odds, only in biomes listing the
//     same placement, and none under a player's build (own streams; kelp
//     and pickles keep their draws).
//   - the ore step as 26.3's thirty placements (the upper coal, the iron
//     upper/middle/small split, large copper in the dripstone caves, the
//     badlands' extra gold, lower gold, the diamond medium/large/buried
//     split), every blob — ores, soil and the stone variants — OreFeature's
//     ellipsoid across chunk borders with its discard on air exposure, a
//     blob with a player's build in its box left out. Every underground
//     changes.
//   - dungeons as vanilla's monster_room (ten a chunk, y 0 to the top) and
//     monster_room_deep (four, below 0): MonsterRoomFeature's validity check
//     (solid floor and ceiling, one to five openings), floor gaps, mossy
//     floors and 0-2 chests; one with a build or dig in its box is left
//     out. The old 48-block-grid dungeons go, except one a player touched,
//     which stays as it was.
//   - amethyst geodes as GeodeFeature: three or four points' distance
//     field with noise, vanilla's layer thresholds, a crack (95%), no geode
//     where its points meet air or fluid, budding amethyst one in twelve
//     and buds of every tier; planned once and left out whole where a
//     player built or dug. Geodes now go in before the dungeons and ores.
//   - the deep dark's sculk as vanilla grows it: sculk_patch_deep_dark
//     (SculkSpreader's world-generation charge cursors: veins, sculk,
//     sensors, can-summon shriekers, a catalyst on half) and sculk_vein on
//     floors, walls and ceilings, in place of the 85% floor carpet; planned
//     per origin chunk, laid only on cells as the plan found them, a patch
//     with a player's build or dig in its box rolled back.
//   - the End's spikes as EndSpikeFeature lays them out from the seed
//     (radius 2-5, height 76-103, two caged in iron bars, bedrock and fire
//     under each crystal, obsidian to the floor), a spike a player touched
//     keeping the old pillar; and the exit podium pre-placed, inactive, at
//     the island's top unless someone built or dug round 0,0.
//   - village decor: the pools' feature and empty elements re-rolled after
//     assembly with vanilla's weights, so some lamps give way to the
//     biome's trees, hay/melon/pumpkin/snow/ice piles, plains flowers,
//     desert cacti or taiga grass and berry bushes (or to nothing), and the
//     trees pools grow their trees; village layouts, beds and job sites
//     are unchanged.
//   - ground cover as 26.3's placements, in place of the per-column hash
//     scatter: each biome's flower set with its providers (the plains'
//     threshold tulips, the flower forest's noise bands, the meadow's dual
//     noise, pink petals, forest lilacs/peonies/rose bushes/lilies of the
//     valley, blue orchids, closed eyeblossoms), the patch_grass_* counts,
//     dead bushes, cactus columns with flowers, berry bushes, the bamboo
//     jungle's noise-counted bamboo with podzol discs, red shrubs and the
//     mushroom fields' mushrooms — its own stream, every plant guarded; the
//     old scatter is still drawn and laid first so no other feature moves,
//     then taken back up.
const GenVersion = 26
