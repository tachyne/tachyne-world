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
const GenVersion = 20
