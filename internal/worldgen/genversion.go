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
const GenVersion = 12
