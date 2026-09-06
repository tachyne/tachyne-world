package worldgen

// IsSturdyTop approximates BlockState.isFaceSturdy(level, pos, Direction.UP)
// for the block shapes the engine models: a full cube, the top or double
// half of a slab, an upside-down stair, or a closed trapdoor in its top
// half all present a full, solid top face; a bottom slab, an upright stair
// or an open trapdoor do not. Redstone dust (RedStoneWireBlock.canSurviveOn)
// and other floor-mounted blocks that need a sturdy face rely on it.
func IsSturdyTop(state uint32) bool {
	if IsFullCube(state) {
		return true
	}
	info, ok := InfoForState(state)
	if !ok {
		return false
	}
	if info.HasProperty("type") && info.HasProperty("waterlogged") && !info.HasProperty("facing") { // a slab
		t := GetProperty(info, state, "type")
		return t == "top" || t == "double"
	}
	if info.HasProperty("shape") && info.HasProperty("half") { // stairs
		return GetProperty(info, state, "half") == "top"
	}
	if info.HasProperty("open") && info.HasProperty("half") { // a trapdoor
		return GetProperty(info, state, "half") == "top" && GetProperty(info, state, "open") == "false"
	}
	return false
}
