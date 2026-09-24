package worldgen

import (
	"sort"
	"sync"
)

// Horizontal connection support for multi-block "connecting" blocks — fences,
// glass panes, and iron bars, which carry boolean north/east/south/west state.
// Placement and neighbour updates use these to compute a block's connection
// state from its surroundings (see the server's interaction layer). Walls use a
// none/low/tall enum instead and are not handled here yet.

// InfoForState returns the state layout for the block that owns `state` — any
// state in its range, not just its default (a neighbouring fence already has its
// connections set). A binary search over the ranges, sorted once: it runs in
// hot loops (a villager's path search asks it of every cell for doors), and
// the old scan of all blockInfo entries per call was the largest single CPU
// cost in the hub.
func InfoForState(state uint32) (BlockInfo, bool) {
	rs := infoRanges()
	lo, hi := 0, len(rs)-1
	for lo <= hi {
		mid := (lo + hi) / 2
		switch r := &rs[mid]; {
		case state < r.min:
			hi = mid - 1
		case state >= r.end:
			lo = mid + 1
		default:
			return r.info, true
		}
	}
	return BlockInfo{}, false
}

type infoRange struct {
	min, end uint32 // [min, end)
	info     BlockInfo
}

var (
	infoOnce   sync.Once
	infoSorted []infoRange
)

// infoRanges is blockInfo as sorted state ranges, built on first use.
func infoRanges() []infoRange {
	infoOnce.Do(func() {
		for _, info := range blockInfo {
			size := uint32(1)
			for _, p := range info.Props {
				size *= uint32(len(p.Vals))
			}
			infoSorted = append(infoSorted, infoRange{info.Min, info.Min + size, info})
		}
		sort.Slice(infoSorted, func(i, j int) bool { return infoSorted[i].min < infoSorted[j].min })
	})
	return infoSorted
}

// IsHorizontalConnector reports whether a block connects via boolean
// north/east/south/west (fences, glass panes, iron bars).
func IsHorizontalConnector(info BlockInfo) bool {
	if !isBoolProp(info, "north") || !isBoolProp(info, "east") ||
		!isBoolProp(info, "south") || !isBoolProp(info, "west") {
		return false
	}
	// A boolean "up" alongside them means the four sides are FACES, not
	// connections: glow lichen, vines, sculk veins, resin clumps, chorus
	// plants, fire and the mushroom blocks all carry the same four names for
	// something else entirely. Connecting one to its neighbour the way a fence
	// connects grows a face with nothing behind it — reported in game as
	// lichen sprouting outcroppings when two are placed side by side. A real
	// connector (fence, glass pane, iron bars) has no "up".
	return !isBoolProp(info, "up")
}

// IsWallConnector reports whether a block is a wall: its sides are the
// three-valued none/low/tall enum (plus an "up" center-post boolean).
func IsWallConnector(info BlockInfo) bool {
	for _, p := range info.Props {
		if p.Name == "north" {
			return len(p.Vals) == 3 && p.Vals[0] == "none"
		}
	}
	return false
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
