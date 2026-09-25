package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The happy ghast growth chain (1.21.6 "Chase the Skies"). A placed dried_ghast
// block hydrates a step every 5000 ticks while waterlogged and dries out
// otherwise; once fully hydrated it hatches a ghastling — a baby happy ghast that matures (via updateBreeding)
// into a rideable adult. Riding + the harness live in harness.go.

var (
	driedGhastBase = worldgen.BlockBase("dried_ghast") // facing(4) × hydration(0-3) × waterlogged(2)
	driedGhastMax  = driedGhastBase + 31
)

func isDriedGhast(state uint32) bool { return state >= driedGhastBase && state <= driedGhastMax }

// driedGhastDelay is DriedGhastBlock.HYDRATION_TICK_DELAY: a random tick
// on a wet (waterlogged) or partly hydrated dried ghast schedules one step
// this far ahead, unless one is already scheduled.
const driedGhastDelay = 5000

// tickDriedGhast is DriedGhastBlock.randomTick: it only schedules the next
// hydration step. Returns true if the state was a dried ghast (so
// randomTickBlock stops).
func (h *hub) tickDriedGhast(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isDriedGhast(state) {
		return false
	}
	if !boolProp(state, "waterlogged") && driedGhastHydration(state) == 0 {
		return true
	}
	key := simPos{dim: dim, blockPos: blockPos{x, y, z}}
	if due, armed := h.driedGhastDue[key]; armed && due > h.tick.Load() {
		return true // hasScheduledTick (a past due is a leftover of a broken block)
	}
	if h.driedGhastDue == nil {
		h.driedGhastDue = map[simPos]uint64{}
	}
	h.driedGhastDue[key] = h.tick.Load() + driedGhastDelay
	h.scheduleIn(dim, key.blockPos, driedGhastDelay)
	return true
}

// driedGhastHydration is the HYDRATION_LEVEL property, 0-3.
func driedGhastHydration(state uint32) int {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return 0
	}
	if v := worldgen.GetProperty(info, state, "hydration"); v != "" {
		return int(v[0] - '0')
	}
	return 0
}

// driedGhastStep is DriedGhastBlock.tick, on its own scheduled tick only (a
// neighbour's update reaching the cell does nothing): waterlogged, it takes
// a step of water with the transition sound — or, full, hatches; dry, it
// loses one. Each step is a BLOCK_CHANGE.
func (h *hub) driedGhastStep(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	if !isDriedGhast(state) {
		return false
	}
	key := simPos{dim: dim, blockPos: pos}
	due, armed := h.driedGhastDue[key]
	if !armed || h.tick.Load() < due {
		return true
	}
	delete(h.driedGhastDue, key)
	info, _ := worldgen.InfoForState(state)
	hyd := driedGhastHydration(state)
	x, y, z := pos.x, pos.y, pos.z
	switch {
	case boolProp(state, "waterlogged") && hyd >= 3:
		h.hatchGhastling(players, dim, x, y, z, state)
	case boolProp(state, "waterlogged"):
		h.playSoundDim(players, dim, "minecraft:block.dried_ghast.transition", sndBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
		h.setBlockAt(players, dim, pos, worldgen.SetProperty(info, state, "hydration", string(rune('0'+hyd+1))))
		h.vib(dim, freqBlockChange, x, y, z, 0)
	case hyd > 0:
		h.setBlockAt(players, dim, pos, worldgen.SetProperty(info, state, "hydration", string(rune('0'+hyd-1))))
		h.vib(dim, freqBlockChange, x, y, z, 0)
	}
	return true
}

// hatchGhastling consumes the dried_ghast and spawns a baby happy ghast at the
// block's bottom-centre, head facing the block's FACING (vanilla spawnGhastling).
// It matures into an adult via updateBreeding. The baby flag is metadata index
// 16 — stable through 26.2 (renders a small ghastling); on pre-1.21.6 clients the
// mob is a substituted Ghast and the gateway drops the flag.
func (h *hub) hatchGhastling(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	h.setBlockAt(players, dim, blockPos{x, y, z}, worldgen.Air)
	m := h.spawnSpecies(players, entityHappyGhast, dim, float64(x)+0.5, float64(y), float64(z)+0.5)
	if m == nil {
		return
	}
	m.baby, m.growLeft = true, growUpTicks
	if info, ok := worldgen.InfoForState(state); ok {
		m.yaw = facingYaw(worldgen.GetProperty(info, state, "facing"))
		m.syaw = m.yaw
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	h.playSoundDim(players, dim, "minecraft:entity.ghastling.spawn", sndBlock, m.x, m.y, m.z, 1, 1)
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

// flyAim sets the height a flier is being led to this update.
func (m *mob) flyAim(now uint64, y float64) { m.flyAimY, m.flyAimAt = y, now+1 } // +1: zero means never

// flyAimed is the height a flier was led to within the last few ticks.
func (m *mob) flyAimed(now uint64) (float64, bool) {
	return m.flyAimY, m.flyAimAt != 0 && now+1-m.flyAimAt <= 4
}

// ghastlingFollowPlayer is HappyGhastAi's BabyFollowAdult on
// NEAREST_VISIBLE_PLAYER: a ghastling keeps near the nearest player within
// sixteen blocks, closing to three, at 1.1 times its speed — before it
// looks for a grown happy ghast to follow.
func (h *hub) ghastlingFollowPlayer(players map[int32]*tracked, m *mob) bool {
	if !m.baby {
		return false
	}
	var best *tracked
	bestD := 16.0
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if d := dist3(t.x, t.y, t.z, m.x, m.y, m.z); d < bestD {
			best, bestD = t, d
		}
	}
	if best == nil {
		return false
	}
	m.flyAim(h.tick.Load(), best.y)
	if bestD <= 3 {
		m.vx, m.vz = 0, 0
		return true
	}
	h.steerTo(m, best.x, best.z, 1.1)
	return true
}
