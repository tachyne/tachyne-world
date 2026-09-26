package server

import (
	"log"
	"sync/atomic"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Nether portals. Flint & steel on an obsidian frame lights it: the frame is
// validated (2-21 interior, obsidian sides + top + bottom, x or z plane) and
// filled with portal blocks. The hub counts contact ticks for players
// standing in a portal (80 = vanilla 4s; instant in creative) and flags the
// connection via pendingDim — the connection goroutine performs the actual
// switch (it owns the chunk view), landing at 8:1 coordinates where it
// builds the return portal if none exists.

// portalX/portalZ are the two nether_portal orientations; they are vars (not
// consts) because worldgen resolves block-state ids by name at startup.
var (
	portalX = worldgen.NetherPortal     // axis=x
	portalZ = worldgen.NetherPortal + 1 // axis=z
)

const (
	portalDwellTicks = 80
	portalMaxSpan    = 21
)

func isPortalBlock(s uint32) bool { return s == portalX || s == portalZ }

// detectPortalFrame looks for a valid obsidian frame around an interior air
// cell. Returns the interior's min corner, dimensions and the portal axis
// state, or ok=false.
func detectPortalFrame(w *world.World, x, y, z int) (x0, y0, z0, wid, hgt int, state uint32, ok bool) {
	obs := func(bx, by, bz int) bool { return w.At(bx, by, bz) == worldgen.Obsidian }
	hollow := func(bx, by, bz int) bool { // PortalShape.isEmpty: air, #fire or portal
		s := w.At(bx, by, bz)
		return s == worldgen.Air || isFire(s) || isPortalBlock(s)
	}
	for _, axis := range [2]uint32{portalX, portalZ} {
		dx, dz := 1, 0
		if axis == portalZ {
			dx, dz = 0, 1
		}
		// Slide to the frame's left edge and floor from the clicked cell.
		x0, y0, z0 = x, y, z
		for i := 0; i < portalMaxSpan && hollow(x0-dx, y0, z0-dz); i++ {
			x0, z0 = x0-dx, z0-dz
		}
		for i := 0; i < portalMaxSpan && hollow(x0, y0-1, z0); i++ {
			y0--
		}
		// Measure the interior.
		wid = 0
		for wid < portalMaxSpan && hollow(x0+dx*wid, y0, z0+dz*wid) {
			wid++
		}
		hgt = 0
		for hgt < portalMaxSpan && hollow(x0, y0+hgt, z0) {
			hgt++
		}
		if wid < 2 || hgt < 3 {
			continue
		}
		// Validate the full rectangle: interior hollow, edges obsidian.
		valid := true
		for i := 0; i < wid && valid; i++ {
			cx, cz := x0+dx*i, z0+dz*i
			if !obs(cx, y0-1, cz) || !obs(cx, y0+hgt, cz) {
				valid = false
			}
			for j := 0; j < hgt && valid; j++ {
				if !hollow(cx, y0+j, cz) {
					valid = false
				}
			}
		}
		for j := 0; j < hgt && valid; j++ {
			if !obs(x0-dx, y0+j, z0-dz) || !obs(x0+dx*wid, y0+j, z0+dz*wid) {
				valid = false
			}
		}
		if valid {
			return x0, y0, z0, wid, hgt, axis, true
		}
	}
	return 0, 0, 0, 0, 0, 0, false
}

// lightPortal fills a validated frame's interior with portal blocks. The End
// is no portal dimension: there the lighter just starts a fire.
func (s *Server) lightPortal(p *player, x, y, z int) bool {
	if p.dim != dimOverworld && p.dim != dimNether {
		return false
	}
	w := s.worldFor(p)
	x0, y0, z0, wid, hgt, state, ok := detectPortalFrame(w, x, y, z)
	if !ok {
		return false
	}
	dx, dz := 1, 0
	if state == portalZ {
		dx, dz = 0, 1
	}
	for i := 0; i < wid; i++ {
		for j := 0; j < hgt; j++ {
			bx, by, bz := x0+dx*i, y0+j, z0+dz*i
			w.SetBlock(bx, by, bz, state)
			s.hub.post(evBlock{x: bx, y: by, z: bz, dim: p.dim, state: state, by: 0})
			p.sendEv(blockSetEv(bx, by, bz, state))
		}
	}
	return true
}

// fireOnPlace is BaseFireBlock.onPlace: a fire that appears inside an empty
// obsidian frame — from a fire charge, a dispenser, lightning, a spreading
// blaze — lights the portal, in the overworld and the Nether only
// (inPortalDimension).
func (h *hub) fireOnPlace(players map[int32]*tracked, dim int, pos blockPos, old, state uint32) {
	if !isFire(state) || isFire(old) || (dim != dimOverworld && dim != dimNether) {
		return
	}
	w := h.worldFor(dim)
	x0, y0, z0, wid, hgt, axis, ok := detectPortalFrame(w, pos.x, pos.y, pos.z)
	if !ok {
		return
	}
	dx, dz := 1, 0
	if axis == portalZ {
		dx, dz = 0, 1
	}
	for i := 0; i < wid; i++ {
		for j := 0; j < hgt; j++ {
			h.setBlockAt(players, dim, blockPos{x0 + dx*i, y0 + j, z0 + dz*i}, axis)
		}
	}
}

// updatePortalDwell runs every hub tick: players standing in portal blocks
// accumulate contact; at the threshold the connection is flagged to switch.
func (h *hub) updatePortalDwell(players map[int32]*tracked) {
	for _, t := range players {
		if t.dim > 1 {
			continue // nether portals link the overworld and nether only
		}
		feet := h.worldFor(t.dim).At(floorInt(t.x), floorInt(t.y+0.05), floorInt(t.z))
		if t.portalLatch { // vanilla: an arrival portal is inert until you step off it
			if !isPortalBlock(feet) {
				t.portalLatch = false
			}
			t.portalTicks = 0
			continue
		}
		if !isPortalBlock(feet) {
			// PortalProcessor.decayTick: out of the portal the wait drains
			// four ticks for every one, so a step out and back in keeps most
			// of it.
			t.portalTicks = max(t.portalTicks-4, 0)
			continue
		}
		// ServerLevel.isAllowedToEnterPortal: the rule gates the way IN to the
		// Nether only. Coming back out always works, or the rule would strand
		// whoever was already there when it was turned off.
		if t.dim == dimOverworld && !h.rules.AllowNether {
			t.portalTicks = 0
			continue
		}
		// players_nether_portal_default_delay / _creative_delay
		// (NetherPortalBlock.getPortalTransitionTime): the ticks you stand in
		// a portal before it takes you, counted every tick; the portal fires
		// on the tick the count has reached the delay (portalTime++ >= delay).
		delay := h.rules.PortalDelay
		if t.gamemode == gmCreative {
			delay = h.rules.PortalDelayCreate
		}
		ready := t.portalTicks >= delay
		t.portalTicks++
		if ready && t.p.pendingDim.Load() < 0 {
			t.portalTicks = 0
			// NetherPortalBlock.getPortalDestination: the far portal is
			// searched for (or built) now, on the hub, where the world is
			// written; the connection only carries the player there.
			entry := blockPos{floorInt(t.x), floorInt(t.y + 0.05), floorInt(t.z)}
			a, ok := h.netherPortalExit(players, t.dim, entry, t.x, t.y, t.z, playerWidth(t), playerHeight(t), t.gamemode == gmSpectator)
			if !ok {
				continue
			}
			t.p.pendingDestOK = false
			t.p.pendingAt = true
			t.p.pendingPos = [3]float64{a.x, a.y, a.z}
			t.p.pendingYaw = t.yaw + a.turn
			t.p.pendingDim.Store(int32(a.dim)) // release: fields above are visible after Load
			log.Printf("portal: %q dwell complete — flagged switch to dim %d at (%.1f,%.1f,%.1f)",
				t.p.name, a.dim, a.x, a.y, a.z)
		}
	}
}

// checkPendingDim runs on the connection goroutine between packets: a switch
// the hub flagged (a portal, a respawn, a teleport) is carried out.
func (s *Server) checkPendingDim(p *player) {
	dim := p.pendingDim.Load()
	if dim < 0 {
		return
	}
	p.pendingDim.Store(-1)
	log.Printf("portal: %q connection picked up switch to dim %d (currently %d)", p.name, dim, p.dim)
	switch {
	case p.pendingAt: // a nether portal: the exact spot in the far portal
		p.pendingAt = false
		s.switchDimensionAt(p, int(dim), p.pendingPos[0], p.pendingPos[1], p.pendingPos[2], p.pendingYaw)
	case p.pendingDestOK: // a known spot: a bed, the spawn, a teleport
		s.switchDimensionTo(p, int(dim), p.pendingDest)
	default:
		s.switchDimension(p, int(dim))
	}
}

// portalIntact: a portal block must be held by its frame — obsidian or more
// portal above AND below, and obsidian/portal on both sides along its axis.
// Anything else is an orphan (demolished frame) and pops.
func portalIntact(w *world.World, x, y, z int, state uint32) bool {
	holds := func(s uint32) bool { return s == worldgen.Obsidian || isPortalBlock(s) }
	if !holds(w.At(x, y-1, z)) || !holds(w.At(x, y+1, z)) {
		return false
	}
	if state == portalX {
		return holds(w.At(x-1, y, z)) && holds(w.At(x+1, y, z))
	}
	return holds(w.At(x, y, z-1)) && holds(w.At(x, y, z+1))
}

// updatePortalBlock is the scheduled step for portal blocks (any dimension):
// orphans pop, and the pop cascades through the rest of the sheet.
func (h *hub) updatePortalBlock(players map[int32]*tracked, pos blockPos, state uint32) {
	if !portalIntact(h.rsWorld(), pos.x, pos.y, pos.z, state) { // the portal's own dimension: h.world is the overworld
		h.setBlockAt(players, h.rsDim, pos, worldgen.Air)
		h.scheduleAroundIn(h.rsDim, pos, 1)
	}
}

// cascadeOrphanPortals pops portal blocks left unsupported by an edit at pos
// — a direct BFS so it works in every dimension (the scheduled simulation is
// overworld-only). Called from the block-edit handler.
func (h *hub) cascadeOrphanPortals(players map[int32]*tracked, dim int, pos blockPos) {
	w := h.worldFor(dim)
	queue := []blockPos{pos}
	seen := map[blockPos]bool{}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
			n := blockPos{c.x + d[0], c.y + d[1], c.z + d[2]}
			if seen[n] {
				continue
			}
			st := w.At(n.x, n.y, n.z)
			if !isPortalBlock(st) || portalIntact(w, n.x, n.y, n.z, st) {
				continue
			}
			seen[n] = true
			w.SetBlock(n.x, n.y, n.z, worldgen.Air)
			body := blockSetEv(n.x, n.y, n.z, worldgen.Air)
			for _, t := range players {
				if t.dim == dim {
					t.p.trySendEv(body)
				}
			}
			queue = append(queue, n)
		}
	}
}

// pendingDimInit is the sentinel for "no switch requested".
func pendingDimInit(v *atomic.Int32) { v.Store(-1) }
