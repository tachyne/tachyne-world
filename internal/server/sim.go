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
	lavaDelayNether   = 10   // …and in the Nether
	maxUpdatesPerTick = 8192 // cap per tick so a big flood can't stall the loop
)

var (
	horizNeighbors = [4]blockPos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}}
	// horizFaces are horizNeighbors' directions as faces, for the fluid face test.
	horizFaces   = [4]int{worldgen.FaceEast, worldgen.FaceWest, worldgen.FaceSouth, worldgen.FaceNorth}
	allNeighbors = [6]blockPos{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}}
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

// scheduleIn queues a block update in a specific dimension.
func (h *hub) scheduleIn(dim int, pos blockPos, delay uint64) {
	if dim == 0 && !h.ownedBlock(pos.x, pos.z) {
		return // don't simulate blocks outside this pod's region
	}
	due := h.tick.Load() + delay
	h.pending[due] = append(h.pending[due], simPos{dim: dim, blockPos: pos})
}

// scheduleAroundIn queues a block and its six neighbours in a dimension.
func (h *hub) scheduleAroundIn(dim int, pos blockPos, delay uint64) {
	h.scheduleIn(dim, pos, delay)
	for _, d := range allNeighbors {
		h.scheduleIn(dim, blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}, delay)
	}
}

// runUpdates is a tick's block-update phase, in vanilla's order: the
// scheduled block ticks (blockticks.go), then the simulation queue below
// (fluids, falling blocks, fire, growth…), then the block events (pistons),
// then the moving pistons' own tick, which vanilla runs with the block
// entities after all of that.
func (h *hub) runUpdates(players map[int32]*tracked, age uint64) {
	h.runBlockTicks(players, age)
	h.runSimUpdates(players, age)
	h.runBlockEvents(players)
	h.landMovingBlocks(players, age)
}

// runSimUpdates processes the simulation updates due this tick (capped;
// overflow rolls to the next tick so a large flood spreads its cost instead
// of stalling).
func (h *hub) runSimUpdates(players map[int32]*tracked, age uint64) {
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
	if h.scratchSim == nil {
		h.scratchSim = make(map[simPos]struct{}, len(due))
	}
	clear(h.scratchSim)
	seen := h.scratchSim
	for _, sp := range due {
		if _, ok := seen[sp]; ok {
			continue
		}
		seen[sp] = struct{}{}
		// A scheduled tick runs only where blocks can tick: its chunk and
		// all eight around it loaded (a flowing fluid reads one block over).
		// Elsewhere it waits, as vanilla's ticks wait with their chunk,
		// instead of generating a chunk here on the hub.
		if !h.canTickBlocksAt(sp) {
			h.pending[age+unloadedRetry] = append(h.pending[age+unloadedRetry], sp)
			continue
		}
		h.processUpdate(players, sp.dim, sp.blockPos)
		h.nbRun(players) // any neighbour updates it queued
	}
}

// unloadedRetry is how long an update in an unloaded chunk waits before it
// is looked at again (a second).
const unloadedRetry = 20

// canTickBlocksAt reports whether an update's chunk is ticking.
func (h *hub) canTickBlocksAt(sp simPos) bool {
	return h.worldFor(sp.dim).Ticking(int32(sp.x>>4), int32(sp.z>>4))
}

func (h *hub) processUpdate(players map[int32]*tracked, dim int, pos blockPos) {
	if !h.inWorldY(pos.y) {
		return
	}
	state := h.worldFor(dim).Block(pos.x, pos.y, pos.z)
	switch {
	case state == openEyeblossom || state == closedEyeblossom:
		// A flower woken by the one that turned beside it: it switches on the
		// SHORT sound, and wakes its own neighbours in turn.
		h.switchEyeblossom(players, dim, pos.x, pos.y, pos.z, state, false)
	case h.tickFrostedIce(players, dim, pos, state):
		// Frost Walker's ice ages itself back to water on its own schedule.
	case worldgen.IsConcretePowder(state) && h.powderTouchesWater(dim, pos):
		h.setBlockAt(players, dim, pos, worldgen.ConcreteFor(state))
	case h.updateLeafDistance(players, dim, pos.x, pos.y, pos.z, state):
		// A leaf whose neighbourhood changed recomputes its trunk distance;
		// the write schedules ITS neighbours, so a felled trunk sends the
		// recompute through the canopy as a wave and the rim rots first.
	case worldgen.IsFalling(state) || isStalactite(state):
		h.updateFalling(players, dim, pos, state)
	case h.tickBubbleSource(players, dim, pos, state):
		// Soul sand or magma with water above raises (or drops) its column.
	case worldgen.IsBubbleColumn(state):
		// The column keeps itself: it collapses to water without its source
		// and climbs into source water above; then it spreads as the water
		// source it is.
		if h.updateBubbleColumn(players, dim, pos) {
			h.updateFluid(players, dim, pos, h.worldFor(dim).Block(pos.x, pos.y, pos.z))
		}
	case worldgen.IsFluid(state):
		h.updateFluid(players, dim, pos, state)
	case isFire(state):
		h.inDim(dim, func() { h.updateFire(players, pos) })
	case h.tickFrogspawn(players, dim, pos, state):
		// A clutch bursts into tadpoles once its timer runs out.
	case h.tickSnifferEgg(players, dim, pos.x, pos.y, pos.z, state):
		// The egg cracks twice and then opens.
	case h.tickChorusPlant(players, dim, pos.x, pos.y, pos.z, state):
		// A plant segment that lost its footing pops, taking the rest with it.
	case h.tickCoral(players, dim, pos, state):
		// Coral left out of water bleaches to its dead twin.
	case h.driedGhastStep(players, dim, pos, state):
		// A dried ghast takes a step of water (or loses one) on its own tick.
	case h.tickComposter(players, dim, pos, state):
		// A full composter finishes composting a second after its last item.
	case h.tickDripleaf(players, dim, pos, state):
		// A big dripleaf tipping under a load, or pinned flat by a signal.
	case isPotentSulfur(state):
		// A neighbour changed without a shape update of its own (a bucket's
		// water, a live edit): the vent re-derives its state.
		if ns := potentSulfurValid(h.worldFor(dim), pos, state); ns != state {
			h.setBlockAt(players, dim, pos, ns)
		}
	default:
		h.inDim(dim, func() { h.updateRedstone(players, pos, state) })
	}
	// A waterlogged block's water has its own tick in vanilla, beside the
	// block's: it runs as a source out of any face the block's shape leaves
	// open (bug #25). It follows the block's own update rather than replacing
	// it — dispatched in the switch above, it took the place of a dripleaf's
	// tilt, a trapdoor's or rail's redstone and a rod's power, and a
	// waterlogged leaf's distance update kept its water still.
	if now := h.worldFor(dim).Block(pos.x, pos.y, pos.z); worldgen.IsWaterlogged(now) && !worldgen.IsFluid(now) {
		h.updateFluid(players, dim, pos, now)
	}
}

// setBlockAt applies a simulation-driven change in a dimension and broadcasts
// it to the players standing in that dimension.
func (h *hub) setBlockAt(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	w := h.worldFor(dim)
	old := w.At(pos.x, pos.y, pos.z)
	w.SetBlock(pos.x, pos.y, pos.z, state)
	h.broadcastBlockIn(players, dim, pos.x, pos.y, pos.z, state)
	h.spillContainer(players, dim, pos.x, pos.y, pos.z, old, state)
	h.afterRemoval(players, dim, pos, old, state)
	h.potentSulfurChanged(players, dim, pos, old, state) // the block entity: registry, reset, onPlace
	// Vanilla's setBlock notifies the neighbours, and a block that just lost
	// its floor, wall or ceiling comes down (updateShape → canSurvive). This
	// is every engine-driven change — fluid washing a cell out, a piston, a
	// fire, sand dropping away, farmland turning back to dirt — not only a
	// player's edit (which runs its own sweep from the hub loop). The sweep's
	// own writes come back through here; its queue already walks the
	// cascade, so a nested sweep is skipped.
	if old != state && !h.supportSweep {
		h.supportSweep = true
		h.dropUnsupported(players, dim, pos)
		h.supportSweep = false
	}
	if old != state {
		h.observersSee(players, dim, pos, state) // the shape update an observer watches for
		h.fireBesideHives(players, dim, pos, state)
		// CoralBlock.updateShape: water gone from beside a coral (a sponge, a
		// piston, a flow receding) sets its die tick, not only a player's edit.
		if worldgen.IsWater(old) && !worldgen.IsWater(state) {
			h.scheduleCoralDeath(dim, pos)
		}
		h.fireOnPlace(players, dim, pos, old, state) // a fire inside an obsidian frame lights it
		// A string laid or taken away: the hooks along its line re-check.
		if isTripwire(old) != isTripwire(state) {
			h.inDim(dim, func() { h.tripwireUpdateSource(players, pos) })
		}
		// LightningRodBlock.onPlace: a rod set down powered with no tick of
		// its own pending gets one, which switches it off.
		if isLightningRod(state) && boolProp(state, "powered") {
			if _, ok := h.rsDue[simPos{dim: dim, blockPos: pos}]; !ok {
				h.inDim(dim, func() { h.rsSchedule(pos, 1) })
			}
		}
	}
	// Break the fence and the knot goes with it, dropping whatever it held.
	// Guarded on there being any knot at all: this is the choke point every
	// block change runs through.
	if len(h.knots) > 0 && !isFence(state) {
		h.cutLeashesAt(players, dim, pos)
	}
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
	key := simPos{dim: dim, blockPos: pos}
	// A falling block passes through fluids (the entity has no collision
	// with them) and through frogspawn, which it destroys on the way
	// (FrogspawnBlock.entityInside).
	if b := h.worldFor(dim).Block(below.x, below.y, below.z); worldgen.IsReplaceable(b) || worldgen.IsFluid(b) || b == frogspawnBlock {
		fallen := h.fallDist[key] + 1
		delete(h.fallDist, key)
		h.setBlockAt(players, dim, pos, worldgen.Air)
		h.setBlockAt(players, dim, below, state)
		h.fallDist[simPos{dim: dim, blockPos: below}] = fallen
		if n, ok := h.stalactiteLen[key]; ok { // the column's length travels with its tip
			delete(h.stalactiteLen, key)
			h.stalactiteLen[simPos{dim: dim, blockPos: below}] = n
		}
		h.scheduleIn(dim, below, fallDelay)     // keep falling
		h.scheduleAroundIn(dim, pos, fallDelay) // a block resting on it loses support
		return
	}
	fallen := h.fallDist[key]
	delete(h.fallDist, key)
	// Landed: concrete powder touching water turns to concrete (ConcretePowderBlock).
	if worldgen.IsConcretePowder(state) && h.powderTouchesWater(dim, pos) {
		h.setBlockAt(players, dim, pos, worldgen.ConcreteFor(state))
	}
	if worldgen.IsAnvil(state) && fallen > 0 {
		h.anvilLanded(players, dim, pos, state, fallen)
	}
	if n, ok := h.stalactiteLen[key]; ok {
		delete(h.stalactiteLen, key)
		if isStalactite(state) && fallen > 0 {
			h.stalactiteLanded(players, dim, pos, n, fallen)
		}
	}
	// FallingBlockEntity: a block that cannot stand where it lands breaks
	// into its item instead (callOnBrokenAfterFall + spawnAtLocation). A
	// stalactite never can — nothing holds it up from above — so a fallen
	// column comes down as pointed dripstone, each piece with its crash.
	if isStalactite(state) && !supported(h.worldFor(dim), pos, state) {
		h.setBlockAt(players, dim, pos, worldgen.Air)
		h.levelEvent(players, dim, levelEventDripstoneBreak, pos.x, pos.y, pos.z, 0)
		if h.rules.EntityDrops { // the entity_drops gamerule governs a falling block's drop
			h.spawnBlockDrop(players, dim, itemByName["pointed_dripstone"], 1, pos.x, pos.y, pos.z)
		}
	}
}

// levelEventDripstoneBreak is LevelEvent 1045 (PointedDripstoneBlock.
// onBrokenAfterFall): the landing crash.
const levelEventDripstoneBreak = 1045

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
	logged := worldgen.IsWaterlogged(state) && !worldgen.IsFluid(state)
	water := worldgen.IsWater(state) || logged
	base, delay, dropOff, slopeFind := worldgen.WaterBase, uint64(waterDelay), 1, 4
	if !water {
		base, delay, dropOff, slopeFind = worldgen.LavaBase, uint64(lavaDelay), 2, 2
		if dim == dimNether {
			// LavaFluid in an ultra-warm dimension (the Nether's fast lava):
			// a step every 10 ticks, one level lost a block, slopes sought
			// four away — it runs as far and nearly as fast as water.
			delay, dropOff, slopeFind = lavaDelayNether, 1, 4
		}
	}
	same := func(s uint32) bool {
		if water {
			return worldgen.IsWater(s)
		}
		return worldgen.IsLava(s)
	}
	level := worldgen.FluidLevel(state, base)
	if logged {
		level = 0 // the block's water is a source
	}

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
			state, level = ns, worldgen.FluidLevel(ns, base)
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
	if h.inWorldY(below.y) && fluidPassable(belowB) && !same(belowB) && worldgen.FluidPassesFace(worldgen.FaceDown, state, belowB) {
		h.fluidInto(players, dim, below, base+8, water) // falling
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
	cell := h.worldFor(dim).Block(pos.x, pos.y, pos.z)
	sourceCount, maxAmount := 0, 0
	for i, d := range horizNeighbors {
		nb := h.worldFor(dim).Block(pos.x+d.x, pos.y, pos.z+d.z)
		// The neighbour's fluid reaches this cell only through the faces the
		// two blocks turn to each other (canPassThroughWall); a waterlogged
		// neighbour holds a water source.
		logged := water && worldgen.IsWaterlogged(nb) && !worldgen.IsFluid(nb)
		if !same(nb) && !logged {
			continue
		}
		if !worldgen.FluidPassesFace(worldgen.OppositeFace(horizFaces[i]), nb, cell) {
			continue
		}
		nl := worldgen.FluidLevel(nb, base)
		if logged {
			nl = 0
		}
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
		if belowB := h.worldFor(dim).Block(pos.x, pos.y-1, pos.z); worldgen.IsSolidFull(belowB) || worldgen.IsFluidSource(belowB, base) ||
			(water && worldgen.IsWaterlogged(belowB)) {
			return base, true
		}
	}
	above := h.worldFor(dim).Block(pos.x, pos.y+1, pos.z)
	if (same(above) || (water && worldgen.IsWaterlogged(above))) && worldgen.FluidPassesFace(worldgen.FaceDown, above, cell) { // fluid above → falling
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
		nb := h.worldFor(dim).Block(pos.x+d.x, pos.y, pos.z+d.z)
		if worldgen.IsFluidSource(nb, base) || (base == worldgen.WaterBase && worldgen.IsWaterlogged(nb) && !worldgen.IsFluid(nb)) {
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
	if !worldgen.FluidPassesFace(worldgen.FaceDown, h.worldFor(dim).Block(pos.x, pos.y, pos.z), belowB) {
		return false // isWaterHole asks canPassThroughWall(DOWN) first
	}
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
		h.fluidInto(players, dim, np, out, waterFlow)
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
	h.playSoundDim(players, dim, "minecraft:block.lava.extinguish", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 2.6)
}

// flowDirections returns the horizontal directions a fluid should spread into:
// the passable neighbours whose slope-distance to the nearest drop is minimal
// (FlowingFluid.getSpread). On flat ground every open direction ties, so it
// still spreads outward; near a ledge it runs toward the ledge.
func (h *hub) flowDirections(dim int, pos blockPos, findDist int) []blockPos {
	var dist [4]int
	best := 1 << 30
	src := h.worldFor(dim).Block(pos.x, pos.y, pos.z)
	for i, d := range horizNeighbors {
		n := blockPos{pos.x + d.x, pos.y, pos.z + d.z}
		nb := h.worldFor(dim).Block(n.x, n.y, n.z)
		if !fluidPassable(nb) || !worldgen.FluidPassesFace(horizFaces[i], src, nb) {
			dist[i] = 1 << 30 // impassable, or a face the source's block closes
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
		if !fluidPassable(h.worldFor(dim).Block(n.x, n.y, n.z)) {
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
	return h.inWorldY(b) && fluidPassable(h.worldFor(dim).Block(pos.x, b, pos.z))
}
