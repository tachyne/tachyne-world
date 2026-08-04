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

func (h *hub) inWorldY(y int) bool { return h.inWorldYIn(0, y) }

// inWorldYIn bounds a y against the given dimension's own ceiling.
func (h *hub) inWorldYIn(dim, y int) bool {
	return y >= worldgen.MinY && y < h.worldFor(dim).Ceiling()
}

// simPos is a scheduled position together with the dimension it belongs to.
// The dimension is part of the key because the same x/y/z exists in all three
// worlds: without it, a Nether update rewrites the overworld.
type simPos struct {
	dim      int
	blockPos // the block itself, embedded so a simPos reads like one: .x/.y/.z
}

// at returns another block in the same dimension as p — the common move when
// a block entity reaches for a neighbour (the cell a hopper pulls from, the one
// a dispenser faces) and must not stray into another world's copy of it.
func (p simPos) at(b blockPos) simPos { return simPos{dim: p.dim, blockPos: b} }

// schedule queues an OVERWORLD block update `delay` ticks from now.
func (h *hub) schedule(pos blockPos, delay uint64) { h.scheduleIn(0, pos, delay) }

// scheduleIn queues a block update in a specific dimension.
func (h *hub) scheduleIn(dim int, pos blockPos, delay uint64) {
	if dim == 0 && !h.ownedBlock(pos.x, pos.z) {
		return // don't simulate blocks outside this pod's region
	}
	due := h.tick.Load() + delay
	h.pending[due] = append(h.pending[due], simPos{dim: dim, blockPos: pos})
}

// scheduleAround queues a block and its six neighbours in the overworld.
func (h *hub) scheduleAround(pos blockPos, delay uint64) { h.scheduleAroundIn(0, pos, delay) }

// scheduleAroundIn queues a block and its six neighbours in a dimension.
func (h *hub) scheduleAroundIn(dim int, pos blockPos, delay uint64) {
	h.scheduleIn(dim, pos, delay)
	for _, d := range allNeighbors {
		h.scheduleIn(dim, blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}, delay)
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
	// same cells exponentially and never settles. Keyed by dimension too, so the
	// same coordinates in two worlds are two distinct updates.
	seen := make(map[simPos]struct{}, len(due))
	for _, sp := range due {
		if _, ok := seen[sp]; ok {
			continue
		}
		seen[sp] = struct{}{}
		h.processUpdate(players, sp.dim, sp.blockPos)
	}
}

func (h *hub) processUpdate(players map[int32]*tracked, dim int, pos blockPos) {
	if !h.inWorldY(pos.y) {
		return
	}
	state := h.worldFor(dim).Block(pos.x, pos.y, pos.z)
	switch {
	case h.tickFrostedIce(players, dim, pos, state):
		// Frost Walker's ice ages itself back to water on its own schedule.
	case worldgen.IsConcretePowder(state) && h.powderTouchesWater(dim, pos):
		h.setBlockAt(players, dim, pos, worldgen.ConcreteFor(state))
	case h.updateLeafDistance(players, dim, pos.x, pos.y, pos.z, state):
		// A leaf whose neighbourhood changed recomputes its trunk distance;
		// the write schedules ITS neighbours, so a felled trunk sends the
		// recompute through the canopy as a wave and the rim rots first.
	case worldgen.IsFalling(state):
		h.updateFalling(players, dim, pos, state)
	case worldgen.IsFluid(state):
		h.updateFluid(players, dim, pos, state)
	case isFire(state):
		h.updateFire(players, pos)
	case h.tickSnifferEgg(players, dim, pos.x, pos.y, pos.z, state):
		// The egg cracks twice and then opens.
	case h.tickChorusPlant(players, dim, pos.x, pos.y, pos.z, state):
		// A plant segment that lost its footing pops, taking the rest with it.
	case h.tickCoral(players, dim, pos, state):
		// Coral left out of water bleaches to its dead twin.
	case h.tickComposter(players, dim, pos, state):
		// A full composter finishes composting a second after its last item.
	default:
		h.updateRedstone(players, pos, state)
	}
}

// setBlock applies an OVERWORLD simulation change and broadcasts it.
func (h *hub) setBlock(players map[int32]*tracked, pos blockPos, state uint32) {
	h.setBlockAt(players, 0, pos, state)
}

// setBlockAt applies a simulation-driven change in a dimension and broadcasts
// it to the players standing in that dimension.
func (h *hub) setBlockAt(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	h.worldFor(dim).SetBlock(pos.x, pos.y, pos.z, state)
	h.broadcastBlockIn(players, dim, pos.x, pos.y, pos.z, state)
	h.spillContainer(players, dim, pos.x, pos.y, pos.z, state)
}

// broadcastBlock sends a Block Update to overworld players tracking the chunk.
func (h *hub) broadcastBlock(players map[int32]*tracked, x, y, z int, state uint32) {
	h.broadcastBlockIn(players, 0, x, y, z, state)
}

// broadcastBlockIn sends a Block Update to players in `dim` tracking the chunk.
func (h *hub) broadcastBlockIn(players map[int32]*tracked, dim, x, y, z int, state uint32) {
	bcx, bcz := chunkFloor(float64(x)), chunkFloor(float64(z))
	body := blockSetEv(x, y, z, state)
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		if abs(chunkFloor(t.x)-bcx) <= viewRadius && abs(chunkFloor(t.z)-bcz) <= viewRadius {
			t.p.trySendEv(body)
		}
	}
}

// updateFalling drops a gravity-affected block one cell if the space below is
// replaceable, then reschedules so it keeps falling and the block above re-checks.
func (h *hub) updateFalling(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	below := blockPos{pos.x, pos.y - 1, pos.z}
	if !h.inWorldY(below.y) {
		return
	}
	if worldgen.IsReplaceable(h.worldFor(dim).Block(below.x, below.y, below.z)) {
		h.setBlockAt(players, dim, pos, worldgen.Air)
		h.setBlockAt(players, dim, below, state)
		h.scheduleIn(dim, below, fallDelay)     // keep falling
		h.scheduleAroundIn(dim, pos, fallDelay) // a block resting on it loses support
		return
	}
	// Landed: concrete powder touching water turns to concrete (ConcretePowderBlock).
	if worldgen.IsConcretePowder(state) && h.powderTouchesWater(dim, pos) {
		h.setBlockAt(players, dim, pos, worldgen.ConcreteFor(state))
	}
}

// powderTouchesWater reports whether water sits on any non-down side of a
// concrete-powder cell (vanilla ConcretePowderBlock.touchesLiquid).
func (h *hub) powderTouchesWater(dim int, pos blockPos) bool {
	for _, d := range lavaContactDirs { // up + 4 horizontals
		if worldgen.IsWater(h.worldFor(dim).Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)) {
			return true
		}
	}
	return false
}

// updateFluid is vanilla FlowingFluid.tick(pos): first a non-source cell
// recomputes itself from its neighbours (getNewLiquid — the level/recede half),
// then the cell spreads down and to the sides (the spread half). Levels:
// base+0 source, base+1..7 flowing (1 strongest), base+8 falling.
func (h *hub) updateFluid(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
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
		if h.lavaSolidify(players, dim, pos, level) {
			return
		}
	} else {
		for _, d := range lavaContactDirs {
			n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
			if worldgen.IsLava(h.worldFor(dim).Block(n.x, n.y, n.z)) {
				h.scheduleIn(dim, n, 1)
			}
		}
	}

	// 1. A non-source cell recomputes itself from its neighbours. This is what
	//    makes flowing fluid LEVEL out and RECEDE: if the surrounding fluid
	//    weakened (a source was removed, or a higher neighbour dried up), the
	//    computed state drops a level or empties, and stale edge blocks vanish
	//    instead of floating. Sources are permanent and skip this.
	if level != 0 {
		ns, ok := h.getNewLiquid(dim, pos, water, base, dropOff)
		if !ok {
			h.setBlockAt(players, dim, pos, worldgen.Air)
			h.scheduleAroundIn(dim, pos, delay)
			return
		}
		if ns != state {
			h.setBlockAt(players, dim, pos, ns)
			h.scheduleAroundIn(dim, pos, delay)
			state, level = ns, int(ns-base)
		}
	}

	// 2. spread — down first, then to the sides.
	below := blockPos{pos.x, pos.y - 1, pos.z}
	belowB := h.worldFor(dim).Block(below.x, below.y, below.z)
	if !water && h.inWorldY(below.y) && worldgen.IsWater(belowB) {
		h.setBlockAt(players, dim, below, worldgen.Stone) // lava landing on water → stone
		h.fizz(players, dim, below)
		h.scheduleAroundIn(dim, below, delay)
		return
	}
	// Flow straight down into an open cell (never into existing same-fluid — that
	// cell turns to falling on its own tick via getNewLiquid's "fluid above" rule).
	if h.inWorldY(below.y) && worldgen.IsReplaceable(belowB) && !same(belowB) {
		h.setBlockAt(players, dim, below, base+8) // falling
		h.scheduleIn(dim, below, delay)
		if water {
			h.wakePowder(dim, below) // flowing water solidifies concrete powder it reaches
		}
		// Vanilla only pools sideways over a drop when boxed by 3+ sources.
		if h.sourceNeighborCount(dim, pos, base) >= 3 {
			h.spreadSides(players, dim, pos, base, level, dropOff, delay, slopeFind)
		}
		return
	}
	// Can't flow down. Spread to the sides UNLESS this is a flowing cell feeding
	// a hole below (below is same-fluid or could take fluid): then it just feeds
	// the column and does NOT gush sideways — this is what keeps a vertical drop
	// or a wall-hole from leaking, and stops mid-air outward spread off a ledge.
	if level == 0 || !h.fluidHoleBelow(dim, pos, same) {
		h.spreadSides(players, dim, pos, base, level, dropOff, delay, slopeFind)
	}
}

// getNewLiquid is vanilla FlowingFluid.getNewLiquid: the fluid state this cell
// SHOULD hold given its neighbours (ok=false ⇒ it should be empty/air). Source
// conversion (2+ source neighbours over solid ground) is water-only; a fluid
// directly above makes this cell falling; otherwise the level is the strongest
// horizontal neighbour minus the dropOff.
func (h *hub) getNewLiquid(dim int, pos blockPos, water bool, base uint32, dropOff int) (uint32, bool) {
	same := func(s uint32) bool {
		if water {
			return worldgen.IsWater(s)
		}
		return worldgen.IsLava(s)
	}
	sourceCount, maxAmount := 0, 0
	for _, d := range horizNeighbors {
		nb := h.worldFor(dim).Block(pos.x+d.x, pos.y, pos.z+d.z)
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
	// Infinite sources: vanilla gates each fluid on its own conversion rule.
	// Water converts by default, lava does not (it is an experimental rule).
	if sourceCount >= 2 && ((water && h.rules.WaterSourceCnv) || (!water && h.rules.LavaSourceCnv)) {
		if belowB := h.worldFor(dim).Block(pos.x, pos.y-1, pos.z); worldgen.IsSolidFull(belowB) || belowB == base {
			return base, true
		}
	}
	if same(h.worldFor(dim).Block(pos.x, pos.y+1, pos.z)) { // fluid above → falling
		return base + 8, true
	}
	if newAmount := maxAmount - dropOff; newAmount > 0 {
		return base + uint32(8-newAmount), true
	}
	return 0, false // dries up
}

// sourceNeighborCount counts the horizontal source (level-0) neighbours of a
// fluid cell (FlowingFluid.sourceNeighborCount) — the ≥3 pooling gate.
func (h *hub) sourceNeighborCount(dim int, pos blockPos, base uint32) int {
	n := 0
	for _, d := range horizNeighbors {
		if h.worldFor(dim).Block(pos.x+d.x, pos.y, pos.z+d.z) == base {
			n++
		}
	}
	return n
}

// fluidHoleBelow is vanilla FlowingFluid.isWaterHole: the cell below can take
// this fluid (it already holds it, or it is replaceable) — so the fluid should
// feed downward rather than spread sideways.
func (h *hub) fluidHoleBelow(dim int, pos blockPos, same func(uint32) bool) bool {
	belowB := h.worldFor(dim).Block(pos.x, pos.y-1, pos.z)
	return same(belowB) || worldgen.IsReplaceable(belowB)
}

// powderWakeDirs are the cells that may hold concrete powder a fluid at the
// centre touches: the cell below the fluid and the four horizontal neighbours
// (powder converts on water above or beside it, never below).
var powderWakeDirs = [5]blockPos{{0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}}

// wakePowder re-checks concrete powder touching a freshly-placed WATER cell so
// FLOWING water solidifies it, not only hand-placed water. tachyne's sim
// setBlock doesn't fire neighbour updates the way vanilla's flag-3 setBlock
// does, so the powder must be woken explicitly (vanilla neighbourChanged →
// ConcretePowderBlock.touchesLiquid).
func (h *hub) wakePowder(dim int, pos blockPos) {
	for _, d := range powderWakeDirs {
		n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
		if worldgen.IsConcretePowder(h.worldFor(dim).Block(n.x, n.y, n.z)) {
			h.scheduleIn(dim, n, 1)
		}
	}
}

// spreadSides is vanilla FlowingFluid.spreadToSides: place flowing fluid one
// step weaker in the direction(s) with the shortest slope-distance to a drop
// (flowDirections). A falling cell spreads at full strength on landing.
func (h *hub) spreadSides(players map[int32]*tracked, dim int, pos blockPos, base uint32, level, dropOff int, delay uint64, slopeFind int) {
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
	waterFlow := base == worldgen.WaterBase
	for _, d := range h.flowDirections(dim, pos, slopeFind) {
		np := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		h.setBlockAt(players, dim, np, out)
		h.scheduleIn(dim, np, delay)
		if waterFlow {
			h.wakePowder(dim, np) // flowing water solidifies concrete powder beside it
		}
	}
}

// lavaContactDirs are the directions from which water solidifies a lava block:
// up plus the four horizontals (the opposites of lava's flow directions, per
// LiquidBlock.POSSIBLE_FLOW_DIRECTIONS).
var lavaContactDirs = [5]blockPos{{0, 1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}}

// lavaSolidify turns a lava block that water is touching into obsidian (source,
// level 0) or cobblestone (flowing). Returns true if it solidified.
func (h *hub) lavaSolidify(players map[int32]*tracked, dim int, pos blockPos, level int) bool {
	for _, d := range lavaContactDirs {
		n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
		if worldgen.IsWater(h.worldFor(dim).Block(n.x, n.y, n.z)) {
			block := worldgen.Cobblestone
			if level == 0 {
				block = worldgen.Obsidian
			}
			h.setBlockAt(players, dim, pos, block)
			h.fizz(players, dim, pos)
			h.scheduleAroundIn(dim, pos, 1)
			return true
		}
	}
	return false
}

// fizz plays the lava-quench sound where a fluid solidified (vanilla levelEvent 1501).
func (h *hub) fizz(players map[int32]*tracked, dim int, pos blockPos) {
	h.playSound(players, "minecraft:block.lava.extinguish", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 2.6)
}

// flowDirections returns the horizontal directions a fluid should spread into:
// the passable neighbours whose slope-distance to the nearest drop is minimal
// (FlowingFluid.getSpread). On flat ground every open direction ties, so it
// still spreads outward; near a ledge it runs toward the ledge.
func (h *hub) flowDirections(dim int, pos blockPos, findDist int) []blockPos {
	var dist [4]int
	best := 1 << 30
	for i, d := range horizNeighbors {
		n := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		if !worldgen.IsReplaceable(h.worldFor(dim).Block(n.x, n.y, n.z)) {
			dist[i] = 1 << 30 // impassable — never a candidate
			continue
		}
		if h.fluidHole(dim, n) {
			dist[i] = 0
		} else {
			dist[i] = h.slopeDist(dim, n, 1, findDist, i^1)
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
func (h *hub) slopeDist(dim int, pos blockPos, depth, findDist, from int) int {
	best := findDist + 1
	for i, d := range horizNeighbors {
		if i == from {
			continue
		}
		n := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		if !worldgen.IsReplaceable(h.worldFor(dim).Block(n.x, n.y, n.z)) {
			continue
		}
		if h.fluidHole(dim, n) {
			return depth
		}
		if depth < findDist {
			if r := h.slopeDist(dim, n, depth+1, findDist, i^1); r < best {
				best = r
			}
		}
	}
	return best
}

// fluidHole reports whether fluid at pos could fall (the cell below is open).
func (h *hub) fluidHole(dim int, pos blockPos) bool {
	b := pos.y - 1
	return h.inWorldY(b) && worldgen.IsReplaceable(h.worldFor(dim).Block(pos.x, b, pos.z))
}
