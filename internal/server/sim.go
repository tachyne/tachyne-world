package server

import "tachyne/internal/worldgen"

// World simulation: falling blocks and fluid flow, driven by the hub's tick loop.
// Each changed block schedules re-evaluations of itself and its neighbours; the
// tick loop processes the ones that come due. Everything here runs on the hub
// goroutine, so world reads/writes and broadcasts need no extra locking.

type blockPos struct{ x, y, z int }

const (
	fallDelay         = 1    // ticks between a falling block's steps (≈ gravity)
	waterDelay        = 5    // ticks between water spread steps (vanilla)
	lavaDelay         = 30   // ticks between lava spread steps (vanilla overworld)
	maxUpdatesPerTick = 8192 // cap per tick so a big flood can't stall the loop
)

var (
	horizNeighbors = [4]blockPos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}}
	allNeighbors   = [6]blockPos{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
)

func inWorldY(y int) bool {
	return y >= worldgen.MinY && y < worldgen.MinY+worldgen.SectionCount*16
}

// schedule queues a block update `delay` ticks from now.
func (h *hub) schedule(pos blockPos, delay uint64) {
	if !h.ownedBlock(pos.x, pos.z) {
		return // don't simulate blocks outside this pod's region
	}
	due := h.tick.Load() + delay
	h.pending[due] = append(h.pending[due], pos)
}

// scheduleAround queues a block and its six neighbours.
func (h *hub) scheduleAround(pos blockPos, delay uint64) {
	h.schedule(pos, delay)
	for _, d := range allNeighbors {
		h.schedule(blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}, delay)
	}
}

// runUpdates processes the block updates due this tick (capped; overflow rolls
// to the next tick so a large flood spreads its cost instead of stalling).
func (h *hub) runUpdates(players map[int32]*tracked, age uint64) {
	due := h.pending[age]
	if due == nil {
		return
	}
	delete(h.pending, age)
	if len(due) > maxUpdatesPerTick {
		h.pending[age+1] = append(h.pending[age+1], due[maxUpdatesPerTick:]...)
		due = due[:maxUpdatesPerTick]
	}
	for _, pos := range due {
		h.processUpdate(players, pos)
	}
}

func (h *hub) processUpdate(players map[int32]*tracked, pos blockPos) {
	if !inWorldY(pos.y) {
		return
	}
	state := h.world.Block(pos.x, pos.y, pos.z)
	switch {
	case worldgen.IsFalling(state):
		h.updateFalling(players, pos, state)
	case worldgen.IsFluid(state):
		h.updateFluid(players, pos, state)
	case isFire(state):
		h.updateFire(players, pos)
	default:
		h.updateRedstone(players, pos, state)
	}
}

// setBlock applies a simulation-driven change and broadcasts it to nearby players.
func (h *hub) setBlock(players map[int32]*tracked, pos blockPos, state uint32) {
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	h.broadcastBlock(players, pos.x, pos.y, pos.z, state)
	h.spillContainer(players, pos.x, pos.y, pos.z, state)
}

// broadcastBlock sends a Block Update to every player tracking the chunk.
func (h *hub) broadcastBlock(players map[int32]*tracked, x, y, z int, state uint32) {
	bcx, bcz := chunkFloor(float64(x)), chunkFloor(float64(z))
	body := blockSetEv(x, y, z, state)
	for _, t := range players {
		if t.dim != 0 {
			continue // hub simulation is overworld-only (v1)
		}
		if abs(chunkFloor(t.x)-bcx) <= viewRadius && abs(chunkFloor(t.z)-bcz) <= viewRadius {
			t.p.trySendEv(body)
		}
	}
}

// updateFalling drops a gravity-affected block one cell if the space below is
// replaceable, then reschedules so it keeps falling and the block above re-checks.
func (h *hub) updateFalling(players map[int32]*tracked, pos blockPos, state uint32) {
	below := blockPos{pos.x, pos.y - 1, pos.z}
	if !inWorldY(below.y) {
		return
	}
	if worldgen.IsReplaceable(h.world.Block(below.x, below.y, below.z)) {
		h.setBlock(players, pos, worldgen.Air)
		h.setBlock(players, below, state)
		h.schedule(below, fallDelay)     // keep falling
		h.scheduleAround(pos, fallDelay) // a block resting on it loses support
	}
}

// updateFluid spreads or recedes a fluid cell. Levels: base+0 source, base+1..7
// flowing (1 strongest), base+8 falling.
func (h *hub) updateFluid(players map[int32]*tracked, pos blockPos, state uint32) {
	water := worldgen.IsWater(state)
	base, maxLevel, delay, step := worldgen.WaterBase, 7, uint64(waterDelay), 1
	if !water {
		base, maxLevel, delay, step = worldgen.LavaBase, 6, uint64(lavaDelay), 2
	}
	same := func(s uint32) bool {
		if water {
			return worldgen.IsWater(s)
		}
		return worldgen.IsLava(s)
	}
	level := int(state - base)

	// Water/lava contact solidifies (LiquidBlock.shouldSpreadLiquid + LavaFluid.
	// spreadTo, reimplemented from the vanilla 1.21.5 source). Lava touched by
	// water above or beside it turns to obsidian (source) / cobblestone (flowing).
	if !water {
		if h.lavaSolidify(players, pos, level) {
			return
		}
	} else {
		// A processed water cell wakes any adjacent lava so it can solidify.
		for _, d := range lavaContactDirs {
			n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
			if worldgen.IsLava(h.world.Block(n.x, n.y, n.z)) {
				h.schedule(n, 1)
			}
		}
	}

	// Receding: a non-source cell that lost its path back to a source dries up,
	// which then makes its neighbours re-check — so removing a source drains it.
	if level >= 1 && !h.fluidSupported(pos, base, level, same) {
		h.setBlock(players, pos, worldgen.Air)
		h.scheduleAround(pos, delay)
		return
	}

	// Flow straight down. Lava reaching water below forms stone (spreadTo DOWN).
	below := blockPos{pos.x, pos.y - 1, pos.z}
	if !water && inWorldY(below.y) && worldgen.IsWater(h.world.Block(below.x, below.y, below.z)) {
		h.setBlock(players, below, worldgen.Stone)
		h.fizz(players, below)
		h.scheduleAround(below, delay)
		return
	}
	canFall := inWorldY(below.y) && worldgen.IsReplaceable(h.world.Block(below.x, below.y, below.z))
	if canFall {
		h.setBlock(players, below, base+8)
		h.schedule(below, delay)
	}

	// Spread sideways only when it can't keep falling. Sources and just-landed
	// falling fluid spread at full strength (level = step). Direction is chosen
	// by slope-distance to the nearest drop (FlowingFluid.getSpread), so fluid
	// runs toward holes instead of flooding every neighbour equally.
	if !canFall {
		next := level + step
		if level == 0 || level >= 8 {
			next = step
		}
		if next <= maxLevel {
			findDist := 4 // getSlopeFindDistance: water 4, lava 2 (overworld)
			if !water {
				findDist = 2
			}
			for _, d := range h.flowDirections(pos, findDist) {
				n := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
				h.setBlock(players, n, base+uint32(next))
				h.schedule(n, delay)
			}
		}
	}
}

// lavaContactDirs are the directions from which water solidifies a lava block:
// up plus the four horizontals (the opposites of lava's flow directions, per
// LiquidBlock.POSSIBLE_FLOW_DIRECTIONS).
var lavaContactDirs = [5]blockPos{{0, 1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}}

// lavaSolidify turns a lava block that water is touching into obsidian (source,
// level 0) or cobblestone (flowing). Returns true if it solidified.
func (h *hub) lavaSolidify(players map[int32]*tracked, pos blockPos, level int) bool {
	for _, d := range lavaContactDirs {
		n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
		if worldgen.IsWater(h.world.Block(n.x, n.y, n.z)) {
			block := worldgen.Cobblestone
			if level == 0 {
				block = worldgen.Obsidian
			}
			h.setBlock(players, pos, block)
			h.fizz(players, pos)
			h.scheduleAround(pos, 1)
			return true
		}
	}
	return false
}

// fizz plays the lava-quench sound where a fluid solidified (vanilla levelEvent 1501).
func (h *hub) fizz(players map[int32]*tracked, pos blockPos) {
	h.playSound(players, "minecraft:block.lava.extinguish", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 2.6)
}

// flowDirections returns the horizontal directions a fluid should spread into:
// the passable neighbours whose slope-distance to the nearest drop is minimal
// (FlowingFluid.getSpread). On flat ground every open direction ties, so it
// still spreads outward; near a ledge it runs toward the ledge.
func (h *hub) flowDirections(pos blockPos, findDist int) []blockPos {
	var dist [4]int
	best := 1 << 30
	for i, d := range horizNeighbors {
		n := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		if !worldgen.IsReplaceable(h.world.Block(n.x, n.y, n.z)) {
			dist[i] = 1 << 30 // impassable — never a candidate
			continue
		}
		if h.fluidHole(n) {
			dist[i] = 0
		} else {
			dist[i] = h.slopeDist(n, 1, findDist, i^1)
		}
		if dist[i] < best {
			best = dist[i]
		}
	}
	if best == 1<<30 {
		return nil // fully boxed in
	}
	out := make([]blockPos, 0, 4)
	for i, d := range horizNeighbors {
		if dist[i] == best {
			out = append(out, d)
		}
	}
	return out
}

// slopeDist is the shortest horizontal step count from pos to a cell with a drop
// below it, searching up to findDist (FlowingFluid.getSlopeDistance). `from` is
// the reverse direction index to skip. Returns findDist+1 when no drop is found.
func (h *hub) slopeDist(pos blockPos, depth, findDist, from int) int {
	best := findDist + 1
	for i, d := range horizNeighbors {
		if i == from {
			continue
		}
		n := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		if !worldgen.IsReplaceable(h.world.Block(n.x, n.y, n.z)) {
			continue
		}
		if h.fluidHole(n) {
			return depth
		}
		if depth < findDist {
			if r := h.slopeDist(n, depth+1, findDist, i^1); r < best {
				best = r
			}
		}
	}
	return best
}

// fluidHole reports whether fluid at pos could fall (the cell below is open).
func (h *hub) fluidHole(pos blockPos) bool {
	b := pos.y - 1
	return inWorldY(b) && worldgen.IsReplaceable(h.world.Block(pos.x, b, pos.z))
}

// fluidSupported reports whether a flowing/falling cell still has a path to a
// source: fed from directly above, or a horizontal neighbour closer to a source.
func (h *hub) fluidSupported(pos blockPos, base uint32, level int, same func(uint32) bool) bool {
	if same(h.world.Block(pos.x, pos.y+1, pos.z)) {
		return true // fed from above
	}
	if level >= 8 {
		return false // falling fluid is only supported from above
	}
	for _, d := range horizNeighbors {
		ns := h.world.Block(pos.x+d.x, pos.y, pos.z+d.z)
		if same(ns) {
			nl := int(ns - base)
			if nl == 8 {
				nl = 0 // falling counts as full strength
			}
			if nl < level {
				return true
			}
		}
	}
	return false
}
