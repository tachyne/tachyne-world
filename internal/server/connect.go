package server

import (
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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
	return connectStateAt(w, x, y, z, state)
}

// connectStateAt is connectState for any caller: the world is all it reads,
// so the hub's shape updates (shapeupdate.go) use it too.
func connectStateAt(w *world.World, x, y, z int, state uint32) uint32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state
	}
	if isTripwire(state) {
		// TripWireBlock.shouldConnectTo: more tripwire, or a hook facing back
		// at it — never a solid block, which the fence rule below would take.
		for _, d := range hConnectDirs {
			nb := w.Block(x+d.dx, y, z+d.dz)
			v := isTripwire(nb)
			if isTripwireHook(nb) {
				v = stateFacing(nb) == oppositeFacing(d.name)
			}
			state = worldgen.SetProperty(info, state, d.name, strconv.FormatBool(v))
		}
		return state
	}
	if worldgen.IsWallConnector(info) {
		return wallState(w, x, y, z, info, state)
	}
	if _, ok := stairInfo(state); ok {
		return stairShape(w, x, y, z, info, state)
	}
	if !worldgen.IsHorizontalConnector(info) {
		return state
	}
	for _, d := range hConnectDirs {
		v := "false"
		if connectsTo(state, w.Block(x+d.dx, y, z+d.dz), d.dx != 0) {
			v = "true"
		}
		state = worldgen.SetProperty(info, state, d.name, v)
	}
	return state
}

// connectsTo reports whether a fence, pane or bars block attaches to the
// neighbour nb on a side along the X axis (sideAxisX) or the Z axis.
//
// FenceBlock.connectsTo: another fence of its kind (wooden to wooden, the
// nether-brick fence to its own), a fence gate hung across that side, or a
// sturdy face that is not one of the blocks nothing connects to.
// IronBarsBlock.attachsTo (panes, iron and copper bars): any other pane or
// bars, a wall, or such a sturdy face. A pane never joins a fence, nor a
// fence a pane.
func connectsTo(self, nb uint32, sideAxisX bool) bool {
	if isPaneOrBars(self) {
		if isPaneOrBars(nb) {
			return true
		}
		if _, ok := wallInfo(nb); ok {
			return true
		}
		return !connectException(nb) && worldgen.IsSolidFull(nb)
	}
	if isFenceBlock(nb) {
		return isFenceBlock(self) && isWoodenFence(self) == isWoodenFence(nb)
	}
	if info, ok := gateInfo(nb); ok {
		// FenceGateBlock.connectsToDirection: the gate's facing runs across
		// the side, so its posts line up with the fence.
		return facingAxisX(worldgen.GetProperty(info, nb, "facing")) != sideAxisX
	}
	if info, ok := worldgen.InfoForState(nb); ok && worldgen.IsHorizontalConnector(info) {
		return false // a pane, bars or tripwire beside a fence
	}
	return !connectException(nb) && worldgen.IsSolidFull(nb)
}

var netherBrickFence = worldgen.BlockBase("nether_brick_fence")

// isFenceBlock is the FenceBlock family, by name.
func isFenceBlock(s uint32) bool {
	n, ok := worldgen.StateName(s)
	return ok && strings.HasSuffix(n, "_fence")
}

// isWoodenFence is #wooden_fences: every fence but the nether-brick one.
func isWoodenFence(s uint32) bool { return !sameBlockFamily(s, netherBrickFence) }

// connectException is Block.isExceptionForConnection: full blocks that
// fences, panes and walls still do not join — leaves, the barrier, pumpkins,
// jack o'lanterns, melons and shulker boxes.
func connectException(s uint32) bool {
	if _, _, _, leaf := leafInfo(s); leaf || isShulkerBox(s) {
		return true
	}
	return connectExceptionStates[s]
}

var connectExceptionStates = func() map[uint32]bool {
	out := map[uint32]bool{}
	for _, n := range []string{"barrier", "carved_pumpkin", "jack_o_lantern", "melon", "pumpkin"} {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			for s := lo; s <= hi; s++ {
				out[s] = true
			}
		}
	}
	return out
}()

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
		isStair := false
		if _, ok := stairInfo(cur); ok {
			isStair = true
		}
		switch {
		case worldgen.IsWallConnector(info):
			s.refreshWallColumn(w, dim, nx, y, nz)
		case worldgen.IsHorizontalConnector(info), isStair:
			if ns := s.connectState(w, nx, y, nz, cur); ns != cur {
				w.SetBlock(nx, y, nz, ns)
				s.hub.post(evBlock{x: nx, y: y, z: nz, dim: dim, state: ns, by: 0})
			}
		}
	}
	s.refreshWallColumn(w, dim, x, y-1, z)
}
