package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Enderman block-carry — a port of vanilla's EnderMan Take/LeaveBlockGoal + the
// DATA_CARRY_STATE metadata. An enderman occasionally lifts a holdable block out
// of the world (leaving air) and, much more rarely, sets it back down somewhere
// nearby. The carried block rides the entity as OPTIONAL_BLOCK_STATE metadata so
// the client renders it above the enderman's hands.

const (
	// EnderMan.DATA_CARRY_STATE metadata index (1.21.5: Entity 0-7, LivingEntity
	// 8-14, Mob 15, then the enderman's carry state) and its serializer type id
	// (OPTIONAL_BLOCK_STATE = 15 in canonical-770 numbering; stable to 26.2).
	endermanCarryIndex = 16
	metaTypeOptState   = 15

	// Vanilla rolls these each game tick (TakeBlockGoal nextInt(20),
	// LeaveBlockGoal nextInt(2000)). endermanCarry runs on the mob-update cadence
	// (every mobMoveInterval ticks), so the odds are divided by that interval to
	// keep the same real rate (≈1 pickup/sec, ≈1 placement/100 s).
	endermanPickupOdds = 20 / mobMoveInterval
	endermanPlaceOdds  = 2000 / mobMoveInterval
)

// holdableSpan is a holdable block's state range plus the default state an
// enderman carries away (vanilla lifts block.defaultBlockState(), not the exact
// state found).
type holdableSpan struct{ lo, hi, def uint32 }

// endermanHoldable is the #minecraft:enderman_holdable block tag expanded to
// state ranges. An enderman only picks up blocks whose state falls in one of
// these; it then holds the block's default state. Read from the generated
// tag, so a flower a new version adds to #small_flowers (the golden
// dandelion, in 26.3) is holdable without anyone remembering to list it.
var endermanHoldable = func() []holdableSpan {
	names := worldgen.BlockTagNames("enderman_holdable")
	spans := make([]holdableSpan, 0, len(names))
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			spans = append(spans, holdableSpan{lo, hi, worldgen.BlockID(n)})
		}
	}
	return spans
}()

// endermanVegetation is the holdable blocks that are VegetationBlocks —
// the small flowers, the mushrooms, the fungi and roots and the cactus
// flower. Their updateShape turns them to air where they cannot survive,
// and LeaveBlockGoal runs updateShape on the carried block before it asks
// canSurvive (see endermanPlaceBlock).
var endermanVegetation = func() [][2]uint32 {
	out := worldgen.BlockTag("small_flowers")
	for _, n := range []string{"brown_mushroom", "red_mushroom", "crimson_fungus", "warped_fungus",
		"crimson_roots", "warped_roots", "cactus_flower"} {
		lo, hi := worldgen.BlockRange(n)
		out = append(out, [2]uint32{lo, hi})
	}
	return out
}()

// endermanHoldableDefault returns the default state to carry if `state` belongs
// to a holdable block, else 0 (endermen never carry air, whose id is 0, so 0 is
// an unambiguous "no").
func endermanHoldableDefault(state uint32) uint32 {
	for _, s := range endermanHoldable {
		if state >= s.lo && state <= s.hi {
			return s.def
		}
	}
	return 0
}

// enderCarryMeta builds the DATA_CARRY_STATE metadata (OPTIONAL_BLOCK_STATE: a
// single VarInt, 0 = empty). The state id is canonical-770; the gateway remaps
// it per client version.
func enderCarryMeta(eid int32, state uint32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, endermanCarryIndex)
	b = protocol.AppendVarInt(b, metaTypeOptState)
	b = protocol.AppendVarInt(b, int32(state))
	return protocol.AppendU8(b, itemMetaEnd)
}

// endermanCarry advances one enderman's pick-up/put-down behaviour. Like
// vanilla, it needs the mobGriefing gamerule, and it works in any dimension:
// a Nether enderman lifts nylium and fungi (#enderman_holdable).
func (h *hub) endermanCarry(players map[int32]*tracked, m *mob) {
	if m.dying != 0 || !h.rules.MobGriefing {
		return
	}
	if m.carriedBlock == 0 {
		h.endermanTakeBlock(players, m)
	} else {
		h.endermanPlaceBlock(players, m)
	}
}

// endermanTakeBlock is EnderMan.EndermanTakeBlockGoal: with a small chance, lift
// a holdable block from a random cell around the enderman (x±2, y..y+3, z±2),
// leaving air. In the enderman's OWN dimension — this read and wrote the
// overworld whatever dimension the mob was in, so every enderman in the End
// was quietly taking blocks out of the overworld at the matching coordinates.
func (h *hub) endermanTakeBlock(players map[int32]*tracked, m *mob) {
	if h.rng.Intn(endermanPickupOdds) != 0 {
		return
	}
	x := int(math.Floor(m.x - 2 + h.rng.Float64()*4))
	y := int(math.Floor(m.y + h.rng.Float64()*3))
	z := int(math.Floor(m.z - 2 + h.rng.Float64()*4))
	if !h.inWorldY(y) {
		return
	}
	def, ok := h.endermanTakeable(m, x, y, z)
	if !ok {
		return
	}
	pos := blockPos{x, y, z}
	h.setBlockAt(players, m.dim, pos, worldgen.Air)
	h.scheduleAroundIn(m.dim, pos, 1) // let neighbours (fluids/falling blocks) react
	h.vib(m.dim, freqBlockDestroy, x, y, z, m.eid)
	m.carriedBlock = def
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(enderCarryMeta(m.eid, def)))
}

// endermanTakeable is the pair of tests EndermanTakeBlockGoal.tick makes on
// the cell it rolled: the block is in #enderman_holdable, AND a ray from the
// enderman reaches it. The engine had only the first, so an enderman could
// lift a block it could not see — one buried in a hillside beside it, or on
// the far side of a wall. That matters more than it sounds, because an
// enderman holding a block is exempt from BOTH despawn and the monster-cap
// census (Mob.requiresCustomPersistence), so every extra pick-up is one more
// permanent enderman and one more free slot for the spawner.
//
// The ray is vanilla's own odd one: from the enderman's BLOCK centre taken at
// the TARGET's height, to the target's centre, clipped against block
// OUTLINES — a flower, a tuft of grass or a torch in the way stops it — and
// the block must be the first thing it meets (or nothing at all, when the
// ray starts in the target's own cell).
func (h *hub) endermanTakeable(m *mob, x, y, z int) (uint32, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, false
	}
	def := endermanHoldableDefault(w.At(x, y, z))
	if def == 0 {
		return 0, false
	}
	hit, _ := h.clipOutline(m.dim,
		math.Floor(m.x)+0.5, float64(y)+0.5, math.Floor(m.z)+0.5,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5)
	if hit != (blockPos{x, y, z}) {
		return 0, false
	}
	return def, true
}

// endermanPlaceBlock is EnderMan.EndermanLeaveBlockGoal: much more rarely, set
// the carried block down on a solid full block in a random cell (x±1, y..y+2,
// z±1) whose target cell is empty — and only where the block can stay
// (carried.canSurvive: a flower needs soil, a cactus clear sides) with no
// entity in the cell but the enderman itself. Without those two checks a
// carried poppy was set down on bare stone; vanilla loses it there instead.
func (h *hub) endermanPlaceBlock(players map[int32]*tracked, m *mob) {
	if h.rng.Intn(endermanPlaceOdds) != 0 {
		return
	}
	x := int(math.Floor(m.x - 1 + h.rng.Float64()*2))
	y := int(math.Floor(m.y + h.rng.Float64()*2))
	z := int(math.Floor(m.z - 1 + h.rng.Float64()*2))
	if !h.inWorldY(y) {
		return
	}
	w := h.worldFor(m.dim)
	pos := blockPos{x, y, z}
	// Block.updateFromNeighbourShapes first: grass set under snow goes down
	// snowy. A flower, a mushroom or a fungus that cannot survive here comes
	// out of it as AIR, and air passes every test below — so the enderman
	// "places" nothing and its hands are empty. That is vanilla's, and it is
	// where the flowers endermen carry over bare stone go.
	carried := m.carriedBlock
	if inRanges2(carried, endermanVegetation) && !supported(w, pos, carried) {
		carried = worldgen.Air
	} else {
		carried = updateFromNeighbourShapes(w, pos, carried)
	}
	if !isAirState(w.At(x, y, z)) { // vanilla canPlaceBlock: target empty
		return
	}
	below := w.At(x, y-1, z) // …on a solid full block that isn't bedrock
	if below == worldgen.Bedrock || !worldgen.IsSolidFull(below) {
		return
	}
	if carried != worldgen.Air && !supported(w, pos, carried) {
		return
	}
	if h.entityInCell(players, m.dim, pos, m.eid) {
		return
	}
	if carried != worldgen.Air {
		h.setBlockAt(players, m.dim, pos, carried)
		h.scheduleAroundIn(m.dim, pos, 1)
	}
	h.vib(m.dim, freqBlockPlace, x, y, z, m.eid)
	m.carriedBlock = 0
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(enderCarryMeta(m.eid, 0)))
}

// entityInCell is level.getEntities(except, AABB.unitCubeFromLowerCorner(pos))
// being non-empty: any player, mob, dropped item or end crystal whose box
// overlaps the cell, other than the entity asking.
func (h *hub) entityInCell(players map[int32]*tracked, dim int, pos blockPos, except int32) bool {
	bx, by, bz := float64(pos.x), float64(pos.y), float64(pos.z)
	hits := func(d int, x, y, z, hw, ht float64) bool {
		return d == dim && x+hw > bx && x-hw < bx+1 && z+hw > bz && z-hw < bz+1 && y+ht > by && y < by+1
	}
	for _, o := range players {
		if !o.dead && o.p.eid != except && hits(o.dim, o.x, o.y, o.z, 0.3, 1.8) {
			return true
		}
	}
	for _, o := range h.mobs {
		if b := o.box(); o.eid != except && o.dying == 0 && hits(o.dim, o.x, o.y, o.z, b.w/2, b.h) {
			return true
		}
	}
	for _, it := range h.items {
		if hits(it.dim, it.x, it.y, it.z, 0.125, 0.25) {
			return true
		}
	}
	for _, c := range h.crystals {
		if hits(c.dim, c.x, c.y, c.z, 1, 2) {
			return true
		}
	}
	return false
}
