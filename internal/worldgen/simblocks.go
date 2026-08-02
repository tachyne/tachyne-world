package worldgen

// Block classification for world simulation (falling blocks + fluids). Keeping
// this in worldgen — the package that owns block-state IDs — lets the server's
// tick loop ask domain questions ("is this a fluid?") without hard-coding IDs.

var (
	RedSand = blockBase("red_sand")

	// Fluids are a contiguous run of 16 level states: base+0 is the source,
	// base+1..7 are flowing (1 = strongest), base+8 is falling.
	WaterBase = blockBase("water")
	LavaBase  = blockBase("lava")

	concretePowderLo = blockBase("white_concrete_powder") // white..black concrete powder (16 states)
	concretePowderHi = blockBase("black_concrete_powder")
	concreteLo       = blockBase("white_concrete") // aligned 1:1 with the powders
)

// IsFalling reports whether a block is affected by gravity (sand, gravel, …).
func IsFalling(state uint32) bool {
	switch state {
	case Sand, RedSand, Gravel:
		return true
	}
	return state >= concretePowderLo && state <= concretePowderHi
}

// IsLeaves reports whether a state is one of the generated leaf families (oak,
// spruce, birch — contiguous state ranges). Leaves collide, but dropped items
// must fall THROUGH a canopy or they collect on top of trees out of reach.
// IsLog reports whether a state is any overworld log — the trunk family the
// tree placers write.
func IsLog(state uint32) bool {
	for _, n := range logFamilies {
		lo, hi := BlockRange(n)
		if state >= lo && state <= hi {
			return true
		}
	}
	return false
}

var logFamilies = []string{
	"oak_log", "spruce_log", "birch_log", "jungle_log", "acacia_log",
	"dark_oak_log", "pale_oak_log", "mangrove_log", "cherry_log",
}

// IsDirtTag is vanilla's #dirt block tag — the ground the podzol decorator
// may replace (Feature.isGrassOrDirt tests exactly this tag).
func IsDirtTag(state uint32) bool {
	for _, n := range dirtTag {
		lo, hi := BlockRange(n)
		if state >= lo && state <= hi {
			return true
		}
	}
	return false
}

var dirtTag = []string{
	"dirt", "grass_block", "podzol", "coarse_dirt", "mycelium", "rooted_dirt",
	"moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots",
}

// podzolState is snowy=false — what alter_ground writes.
var podzolState = blockBase("podzol") + 1

// IsLeaves spans EVERY leaf family — the registry lays them out contiguously
// from oak to flowering azalea. This used to stop at birch, which silently
// excluded seven species: their canopies refused the leaf-distance rewrite at
// generation (the live healer papered over it) and dropped items would not
// fall through them.
func IsLeaves(state uint32) bool {
	_, hi := BlockRange("flowering_azalea_leaves")
	return state >= blockBase("oak_leaves") && state <= hi
}

// IsWater / IsLava report fluid membership; FluidLevel extracts 0..15.
func IsWater(state uint32) bool { return state >= WaterBase && state <= WaterBase+15 }
func IsLava(state uint32) bool  { return state >= LavaBase && state <= LavaBase+15 }
func IsFluid(state uint32) bool { return IsWater(state) || IsLava(state) }

// IsReplaceable reports whether a fluid or falling block may overwrite this block
// (air and small plants — never solid terrain).
func IsReplaceable(state uint32) bool {
	switch state {
	case Air, ShortGrass, Fern, Dandelion, Poppy:
		return true
	}
	return state == blockBase("tall_grass") || state == blockBase("tall_grass")+1 // tall_grass halves
}

// NeedsGroundSupport reports whether a block must rest on the block below it and
// breaks when that support is removed — the small plants (grass, ferns, flowers)
// that pop off when you mine the dirt underneath them.
func NeedsGroundSupport(state uint32) bool {
	switch state {
	case ShortGrass, Fern, Dandelion, Poppy:
		return true
	}
	return state == blockBase("tall_grass") || state == blockBase("tall_grass")+1 // tall_grass halves
}

// IsDoor reports any door block — the only blocks with a "hinge" property.
// Mobs never treat a door as a floor (no perching on door tops; on slopes
// that turned a closed door into a climbable stair).
func IsDoor(state uint32) bool {
	info, ok := InfoForState(state)
	return ok && info.HasProperty("hinge")
}

// IsClosedDoor reports a door with open=false — a wall to walking mobs
// (vanilla mobs never path through closed doors; open ones are passable).
func IsClosedDoor(state uint32) bool {
	if !IsDoor(state) {
		return false
	}
	info, _ := InfoForState(state)
	return GetProperty(info, state, "open") == "false"
}

// IsWoodenDoor reports a door a mob may operate — every door EXCEPT the metal
// ones (iron 5828-5891, copper 24680-24743), which vanilla villagers/zombies
// cannot open. Used to gate villager door-opening to their wooden house doors.
func IsWoodenDoor(state uint32) bool {
	if !IsDoor(state) {
		return false
	}
	if state >= blockBase("iron_door") && state <= blockBase("iron_door")+63 { // iron_door
		return false
	}
	if state >= blockBase("copper_door") && state <= blockBase("copper_door")+63 { // copper_door (all weather stages)
		return false
	}
	return true
}

// IsConcretePowder reports whether a state is one of the 16 concrete powders.
func IsConcretePowder(state uint32) bool {
	return state >= concretePowderLo && state <= concretePowderHi
}

// ConcreteFor maps a concrete-powder state to its solidified concrete (same
// color index; the two blocks are contiguous 16-block runs).
func ConcreteFor(powder uint32) uint32 {
	return concreteLo + (powder - concretePowderLo)
}

// IsWaterlogged reports whether a block state carries waterlogged=true — its
// cell holds a water source that stays behind when the block is broken.
func IsWaterlogged(state uint32) bool {
	info, ok := InfoForState(state)
	if !ok || !info.HasProperty("waterlogged") {
		return false
	}
	return GetProperty(info, state, "waterlogged") == "true"
}
