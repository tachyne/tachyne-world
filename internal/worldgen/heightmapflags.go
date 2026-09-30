package worldgen

import "sync"

// Heightmap membership (Heightmap.Types' isOpaque predicates), one bit per
// live-world heightmap, precomputed for every block state so a heightmap
// update or column scan costs a table read per cell.
//
//   - WORLD_SURFACE: anything that is not air (air, cave air, void air).
//   - OCEAN_FLOOR: #blocks_motion_in_heightmap alone — the ocean's floor,
//     under the water.
//   - MOTION_BLOCKING: #blocks_motion_in_heightmap, or any block holding a
//     fluid (water, lava, a waterlogged block, seagrass, kelp).
//   - MOTION_BLOCKING_NO_LEAVES: the same with
//     #blocks_motion_in_heightmap_no_leaves.
const (
	HMWorldSurface uint8 = 1 << iota
	HMOceanFloor
	HMMotionBlocking
	HMMotionBlockingNoLeaves
)

var (
	hmFlagsOnce sync.Once
	hmFlags     []uint8
)

func buildHeightmapFlags() {
	n := MaxState() + 1
	flags := make([]uint8, n)
	mark := func(tag string, bit uint8) {
		for _, r := range BlockTag(tag) {
			for s := r[0]; s <= r[1] && s < n; s++ {
				flags[s] |= bit
			}
		}
	}
	mark("blocks_motion_in_heightmap", HMOceanFloor|HMMotionBlocking)
	mark("blocks_motion_in_heightmap_no_leaves", HMMotionBlockingNoLeaves)
	air := make([]bool, n)
	for _, r := range BlockTag("air") {
		for s := r[0]; s <= r[1] && s < n; s++ {
			air[s] = true
		}
	}
	air[Air] = true
	for s := uint32(0); s < n; s++ {
		if !air[s] {
			flags[s] |= HMWorldSurface
		}
		if HoldsWater(s) || IsLava(s) { // !getFluidState().isEmpty()
			flags[s] |= HMMotionBlocking | HMMotionBlockingNoLeaves
		}
	}
	hmFlags = flags
}

// HeightmapFlags is the set of live-world heightmaps (HM* bits) a block
// state counts toward.
func HeightmapFlags(state uint32) uint8 {
	hmFlagsOnce.Do(buildHeightmapFlags)
	if int(state) >= len(hmFlags) {
		return 0
	}
	return hmFlags[state]
}
