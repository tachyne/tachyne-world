package server

import (
	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// Multi-block connection handling for fences, glass panes, and iron bars: their
// north/east/south/west state must reflect their neighbours so a ring of fences
// renders as a closed pen rather than isolated posts. We recompute the connection
// state when such a block is placed, and re-evaluate adjacent connectors whenever
// any block is placed or broken next to them.

// hConnectDirs are the four horizontal neighbours a connector links across, named
// by the block-state property each one drives.
var hConnectDirs = []struct {
	name   string
	dx, dz int
}{
	{"north", 0, -1},
	{"south", 0, 1},
	{"west", -1, 0},
	{"east", 1, 0},
}

// connectState sets the connections of a connector block at (x,y,z) from its
// current neighbours: boolean sides for fences/panes/bars, the none/low/tall
// enum + post for walls (walls.go). A non-connector state is returned as-is.
func (s *Server) connectState(w *world.World, x, y, z int, state uint32) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	if worldgen.IsWallConnector(info) {
		return wallState(w, x, y, z, info, state)
	}
	if !worldgen.IsHorizontalConnector(info) {
		return state
	}
	for _, d := range hConnectDirs {
		v := "false"
		if connectsTo(state, w.Block(x+d.dx, y, z+d.dz)) {
			v = "true"
		}
		state = worldgen.SetProperty(info, state, d.name, v)
	}
	return state
}

// connectsTo reports whether a fence/pane/bars attaches to neighbour block nb:
// another boolean connector, or a full solid block. Panes and iron bars also
// attach to walls (vanilla IronBarsBlock); fences do not.
func connectsTo(self, nb uint32) bool {
	if info, ok := worldgen.InfoForState(nb); ok && worldgen.IsHorizontalConnector(info) {
		return true
	}
	if isPaneOrBars(self) {
		if _, ok := wallInfo(nb); ok {
			return true
		}
	}
	return worldgen.IsSolidFull(nb)
}

// breakUnsupportedAbove removes plants stacked directly above (x,y,z) that just
// lost the block they were resting on — e.g. the grass/flowers on top of a dirt
// block you mined. Cascades upward so a two-tall plant (tall grass) goes fully.
func (s *Server) breakUnsupportedAbove(x, y, z int) {
	for ay := y + 1; ; ay++ {
		// world.At (not Block) so we see decoration plants — naturally-generated
		// grass/flowers aren't in the edit overlay, so Block would miss them and
		// they'd be left floating.
		above := s.world.At(x, ay, z)
		if !worldgen.NeedsGroundSupport(above) {
			break
		}
		s.world.SetBlock(x, ay, z, worldgen.Air)
		s.hub.post(evBlock{x: x, y: ay, z: z, state: worldgen.Air, by: 0})
		s.hub.post(evDrop{x: x, y: ay, z: z, state: above}) // e.g. grass → 12.5% wheat seeds
	}
}

// updateConnectNeighbors re-evaluates the four horizontal neighbours of (x,y,z):
// any that is a connector recomputes its connections and, if it changed, the new
// state is persisted and broadcast to everyone (by:0 — no editor to exclude).
// Walls also re-evaluate below the edit (their tall sides and post depend on
// what covers them) and cascade down their column.
func (s *Server) updateConnectNeighbors(w *world.World, dim, x, y, z int) {
	for _, d := range hConnectDirs {
		nx, nz := x+d.dx, z+d.dz
		cur := w.Block(nx, y, nz)
		info, ok := worldgen.InfoForState(cur)
		if !ok {
			continue
		}
		switch {
		case worldgen.IsWallConnector(info):
			s.refreshWallColumn(w, dim, nx, y, nz)
		case worldgen.IsHorizontalConnector(info):
			if ns := s.connectState(w, nx, y, nz, cur); ns != cur {
				w.SetBlock(nx, y, nz, ns)
				s.hub.post(evBlock{x: nx, y: y, z: nz, dim: dim, state: ns, by: 0})
			}
		}
	}
	s.refreshWallColumn(w, dim, x, y-1, z)
}
