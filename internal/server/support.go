package server

import (
	"strings"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Whether a block has what it needs to stay where it is.
//
// worldgen.SupportFor classifies every block into the SHAPE of its
// requirement; this applies the shape at a position. One predicate, consulted
// at both ends: placement refuses a block that would not survive, and a
// neighbour change knocks down anything that no longer does. Before this,
// support was a six-block list checked only directly above an edit, so a wall
// torch survived the wall it was on and a rail could be placed in mid-air.

// plantSoils are the blocks a plant will root in — vanilla's dirt tag plus the
// handful of other grounds saplings and flowers accept.
var plantSoils = func() map[uint32]bool {
	out := map[uint32]bool{}
	for _, n := range []string{
		"dirt", "grass_block", "podzol", "coarse_dirt", "rooted_dirt", "mycelium",
		"moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots", "farmland",
		"sand", "red_sand", "suspicious_sand", "soul_sand", "soul_soil",
		"crimson_nylium", "warped_nylium", "clay", "gravel", "terracotta",
		"snow_block", "powder_snow", "end_stone", "netherrack",
	} {
		lo, hi, ok := worldgen.BlockRangeOK(n)
		if !ok {
			continue
		}
		for s := lo; s <= hi; s++ {
			out[s] = true
		}
	}
	return out
}()

// Column plants stand on their own kind. Vanilla's canSurvive for sugar cane
// and cactus accepts "the block below is this block" before it ever looks at
// the ground, and #bamboo_plantable_on lists bamboo and bamboo_sapling
// alongside the soils.
//
// Without this a column's SECOND segment reads as unsupported, and because
// dropUnsupported runs on every nearby block edit, breaking any block beside a
// cane, cactus or bamboo destroyed the entire stack above its base.
//
// NOTE the GROUND rule stays deliberately laxer than vanilla: vanilla also
// wants water beside the soil under sugar cane and refuses a cactus with a
// solid neighbour. Enforcing those here would delete every farm built while
// this engine allowed them, so that tightening is a separate, announced call.
var (
	caneStates        = plantStates("sugar_cane")
	cactusStates      = plantStates("cactus")
	cactusFlowerState = worldgen.BlockBase("cactus_flower")
	bambooStates      = plantStates("bamboo")
	bambooSapStates   = plantStates("bamboo_sapling")
)

// plantStates is a block name's state span; an unknown name yields an empty
// range that matches nothing.
func plantStates(name string) stateRange {
	lo, hi, ok := worldgen.BlockRangeOK(name)
	if !ok {
		return stateRange{1, 0}
	}
	return stateRange{lo, hi}
}

func inStates(s uint32, r stateRange) bool { return s >= r.lo && s <= r.hi }

// stacksOnItself reports whether a column plant may stand on the block below.
func stacksOnItself(state, below uint32) bool {
	switch {
	case inStates(state, caneStates):
		return inStates(below, caneStates)
	case inStates(state, cactusStates):
		return inStates(below, cactusStates)
	case inStates(state, bambooStates), inStates(state, bambooSapStates):
		return inStates(below, bambooStates) || inStates(below, bambooSapStates)
	}
	return false
}

// canPlaceAt is supported plus the rules vanilla checks only when a block is
// placed, never afterwards: a wall hanging sign must be held from either
// side along its facing's clockwise axis — a full sturdy face there, or
// another wall hanging sign turned the same way (WallHangingSignBlock.canPlace).
func canPlaceAt(w *world.World, pos blockPos, state uint32) bool {
	if !supported(w, pos, state) {
		return false
	}
	k, ok := signKind(state)
	if w == nil || !ok || k != signHangingWall {
		return true
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	f := worldgen.GetProperty(info, state, "facing")
	cw := clockwiseFacing(f)
	for _, side := range []string{cw, oppositeOf(cw)} {
		dx, dz := facingDelta(side)
		n := w.At(pos.x+dx, pos.y, pos.z+dz)
		if nk, ok := signKind(n); ok && nk == signHangingWall {
			if ni, ok := worldgen.InfoForState(n); ok && facingAxisX(worldgen.GetProperty(ni, n, "facing")) == facingAxisX(f) {
				return true
			}
			continue
		}
		if holdsBlock(n) {
			return true
		}
	}
	return false
}

// supported reports whether the block at pos can stay there. Taken over a
// world rather than the hub so the placement path — which runs on a session
// goroutine — can ask the same question the tick loop asks.
func supported(w *world.World, pos blockPos, state uint32) bool {
	if w == nil {
		return true
	}
	below := func() uint32 { return w.At(pos.x, pos.y-1, pos.z) }
	above := func() uint32 { return w.At(pos.x, pos.y+1, pos.z) }
	behind := func() uint32 {
		info, ok := worldgen.InfoForState(state)
		if !ok {
			return worldgen.Air
		}
		// The block a wall-mounted block hangs on is opposite the way it faces.
		dx, dz := facingDelta(oppositeOf(worldgen.GetProperty(info, state, "facing")))
		return w.At(pos.x+dx, pos.y, pos.z+dz)
	}
	prop := func(name string) string {
		info, ok := worldgen.InfoForState(state)
		if !ok {
			return ""
		}
		return worldgen.GetProperty(info, state, name)
	}

	// The upper half of a two-block block rests on its own lower half, whatever
	// that lower half needs. Without this, every tall grass, sunflower and open
	// door in the world reads as unsupported — the top half sits on a plant or
	// on a non-collidable open door, neither of which holds anything.
	if info, ok := worldgen.InfoForState(state); ok && isBed(info) {
		return bedPartnerStands(w, pos, state, info) // BedBlock.updateShape
	}
	if prop("half") == "upper" {
		return sameBlockFamily(below(), state) || holdsBlock(below())
	}
	// …and the other way (DoorBlock/DoublePlantBlock.updateShape): a lower
	// half whose upper half has gone goes too.
	if prop("half") == "lower" {
		// (Not a pitcher crop: young ones are a lower half alone, and
		// PitcherCropBlock.updateShape only asks canSurvive.)
		if info, ok := worldgen.InfoForState(state); ok && isTwoTall(info) && !isPitcherCrop(state) {
			up := above()
			if ui, ok := worldgen.InfoForState(up); !ok || !sameBlockFamily(up, state) || worldgen.GetProperty(ui, up, "half") != "upper" {
				return false
			}
		}
	}

	if isWire(state) { // RedStoneWireBlock.canSurviveOn: a sturdy top face or a hopper
		return canHoldDust(below())
	}
	if isCocoa(state) { // CocoaBlock.canSurvive: a jungle log where it faces
		dx, dz := facingDelta(prop("facing"))
		return inRanges2(w.At(pos.x+dx, pos.y, pos.z+dz), cocoaSupports)
	}
	if isMultiface(state) { // vines, lichen, sculk veins, resin: any face still attached
		_, ok := multifaceUpdated(w, pos, state)
		return ok
	}
	if isScaffolding(state) { // ScaffoldingBlock.canSurvive: distance < 7
		return scaffoldDistance(w, pos) < 7
	}
	if isChorusPlant(state) || isChorusFlower(state) {
		return chorusSurvives(w, pos, state)
	}
	if state >= snowLayer1 && state <= snowLayer1+7 { // SnowLayerBlock.canSurvive
		return snowCanStandOn(below())
	}
	if state == brownMushroomState || state == redMushroomState {
		// MushroomBlock.canSurvive: always on mycelium, podzol or nylium;
		// elsewhere only on an opaque cube and in light under 13 — a
		// mushroom in open daylight goes at its next neighbour update.
		b := below()
		if mushroomOverride[b] {
			return true
		}
		sky, blk := w.LightAt(pos.x, pos.y, pos.z)
		return max(sky, blk) < 13 && worldgen.IsSolidFull(b)
	}
	if isCarpetBlock(state) {
		// CarpetBlock.canSurvive: !isEmptyBlock(below) — anything but air
		// holds a carpet: a torch, a flower, a fence, water.
		b := below()
		return b != worldgen.Air && b != caveAirState && b != voidAirState
	}
	if isFire(state) && state != soulFire {
		// FireBlock.canSurvive, which its updateShape asks on every
		// neighbour change: a sturdy floor, or something beside it that can
		// burn — a fire left with neither goes out at once.
		if holdsBlock(below()) {
			return true
		}
		for _, d := range sixDirs {
			if isFlammable(w.At(pos.x+d.x, pos.y+d.y, pos.z+d.z)) {
				return true
			}
		}
		return false
	}
	if state == soulFire { // SoulFireBlock.canSurvive: its soul block below
		return soulFireBase(below())
	}
	if k, ok := signKind(state); ok && k == signHangingWall {
		// WallHangingSignBlock never overrides canSurvive: once placed it
		// stays, whatever happens to its sides (canPlaceAt checks them).
		return true
	}
	switch worldgen.SupportFor(state) {
	case worldgen.SupportFloor:
		return holdsBlock(below())
	case worldgen.SupportSoil:
		if inStates(state, caneStates) { // SugarCaneBlock.canSurvive
			b := below()
			if inStates(b, caneStates) {
				return true
			}
			if !worldgen.IsDirtTag(b) && !inRanges2(b, sandStates) {
				return false
			}
			for _, d := range [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
				n := w.At(pos.x+d[0], pos.y-1, pos.z+d[2])
				if worldgen.IsWater(n) || worldgen.IsWaterlogged(n) || n == frostedIceState {
					return true
				}
			}
			return false
		}
		if state == cactusFlowerState { // CactusFlowerBlock.mayPlaceOn: #support_override_cactus_flower, or a sturdy centre
			b := below()
			return inStates(b, cactusStates) || (b >= farmlandMin && b <= farmlandMin+7) || holdsBlock(b)
		}
		if inStates(state, cactusStates) { // CactusBlock.canSurvive
			for _, d := range [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
				n := w.At(pos.x+d[0], pos.y, pos.z+d[2])
				if worldgen.IsSolid(n) || worldgen.IsLava(n) { // isSolid: a carpet, candle or pot beside it is harmless
					return false
				}
			}
			b := below()
			return (inStates(b, cactusStates) || inRanges2(b, sandStates)) && !worldgen.IsFluid(above())
		}
		if inStates(state, bambooStates) || inStates(state, bambooSapStates) { // BambooStalkBlock: #bamboo_plantable_on
			b := below()
			return inStates(b, bambooStates) || inStates(b, bambooSapStates) || worldgen.IsDirtTag(b) ||
				inRanges2(b, sandStates) || inRanges2(b, gravelStates)
		}
		return plantSoils[below()] || stacksOnItself(state, below())
	case worldgen.SupportFarmland:
		// CropBlock.canSurvive: farmland under it, and a raw brightness of
		// eight or more (sky light at full day, or block light) where it grows.
		sky, blk := w.LightAt(pos.x, pos.y, pos.z)
		return isFarmland(below()) && max(sky, blk) >= 8
	case worldgen.SupportWall:
		return holdsBlock(behind())
	case worldgen.SupportCeiling:
		a := above()
		if k, ok := signKind(state); ok && k == signHangingCeiling {
			// CeilingHangingSignBlock.canSurvive is a sturdy CENTER on the face
			// above, and a hanging sign's support shape is its own outline —
			// which is how signs chain one under another.
			if ak, ok := signKind(a); ok && (ak == signHangingCeiling || ak == signHangingWall) {
				return true
			}
		}
		if inRange(state, hangingMossRng) && inRange(a, hangingMossRng) {
			return true // HangingMossBlock.canStayAtPosition: or more moss above
		}
		return holdsBlock(a)
	case worldgen.SupportFace:
		switch prop("face") {
		case "floor":
			return holdsBlock(below())
		case "ceiling":
			return holdsBlock(above())
		default:
			return holdsBlock(behind())
		}
	case worldgen.SupportAttached:
		// AmethystClusterBlock.canSurvive: the block it points away from —
		// below a bud facing up, above one facing down, behind one on a wall.
		// (Lichen and sculk vein share this class but are multiface blocks,
		// answered above; this is left for anything else it may hold.)
		if st, f := amethystStage(state); st >= 0 && f != "" {
			switch f {
			case "up":
				return holdsBlock(below())
			case "down":
				return holdsBlock(above())
			}
			return holdsBlock(behind())
		}
		if holdsBlock(behind()) {
			return true
		}
		for _, d := range supportNeighbours {
			if holdsBlock(w.At(pos.x+d[0], pos.y+d[1], pos.z+d[2])) {
				return true
			}
		}
		return false
	case worldgen.SupportWater:
		b := below()
		return worldgen.IsWater(b) || b == worldgen.BlockBase("ice")
	case worldgen.SupportGrowsUp, worldgen.SupportGrowsDown:
		// GrowingPlantBlock.canSurvive: the cell OPPOSITE the way it grows
		// holds it — another length of the same plant, or a sturdy face.
		anchor := below()
		if worldgen.SupportFor(state) == worldgen.SupportGrowsDown {
			anchor = above()
		}
		if anchor == magmaBlockState {
			if g, ok := plantFamily(state); ok && g.intoWater {
				return false // KelpBlock.canAttachTo: #cannot_support_kelp (magma)
			}
		}
		return sameGrowingPlant(state, anchor) || holdsBlock(anchor)
	case worldgen.SupportMossCarpet:
		// MossyCarpetBlock.canSurvive: the base layer needs only something —
		// anything that is not air — under it; a layer growing up a wall
		// needs a BASE carpet directly beneath.
		b := below()
		if prop("bottom") == "true" {
			return b != worldgen.Air
		}
		// (MossyCarpetBlock.updateShape also clears an upper layer left with
		// no side at all — hasFaces.)
		return worldgen.SupportFor(b) == worldgen.SupportMossCarpet && bottomProp(b) && mossCarpetHasSide(state)
	case worldgen.SupportBell:
		// BellBlock.canSurvive: the attachment says which way it hangs. On a
		// wall — one or two — it asks only for the wall it faces
		// (canAttach(pos, facing)); a double-wall bell that loses its other
		// wall becomes single-wall first (shapeupdate.go shapeBell).
		switch prop("attachment") {
		case "ceiling":
			return holdsBlock(above())
		case "floor":
			return holdsBlock(below())
		default: // single_wall and double_wall: the wall it faces
			dx, dz := facingDelta(prop("facing"))
			return holdsBlock(w.At(pos.x+dx, pos.y, pos.z+dz))
		}
	case worldgen.SupportStem:
		// BigDripleafBlock / BigDripleafStemBlock.canSurvive: the plant roots
		// in #supports_big_dripleaf or stands on more of itself — a leaf on
		// its stem, which has no collision and so is no floor — and a stem
		// also carries the rest of the plant above.
		b := below()
		if isBigDripleaf(state) { // a leaf stands on a leaf, a stem or the ground
			return worldgen.SupportFor(b) == worldgen.SupportStem || supportsBigDripleaf[b]
		}
		rooted := (worldgen.SupportFor(b) == worldgen.SupportStem && !isBigDripleaf(b)) || supportsBigDripleaf[b]
		return rooted && worldgen.SupportFor(above()) == worldgen.SupportStem // a stem or the leaf above
	case worldgen.SupportSpawn:
		// FrogspawnBlock.mayPlaceOn: the fluid under it is #supports_frogspawn
		// — a water SOURCE, which a waterlogged block or a bed of seagrass
		// holds too, and running water does not — and the clutch's own cell
		// holds no fluid. What lies above it does not matter.
		b := below()
		return worldgen.IsFluidSource(b, worldgen.WaterBase) || (worldgen.HoldsWater(b) && !worldgen.IsWater(b))
	case worldgen.SupportHangable:
		if prop("hanging") == "true" {
			return holdsBlock(above())
		}
		return holdsBlock(below())
	case worldgen.SupportSpeleothem:
		if prop("vertical_direction") == "up" {
			return holdsBlock(below())
		}
		return holdsBlock(above())
	}
	return true
}

var dirtPathState = worldgen.BlockBase("dirt_path")

// carpetBlocks are the CarpetBlock family: the sixteen wool carpets and the
// moss carpet (the pale moss carpet is a MossyCarpetBlock, with its own rule).
var carpetBlocks = func() map[uint32]bool {
	out := map[uint32]bool{}
	for _, n := range worldgen.AllBlockNames() {
		if strings.HasSuffix(n, "_carpet") && n != "pale_moss_carpet" {
			if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
				for s := lo; s <= hi; s++ {
					out[s] = true
				}
			}
		}
	}
	return out
}()

func isCarpetBlock(s uint32) bool { return carpetBlocks[s] }

// mossCarpetHasSide is MossyCarpetBlock.hasFaces for a layer without a
// base: some side still climbs a wall.
func mossCarpetHasSide(s uint32) bool {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return false
	}
	for _, f := range paleCarpetSides {
		if worldgen.GetProperty(info, s, f.prop) != "none" {
			return true
		}
	}
	return false
}

// bottomProp reads a state's "bottom" flag — the pale moss carpet's BASE,
// which vanilla spells "bottom" on the wire.
func bottomProp(s uint32) bool {
	info, ok := worldgen.InfoForState(s)
	return ok && worldgen.GetProperty(info, s, "bottom") == "true"
}

// coversSoil is DirtPathBlock.canSurvive and FarmBlock.canSurvive inverted —
// one predicate, because the two blocks ask the same question and answer it
// the same way: a solid block laid on top turns the ground back to dirt, and a
// fence gate, which both classes name explicitly, does not count as one.
func coversSoil(above uint32) bool {
	return solidLid(above) && !isFenceGate(above)
}

// solidLid is BlockStateBase.isSolid, which is what FarmBlock and
// DirtPathBlock ask about the block above them. It is a THRESHOLD on the
// collision shape's bounds, not "does it collide at all": the bounds must
// average 0.729 of a block across the three axes or stand a full block tall.
// A slab, a chest, a door and a ladder pass it; a carpet, a candle, a skull
// and a pitcher crop do not, which is why a field keeps its tilth under a
// crop and loses it under a chest. The table is generated per state from the
// real collision shapes (scripts/gen_solid.py).
func solidLid(above uint32) bool {
	// A block mid-push rides in a moving_piston cell, whose shape is worked
	// out as it travels. Vanilla never calls a dynamically shaped block solid
	// (and FarmBlock names this one outright anyway), so the journey leaves
	// the row alone — only the block that comes to rest there ploughs it up.
	if isMovingPiston(above) {
		return false
	}
	return worldgen.IsSolid(above)
}

// isFenceGate asks the state its own name rather than keeping a list, so a
// new wood type needs nothing here.
func isFenceGate(state uint32) bool {
	n, ok := worldgen.StateName(state)
	return ok && strings.HasSuffix(n, "_fence_gate")
}

// sameGrowingPlant reports whether a cell holds the same growing plant as the
// one asking — its tip or its grown body, which is what a stalk of kelp or a
// curtain of cave vines hangs from.
func sameGrowingPlant(state, other uint32) bool {
	a, ok := plantFamily(state)
	b, ok2 := plantFamily(other)
	return ok && ok2 && a.headLo == b.headLo // GrowingPlantBlock: its own head or body, not another plant's
}

// plantFamily finds the growing plant a head or body state belongs to.
func plantFamily(state uint32) (growingPlant, bool) {
	if g, ok := growingPlantOf(state); ok {
		return g, true
	}
	return growingPlantOfBody(state)
}

// holdsBlock reports whether a block can hold another one against it.
//
// Deliberately NOT "is a full opaque cube": vanilla asks whether the FACE is
// sturdy, which a top slab, a stair, a fence post and a pane of glass all
// satisfy. Testing for a full cube instead would call every torch on a fence
// and every carpet on a slab unsupported and tear down builds that have stood
// for months. Collidable-and-not-replaceable is the conservative reading —
// it never destroys something legitimate, at the price of allowing a few
// attachments vanilla would refuse (a torch on a torch). A real per-face shape
// test is the way to tighten this.
func holdsBlock(state uint32) bool {
	return state != worldgen.Air && worldgen.Collides(state) && !worldgen.IsReplaceable(state)
}

// sameBlockFamily reports whether two states belong to the same block — used
// to let an upper half rest on its own lower half.
func sameBlockFamily(a, b uint32) bool {
	ia, oka := worldgen.InfoForState(a)
	ib, okb := worldgen.InfoForState(b)
	return oka && okb && ia.Min == ib.Min
}

// sameBlock is sameBlockFamily that also knows a block with no properties
// (short grass, a fern), whose one state has no layout to compare.
func sameBlock(a, b uint32) bool {
	return a == b || sameBlockFamily(a, b)
}

// oppositeOf flips a facing name. A wall block's support is behind it.
func oppositeOf(facing string) string {
	switch facing {
	case "north":
		return "south"
	case "south":
		return "north"
	case "east":
		return "west"
	case "west":
		return "east"
	}
	return facing
}

// dropUnsupported knocks down anything around an edit that just lost what it
// was standing on, hanging from or fixed to, and cascades — a two-tall plant
// or a stack of torches goes all the way.
func (h *hub) dropUnsupported(players map[int32]*tracked, dim int, pos blockPos) {
	queue := []blockPos{pos}
	// Bounded by the work done, not only the queue's length: a queue that
	// pops one cell and pushes one back never grows, and two cells handing
	// each other back once spun the hub until the liveness probe killed the
	// pod (2026-09-23).
	for steps := 0; len(queue) > 0 && len(queue) < 512 && steps < 4096; steps++ {
		p := queue[0]
		queue = queue[1:]
		for _, d := range supportNeighbours {
			n := blockPos{p.x + d[0], p.y + d[1], p.z + d[2]}
			if !h.inWorldY(n.y) {
				continue
			}
			// world.At, not Block: naturally generated plants are not in the
			// edit overlay, and Block would miss them entirely.
			st := h.worldFor(dim).At(n.x, n.y, n.z)
			// A dirt path or a patch of farmland under a solid block turns
			// back into dirt rather than falling — canSurvive is about what
			// is ABOVE them for both, and they share vanilla's turnToDirt:
			// the block converts where it stands instead of dropping.
			above := h.worldFor(dim).At(n.x, n.y+1, n.z)
			if st == dirtPathState && coversSoil(above) {
				h.turnToBaseBlock(players, dim, n, worldgen.Dirt)
				queue = append(queue, n)
				continue
			}
			if isFarmland(st) && coversSoil(above) {
				h.turnFarmlandToDirt(players, dim, n.x, n.y, n.z)
				queue = append(queue, n)
				continue
			}
			// A block whose state follows this neighbour takes the new state
			// first (shapeupdate.go), then answers the survival question as
			// that state.
			if ns, ok := shapeUpdated(h.worldFor(dim), n, st, [3]int{-d[0], -d[1], -d[2]}); ok && ns != st {
				h.setBlockAt(players, dim, n, ns)
				queue = append(queue, n)
				st = h.worldFor(dim).At(n.x, n.y, n.z) // the write's own sweep may have moved it on
			}
			// Multiface blocks and scaffolding update their STATE on a neighbour
			// change (a lost face, a new distance) and only drop past the last
			// face / at distance 7 — vanilla's updateShape for both.
			if isMultiface(st) || isScaffolding(st) || isChorusPlant(st) {
				var ns uint32
				var ok bool
				switch {
				case isMultiface(st):
					ns, ok = multifaceUpdated(h.worldFor(dim), n, st)
				case isScaffolding(st):
					ns, ok = scaffoldUpdated(h.worldFor(dim), n, st)
				default: // a chorus stem re-wires its connections (ChorusPlantBlock.updateShape)
					ns, ok = h.chorusPlantAt(dim, n.x, n.y, n.z), chorusSurvives(h.worldFor(dim), n, st)
				}
				if ok {
					if ns != st {
						h.setBlockAt(players, dim, n, ns)
						queue = append(queue, n)
					}
					continue
				}
				h.setBlockAt(players, dim, n, worldgen.Air)
				h.dropLoose(players, dim, n, st)
				queue = append(queue, n)
				continue
			}
			if (worldgen.SupportFor(st) == worldgen.SupportNone && !isBedBlock(st) && !isFire(st)) || supported(h.worldFor(dim), n, st) {
				continue
			}
			// A stalactite does not break when its grip goes — it FALLS, whole,
			// and the tip is the end that hurts (PointedDripstoneBlock.tick →
			// spawnFallingStalactite).
			// Nothing changes here this tick — the fall is a scheduled update,
			// and its own write sweeps around it — so the cell is not queued:
			// a stalactite still hanging beside it would find it unsupported
			// again, and the two would re-queue each other for ever.
			if isStalactite(st) {
				h.dropStalactite(players, dim, n)
				continue
			}
			h.setBlockAt(players, dim, n, worldgen.Air)
			h.dropLoose(players, dim, n, st)
			queue = append(queue, n)
		}
	}
}

// supportNeighbours are the six cells whose block can depend on a change here.
var supportNeighbours = [6][3]int{
	{0, 1, 0}, {0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1},
}

// dropLoose spawns what a block that fell down on its own leaves behind: the
// no-tool loot, since nobody mined it.
func (h *hub) dropLoose(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	if !h.rules.DoTileDrops {
		return
	}
	drops := h.evalBlockLoot(lootCtx{state: state, rng: h.rng.Intn, randf: h.rng.Float64})
	if drops == nil {
		drops = h.rollDrops(state)
	}
	for _, d := range drops {
		h.spawnBlockDrop(players, dim, d.item, d.count, pos.x, pos.y, pos.z)
	}
}

// sandStates is the #sand tag (SugarCaneBlock / CactusBlock floors).
var sandStates = blockRange("sand", "red_sand", "suspicious_sand")

var frostedIceState = func() uint32 {
	lo, _, ok := worldgen.BlockRangeOK("frosted_ice")
	if !ok {
		return 0
	}
	return lo
}()

// gravelStates: the gravels bamboo may root in (#bamboo_plantable_on).
var gravelStates = blockRange("gravel", "suspicious_gravel")

// chorusSurvives is ChorusPlantBlock.canSurvive / ChorusFlowerBlock.canSurvive.
// A stem stands on end stone or another stem, or hangs off a horizontal
// stem that itself has one below — unless it is boxed in above and below. A
// flower stands on a stem or end stone, or on air with exactly one stem
// beside it and nothing else around.
func chorusSurvives(w *world.World, pos blockPos, state uint32) bool {
	at := func(dx, dy, dz int) uint32 { return w.At(pos.x+dx, pos.y+dy, pos.z+dz) }
	below := at(0, -1, 0)
	if isChorusFlower(state) {
		if isChorusPlant(below) || below == endStoneBlock {
			return true
		}
		if below != worldgen.Air {
			return false
		}
		one := false
		for _, d := range [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			n := at(d[0], 0, d[2])
			switch {
			case isChorusPlant(n):
				if one {
					return false
				}
				one = true
			case n != worldgen.Air:
				return false
			}
		}
		return one
	}
	boxed := at(0, 1, 0) != worldgen.Air && below != worldgen.Air
	for _, d := range [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		n := at(d[0], 0, d[2])
		if isChorusPlant(n) {
			if boxed {
				return false
			}
			if nb := at(d[0], -1, d[2]); isChorusPlant(nb) || nb == endStoneBlock {
				return true
			}
		}
	}
	return isChorusPlant(below) || below == endStoneBlock
}

// bedPartnerStands is BedBlock.updateShape's test: the other half — the head
// one block along the bed's facing from the foot — is still this bed's.
func bedPartnerStands(w *world.World, pos blockPos, state uint32, info worldgen.BlockInfo) bool {
	facing := worldgen.GetProperty(info, state, "facing")
	dx, dz := facingDelta(facing)
	part := worldgen.GetProperty(info, state, "part")
	if part == "head" {
		dx, dz = -dx, -dz
	}
	o := w.At(pos.x+dx, pos.y, pos.z+dz)
	oi, ok := worldgen.InfoForState(o)
	return ok && isBed(oi) && sameBlockFamily(o, state) &&
		worldgen.GetProperty(oi, o, "part") != part && worldgen.GetProperty(oi, o, "facing") == facing
}

var (
	// mushroomOverride is #overrides_mushroom_light_requirement.
	mushroomOverride = func() map[uint32]bool {
		m := map[uint32]bool{}
		for _, n := range []string{"mycelium", "podzol", "crimson_nylium", "warped_nylium"} {
			lo, hi, ok := worldgen.BlockRangeOK(n)
			for s := lo; ok && s <= hi; s++ {
				m[s] = true
			}
		}
		return m
	}()
)
