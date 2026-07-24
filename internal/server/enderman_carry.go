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
// these; it then holds the block's default state.
var endermanHoldable = func() []holdableSpan {
	names := []string{
		// #small_flowers
		"dandelion", "open_eyeblossom", "poppy", "blue_orchid", "allium",
		"azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip",
		"oxeye_daisy", "cornflower", "lily_of_the_valley", "wither_rose",
		"torchflower", "closed_eyeblossom",
		// #dirt
		"dirt", "grass_block", "podzol", "coarse_dirt", "mycelium", "rooted_dirt",
		"moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots",
		// the rest of the tag
		"sand", "red_sand", "gravel", "brown_mushroom", "red_mushroom", "tnt",
		"cactus", "clay", "pumpkin", "carved_pumpkin", "melon", "crimson_fungus",
		"crimson_nylium", "crimson_roots", "warped_fungus", "warped_nylium",
		"warped_roots", "cactus_flower",
	}
	spans := make([]holdableSpan, 0, len(names))
	for _, n := range names {
		lo, hi := worldgen.BlockRange(n)
		spans = append(spans, holdableSpan{lo, hi, worldgen.BlockID(n)})
	}
	return spans
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
// vanilla, it needs the mobGriefing gamerule and only touches the overworld
// (the hub simulates block edits there).
func (h *hub) endermanCarry(players map[int32]*tracked, m *mob) {
	if m.dim != 0 || m.dying != 0 || !h.rules.MobGriefing {
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
// leaving air.
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
	def := endermanHoldableDefault(h.world.At(x, y, z))
	if def == 0 {
		return
	}
	pos := blockPos{x, y, z}
	h.setBlock(players, pos, worldgen.Air)
	h.scheduleAround(pos, 1) // let neighbours (fluids/falling blocks) react
	m.carriedBlock = def
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(enderCarryMeta(m.eid, def)))
}

// endermanPlaceBlock is EnderMan.EndermanLeaveBlockGoal: much more rarely, set
// the carried block down on a solid full block in a random cell (x±1, y..y+2,
// z±1) whose target cell is empty.
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
	if h.world.At(x, y, z) != worldgen.Air { // vanilla canPlaceBlock: target empty
		return
	}
	below := h.world.At(x, y-1, z) // …on a solid full block that isn't bedrock
	if below == worldgen.Bedrock || !worldgen.IsSolidFull(below) {
		return
	}
	pos := blockPos{x, y, z}
	h.setBlock(players, pos, m.carriedBlock)
	h.scheduleAround(pos, 1)
	m.carriedBlock = 0
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(enderCarryMeta(m.eid, 0)))
}
