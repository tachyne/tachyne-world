package server

// walls.go — vanilla WallBlock connection logic. Walls carry a three-valued
// none/low/tall enum per side plus an "up" center-post boolean, so they need
// their own state math beside the boolean fence/pane connectors:
//   - a side connects to walls, iron bars / glass panes, sturdy faces,
//     and fence gates whose facing runs across the connection axis
//     (WallBlock.connectsTo);
//   - a connected side is TALL when covered from above (a solid block, or a
//     wall above with that side connected), else LOW (makeWallState);
//   - the post shows when the wall above has a post, when the connections
//     form an end/corner/T (asymmetric N/S or E/W, or none at all), and on
//     covered straight runs; a straight run of matching TALL sides drops it
//     (shouldRaisePost).
// "Covered" is vanilla's isCovered: the bottom face of the collision shape
// above covers a test square — the 2×2 centre for the post, a 2-wide strip
// from the centre out to the side for a side — and #wall_post_override
// (torches, signs, banners, pressure plates) raises the post too.

import (
	"strings"

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
	for _, n := range worldgen.AllBlockNames() { // IronBarsBlock: the copper bars, every stage and wax
		if strings.HasSuffix(n, "copper_bars") {
			names = append(names, n)
		}
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

// wallConnectsTo mirrors WallBlock.connectsTo for the neighbour nb, whose
// face toward the wall is back: its sturdiness is asked of that face alone
// (isFaceSturdy), so the hinge side of an open trapdoor or the back of a
// stair holds a wall, and glass does, though none of them is an opaque cube.
func wallConnectsTo(nb uint32, back int) bool {
	sideAxisX := back == worldgen.FaceWest || back == worldgen.FaceEast
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
	return !connectException(nb) && worldgen.IsFaceSturdy(nb, back)
}

// wallState is a wall placed fresh (WallBlock.getStateForPlacement): all
// four sides read from the neighbours, then the heights and the post.
func wallState(w *world.World, x, y, z int, info worldgen.BlockInfo, state uint32) uint32 {
	conn := map[string]bool{}
	for _, d := range hConnectDirs {
		conn[d.name] = wallConnectsTo(w.Block(x+d.dx, y, z+d.dz), d.back)
	}
	return wallShape(w, x, y, z, info, state, conn)
}

// wallUpdated is WallBlock.updateShape for a wall told that the cell one
// step along d changed. The cell below is nothing to a wall; the cell above
// re-reads the heights and the post over the sides it already has
// (topUpdate); a side re-reads THAT side's connection alone and keeps the
// other three as they are (sideUpdate). A side is never re-read because
// some other neighbour changed: a wall set beside a block it did not join
// stays unjoined until that block itself changes.
func wallUpdated(w *world.World, n blockPos, info worldgen.BlockInfo, state uint32, d [3]int) uint32 {
	if d[1] < 0 {
		return state
	}
	conn := map[string]bool{}
	for _, hd := range hConnectDirs {
		conn[hd.name] = worldgen.GetProperty(info, state, hd.name) != "none" // isConnected
		if d[1] == 0 && d[0] == hd.dx && d[2] == hd.dz {
			conn[hd.name] = wallConnectsTo(w.Block(n.x+hd.dx, n.y, n.z+hd.dz), hd.back)
		}
	}
	return wallShape(w, n.x, n.y, n.z, info, state, conn)
}

// Wall cover tests on the face of the block above (x, z in sixteenths):
// TEST_SHAPE_POST, a 2×2 column at the centre, and TEST_SHAPES_WALL, a
// 2-wide strip from the centre (9/16 through it) out to each side's edge.
var (
	wallPostTest  = [4]float64{7.0 / 16, 7.0 / 16, 9.0 / 16, 9.0 / 16}
	wallSideTests = map[string][4]float64{
		"north": {7.0 / 16, 0, 9.0 / 16, 9.0 / 16},
		"south": {7.0 / 16, 7.0 / 16, 9.0 / 16, 1},
		"west":  {0, 7.0 / 16, 9.0 / 16, 9.0 / 16},
		"east":  {7.0 / 16, 7.0 / 16, 1, 9.0 / 16},
	}
	wallPostOverride = worldgen.BlockTag("wall_post_override")
)

// wallShape is WallBlock.updateShape(…, north, east, south, west): each
// joined side is TALL under cover and LOW otherwise (updateSides), then the
// post (shouldRaisePost).
func wallShape(w *world.World, x, y, z int, info worldgen.BlockInfo, state uint32, conn map[string]bool) uint32 {
	above := w.Block(x, y+1, z)
	aboveInfo, aboveWall := wallInfo(above)
	side := map[string]string{}
	for _, d := range hConnectDirs {
		v := "none"
		if conn[d.name] {
			v = "low"
			if worldgen.FaceCovers(above, worldgen.FaceDown, wallSideTests[d.name]) {
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
	case inRanges2(above, wallPostOverride) || worldgen.FaceCovers(above, worldgen.FaceDown, wallPostTest):
		up = "true" // something rests on the wall's centre (a slab, a torch, a sign, …)
	}
	return worldgen.SetProperty(info, state, "up", up)
}

// refreshWallColumn updates the wall at (x,y,z), told the cell one step
// along d changed, and cascades DOWNWARD while states keep changing — a
// wall's tall sides and post depend on the wall above it, so a change at the
// top of a stack ripples down.
func (s *Server) refreshWallColumn(w *world.World, dim, x, y, z int, d [3]int) {
	for {
		cur := w.Block(x, y, z)
		info, ok := wallInfo(cur)
		if !ok {
			return
		}
		ns := wallUpdated(w, blockPos{x, y, z}, info, cur, d)
		if ns == cur {
			return
		}
		w.SetBlock(x, y, z, ns)
		s.hub.post(evBlock{x: x, y: y, z: z, dim: dim, state: ns, by: 0})
		y, d = y-1, [3]int{0, 1, 0}
	}
}
