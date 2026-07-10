package server

import "tachyne/internal/worldgen"

// The happy ghast growth chain (1.21.6 "Chase the Skies"). A placed dried_ghast
// block hydrates while it sits in water or is rained on; once fully hydrated it
// hatches a ghastling — a baby happy ghast that matures (via updateBreeding)
// into a rideable adult. Riding + the harness live in harness.go.

var (
	driedGhastBase = worldgen.BlockBase("dried_ghast") // facing(4) × hydration(0-3) × waterlogged(2)
	driedGhastMax  = driedGhastBase + 31
)

func isDriedGhast(state uint32) bool { return state >= driedGhastBase && state <= driedGhastMax }

// waterAdjacent reports whether any of the six neighbours of a cell is water —
// our stand-in for vanilla's "waterlogged" hydration source, since placement
// doesn't waterlog blocks here.
func (h *hub) waterAdjacent(x, y, z int) bool {
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if worldgen.IsWater(h.world.At(x+d[0], y+d[1], z+d[2])) {
			return true
		}
	}
	return false
}

// driedGhastStepChance rate-matches vanilla's HYDRATION_TICK_DELAY of 5000 ticks
// per hydration step: our random tick hits a given block roughly every ~1366
// ticks, so advancing on 1-in-4 of them averages ~5460 ticks per step. (Vanilla
// schedules a delayed tick; the engine has no per-block scheduled-state tracking,
// so a probabilistic gate on the random tick reproduces the same average rate.)
const driedGhastStepChance = 4

// tickDriedGhast advances a dried_ghast one hydration step: it fills while wet
// (submerged in water — the engine has no waterlogging, so "water adjacent"
// stands in for vanilla's WATERLOGGED, and vanilla never hydrates from rain) and
// dries out otherwise. At full hydration a wet step hatches a ghastling and
// consumes the block. Returns true if the state was a dried ghast (so
// randomTickBlock stops).
func (h *hub) tickDriedGhast(players map[int32]*tracked, x, y, z int, state uint32) bool {
	if !isDriedGhast(state) {
		return false
	}
	if h.rng.Intn(driedGhastStepChance) != 0 {
		return true // rate-match vanilla's 5000-tick step delay
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	hyd := 0
	if v := worldgen.GetProperty(info, state, "hydration"); v != "" {
		hyd = int(v[0] - '0')
	}
	wet := h.waterAdjacent(x, y, z)
	switch {
	case wet && hyd >= 3:
		h.hatchGhastling(players, x, y, z, state)
	case wet && hyd < 3:
		h.playSound(players, "minecraft:block.dried_ghast.transition", sndNeutral, float64(x), float64(y), float64(z), 1, 1)
		h.setBlock(players, blockPos{x, y, z}, worldgen.SetProperty(info, state, "hydration", string(rune('0'+hyd+1))))
	case !wet && hyd > 0:
		h.setBlock(players, blockPos{x, y, z}, worldgen.SetProperty(info, state, "hydration", string(rune('0'+hyd-1))))
	}
	return true
}

// hatchGhastling consumes the dried_ghast and spawns a baby happy ghast at the
// block's bottom-centre, head facing the block's FACING (vanilla spawnGhastling).
// It matures into an adult via updateBreeding. The baby flag is metadata index
// 16 — stable through 26.2 (renders a small ghastling); on pre-1.21.6 clients the
// mob is a substituted Ghast and the gateway drops the flag.
func (h *hub) hatchGhastling(players map[int32]*tracked, x, y, z int, state uint32) {
	h.setBlock(players, blockPos{x, y, z}, worldgen.Air)
	m := h.spawnSpecies(players, entityHappyGhast, 0, float64(x)+0.5, float64(y), float64(z)+0.5)
	m.baby, m.growLeft = true, growUpTicks
	if info, ok := worldgen.InfoForState(state); ok {
		m.yaw = facingYaw(worldgen.GetProperty(info, state, "facing"))
		m.syaw = m.yaw
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	h.playSound(players, "minecraft:entity.ghastling.spawn", sndNeutral, float64(x), float64(y), float64(z), 1, 1)
}

// facingYaw maps a horizontal facing property to a Minecraft yaw (Direction.getYRot).
func facingYaw(facing string) float32 {
	switch facing {
	case "south":
		return 0
	case "west":
		return 90
	case "north":
		return 180
	case "east":
		return 270
	}
	return 0
}
