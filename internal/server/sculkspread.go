package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Sculk spreading — SculkSpreader, SculkBlock, SculkVeinBlock and
// SculkBehaviour, driven by a catalyst the way SculkCatalystBlockEntity
// drives them.
//
// A mob dying within eight blocks of a catalyst hands the catalyst its
// experience as CHARGE: one cursor per thousand points (at most 32 cursors a
// catalyst), starting in the cell the mob died in. Every tick the catalyst's
// chunk is loaded, each cursor takes a step: the block under it spreads
// veins, spends some charge (a vein turns the #sculk_replaceable block it
// clings to into sculk for one point; a sculk block may grow a sensor or,
// one time in eleven, a shrieker that cannot summon), and the cursor then
// hops to a neighbouring sculk or vein, preferring a vein that still has
// ground to eat. A cursor loses charge the further it wanders (the decay
// penalty), and two cursors on one block merge. So a bloom creeps out over
// seconds instead of appearing at once, and only natural ground turns.
//
// In-flight cursors live on the hub and are not saved: a restart drops the
// charge still travelling (vanilla keeps it in the catalyst's block entity).

// The level (catalyst) spreader's numbers — SculkSpreader.createLevelSpreader.
const (
	sculkGrowthSpawnCost = 10 // charge a sensor or shrieker costs
	sculkNoGrowthRadius  = 4  // no growth this close to the catalyst
	sculkChargeDecayRate = 10 // 1 in N updates spends charge
	sculkAdditionalDecay = 5  // 1 in N of those decays instead of growing
	sculkMaxCharge       = 1000
	sculkMaxCursors      = 32
	sculkMaxCursorReach  = 1024 // chessboard distance a cursor may stray
	sculkMaxGrowthRadius = 24   // MAX_GROWTH_RATE_RADIUS: where the decay penalty tops out
	sculkShriekerOdds    = 11   // SHRIEKER_PLACEMENT_RATE

	worldEventSculkCharge = 3006 // PARTICLES_SCULK_CHARGE, data: (count<<6)|faces, 0 = pop
	particleSculkSoul     = 36   // canonical 770 minecraft:sculk_soul
)

var (
	sculkGrowthInhibitors = worldgen.BlockTag("sculk_growth_inhibitors")
	fireTag               = worldgen.BlockTag("fire")
	movingPistonRange     = blockRange("moving_piston")
	airStates             = blockRange("air", "cave_air", "void_air")
)

// sculkCursor is SculkSpreader.ChargeCursor. facings is nil until the cursor
// first stands on sculk or a vein (then the faces it had there, possibly
// none), as vanilla's nullable set.
type sculkCursor struct {
	pos         blockPos
	charge      int
	updateDelay int
	decayDelay  int
	facings     *uint8 // Direction-ordinal face bits, or nil
}

// sculkSpreader is one catalyst's SculkSpreader: its cursors, in order.
type sculkSpreader struct {
	cursors []*sculkCursor
}

// Directions in vanilla's ordinal order: down, up, north, south, west, east.
var sculkDirs = [6]blockPos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func dirOpposite(d int) int { return d ^ 1 }
func dirAxis(d int) int     { return d / 2 }
func (p blockPos) off(d int) blockPos {
	o := sculkDirs[d]
	return blockPos{p.x + o.x, p.y + o.y, p.z + o.z}
}

// shuffledDirs is Direction.allShuffled.
func (h *hub) shuffledDirs() [6]int {
	ds := [6]int{0, 1, 2, 3, 4, 5}
	h.rng.Shuffle(6, func(i, j int) { ds[i], ds[j] = ds[j], ds[i] })
	return ds
}

// sculkNonCorner is ChargeCursor.NON_CORNER_NEIGHBOURS: the 18 cells of the
// 3×3×3 cube around a block that share a face or an edge with it.
var sculkNonCorner = func() []blockPos {
	var out []blockPos
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if (dx == 0 || dy == 0 || dz == 0) && !(dx == 0 && dy == 0 && dz == 0) {
					out = append(out, blockPos{dx, dy, dz})
				}
			}
		}
	}
	return out
}()

// ---- sculk vein state math --------------------------------------------------
//
// sculk_vein properties down,east,north,south,up,waterlogged,west (true
// first, last fastest): each false adds its place value.

var veinFacePlace = [6]uint32{64, 4, 16, 8, 1, 32} // by Direction ordinal

func isSculkVein(s uint32) bool { return s >= sculkVeinBase && s < sculkVeinBase+128 }

// veinFaces packs a vein's faces as Direction-ordinal bits (MultifaceBlock.pack).
func veinFaces(s uint32) uint8 {
	if !isSculkVein(s) {
		return 0
	}
	off := s - sculkVeinBase
	var m uint8
	for d, p := range veinFacePlace {
		if off&p == 0 {
			m |= 1 << d
		}
	}
	return m
}

func veinWaterlogged(s uint32) bool { return isSculkVein(s) && (s-sculkVeinBase)&2 == 0 }

// veinState builds a sculk_vein with exactly these faces.
func veinState(faces uint8, waterlogged bool) uint32 {
	off := uint32(0)
	for d, p := range veinFacePlace {
		if faces&(1<<d) == 0 {
			off += p
		}
	}
	if !waterlogged {
		off += 2
	}
	return sculkVeinBase + off
}

func hasFace(s uint32, d int) bool { return veinFaces(s)&(1<<d) != 0 }

// ---- block predicates the spread asks ----------------------------------------

func isAirState(s uint32) bool { return inRanges2(s, airStates) }

// fluidEmpty is getFluidState().isEmpty().
func fluidEmpty(s uint32) bool { return !worldgen.HoldsWater(s) && !worldgen.IsLava(s) }

// waterSourceIn is getFluidState().isSourceOfType(WATER): still water, a
// bubble column, or a waterlogged block.
func waterSourceIn(s uint32) bool {
	if worldgen.IsWater(s) {
		return worldgen.FluidLevel(s, worldgen.WaterBase) == 0
	}
	return worldgen.HoldsWater(s)
}

// replaceableState is BlockState.canBeReplaced(): air, the fluids, and the
// small plants anything overwrites.
func replaceableState(s uint32) bool {
	return isAirState(s) || worldgen.IsReplaceable(s) || worldgen.IsFluid(s)
}

// faceSturdy stands in for isFaceSturdy / MultifaceBlock.canAttachTo: the
// engine's per-face shape test (holdsBlock) that the multiface survival
// rule already uses, so a vein placed here is never torn down there.
func faceSturdy(s uint32) bool { return holdsBlock(s) }

// ---- the multiface spreader with the sculk vein config ------------------------

type veinSpreadType int

const (
	spreadSamePosition veinSpreadType = iota
	spreadSamePlane
	spreadWrapAround
)

var (
	veinSpreadDefault   = []veinSpreadType{spreadSamePosition, spreadSamePlane, spreadWrapAround}
	veinSpreadSameSpace = []veinSpreadType{spreadSamePosition}
)

// spreadTarget is MultifaceSpreader.SpreadType.getSpreadPos.
func spreadTarget(t veinSpreadType, pos blockPos, spreadDir, fromFace int) (blockPos, int) {
	switch t {
	case spreadSamePosition:
		return pos, spreadDir
	case spreadSamePlane:
		return pos.off(spreadDir), fromFace
	}
	return pos.off(spreadDir).off(fromFace), dirOpposite(spreadDir)
}

// veinValidPlacement is MultifaceBlock.isValidStateForPlacement for sculk
// vein: the face is not already there, and the block it would cling to
// offers a full face.
func (h *hub) veinValidPlacement(dim int, existing uint32, pos blockPos, d int) bool {
	if isSculkVein(existing) && hasFace(existing, d) {
		return false
	}
	n := pos.off(d)
	return faceSturdy(h.worldFor(dim).At(n.x, n.y, n.z))
}

// veinStateCanBeReplaced is SculkVeinSpreaderConfig.stateCanBeReplaced: never
// against sculk, a catalyst or a moving piston; never wrapping round a solid
// corner; not into lava or flowing water or fire; otherwise into anything
// replaceable, another vein, or still water.
func (h *hub) veinStateCanBeReplaced(dim int, source, placement blockPos, d int, existing uint32) bool {
	w := h.worldFor(dim)
	ap := placement.off(d)
	against := w.At(ap.x, ap.y, ap.z)
	if against == sculkBlockState || isCatalyst(against) || inRanges2(against, movingPistonRange) {
		return false
	}
	if abs(source.x-placement.x)+abs(source.y-placement.y)+abs(source.z-placement.z) == 2 {
		np := source.off(dirOpposite(d))
		if faceSturdy(w.At(np.x, np.y, np.z)) {
			return false
		}
	}
	if !fluidEmpty(existing) && !waterSourceIn(existing) {
		return false
	}
	if inRanges2(existing, fireTag) {
		return false
	}
	return replaceableState(existing) || isSculkVein(existing) ||
		(worldgen.IsWater(existing) && waterSourceIn(existing))
}

// veinPlacementState is MultifaceBlock.getStateForPlacement: join an existing
// vein, or start one (waterlogged in still water) with just this face.
func (h *hub) veinPlacementState(dim int, old uint32, pos blockPos, d int) (uint32, bool) {
	if !h.veinValidPlacement(dim, old, pos, d) {
		return 0, false
	}
	var faces uint8
	wl := false
	switch {
	case isSculkVein(old):
		faces, wl = veinFaces(old), veinWaterlogged(old)
	case waterSourceIn(old):
		wl = true
	}
	return veinState(faces|1<<d, wl), true
}

// veinSpreadAll is MultifaceSpreader.spreadAll with the sculk vein config:
// from every face the source can spread from, toward every direction off
// that face's axis, the first spread type that fits places a vein face.
// It returns how many faces were placed.
func (h *hub) veinSpreadAll(players map[int32]*tracked, dim int, state uint32, pos blockPos, types []veinSpreadType) int {
	w := h.worldFor(dim)
	other := !isSculkVein(state) // isOtherBlockValidAsSource
	n := 0
	for from := 0; from < 6; from++ {
		if !other && !hasFace(state, from) {
			continue
		}
		for dir := 0; dir < 6; dir++ {
			if dirAxis(dir) == dirAxis(from) {
				continue
			}
			if !other && hasFace(state, dir) {
				continue
			}
			for _, t := range types {
				tp, face := spreadTarget(t, pos, dir, from)
				existing := w.At(tp.x, tp.y, tp.z)
				if !h.veinStateCanBeReplaced(dim, pos, tp, face, existing) || !h.veinValidPlacement(dim, existing, tp, face) {
					continue
				}
				if ns, ok := h.veinPlacementState(dim, existing, tp, face); ok && ns != existing {
					h.setBlockAt(players, dim, tp, ns)
					n++
				}
				break
			}
		}
	}
	return n
}

// veinRegrow is SculkVeinBlock.regrow: a vein with those of the remembered
// faces that still have something to cling to.
func (h *hub) veinRegrow(players map[int32]*tracked, dim int, pos blockPos, existing uint32, faces uint8) bool {
	w := h.worldFor(dim)
	var got uint8
	for d := 0; d < 6; d++ {
		if faces&(1<<d) == 0 {
			continue
		}
		if n := pos.off(d); faceSturdy(w.At(n.x, n.y, n.z)) {
			got |= 1 << d
		}
	}
	if got == 0 {
		return false
	}
	h.setBlockAt(players, dim, pos, veinState(got, !fluidEmpty(existing)))
	return true
}

// veinHasSubstrate is SculkVeinBlock.hasSubstrateAccess: a vein clinging to
// a block the spread can still turn.
func (h *hub) veinHasSubstrate(dim int, state uint32, pos blockPos) bool {
	if !isSculkVein(state) {
		return false
	}
	w := h.worldFor(dim)
	for d := 0; d < 6; d++ {
		if n := pos.off(d); hasFace(state, d) && inRanges2(w.At(n.x, n.y, n.z), sculkReplaceable) {
			return true
		}
	}
	return false
}

// veinDischarged is SculkVeinBlock.onDischarged: faces lying on sculk are
// dropped (the sculk under them is the vein grown up), and a vein with no
// face left becomes air, or water if it was waterlogged. It works from the
// state it is handed, as vanilla does.
func (h *hub) veinDischarged(players map[int32]*tracked, dim int, state uint32, pos blockPos) {
	if !isSculkVein(state) {
		return
	}
	w := h.worldFor(dim)
	faces := veinFaces(state)
	for d := 0; d < 6; d++ {
		if n := pos.off(d); faces&(1<<d) != 0 && w.At(n.x, n.y, n.z) == sculkBlockState {
			faces &^= 1 << d
		}
	}
	ns := veinState(faces, veinWaterlogged(state))
	if faces == 0 {
		ns = worldgen.Air
		if !fluidEmpty(w.At(pos.x, pos.y, pos.z)) {
			ns = worldgen.WaterBase
		}
	}
	if w.At(pos.x, pos.y, pos.z) != ns {
		h.setBlockAt(players, dim, pos, ns)
	}
}

// veinPlaceSculk is SculkVeinBlock.attemptPlaceSculk: a face (in random
// order) lying on #sculk_replaceable turns that block into sculk, veins
// spread over the new sculk, and the veins now lying on it are discharged.
func (h *hub) veinPlaceSculk(players map[int32]*tracked, dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	state := w.At(pos.x, pos.y, pos.z)
	for _, support := range h.shuffledDirs() {
		if !hasFace(state, support) {
			continue
		}
		sp := pos.off(support)
		if !inRanges2(w.At(sp.x, sp.y, sp.z), sculkReplaceable) {
			continue
		}
		h.setBlockAt(players, dim, sp, sculkBlockState)
		h.playSoundDim(players, dim, "minecraft:block.sculk.spread", sndBlock,
			float64(sp.x)+0.5, float64(sp.y)+0.5, float64(sp.z)+0.5, 1, 1)
		h.veinSpreadAll(players, dim, sculkBlockState, sp, veinSpreadDefault)
		skip := dirOpposite(support)
		for d := 0; d < 6; d++ {
			if d == skip {
				continue
			}
			vp := sp.off(d)
			if vs := w.At(vp.x, vp.y, vp.z); isSculkVein(vs) {
				h.veinDischarged(players, dim, vs, vp)
			}
		}
		return true
	}
	return false
}

// ---- SculkBehaviour ------------------------------------------------------------

type sculkBehaviour int

const (
	behaviourDefault sculkBehaviour = iota // SculkBehaviour.DEFAULT: any other block
	behaviourSculk                         // SculkBlock
	behaviourVein                          // SculkVeinBlock
)

func sculkBehaviourOf(s uint32) sculkBehaviour {
	switch {
	case s == sculkBlockState:
		return behaviourSculk
	case isSculkVein(s):
		return behaviourVein
	}
	return behaviourDefault
}

// attemptSpreadVein is SculkBehaviour.attemptSpreadVein. The default (a cell
// that is neither sculk nor vein, like the air a mob died in) lines its own
// cell with veins on a fresh cursor, regrows the veins the cursor remembers
// in air or water, and otherwise spreads as sculk would.
func (h *hub) attemptSpreadVein(players map[int32]*tracked, dim int, b sculkBehaviour, pos blockPos, state uint32, facings *uint8) bool {
	if b == behaviourDefault {
		switch {
		case facings == nil:
			cur := h.worldFor(dim).At(pos.x, pos.y, pos.z)
			return h.veinSpreadAll(players, dim, cur, pos, veinSpreadSameSpace) > 0
		case *facings != 0:
			if !isAirState(state) && !waterSourceIn(state) {
				return false
			}
			return h.veinRegrow(players, dim, pos, state, *facings)
		}
	}
	return h.veinSpreadAll(players, dim, state, pos, veinSpreadDefault) > 0
}

// attemptUseCharge is SculkBehaviour.attemptUseCharge for each block kind.
func (h *hub) attemptUseCharge(players map[int32]*tracked, dim int, b sculkBehaviour, c *sculkCursor, origin blockPos) int {
	switch b {
	case behaviourVein:
		// SculkVeinBlock: turn the ground for one point, else sometimes halve.
		if h.veinPlaceSculk(players, dim, c.pos) {
			return c.charge - 1
		}
		if h.rng.Intn(sculkChargeDecayRate) == 0 {
			return int(math.Floor(float64(float32(c.charge) * 0.5)))
		}
		return c.charge
	case behaviourSculk:
		return h.sculkUseCharge(players, dim, c, origin)
	}
	if c.decayDelay > 0 { // DEFAULT: a cursor off sculk lives one step
		return c.charge
	}
	return 0
}

// sculkUseCharge is SculkBlock.attemptUseCharge: one update in ten spends
// charge. Away from the catalyst with room above, it grows a sensor or a
// shrieker (likelier the more charge there is) for ten points; otherwise it
// sometimes decays — by one near the catalyst, by more the further out.
func (h *hub) sculkUseCharge(players map[int32]*tracked, dim int, c *sculkCursor, origin blockPos) int {
	charge := c.charge
	if charge == 0 || h.rng.Intn(sculkChargeDecayRate) != 0 {
		return charge
	}
	close := blockDistSqr(c.pos, origin) < sculkNoGrowthRadius*sculkNoGrowthRadius
	if !close && h.canPlaceGrowth(dim, c.pos) {
		if h.rng.Intn(sculkGrowthSpawnCost) < charge {
			at := c.pos.off(1)
			gs, snd := h.sculkGrowthState(dim, at)
			h.setBlockAt(players, dim, at, gs)
			h.sculkIndexOnBlockChange(dim, at.x, at.y, at.z, gs) // a grown sensor/shrieker listens
			h.playSoundDim(players, dim, snd, sndBlock,
				float64(c.pos.x)+0.5, float64(c.pos.y)+0.5, float64(c.pos.z)+0.5, 1, 1)
		}
		return max(0, charge-sculkGrowthSpawnCost)
	}
	if h.rng.Intn(sculkAdditionalDecay) != 0 {
		return charge
	}
	if close {
		return charge - 1
	}
	return charge - sculkDecayPenalty(c.pos, origin, charge)
}

func blockDistSqr(a, b blockPos) int {
	dx, dy, dz := a.x-b.x, a.y-b.y, a.z-b.z
	return dx*dx + dy*dy + dz*dz
}

// sculkDecayPenalty is SculkBlock.getDecayPenalty: up to half the charge,
// growing with the square of the distance past the no-growth radius.
func sculkDecayPenalty(pos, origin blockPos, charge int) int {
	outer := float32(math.Sqrt(float64(blockDistSqr(pos, origin)))) - sculkNoGrowthRadius
	outerSq := outer * outer
	reach := float32((sculkMaxGrowthRadius - sculkNoGrowthRadius) * (sculkMaxGrowthRadius - sculkNoGrowthRadius))
	factor := min(float32(1), outerSq/reach)
	return max(1, int(float32(charge)*factor*0.5))
}

// canPlaceGrowth is SculkBlock.canPlaceGrowth: air or still water above,
// and no more than two sensors or shriekers already in the 9×3×9 box.
func (h *hub) canPlaceGrowth(dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	above := w.At(pos.x, pos.y+1, pos.z)
	if !isAirState(above) && above != worldgen.WaterBase {
		return false
	}
	found := 0
	for x := pos.x - 4; x <= pos.x+4; x++ {
		for y := pos.y; y <= pos.y+2; y++ {
			for z := pos.z - 4; z <= pos.z+4; z++ {
				if inRanges2(w.At(x, y, z), sculkGrowthInhibitors) {
					if found++; found > 2 {
						return false
					}
				}
			}
		}
	}
	return true
}

// sculkGrowthState is SculkBlock.getRandomGrowthState: a sensor, or one time
// in eleven a shrieker — which, grown by a catalyst, cannot summon a Warden.
// Waterlogged when it grows into water. It also names the placing sound.
func (h *hub) sculkGrowthState(dim int, at blockPos) (uint32, string) {
	wl := !fluidEmpty(h.worldFor(dim).At(at.x, at.y, at.z))
	if h.rng.Intn(sculkShriekerOdds) == 0 {
		s := shriekerBase + 4 + 2 + 1 // can_summon=false, shrieking=false, waterlogged=false
		if wl {
			s--
		}
		return s, "minecraft:block.sculk_shrieker.place"
	}
	s := sculkSensorBase + 1 // power 0, inactive, waterlogged=false
	if wl {
		s--
	}
	return s, "minecraft:block.sculk_sensor.place"
}

// ---- the cursor --------------------------------------------------------------

// update is ChargeCursor.update: after its delay, the block under the cursor
// spreads veins, spends charge, and the cursor moves on (or, spent, is
// discharged where it stands).
func (h *hub) updateCursor(players map[int32]*tracked, dim int, c *sculkCursor, origin blockPos) {
	if c.charge <= 0 {
		return
	}
	if c.updateDelay > 0 {
		c.updateDelay--
		return
	}
	w := h.worldFor(dim)
	state := w.At(c.pos.x, c.pos.y, c.pos.z)
	b := sculkBehaviourOf(state)
	if h.attemptSpreadVein(players, dim, b, c.pos, state, c.facings) {
		if b != behaviourSculk { // canChangeBlockStateOnSpread
			state = w.At(c.pos.x, c.pos.y, c.pos.z)
			b = sculkBehaviourOf(state)
		}
		h.playSoundDim(players, dim, "minecraft:block.sculk.spread", sndBlock,
			float64(c.pos.x)+0.5, float64(c.pos.y)+0.5, float64(c.pos.z)+0.5, 1, 1)
	}
	c.charge = h.attemptUseCharge(players, dim, b, c, origin)
	if c.charge <= 0 {
		h.cursorDischarged(players, dim, b, state, c.pos)
		return
	}
	if to, ok := h.cursorMove(dim, c.pos); ok {
		h.cursorDischarged(players, dim, b, state, c.pos)
		c.pos = to
		state = w.At(to.x, to.y, to.z)
	}
	if nb := sculkBehaviourOf(state); nb != behaviourDefault {
		f := veinFaces(state) // MultifaceBlock.availableFaces: none for a sculk block
		c.facings = &f
	}
	// The behaviour of the block the cursor stood on sets its delays.
	if b == behaviourDefault {
		c.decayDelay = max(c.decayDelay-1, 0)
	} else {
		c.decayDelay = 1
	}
	c.updateDelay = 1 // getSculkSpreadDelay
}

func (h *hub) cursorDischarged(players map[int32]*tracked, dim int, b sculkBehaviour, state uint32, pos blockPos) {
	if b == behaviourVein {
		h.veinDischarged(players, dim, state, pos)
	}
}

// cursorMove is ChargeCursor.getValidMovementPos: of the 18 edge and face
// neighbours, in random order, the last sculk or vein reachable without
// passing through a solid face — or the first vein with ground to eat.
func (h *hub) cursorMove(dim int, pos blockPos) (blockPos, bool) {
	w := h.worldFor(dim)
	order := make([]int, len(sculkNonCorner))
	for i := range order {
		order[i] = i
	}
	h.rng.Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	to, found := pos, false
	for _, i := range order {
		o := sculkNonCorner[i]
		n := blockPos{pos.x + o.x, pos.y + o.y, pos.z + o.z}
		s := w.At(n.x, n.y, n.z)
		if sculkBehaviourOf(s) == behaviourDefault || !h.cursorUnobstructed(dim, pos, o) {
			continue
		}
		to, found = n, true
		if h.veinHasSubstrate(dim, s, n) {
			break
		}
	}
	return to, found
}

// cursorUnobstructed is ChargeCursor.isMovementUnobstructed: a step across
// an edge needs one of the two cells it cuts past to be open.
func (h *hub) cursorUnobstructed(dim int, from, delta blockPos) bool {
	if abs(delta.x)+abs(delta.y)+abs(delta.z) == 1 {
		return true
	}
	w := h.worldFor(dim)
	open := func(dx, dy, dz int) bool {
		return !faceSturdy(w.At(from.x+dx, from.y+dy, from.z+dz))
	}
	sx, sy, sz := sign1(delta.x), sign1(delta.y), sign1(delta.z)
	switch {
	case delta.x == 0:
		return open(0, sy, 0) || open(0, 0, sz)
	case delta.y == 0:
		return open(sx, 0, 0) || open(0, 0, sz)
	}
	return open(sx, 0, 0) || open(0, sy, 0)
}

// sign1 is the direction vanilla picks per axis: negative, else positive.
func sign1(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}

// ---- the spreader ------------------------------------------------------------

// addCursors is SculkSpreader.addCursors: the charge in cursors of at most
// a thousand, while the catalyst has room for them.
func (sp *sculkSpreader) addCursors(at blockPos, charge int) {
	for charge > 0 {
		c := min(charge, sculkMaxCharge)
		if len(sp.cursors) < sculkMaxCursors {
			sp.cursors = append(sp.cursors, &sculkCursor{pos: at, charge: c, decayDelay: 1})
		}
		charge -= c
	}
}

// updateCursors is SculkSpreader.updateCursors: every cursor steps, a spent
// one pops, cursors that meet on one block merge (while the sum stays within
// a thousand), and each charged block shows its charge particles.
func (h *hub) updateCursors(players map[int32]*tracked, dim int, sp *sculkSpreader, origin blockPos) {
	if len(sp.cursors) == 0 {
		return
	}
	var kept []*sculkCursor
	merge := map[blockPos]*sculkCursor{}
	chargeAt := map[blockPos]int{}
	var order []blockPos
	for _, c := range sp.cursors {
		if max(abs(c.pos.x-origin.x), abs(c.pos.y-origin.y), abs(c.pos.z-origin.z)) > sculkMaxCursorReach {
			continue // isPosUnreasonable: dropped
		}
		h.updateCursor(players, dim, c, origin)
		if c.charge <= 0 {
			h.levelEvent(players, dim, worldEventSculkCharge, c.pos.x, c.pos.y, c.pos.z, 0)
			continue
		}
		if _, seen := chargeAt[c.pos]; !seen {
			order = append(order, c.pos)
		}
		chargeAt[c.pos] += c.charge
		ex := merge[c.pos]
		switch {
		case ex == nil:
			merge[c.pos] = c
			kept = append(kept, c)
		case c.charge+ex.charge <= sculkMaxCharge:
			ex.charge += c.charge // mergeWith
			c.charge = 0
			ex.updateDelay = min(ex.updateDelay, c.updateDelay)
		default:
			kept = append(kept, c)
			if c.charge < ex.charge {
				merge[c.pos] = c
			}
		}
	}
	for _, p := range order {
		charge, c := chargeAt[p], merge[p]
		if charge > 0 && c != nil && c.facings != nil {
			n := int(math.Log1p(float64(charge))/float64(float32(2.3))) + 1
			h.levelEvent(players, dim, worldEventSculkCharge, p.x, p.y, p.z, int32(n<<6)+int32(*c.facings))
		}
	}
	sp.cursors = kept
}

// tickSculkSpread is SculkCatalystBlockEntity.serverTick for every catalyst
// holding charge: its cursors step while its chunk is loaded. A catalyst
// that is gone takes its charge with it (the block entity is removed).
func (h *hub) tickSculkSpread(players map[int32]*tracked) {
	for pos, sp := range h.sculkSpread {
		w := h.worldFor(pos.dim)
		if !w.Loaded(int32(pos.x>>4), int32(pos.z>>4)) {
			continue
		}
		if !isCatalyst(w.At(pos.x, pos.y, pos.z)) {
			delete(h.sculkSpread, pos)
			continue
		}
		h.updateCursors(players, pos.dim, sp, pos.blockPos)
		if len(sp.cursors) == 0 {
			delete(h.sculkSpread, pos)
		}
	}
}

// catalystHears is CatalystListener.handleGameEvent for ENTITY_DIE: the
// nearest catalyst within eight blocks of a death takes it. The death's
// experience (if any) becomes charge at the cell half a block above where
// it died, and the catalyst blooms either way; the caller drops no orbs
// when it returns true.
func (h *hub) catalystHears(players map[int32]*tracked, dim int, x, y, z float64, xp int) bool {
	if len(h.catalysts) == 0 {
		return false
	}
	mx, my, mz := floorInt(x), floorInt(y), floorInt(z)
	w := h.worldFor(dim)
	var best simPos
	found := false
	bestD := math.Inf(1)
	for pos := range h.catalysts {
		if pos.dim != dim {
			continue
		}
		dx, dy, dz := pos.x-mx, pos.y-my, pos.z-mz
		if dx*dx+dy*dy+dz*dz > 64 || !isCatalyst(w.At(pos.x, pos.y, pos.z)) {
			continue
		}
		// Listeners hear a BY_DISTANCE event nearest first.
		cx, cy, cz := float64(pos.x)+0.5-x, float64(pos.y)+0.5-y, float64(pos.z)+0.5-z
		if d := cx*cx + cy*cy + cz*cz; d < bestD {
			bestD, best, found = d, pos, true
		}
	}
	if !found {
		return false
	}
	if xp > 0 {
		sp := h.sculkSpread[best]
		if sp == nil {
			sp = &sculkSpreader{}
			h.sculkSpread[best] = sp
		}
		sp.addCursors(blockPos{mx, floorInt(y + 0.5), mz}, xp)
	}
	// CatalystListener.bloom: BLOOM on for eight ticks, souls, the sound.
	h.setBlockAt(players, dim, best.blockPos, catalystWith(true))
	h.sculkDue[best] = h.tick.Load() + 8
	bx, by, bz := float64(best.x)+0.5, float64(best.y), float64(best.z)+0.5
	h.spawnParticles(players, dim, particleSculkSoul, bx, by+1.15, bz, 0.2, 0, 2)
	h.playSoundDim(players, dim, "minecraft:block.sculk_catalyst.bloom", sndBlock,
		bx, by+0.5, bz, 2, 0.6+h.rng.Float32()*0.4)
	return true
}
