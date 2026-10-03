package worldgen

import "math"

// Deep-dark sculk: vanilla's sculk_patch_deep_dark and sculk_vein.
//
// sculk_patch_deep_dark tries 256 times a chunk (uniform from the bottom
// to y=256, in the deep dark): from an air or water cell touching a full
// block, ten charges of 32 run SculkSpreader's world-generation cursors for
// 64 steps — veins creep over the faces around the cursor, a vein turns the
// block it clings to into sculk, sculk sprouts a sensor or (one in eleven)
// a can-summon shrieker, and the charge wanders to neighbouring sculk
// within twelve blocks of the patch's start — then half the patches get a
// catalyst at their start on a sturdy floor. sculk_vein tries 204–250
// times: MultifaceGrowthFeature puts a vein on the stone, deepslate, tuff,
// calcite or dripstone face of an air cell (floor, wall or ceiling) and
// spreads it once.
//
// Each origin chunk's patches and veins are planned on the chunk's terrain
// as generated before decoration (the owRegion scratch view), in order, on
// their own streams, and cached; each chunk pass writes its part of the 3×3
// origins' plans onto cells still as the plan found them, so a structure
// stamped since keeps its blocks. A patch with a player's build or dug cell
// in its box is rolled back whole.

var (
	wgSculk         = blockBase("sculk")
	wgSculkVein     = blockBase("sculk_vein")
	wgSculkSensor   = blockBase("sculk_sensor")
	wgSculkShrieker = blockBase("sculk_shrieker")
	wgSculkCatalyst = blockBase("sculk_catalyst")
)

// SculkSpreader.createWorldGenSpreader and the two features' settings.
const (
	sculkGrowthCost     = 50 // growthSpawnCost
	sculkNoGrowthRadius = 1
	sculkChargeDecay    = 5  // chargeDecayRate
	sculkExtraDecay     = 10 // additionalDecayRate
	sculkMaxCursors     = 32
	sculkMaxSpreadSq    = 144 // MAX_WORLDGEN_SPREAD 12, squared
	sculkPatchTries     = 256
	sculkPatchCharges   = 10
	sculkPatchCharge    = 32
	sculkPatchSteps     = 64
	sculkVeinTriesMin   = 204
	sculkVeinTriesMax   = 250
	sculkPatchSalt      = 0x5C_0026_01
	sculkVeinSalt       = 0x5C_0026_02
)

// Directions in Direction.values() order: down, up, north, south, west,
// east (FaceDown … FaceEast).
var sculkDir = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

var sculkDirName = [6]string{"down", "up", "north", "south", "west", "east"}

func sculkOpp(d int) int { return d ^ 1 }

func sculkStep(p [3]int, d int) [3]int {
	return [3]int{p[0] + sculkDir[d][0], p[1] + sculkDir[d][1], p[2] + sculkDir[d][2]}
}

// Vein states as face masks (bit d per face, bit 6 waterlogged), both ways.
var (
	veinLo, veinHi = BlockRange("sculk_vein")
	veinMasks      = func() []uint8 {
		info, _ := InfoForState(veinLo)
		out := make([]uint8, veinHi-veinLo+1)
		for s := veinLo; s <= veinHi; s++ {
			var m uint8
			for d, n := range sculkDirName {
				if GetProperty(info, s, n) == "true" {
					m |= 1 << d
				}
			}
			if GetProperty(info, s, "waterlogged") == "true" {
				m |= 1 << 6
			}
			out[s-veinLo] = m
		}
		return out
	}()
	veinByMask = func() [128]uint32 {
		var out [128]uint32
		for i, m := range veinMasks {
			out[m] = veinLo + uint32(i)
		}
		return out
	}()
)

func isVein(s uint32) bool { return s >= veinLo && s <= veinHi }

func veinHasFace(s uint32, d int) bool { return isVein(s) && veinMasks[s-veinLo]&(1<<d) != 0 }

func veinAnyFace(s uint32) bool { return isVein(s) && veinMasks[s-veinLo]&0x3F != 0 }

func veinWaterlogged(s uint32) bool { return isVein(s) && veinMasks[s-veinLo]&(1<<6) != 0 }

func veinWithFace(s uint32, d int, on bool) uint32 {
	m := veinMasks[s-veinLo]
	if on {
		m |= 1 << d
	} else {
		m &^= 1 << d
	}
	return veinByMask[m]
}

var (
	sculkReplaceableWG = BlockTag("sculk_replaceable_world_gen")
	sculkReplaceable   = BlockTag("sculk_replaceable")
	sculkFire          = BlockTag("fire")
	sculkInhibitors    = BlockTag("sculk_growth_inhibitors")
	sculkVeinOn        = rangesOf("stone", "andesite", "diorite", "granite", "dripstone_block", "calcite", "tuff", "deepslate")
	movingPiston       = rangesOf("moving_piston")
	sculkCatalystState = withProps("sculk_catalyst", "bloom", "false")
	sculkShriekerState = withProps("sculk_shrieker", "can_summon", "true", "shrieking", "false")
	sculkSensorState   = BlockID("sculk_sensor")
)

// sculkCell is one planned write: what the cell held when the plan found
// it, and what it becomes.
type sculkCell struct{ orig, s uint32 }

// sculkWorld is a plan's view: pure terrain plus everything planned so
// far, with a journal so a guarded patch can be undone.
type sculkWorld struct {
	view    *owRegion
	orig    map[[3]int]uint32
	journal map[[3]int]sculkUndo
}

type sculkUndo struct {
	had, hadOrig bool
	s            uint32
}

func (w *sculkWorld) get(p [3]int) uint32 { return w.view.read(p[0], p[1], p[2]) }

func (w *sculkWorld) set(p [3]int, s uint32) {
	if p[1] < MinY || p[1] >= MinY+w.view.cells() {
		return
	}
	if w.journal != nil {
		if _, ok := w.journal[p]; !ok {
			prev, had := w.view.capture[p]
			_, hadOrig := w.orig[p]
			w.journal[p] = sculkUndo{had: had, hadOrig: hadOrig, s: prev}
		}
	}
	if _, ok := w.orig[p]; !ok {
		w.orig[p] = w.get(p)
	}
	w.view.capture[p] = s
}

// isWaterFluid is "the cell's fluid is water": water of any level, or a
// waterlogged vein.
func isWaterFluid(s uint32) bool { return (s >= Water && s <= Water+15) || veinWaterlogged(s) }

// isSculkBehaviour: sculk and sculk veins spread charge; everything else
// is SculkBehaviour.DEFAULT.
func isSculkBehaviour(s uint32) bool { return s == wgSculk || isVein(s) }

// veinCanAttach is MultifaceBlock.canAttachTo: the neighbour's face toward
// the vein is full.
func (w *sculkWorld) veinCanAttach(p [3]int, d int) bool {
	return IsFaceSturdy(w.get(sculkStep(p, d)), sculkOpp(d))
}

// veinPlacement is MultifaceBlock.getStateForPlacement(old, pos, dir), or
// 0 when the face cannot go there.
func (w *sculkWorld) veinPlacement(old uint32, p [3]int, d int) uint32 {
	if veinHasFace(old, d) || !w.veinCanAttach(p, d) {
		return 0
	}
	var base uint32
	switch {
	case isVein(old):
		base = old
	case old == Water:
		base = veinByMask[1<<6]
	default:
		base = veinByMask[0]
	}
	return veinWithFace(base, d, true)
}

// spreadType is MultifaceSpreader.SpreadType: 0 same position, 1 same
// plane, 2 wrap around.
func spreadPos(t int, p [3]int, spreadDir, fromFace int) ([3]int, int) {
	switch t {
	case 0:
		return p, spreadDir
	case 1:
		return sculkStep(p, spreadDir), fromFace
	default:
		return sculkStep(sculkStep(p, spreadDir), fromFace), sculkOpp(spreadDir)
	}
}

var (
	veinSpreadOrder     = []int{0, 1, 2}
	veinSameSpaceSpread = []int{0}
)

// veinCanSpreadInto is SculkVeinSpreaderConfig.canSpreadInto.
func (w *sculkWorld) veinCanSpreadInto(src, at [3]int, face int) bool {
	existing := w.get(at)
	against := w.get(sculkStep(at, face))
	if against == wgSculk || inAnyRange(against, sculkCatalystRange) || inAnyRange(against, movingPiston) {
		return false
	}
	if absInt(src[0]-at[0])+absInt(src[1]-at[1])+absInt(src[2]-at[2]) == 2 {
		if IsFaceSturdy(w.get(sculkStep(src, sculkOpp(face))), face) {
			return false
		}
	}
	if IsFluid(existing) && !isWaterFluid(existing) {
		return false
	}
	if inAnyRange(existing, sculkFire) {
		return false
	}
	if !(IsReplaceable(existing) || existing == Air || isVein(existing) || existing == Water) {
		return false
	}
	return w.veinPlacement(existing, at, face) != 0 // isValidStateForPlacement
}

var sculkCatalystRange = rangesOf("sculk_catalyst")

// spreadFromFaceToward is spreadFromFaceTowardDirection: whether a face
// was placed.
func (w *sculkWorld) spreadFromFaceToward(state uint32, p [3]int, fromFace, spreadDir int, types []int) bool {
	if spreadDir/2 == fromFace/2 {
		return false
	}
	if isVein(state) && !(veinHasFace(state, fromFace) && !veinHasFace(state, spreadDir)) {
		return false // a vein spreads only from a face it has toward one it lacks
	}
	for _, t := range types {
		at, face := spreadPos(t, p, spreadDir, fromFace)
		if !w.veinCanSpreadInto(p, at, face) {
			continue
		}
		if ns := w.veinPlacement(w.get(at), at, face); ns != 0 {
			w.set(at, ns)
			return true
		}
		return false
	}
	return false
}

// spreadAll is MultifaceSpreader.spreadAll: every face it can spread from,
// toward every direction; the number of faces placed.
func (w *sculkWorld) spreadAll(state uint32, p [3]int, types []int) int {
	n := 0
	for face := 0; face < 6; face++ {
		if isVein(state) && !veinHasFace(state, face) {
			continue // canSpreadFrom: a non-vein source spreads from every face
		}
		for dir := 0; dir < 6; dir++ {
			if w.spreadFromFaceToward(state, p, face, dir, types) {
				n++
			}
		}
	}
	return n
}

// shuffled is Util.shuffledCopy: Fisher–Yates from the back.
func sculkShuffled(r TreeRNG, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	for i := n; i > 1; i-- {
		j := r.Intn(i)
		out[i-1], out[j] = out[j], out[i-1]
	}
	return out
}

// sculkCursor is SculkSpreader.ChargeCursor.
type sculkCursor struct {
	pos                     [3]int
	charge                  int
	updateDelay, decayDelay int
	facings                 []int // nil: none recorded yet
	hasFacings              bool
}

// nonCornerNeighbours is ChargeCursor.NON_CORNER_NEIGHBOURS.
var nonCornerNeighbours = func() [][3]int {
	var out [][3]int
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if (x == 0 || y == 0 || z == 0) && !(x == 0 && y == 0 && z == 0) {
					out = append(out, [3]int{x, y, z})
				}
			}
		}
	}
	return out
}()

// attemptSpreadVein is the block's SculkBehaviour.attemptSpreadVein.
func (w *sculkWorld) attemptSpreadVein(c *sculkCursor, state uint32) bool {
	if isSculkBehaviour(state) {
		return w.spreadAll(state, c.pos, veinSpreadOrder) > 0
	}
	switch {
	case !c.hasFacings:
		return w.spreadAll(w.get(c.pos), c.pos, veinSameSpaceSpread) > 0
	case len(c.facings) > 0:
		if state != Air && !isWaterFluid(state) {
			return false
		}
		return w.regrow(c.pos, state, c.facings)
	default:
		return w.spreadAll(state, c.pos, veinSpreadOrder) > 0
	}
}

// regrow is SculkVeinBlock.regrow.
func (w *sculkWorld) regrow(p [3]int, existing uint32, faces []int) bool {
	ns := veinByMask[0]
	found := false
	for _, f := range faces {
		if w.veinCanAttach(p, f) {
			ns = veinWithFace(ns, f, true)
			found = true
		}
	}
	if !found {
		return false
	}
	if isWaterFluid(existing) {
		ns = veinByMask[veinMasks[ns-veinLo]|1<<6]
	}
	w.set(p, ns)
	return true
}

// attemptUseCharge is the block's SculkBehaviour.attemptUseCharge: the
// cursor's charge after this step.
func (w *sculkWorld) attemptUseCharge(c *sculkCursor, state uint32, origin [3]int, r TreeRNG, spreadVeins bool) int {
	switch {
	case state == wgSculk:
		charge := c.charge
		if charge == 0 || r.Intn(sculkChargeDecay) != 0 {
			return charge
		}
		d2 := sqDist(c.pos, origin)
		close := d2 < sculkNoGrowthRadius*sculkNoGrowthRadius
		if !close && w.canPlaceGrowth(c.pos) {
			if r.Intn(sculkGrowthCost) < charge {
				up := sculkStep(c.pos, 1)
				g := sculkSensorState
				if r.Intn(11) == 0 {
					g = sculkShriekerState
				}
				if isWaterFluid(w.get(up)) {
					g = withWaterlogged(g)
				}
				w.set(up, g)
			}
			return max(0, charge-sculkGrowthCost)
		}
		if r.Intn(sculkExtraDecay) != 0 {
			return charge
		}
		if close {
			return charge - 1
		}
		outer := math.Sqrt(float64(d2)) - sculkNoGrowthRadius
		reach := float64((24 - sculkNoGrowthRadius) * (24 - sculkNoGrowthRadius))
		f := math.Min(1, outer*outer/reach)
		return charge - max(1, int(float64(charge)*f*0.5))
	case isVein(state):
		if spreadVeins && w.attemptPlaceSculk(c.pos, r) {
			return c.charge - 1
		}
		if r.Intn(sculkChargeDecay) == 0 {
			return int(math.Floor(float64(c.charge) * 0.5))
		}
		return c.charge
	default:
		if c.decayDelay > 0 {
			return c.charge
		}
		return 0
	}
}

func sqDist(a, b [3]int) int {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return dx*dx + dy*dy + dz*dz
}

func withWaterlogged(s uint32) uint32 {
	info, ok := InfoForState(s)
	if !ok {
		return s
	}
	return SetProperty(info, s, "waterlogged", "true")
}

// canPlaceGrowth is SculkBlock.canPlaceGrowth: open (or water) above, and
// at most two sensors or shriekers in the 9×3×9 box above the sculk.
func (w *sculkWorld) canPlaceGrowth(p [3]int) bool {
	above := w.get(sculkStep(p, 1))
	if above != Air && !(above >= Water && above <= Water+15) {
		return false
	}
	n := 0
	for dx := -4; dx <= 4; dx++ {
		for dy := 0; dy <= 2; dy++ {
			for dz := -4; dz <= 4; dz++ {
				if inAnyRange(w.get([3]int{p[0] + dx, p[1] + dy, p[2] + dz}), sculkInhibitors) {
					if n++; n > 2 {
						return false
					}
				}
			}
		}
	}
	return true
}

// attemptPlaceSculk is SculkVeinBlock.attemptPlaceSculk: turn a block a
// face of the vein clings to into sculk, spread veins round it, and let
// the veins beside it that now face sculk drop those faces.
func (w *sculkWorld) attemptPlaceSculk(p [3]int, r TreeRNG) bool {
	state := w.get(p)
	for _, support := range sculkShuffled(r, 6) {
		if !veinHasFace(state, support) {
			continue
		}
		sp := sculkStep(p, support)
		if !inAnyRange(w.get(sp), sculkReplaceableWG) {
			continue
		}
		w.set(sp, wgSculk)
		w.spreadAll(wgSculk, sp, veinSpreadOrder)
		skip := sculkOpp(support)
		for d := 0; d < 6; d++ {
			if d == skip {
				continue
			}
			vp := sculkStep(sp, d)
			if v := w.get(vp); isVein(v) {
				w.veinDischarged(v, vp)
			}
		}
		return true
	}
	return false
}

// veinDischarged is SculkVeinBlock.onDischarged: faces against sculk go;
// with none left the cell is air, or water if it held water.
func (w *sculkWorld) veinDischarged(state uint32, p [3]int) {
	if !isVein(state) {
		return
	}
	for d := 0; d < 6; d++ {
		if veinHasFace(state, d) && w.get(sculkStep(p, d)) == wgSculk {
			state = veinWithFace(state, d, false)
		}
	}
	if !veinAnyFace(state) {
		if isWaterFluid(w.get(p)) {
			state = Water
		} else {
			state = Air
		}
	}
	w.set(p, state)
}

// hasSubstrateAccess is SculkVeinBlock.hasSubstrateAccess.
func (w *sculkWorld) hasSubstrateAccess(s uint32, p [3]int) bool {
	if !isVein(s) {
		return false
	}
	for d := 0; d < 6; d++ {
		if veinHasFace(s, d) && inAnyRange(w.get(sculkStep(p, d)), sculkReplaceable) {
			return true
		}
	}
	return false
}

// validMovementPos is ChargeCursor.getValidMovementPos.
func (w *sculkWorld) validMovementPos(p, origin [3]int, r TreeRNG) ([3]int, bool) {
	best, moved := p, false
	for _, i := range sculkShuffled(r, len(nonCornerNeighbours)) {
		o := nonCornerNeighbours[i]
		n := [3]int{p[0] + o[0], p[1] + o[1], p[2] + o[2]}
		if dx, dz := origin[0]-n[0], origin[2]-n[2]; dx*dx+dz*dz > sculkMaxSpreadSq {
			continue
		}
		t := w.get(n)
		if !isSculkBehaviour(t) || !w.movementUnobstructed(p, n) {
			continue
		}
		best, moved = n, true
		if w.hasSubstrateAccess(t, n) {
			break
		}
	}
	return best, moved
}

// movementUnobstructed is ChargeCursor.isMovementUnobstructed.
func (w *sculkWorld) movementUnobstructed(from, to [3]int) bool {
	dx, dy, dz := to[0]-from[0], to[1]-from[1], to[2]-from[2]
	if absInt(dx)+absInt(dy)+absInt(dz) == 1 {
		return true
	}
	pick := func(v, neg, pos int) int {
		if v < 0 {
			return neg
		}
		return pos
	}
	x, y, z := pick(dx, FaceWest, FaceEast), pick(dy, FaceDown, FaceUp), pick(dz, FaceNorth, FaceSouth)
	open := func(d int) bool { return !IsFaceSturdy(w.get(sculkStep(from, d)), sculkOpp(d)) }
	switch {
	case dx == 0:
		return open(y) || open(z)
	case dy == 0:
		return open(x) || open(z)
	default:
		return open(x) || open(y)
	}
}

// update is ChargeCursor.update for world generation.
func (w *sculkWorld) update(c *sculkCursor, origin [3]int, r TreeRNG, spreadVeins bool) {
	if c.charge <= 0 {
		return
	}
	if c.updateDelay > 0 {
		c.updateDelay--
		return
	}
	state := w.get(c.pos)
	if spreadVeins && w.attemptSpreadVein(c, state) && state != wgSculk {
		state = w.get(c.pos) // canChangeBlockStateOnSpread: all but sculk
	}
	sculkHere := isSculkBehaviour(state) // the behaviour that decides the decay delay
	c.charge = w.attemptUseCharge(c, state, origin, r, spreadVeins)
	if c.charge <= 0 {
		w.veinDischarged(state, c.pos)
		return
	}
	to, ok := w.validMovementPos(c.pos, origin, r)
	if !ok { // world generation: a cursor with nowhere to go ends
		w.veinDischarged(state, c.pos)
		c.charge = 0
		return
	}
	w.veinDischarged(state, c.pos)
	c.pos = to
	if next := w.get(to); isSculkBehaviour(next) {
		c.facings, c.hasFacings = c.facings[:0], true // availableFaces: none for sculk
		for d := 0; d < 6; d++ {
			if veinHasFace(next, d) {
				c.facings = append(c.facings, d)
			}
		}
	}
	if sculkHere {
		c.decayDelay = 1 // sculk and veins: updateDecayDelay is 1
	} else if c.decayDelay > 0 {
		c.decayDelay-- // DEFAULT: max(age-1, 0)
	}
	c.updateDelay = 1 // getSculkSpreadDelay
}

// canSpreadFrom is SculkPatchFeature.canSpreadFrom.
func (w *sculkWorld) canSpreadFrom(p [3]int) bool {
	s := w.get(p)
	if isSculkBehaviour(s) {
		return true
	}
	if s != Air && s != Water {
		return false
	}
	for d := 0; d < 6; d++ {
		if IsFullCube(w.get(sculkStep(p, d))) {
			return true
		}
	}
	return false
}

// patch is sculk_patch_deep_dark: SculkPatchFeature.place (one round of
// ten charges of 32 over 64 steps), then the sequence's catalyst.
func (w *sculkWorld) patch(origin [3]int, r TreeRNG) bool {
	if !w.patchCore(origin, r) {
		return false
	}
	w.catalyst(origin, r)
	return true
}

// patchAncientCity is sculk_patch_ancient_city: the same patch, then an
// overlay of the catalyst and one to three can-summon shriekers, each
// offset up to two blocks on every axis into air over a sturdy floor.
func (w *sculkWorld) patchAncientCity(origin [3]int, r TreeRNG) bool {
	if !w.patchCore(origin, r) {
		return false
	}
	w.catalyst(origin, r)
	n := 1 + r.Intn(3) // CountPlacement UniformInt(1, 3)
	for i := 0; i < n; i++ {
		p := [3]int{origin[0] + r.Intn(5) - 2, origin[1] + r.Intn(5) - 2, origin[2] + r.Intn(5) - 2}
		if w.get(p) == Air && IsFaceSturdy(w.get(sculkStep(p, 0)), FaceUp) {
			w.set(p, sculkShriekerState)
		}
	}
	return true
}

// catalyst is the sequence's catalyst: half the time, on a sturdy floor.
func (w *sculkWorld) catalyst(origin [3]int, r TreeRNG) {
	if r.Float64() < 0.5 && IsFaceSturdy(w.get(sculkStep(origin, 0)), FaceUp) {
		w.set(origin, sculkCatalystState)
	}
}

// patchCore is SculkPatchFeature.place.
func (w *sculkWorld) patchCore(origin [3]int, r TreeRNG) bool {
	if !w.canSpreadFrom(origin) {
		return false
	}
	var cursors []*sculkCursor
	for i := 0; i < sculkPatchCharges; i++ {
		if len(cursors) < sculkMaxCursors {
			cursors = append(cursors, &sculkCursor{pos: origin, charge: sculkPatchCharge, decayDelay: 1})
		}
	}
	for step := 0; step < sculkPatchSteps && len(cursors) > 0; step++ {
		var kept []*sculkCursor
		for _, c := range cursors {
			w.update(c, origin, r, true)
			if c.charge > 0 {
				kept = append(kept, c) // world generation never merges cursors
			}
		}
		cursors = kept
	}
	return true
}

// vein is MultifaceGrowthFeature.place for sculk_vein.
func (w *sculkWorld) vein(origin [3]int, r TreeRNG) {
	airOrWater := func(s uint32) bool { return s == Air || (s >= Water && s <= Water+15) }
	if !airOrWater(w.get(origin)) {
		return
	}
	valid := [6]int{FaceUp, FaceDown, FaceNorth, FaceEast, FaceSouth, FaceWest} // ceiling, floor, walls
	order := sculkShuffled(r, 6)
	dirs := make([]int, 6)
	for i, j := range order {
		dirs[i] = valid[j]
	}
	if w.veinGrowth(origin, w.get(origin), dirs, r) {
		return
	}
	for _, sd := range dirs {
		var except []int
		for _, d := range valid {
			if d != sculkOpp(sd) {
				except = append(except, d)
			}
		}
		perm := sculkShuffled(r, len(except))
		pl := make([]int, len(except))
		for i, j := range perm {
			pl[i] = except[j]
		}
		// Vanilla steps pos from the origin each time, so its twenty-step
		// search only ever looks one cell out.
		p := sculkStep(origin, sd)
		s := w.get(p)
		if !airOrWater(s) && !isVein(s) {
			continue
		}
		if w.veinGrowth(p, s, pl, r) {
			return
		}
	}
}

// veinGrowth is MultifaceGrowthFeature.placeGrowthIfPossible.
func (w *sculkWorld) veinGrowth(p [3]int, old uint32, dirs []int, r TreeRNG) bool {
	for _, d := range dirs {
		if !inAnyRange(w.get(sculkStep(p, d)), sculkVeinOn) {
			continue
		}
		ns := w.veinPlacement(old, p, d)
		if ns == 0 {
			return false
		}
		w.set(p, ns)
		if r.Float64() < 1.0 { // chance_of_spreading
			for _, sd := range sculkShuffled(r, 6) {
				if w.spreadFromFaceToward(ns, p, d, sd, veinSpreadOrder) {
					break
				}
			}
		}
		return true
	}
	return false
}

// sculkPlan is one origin chunk's planned writes.
type sculkPlan struct {
	cells map[[3]int]sculkCell
}

var sculkCache = map[roomKey]*sculkPlan{}

// sculkCacheCap bounds the cached plans: a deep-dark chunk's runs to
// thousands of cells, and the world pod has 2 GiB.
const sculkCacheCap = 1024

// sculkIn is origin chunk (cx, cz)'s sculk plan (nil: none), cached.
func (g *Generator) sculkIn(cx, cz int32) *sculkPlan {
	k := roomKey{g: g, cx: cx, cz: cz}
	roomMu.Lock()
	p, ok := sculkCache[k]
	roomMu.Unlock()
	if ok {
		return p
	}
	p = g.planSculk(cx, cz)
	roomMu.Lock()
	if len(sculkCache) >= sculkCacheCap {
		sculkCache = map[roomKey]*sculkPlan{}
	}
	sculkCache[k] = p
	roomMu.Unlock()
	return p
}

const deepDark = "minecraft:deep_dark"

// planSculk runs origin chunk (cx, cz)'s patches, then its veins.
func (g *Generator) planSculk(cx, cz int32) *sculkPlan {
	ox, oz := int(cx)*16, int(cz)*16
	view := &owRegion{g: g, baseX: ox, baseZ: oz, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	if !view.chunkHasCaveBiome(ox, oz, deepDark) {
		return nil
	}
	w := &sculkWorld{view: view, orig: map[[3]int]uint32{}}
	top := g.Ceiling()
	h := oreUniform(oreAboveBottom(0), oreAbs(256))
	r := newTreeRNG(g.seed^sculkPatchSalt, ox, oz)
	for i := 0; i < sculkPatchTries; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		y := h.sample(r, top)
		if view.caveBiomeAt(x, y, z) != deepDark {
			continue
		}
		w.journal = map[[3]int]sculkUndo{}
		if !w.patch([3]int{x, y, z}, r) || len(w.journal) == 0 {
			continue
		}
		if w.journalTouched() {
			w.undo()
		}
	}
	w.journal = nil
	rv := newTreeRNG(g.seed^sculkVeinSalt, ox, oz)
	n := sculkVeinTriesMin + rv.Intn(sculkVeinTriesMax-sculkVeinTriesMin+1)
	for i := 0; i < n; i++ {
		x, z := ox+rv.Intn(16), oz+rv.Intn(16)
		y := h.sample(rv, top)
		if view.caveBiomeAt(x, y, z) != deepDark {
			continue
		}
		w.journal = map[[3]int]sculkUndo{}
		w.vein([3]int{x, y, z}, rv)
		if len(w.journal) > 0 && w.journalTouched() {
			w.undo()
		}
	}
	p := &sculkPlan{cells: map[[3]int]sculkCell{}}
	for c, o := range w.orig {
		if s := view.capture[c]; s != o {
			p.cells[c] = sculkCell{orig: o, s: s}
		}
	}
	if len(p.cells) == 0 {
		return nil
	}
	return p
}

// journalTouched reports whether a player built or dug in the box of the
// cells the last patch or vein changed.
func (w *sculkWorld) journalTouched() bool {
	x0, y0, z0 := math.MaxInt, math.MaxInt, math.MaxInt
	x1, y1, z1 := math.MinInt, math.MinInt, math.MinInt
	for c := range w.journal {
		x0, y0, z0 = min(x0, c[0]), min(y0, c[1]), min(z0, c[2])
		x1, y1, z1 = max(x1, c[0]), max(y1, c[1]), max(z1, c[2])
	}
	return w.view.g.touchedIn(x0, y0, z0, x1, y1, z1)
}

// undo rolls the journal back.
func (w *sculkWorld) undo() {
	for c, u := range w.journal {
		if u.had {
			w.view.capture[c] = u.s
		} else {
			delete(w.view.capture, c)
		}
		if !u.hadOrig {
			delete(w.orig, c)
		}
	}
	w.journal = map[[3]int]sculkUndo{}
}

// placeSculk writes the 3×3 origin chunks' sculk plans into this chunk,
// each cell only where the chunk still holds what the plan found there.
func (g *Generator) placeSculk(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			p := g.sculkIn(cx+dcx, cz+dcz)
			if p == nil {
				continue
			}
			for c, cell := range p.cells {
				lx, lz := c[0]-baseX, c[2]-baseZ
				if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 {
					continue
				}
				if sectionBlockAt(ch, lx, c[1], lz) == cell.orig {
					setSectionBlock(ch, lx, c[1], lz, cell.s, true)
				}
			}
		}
	}
}
