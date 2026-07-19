package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

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

func (h *hub) inWorldY(y int) bool {
	return y >= worldgen.MinY && y < h.world.Ceiling()
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
	// Dedupe: a position scheduled several times for this tick (scheduleAround
	// overlaps heavily for fluids) must process only ONCE — vanilla scheduleTick
	// is idempotent per position. Without this a receding pool re-processes the
	// same cells exponentially and never settles.
	seen := make(map[blockPos]struct{}, len(due))
	for _, pos := range due {
		if _, ok := seen[pos]; ok {
			continue
		}
		seen[pos] = struct{}{}
		h.processUpdate(players, pos)
	}
}

func (h *hub) processUpdate(players map[int32]*tracked, pos blockPos) {
	if !h.inWorldY(pos.y) {
		return
	}
	state := h.world.Block(pos.x, pos.y, pos.z)
	switch {
	case worldgen.IsConcretePowder(state) && h.powderTouchesWater(pos):
		h.setBlock(players, pos, worldgen.ConcreteFor(state))
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
	if !h.inWorldY(below.y) {
		return
	}
	if worldgen.IsReplaceable(h.world.Block(below.x, below.y, below.z)) {
		h.setBlock(players, pos, worldgen.Air)
		h.setBlock(players, below, state)
		h.schedule(below, fallDelay)     // keep falling
		h.scheduleAround(pos, fallDelay) // a block resting on it loses support
		return
	}
	// Landed: concrete powder touching water turns to concrete (ConcretePowderBlock).
	if worldgen.IsConcretePowder(state) && h.powderTouchesWater(pos) {
		h.setBlock(players, pos, worldgen.ConcreteFor(state))
	}
}

// powderTouchesWater reports whether water sits on any non-down side of a
// concrete-powder cell (vanilla ConcretePowderBlock.touchesLiquid).
func (h *hub) powderTouchesWater(pos blockPos) bool {
	for _, d := range lavaContactDirs { // up + 4 horizontals
		if worldgen.IsWater(h.world.Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)) {
			return true
		}
	}
	return false
}

// updateFluid is vanilla FlowingFluid.tick(pos): first a non-source cell
// recomputes itself from its neighbours (getNewLiquid — the level/recede half),
// then the cell spreads down and to the sides (the spread half). Levels:
// base+0 source, base+1..7 flowing (1 strongest), base+8 falling.
func (h *hub) updateFluid(players map[int32]*tracked, pos blockPos, state uint32) {
	water := worldgen.IsWater(state)
	base, delay, dropOff, slopeFind := worldgen.WaterBase, uint64(waterDelay), 1, 4
	if !water {
		base, delay, dropOff, slopeFind = worldgen.LavaBase, uint64(lavaDelay), 2, 2
	}
	same := func(s uint32) bool {
		if water {
			return worldgen.IsWater(s)
		}
		return worldgen.IsLava(s)
	}
	level := int(state - base)

	// Water/lava contact solidifies (LiquidBlock.shouldSpreadLiquid + LavaFluid.
	// spreadTo). Lava touched by water above or beside it turns to obsidian
	// (source) / cobblestone (flowing); a water cell wakes adjacent lava so it
	// can solidify on its own tick.
	if !water {
		if h.lavaSolidify(players, pos, level) {
			return
		}
	} else {
		for _, d := range lavaContactDirs {
			n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
			if worldgen.IsLava(h.world.Block(n.x, n.y, n.z)) {
				h.schedule(n, 1)
			}
		}
	}

	// 1. A non-source cell recomputes itself from its neighbours. This is what
	//    makes flowing fluid LEVEL out and RECEDE: if the surrounding fluid
	//    weakened (a source was removed, or a higher neighbour dried up), the
	//    computed state drops a level or empties, and stale edge blocks vanish
	//    instead of floating. Sources are permanent and skip this.
	if level != 0 {
		ns, ok := h.getNewLiquid(pos, water, base, dropOff)
		if !ok {
			h.setBlock(players, pos, worldgen.Air)
			h.scheduleAround(pos, delay)
			return
		}
		if ns != state {
			h.setBlock(players, pos, ns)
			h.scheduleAround(pos, delay)
			state, level = ns, int(ns-base)
		}
	}

	// 2. spread — down first, then to the sides.
	below := blockPos{pos.x, pos.y - 1, pos.z}
	belowB := h.world.Block(below.x, below.y, below.z)
	if !water && h.inWorldY(below.y) && worldgen.IsWater(belowB) {
		h.setBlock(players, below, worldgen.Stone) // lava landing on water → stone
		h.fizz(players, below)
		h.scheduleAround(below, delay)
		return
	}
	// Flow straight down into an open cell (never into existing same-fluid — that
	// cell turns to falling on its own tick via getNewLiquid's "fluid above" rule).
	if h.inWorldY(below.y) && worldgen.IsReplaceable(belowB) && !same(belowB) {
		h.setBlock(players, below, base+8) // falling
		h.schedule(below, delay)
		// Vanilla only pools sideways over a drop when boxed by 3+ sources.
		if h.sourceNeighborCount(pos, base) >= 3 {
			h.spreadSides(players, pos, base, level, dropOff, delay, slopeFind)
		}
		return
	}
	// Can't flow down. Spread to the sides UNLESS this is a flowing cell feeding
	// a hole below (below is same-fluid or could take fluid): then it just feeds
	// the column and does NOT gush sideways — this is what keeps a vertical drop
	// or a wall-hole from leaking, and stops mid-air outward spread off a ledge.
	if level == 0 || !h.fluidHoleBelow(pos, same) {
		h.spreadSides(players, pos, base, level, dropOff, delay, slopeFind)
	}
}

// getNewLiquid is vanilla FlowingFluid.getNewLiquid: the fluid state this cell
// SHOULD hold given its neighbours (ok=false ⇒ it should be empty/air). Source
// conversion (2+ source neighbours over solid ground) is water-only; a fluid
// directly above makes this cell falling; otherwise the level is the strongest
// horizontal neighbour minus the dropOff.
func (h *hub) getNewLiquid(pos blockPos, water bool, base uint32, dropOff int) (uint32, bool) {
	same := func(s uint32) bool {
		if water {
			return worldgen.IsWater(s)
		}
		return worldgen.IsLava(s)
	}
	sourceCount, maxAmount := 0, 0
	for _, d := range horizNeighbors {
		nb := h.world.Block(pos.x+d.x, pos.y, pos.z+d.z)
		if !same(nb) {
			continue
		}
		nl := int(nb - base)
		amt := 8 - nl
		if nl == 0 || nl == 8 { // source or falling = full strength
			amt = 8
		}
		if nl == 0 {
			sourceCount++
		}
		if amt > maxAmount {
			maxAmount = amt
		}
	}
	if water && sourceCount >= 2 { // infinite source (RULE_WATER_SOURCE_CONVERSION)
		if belowB := h.world.Block(pos.x, pos.y-1, pos.z); worldgen.IsSolidFull(belowB) || belowB == base {
			return base, true
		}
	}
	if same(h.world.Block(pos.x, pos.y+1, pos.z)) { // fluid above → falling
		return base + 8, true
	}
	if newAmount := maxAmount - dropOff; newAmount > 0 {
		return base + uint32(8-newAmount), true
	}
	return 0, false // dries up
}

// sourceNeighborCount counts the horizontal source (level-0) neighbours of a
// fluid cell (FlowingFluid.sourceNeighborCount) — the ≥3 pooling gate.
func (h *hub) sourceNeighborCount(pos blockPos, base uint32) int {
	n := 0
	for _, d := range horizNeighbors {
		if h.world.Block(pos.x+d.x, pos.y, pos.z+d.z) == base {
			n++
		}
	}
	return n
}

// fluidHoleBelow is vanilla FlowingFluid.isWaterHole: the cell below can take
// this fluid (it already holds it, or it is replaceable) — so the fluid should
// feed downward rather than spread sideways.
func (h *hub) fluidHoleBelow(pos blockPos, same func(uint32) bool) bool {
	belowB := h.world.Block(pos.x, pos.y-1, pos.z)
	return same(belowB) || worldgen.IsReplaceable(belowB)
}

// spreadSides is vanilla FlowingFluid.spreadToSides: place flowing fluid one
// step weaker in the direction(s) with the shortest slope-distance to a drop
// (flowDirections). A falling cell spreads at full strength on landing.
func (h *hub) spreadSides(players map[int32]*tracked, pos blockPos, base uint32, level, dropOff int, delay uint64, slopeFind int) {
	amount := 8 - level
	if level == 0 || level == 8 { // source or falling = full
		amount = 8
	}
	n := amount - dropOff
	if level == 8 {
		n = 7 // vanilla: a landing falling column spreads at amount 7
	}
	if n <= 0 {
		return
	}
	out := base + uint32(8-n) // the flowing level to lay down
	for _, d := range h.flowDirections(pos, slopeFind) {
		np := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		h.setBlock(players, np, out)
		h.schedule(np, delay)
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
	return h.inWorldY(b) && worldgen.IsReplaceable(h.world.Block(pos.x, b, pos.z))
}
