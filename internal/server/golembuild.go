package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Pumpkin-built golems — CarvedPumpkinBlock.trySpawnGolem. Placing a carved
// pumpkin (or jack o'lantern) that completes one of the patterns spawns the
// golem: the pattern's blocks are cleared with break particles, the golem
// stands on the bottom block's cell (+0.05 y), and summoned_entity fires for
// every player within 5 blocks of its body. Vanilla tries snow, then iron,
// then copper (coppergolem.go), so a pumpkin that completes two patterns at
// once builds in that order.
//
//	snow:  "^"          iron:  "~^~"      (^ pumpkin, # snow/iron block,
//	       "#"                 "###"       ~ air; one column / one row, the
//	       "#"                 "~#~"       row along either horizontal axis)

var (
	snowBlockState = worldgen.BlockBase("snow_block")
	ironBlockState = worldgen.BlockBase("iron_block")
)

// checkGolemBuild runs after a block is placed. Returns true when a snow or
// iron golem was built (so the copper pattern is not also tried).
func (h *hub) checkGolemBuild(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isCarvedPumpkin(state) {
		return false
	}
	w := h.worldFor(dim)
	at := func(dx, dy, dz int) uint32 { return w.At(x+dx, y+dy, z+dz) }
	isAir := func(dx, dy, dz int) bool { return at(dx, dy, dz) == worldgen.Air }

	// Snow golem: two snow blocks under the pumpkin.
	if at(0, -1, 0) == snowBlockState && at(0, -2, 0) == snowBlockState {
		h.clearPattern(players, dim, [][3]int{{x, y, z}, {x, y - 1, z}, {x, y - 2, z}})
		m := h.spawnSpecies(players, entitySnowGolem, dim, float64(x)+0.5, float64(y-2)+0.05, float64(z)+0.5)
		if m != nil {
			m.yaw, m.syaw = 0, 0
			h.golemSummoned(players, m, "snow_golem", 0.7, 1.9)
		}
		return true
	}
	// Iron golem: a T of iron blocks under the pumpkin, arms along x or z,
	// with air beside the pumpkin and beside the foot.
	if at(0, -1, 0) == ironBlockState && at(0, -2, 0) == ironBlockState {
		for _, d := range [][2]int{{1, 0}, {0, 1}} {
			ax, az := d[0], d[1]
			if at(ax, -1, az) != ironBlockState || at(-ax, -1, -az) != ironBlockState {
				continue
			}
			if !isAir(ax, 0, az) || !isAir(-ax, 0, -az) || !isAir(ax, -2, az) || !isAir(-ax, -2, -az) {
				continue
			}
			h.clearPattern(players, dim, [][3]int{{x, y, z}, {x, y - 1, z}, {x, y - 2, z},
				{x + ax, y - 1, z + az}, {x - ax, y - 1, z - az}})
			m := h.spawnMobIn(players, entityIronGolem, dim, float64(x)+0.5, float64(y-2)+0.05, float64(z)+0.5)
			if m != nil {
				m.health = 100
				m.setKBResist(1) // IronGolem KNOCKBACK_RESISTANCE
				m.behavior = golemBehavior{}
				m.home = blockPos{x, y - 2, z} // a player's golem has no village: it stays about
				m.yaw, m.syaw = 0, 0
				h.golemSummoned(players, m, "iron_golem", 1.4, 2.7)
			}
			return true
		}
	}
	return false
}

// clearPattern is CarvedPumpkinBlock.clearPatternBlocks: every pattern block
// becomes air with the 2001 break event (particles + sound) of what it was.
func (h *hub) clearPattern(players map[int32]*tracked, dim int, cells [][3]int) {
	w := h.worldFor(dim)
	for _, c := range cells {
		old := w.At(c[0], c[1], c[2])
		h.setBlockLive(players, dim, c[0], c[1], c[2], worldgen.Air)
		h.toNearbyEv(players, dim, float64(c[0]), float64(c[2]), blockBreakEvent(c[0], c[1], c[2], old))
	}
}

// golemSummoned fires summoned_entity for every player whose position is
// within the golem's bounding box inflated by 5 (getEntitiesOfClass on
// getBoundingBox().inflate(5.0)).
func (h *hub) golemSummoned(players map[int32]*tracked, m *mob, name string, width, height float64) {
	half := width/2 + 5
	for _, t := range players {
		if t.dim != m.dim || t.dead {
			continue
		}
		if t.x < m.x-half || t.x > m.x+half || t.z < m.z-half || t.z > m.z+half ||
			t.y < m.y-5 || t.y > m.y+height+5 {
			continue
		}
		h.advance(players, t, "summoned_entity", advMatch{entity: name})
	}
}
