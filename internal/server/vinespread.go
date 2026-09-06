package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Vine spreading — VineBlock.randomTick. One tick in four a vine picks a
// random direction: sideways it either grows a face onto a wall it touches,
// or (over air) starts a new vine round the corner on a wall the existing
// vine's neighbouring face sees, or — rarely — hangs one from the block
// above the gap; upward it climbs the block above or copies some of its
// faces to a new vine on top; downward it copies some faces to the vine (or
// air) below. A vine hemmed in by five or more vines within four blocks
// stops spreading (canSpread). The spreadVines gamerule is not modelled;
// vines spread as they do by default.

var horizontalFaces = []struct {
	prop string
	d    [3]int
}{
	{"north", [3]int{0, 0, -1}}, {"south", [3]int{0, 0, 1}},
	{"west", [3]int{-1, 0, 0}}, {"east", [3]int{1, 0, 0}},
}

// tickVine returns whether the block was a vine (handled).
func (h *hub) tickVine(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if !isVineBlock(state) {
		return false
	}
	if h.rng.Intn(4) != 0 {
		return true
	}
	w := h.worldFor(dim)
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return true
	}
	face := func(s uint32, prop string) bool { return worldgen.GetProperty(info, s, prop) == "true" }
	withFace := func(s uint32, prop string, on bool) uint32 {
		return worldgen.SetProperty(info, s, prop, map[bool]string{true: "true", false: "false"}[on])
	}
	base := worldgen.BlockBase("vine")
	attachable := func(bx, by, bz int) bool { return holdsBlock(w.At(bx, by, bz)) }
	set := func(bx, by, bz int, s uint32) { h.setBlockAt(players, dim, blockPos{bx, by, bz}, s) }

	dir := h.rng.Intn(6) // Direction.getRandom: 0 down, 1 up, 2 north, 3 south, 4 west, 5 east
	if dir >= 2 {
		f := horizontalFaces[dir-2]
		if face(state, f.prop) {
			return true
		}
		if !h.vineCanSpread(w, x, y, z) {
			return true
		}
		tx, tz := x+f.d[0], z+f.d[2]
		edge := w.At(tx, y, tz)
		if edge == worldgen.Air {
			cw, ccw := vineClockwise(f.prop), vineCounterClockwise(f.prop)
			cwd, ccwd := vineDelta(cw), vineDelta(ccw)
			cwHas, ccwHas := face(state, cw), face(state, ccw)
			switch {
			case cwHas && attachable(tx+cwd[0], y, tz+cwd[2]):
				set(tx, y, tz, withFace(base, cw, true))
			case ccwHas && attachable(tx+ccwd[0], y, tz+ccwd[2]):
				set(tx, y, tz, withFace(base, ccw, true))
			default:
				opp := vineOpposite(f.prop)
				switch {
				case cwHas && w.At(tx+cwd[0], y, tz+cwd[2]) == worldgen.Air && attachable(x+cwd[0], y, z+cwd[2]):
					set(tx+cwd[0], y, tz+cwd[2], withFace(base, opp, true))
				case ccwHas && w.At(tx+ccwd[0], y, tz+ccwd[2]) == worldgen.Air && attachable(x+ccwd[0], y, z+ccwd[2]):
					set(tx+ccwd[0], y, tz+ccwd[2], withFace(base, opp, true))
				case h.rng.Float32() < 0.05 && attachable(tx, y+1, tz):
					set(tx, y, tz, withFace(base, "up", true))
				}
			}
		} else if holdsBlock(edge) {
			set(x, y, z, withFace(state, f.prop, true))
		}
		return true
	}
	if dir == 1 { // up
		if attachable(x, y+1, z) {
			if !face(state, "up") {
				set(x, y, z, withFace(state, "up", true))
			}
			return true
		}
		if w.At(x, y+1, z) == worldgen.Air {
			if !h.vineCanSpread(w, x, y, z) {
				return true
			}
			above := state
			for _, f := range horizontalFaces {
				if h.rng.Intn(2) == 0 || !attachable(x+f.d[0], y+1, z+f.d[2]) {
					above = withFace(above, f.prop, false)
				}
			}
			above = withFace(above, "up", false)
			if vineHasHorizontal(info, above) {
				set(x, y+1, z, above)
			}
			return true
		}
	}
	// Down (or a blocked climb): copy some faces onto the vine or air below.
	below := w.At(x, y-1, z)
	if below == worldgen.Air || isVineBlock(below) {
		before := below
		if below == worldgen.Air {
			before = base
		}
		after := before
		for _, f := range horizontalFaces {
			if h.rng.Intn(2) == 0 && face(state, f.prop) {
				after = withFace(after, f.prop, true)
			}
		}
		if after != before && vineHasHorizontal(info, after) {
			set(x, y-1, z, after)
		}
	}
	return true
}

func vineHasHorizontal(info worldgen.BlockInfo, s uint32) bool {
	for _, f := range horizontalFaces {
		if worldgen.GetProperty(info, s, f.prop) == "true" {
			return true
		}
	}
	return false
}

// vineCanSpread is VineBlock.canSpread: fewer than five vines in the
// 9×3×9 box around the vine (itself included).
func (h *hub) vineCanSpread(w interface {
	At(x, y, z int) uint32
}, x, y, z int) bool {
	n := 0
	for dx := -4; dx <= 4; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -4; dz <= 4; dz++ {
				if isVineBlock(w.At(x+dx, y+dy, z+dz)) {
					if n++; n >= 5 {
						return false
					}
				}
			}
		}
	}
	return true
}

func vineDelta(prop string) [3]int {
	for _, f := range horizontalFaces {
		if f.prop == prop {
			return f.d
		}
	}
	return [3]int{}
}

func vineClockwise(prop string) string {
	switch prop {
	case "north":
		return "east"
	case "east":
		return "south"
	case "south":
		return "west"
	}
	return "north"
}

func vineCounterClockwise(prop string) string {
	switch prop {
	case "north":
		return "west"
	case "west":
		return "south"
	case "south":
		return "east"
	}
	return "north"
}

func vineOpposite(prop string) string {
	switch prop {
	case "north":
		return "south"
	case "south":
		return "north"
	case "west":
		return "east"
	}
	return "west"
}
