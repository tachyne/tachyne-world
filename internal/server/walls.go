package server

// walls.go — vanilla WallBlock connection logic. Walls carry a three-valued
// none/low/tall enum per side plus an "up" center-post boolean, so they need
// their own state math beside the boolean fence/pane connectors:
//   - a side connects to walls, iron bars / glass panes, sturdy full faces,
//     and fence gates whose facing runs across the connection axis
//     (WallBlock.connectsTo);
//   - a connected side is TALL when covered from above (a solid block, or a
//     wall above with that side connected), else LOW (makeWallState);
//   - the post shows when the wall above has a post, when the connections
//     form an end/corner/T (asymmetric N/S or E/W, or none at all), and on
//     covered straight runs; a straight run of matching TALL sides drops it
//     (shouldRaisePost).
// Simplifications vs vanilla's voxel-shape cover tests: "covered" for sides
// means a full solid cube or a matching wall side above; for the post, any
// non-air non-water block above (vanilla intersects the exact face shape,
// and WALL_POST_OVERRIDE covers torches/signs — our proxy treats every
// occupant above as covering).

import (
	"github.com/tachyne/tachyne-common/protocol"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// paneBases holds the Min state of every IronBarsBlock-family block (iron
// bars + the 17 glass panes): the family walls connect to and the family
// that attaches to walls.
var paneBases = func() map[uint32]bool {
	m := map[uint32]bool{}
	names := []string{"iron_bars", "glass_pane"}
	for _, c := range dyeColors {
		names = append(names, c+"_stained_glass_pane")
	}
	for _, n := range names {
		id, ok := itemByName[n]
		if !ok {
			continue
		}
		st, ok := protocol.BlockForItem(id)
		if !ok {
			continue
		}
		if info, ok := worldgen.InfoForState(st); ok {
			m[info.Min] = true
		}
	}
	return m
}()

func isPaneOrBars(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && paneBases[info.Min]
}

// wallInfo classifies a state as a wall and returns its layout.
func wallInfo(state uint32) (worldgen.BlockInfo, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok || !worldgen.IsWallConnector(info) {
		return worldgen.BlockInfo{}, false
	}
	return info, true
}

// gateInfo classifies a state as a fence gate (the only block family with an
// in_wall property).
func gateInfo(state uint32) (worldgen.BlockInfo, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("in_wall") || !info.HasProperty("facing") {
		return worldgen.BlockInfo{}, false
	}
	return info, true
}

// wallConnectsTo mirrors WallBlock.connectsTo for the neighbour on a side
// whose connection axis is X (east/west) or Z (north/south).
func wallConnectsTo(nb uint32, sideAxisX bool) bool {
	if _, ok := wallInfo(nb); ok {
		return true
	}
	if isPaneOrBars(nb) {
		return true
	}
	if info, ok := gateInfo(nb); ok {
		// a gate connects when its facing runs across the wall line
		return facingAxisX(worldgen.GetProperty(info, nb, "facing")) != sideAxisX
	}
	return worldgen.IsSolidFull(nb)
}

// wallState recomputes a wall's four sides and post from its neighbours —
// vanilla WallBlock.updateShape/updateSides/shouldRaisePost.
func wallState(w *world.World, x, y, z int, info worldgen.BlockInfo, state uint32) uint32 {
	above := w.Block(x, y+1, z)
	aboveInfo, aboveWall := wallInfo(above)
	aboveSolid := worldgen.IsSolidFull(above)
	side := map[string]string{}
	for _, d := range hConnectDirs {
		v := "none"
		if wallConnectsTo(w.Block(x+d.dx, y, z+d.dz), d.dx != 0) {
			v = "low"
			if aboveSolid || (aboveWall && worldgen.GetProperty(aboveInfo, above, d.name) != "none") {
				v = "tall"
			}
		}
		side[d.name] = v
		state = worldgen.SetProperty(info, state, d.name, v)
	}
	nNone, sNone := side["north"] == "none", side["south"] == "none"
	eNone, wNone := side["east"] == "none", side["west"] == "none"
	up := "false"
	switch {
	case aboveWall && worldgen.GetProperty(aboveInfo, above, "up") == "true":
		up = "true" // continue the post of the wall above
	case (nNone && sNone && eNone && wNone) || nNone != sNone || eNone != wNone:
		up = "true" // an end, corner or T junction
	case (side["north"] == "tall" && side["south"] == "tall") ||
		(side["east"] == "tall" && side["west"] == "tall"):
		up = "false" // a straight tall run stays flush
	case above != worldgen.Air && !worldgen.IsWater(above):
		up = "true" // something rests on the wall (torch, slab, …)
	}
	return worldgen.SetProperty(info, state, "up", up)
}

// refreshWallColumn recomputes the wall at (x,y,z) and cascades DOWNWARD
// while states keep changing — a wall's tall sides and post depend on the
// wall above it, so a change at the top of a stack ripples down.
func (s *Server) refreshWallColumn(w *world.World, dim, x, y, z int) {
	for {
		cur := w.Block(x, y, z)
		info, ok := wallInfo(cur)
		if !ok {
			return
		}
		ns := wallState(w, x, y, z, info, cur)
		if ns == cur {
			return
		}
		w.SetBlock(x, y, z, ns)
		s.hub.post(evBlock{x: x, y: y, z: z, dim: dim, state: ns, by: 0})
		y--
	}
}
