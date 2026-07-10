package server

import "tachyne/internal/worldgen"

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

// connectState sets the north/east/south/west connections of a connector block at
// (x,y,z) from its current neighbours. A non-connector state is returned as-is.
func (s *Server) connectState(x, y, z int, state uint32) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok || !worldgen.IsHorizontalConnector(info) {
		return state
	}
	for _, d := range hConnectDirs {
		v := "false"
		if s.connectsTo(s.world.Block(x+d.dx, y, z+d.dz)) {
			v = "true"
		}
		state = worldgen.SetProperty(info, state, d.name, v)
	}
	return state
}

// connectsTo reports whether a fence/pane/bars attaches to neighbour block nb:
// another connector, or a full solid block.
func (s *Server) connectsTo(nb uint32) bool {
	if info, ok := worldgen.InfoForState(nb); ok && worldgen.IsHorizontalConnector(info) {
		return true
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
func (s *Server) updateConnectNeighbors(x, y, z int) {
	for _, d := range hConnectDirs {
		nx, nz := x+d.dx, z+d.dz
		cur := s.world.Block(nx, y, nz)
		info, ok := worldgen.InfoForState(cur)
		if !ok || !worldgen.IsHorizontalConnector(info) {
			continue
		}
		if ns := s.connectState(nx, y, nz, cur); ns != cur {
			s.world.SetBlock(nx, y, nz, ns)
			s.hub.post(evBlock{x: nx, y: y, z: nz, state: ns, by: 0})
		}
	}
}
