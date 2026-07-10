package worldgen

// Horizontal connection support for multi-block "connecting" blocks — fences,
// glass panes, and iron bars, which carry boolean north/east/south/west state.
// Placement and neighbour updates use these to compute a block's connection
// state from its surroundings (see the server's interaction layer). Walls use a
// none/low/tall enum instead and are not handled here yet.

// InfoForState returns the state layout for the block that owns `state` — any
// state in its range, not just its default (a neighbouring fence already has its
// connections set). Scans orientInfo by range; the table is small and this only
// runs on placement / neighbour updates.
func InfoForState(state uint32) (BlockInfo, bool) {
	for _, info := range orientInfo {
		size := uint32(1)
		for _, p := range info.Props {
			size *= uint32(len(p.Vals))
		}
		if state >= info.Min && state < info.Min+size {
			return info, true
		}
	}
	return BlockInfo{}, false
}

// IsHorizontalConnector reports whether a block connects via boolean
// north/east/south/west (fences, glass panes, iron bars).
func IsHorizontalConnector(info BlockInfo) bool {
	return isBoolProp(info, "north") && isBoolProp(info, "east") &&
		isBoolProp(info, "south") && isBoolProp(info, "west")
}

func isBoolProp(info BlockInfo, name string) bool {
	for _, p := range info.Props {
		if p.Name == name {
			return len(p.Vals) == 2 && p.Vals[0] == "true" && p.Vals[1] == "false"
		}
	}
	return false
}

// IsSolidFull reports whether a block is a full opaque cube a fence can attach to.
// Heuristic via opacity: misses transparent full cubes (glass) and over-counts
// slabs/stairs, but fence-to-fence closure goes through the connector path, so
// the cosmetic edge cases don't affect a pen forming a closed loop.
func IsSolidFull(state uint32) bool {
	return state != Air && SkyOpacity(state) == Opaque
}
