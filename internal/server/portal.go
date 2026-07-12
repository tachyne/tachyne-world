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
	hollow := func(bx, by, bz int) bool {
		s := w.At(bx, by, bz)
		return s == worldgen.Air || isPortalBlock(s)
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

// lightPortal fills a validated frame's interior with portal blocks.
func (s *Server) lightPortal(p *player, x, y, z int) bool {
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

// portalBaseKey normalizes any portal block to its sheet's minimum base cell
// — a stable identity for the link registry.
func portalBaseKey(w *world.World, x, y, z int) blockPos {
	for isPortalBlock(w.At(x, y-1, z)) {
		y--
	}
	state := w.At(x, y, z)
	dx, dz := 1, 0
	if state == portalZ {
		dx, dz = 0, 1
	}
	for isPortalBlock(w.At(x-dx, y, z-dz)) {
		x, z = x-dx, z-dz
	}
	return blockPos{x, y, z}
}

// dimPos keys the portal-link registry across dimensions.
type dimPos struct {
	dim int
	pos blockPos
}

type evPortalLinked struct {
	from, to dimPos
}

func (evPortalLinked) isHubEvent() {}

// updatePortalDwell runs each hub tick: players standing in portal blocks
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
			if t.portalTicks > 0 {
				log.Printf("portal: %q left the portal after %d dwell ticks (feet=%d dim=%d at %.1f,%.1f,%.1f)",
					t.p.name, t.portalTicks, feet, t.dim, t.x, t.y, t.z)
			}
			t.portalTicks = 0
			continue
		}
		t.portalTicks++
		need := portalDwellTicks / survivalTickN // the dwell pass runs once a second
		if t.gamemode == gmCreative {
			need = 1
		}
		log.Printf("portal: %q dwell %d/%d (dim=%d gm=%d pending=%d)",
			t.p.name, t.portalTicks, need, t.dim, t.gamemode, t.p.pendingDim.Load())
		if t.portalTicks >= need && t.p.pendingDim.Load() < 0 {
			t.portalTicks = 0
			from := dimPos{t.dim, portalBaseKey(h.worldFor(t.dim), floorInt(t.x), floorInt(t.y+0.05), floorInt(t.z))}
			t.p.pendingFrom = from
			t.p.pendingDest = blockPos{}
			t.p.pendingDestOK = false
			// A remembered link wins over coordinate math — travelers return
			// through the exact portal pair they came by.
			if to, ok := h.portalLinks[from]; ok {
				tw := h.worldFor(to.dim)
				st := tw.At(to.pos.x, to.pos.y, to.pos.z)
				if isPortalBlock(st) && portalIntact(tw, to.pos.x, to.pos.y, to.pos.z, st) &&
					portalSpotSafe(tw, to.pos.x, to.pos.y, to.pos.z) {
					t.p.pendingDest = to.pos
					t.p.pendingDestOK = true
				} else {
					// The partner is gone: forget the pair so the rebuild
					// records a fresh one instead of failing forever.
					delete(h.portalLinks, from)
					delete(h.portalLinks, to)
					log.Printf("portal: dropped dead pair %v <-> %v", from, to)
				}
			}
			t.p.pendingDim.Store(int32(1 - t.dim)) // release: fields above are visible after Load
			log.Printf("portal: %q dwell complete — flagged switch to dim %d (linked=%v)",
				t.p.name, 1-t.dim, t.p.pendingDestOK)
		}
	}
}

// checkPendingDim runs on the connection goroutine between packets: a portal
// dwell completed → perform the switch and build the arrival portal.
func (s *Server) checkPendingDim(p *player) {
	dim := p.pendingDim.Load()
	if dim < 0 {
		return
	}
	p.pendingDim.Store(-1)
	log.Printf("portal: %q connection picked up switch to dim %d (currently %d)", p.name, dim, p.dim)
	if p.pendingDestOK { // a remembered pair: land at that portal's doorstep
		s.switchDimensionTo(p, int(dim), p.pendingDest)
		return
	}
	s.switchDimension(p, int(dim))
	s.ensureArrivalPortal(p)
	// Record the pair both ways so the return trip uses THIS portal.
	w := s.worldFor(p)
	if px, py, pz, ok := w.NearestEdited(floorInt(p.x), floorInt(p.y), floorInt(p.z), 12, isPortalBlock); ok {
		to := dimPos{p.dim, portalBaseKey(w, px, py, pz)}
		log.Printf("portal: recording pair %v <-> %v", p.pendingFrom, to)
		s.hub.post(evPortalLinked{from: p.pendingFrom, to: to})
	} else {
		log.Printf("portal: NO portal within 12 of arrival (%.0f,%.0f,%.0f) dim %d — pair not recorded",
			p.x, p.y, p.z, p.dim)
	}
}

// ensureArrivalPortal reuses a portal near the landing spot (wide scan — the
// 8:1 rounding drifts coordinates between trips) or builds a lit 2x3 portal
// with a platform. Either way the traveler ends up standing on solid ground
// beside the portal, never hovering at raw scaled coordinates.
func (s *Server) ensureArrivalPortal(p *player) {
	w := s.worldFor(p)
	bx, by, bz := floorInt(p.x), floorInt(p.y), floorInt(p.z)
	px, py, pz, ok := w.NearestEdited(bx, by, bz, 128, isPortalBlock)
	if !ok {
		log.Printf("portal: arrival at (%d,%d,%d) dim %d — no portal edits within 128, building fresh", bx, by, bz, p.dim)
	}
	if ok { // descend to the portal column's base (the standing cell)
		for isPortalBlock(w.At(px, py-1, pz)) {
			py--
		}
		// A matched edit must be a REAL lit portal, not a stray block left by a
		// demolished frame: it needs an obsidian footing and an intact sheet.
		switch {
		case w.At(px, py-1, pz) != worldgen.Obsidian:
			log.Printf("portal: found portal at (%d,%d,%d) rejected — footing is %d, not obsidian", px, py, pz, w.At(px, py-1, pz))
			ok = false
		case !portalIntact(w, px, py, pz, w.At(px, py, pz)):
			log.Printf("portal: found portal at (%d,%d,%d) rejected — sheet not intact", px, py, pz)
			ok = false
		case !portalSpotSafe(w, px, py, pz):
			log.Printf("portal: found portal at (%d,%d,%d) rejected — doorstep unsafe (lava)", px, py, pz)
			ok = false
		}
	}
	if ok {
		// Stand them beside the existing portal, with guaranteed footing.
		p.x, p.y, p.z = float64(px)+0.5, float64(py), float64(pz)+1.5
		sx, sy, sz := px, py-1, pz+1
		if pass := w.At(sx, sy, sz); pass == worldgen.Air || worldgen.IsReplaceable(pass) ||
			worldgen.IsLava(w.At(sx, sy, sz)) {
			w.SetBlock(sx, sy, sz, worldgen.Obsidian)
			s.hub.post(evBlock{x: sx, y: sy, z: sz, dim: p.dim, state: worldgen.Obsidian, by: 0})
			p.sendEv(blockSetEv(sx, sy, sz, worldgen.Obsidian))
		}
		for j := 0; j < 2; j++ { // headroom
			if blk := w.At(sx, py+j, sz); blk != worldgen.Air {
				w.SetBlock(sx, py+j, sz, worldgen.Air)
				s.hub.post(evBlock{x: sx, y: py + j, z: sz, dim: p.dim, state: worldgen.Air, by: 0})
				p.sendEv(blockSetEv(sx, py+j, sz, worldgen.Air))
			}
		}
		p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
		s.hub.post(evMove{eid: p.eid, x: p.x, y: p.y, z: p.z, yaw: p.yaw, pitch: p.pitch, onGround: true})
		return
	}
	set := func(x, y, z int, st uint32) {
		if cur := w.At(x, y, z); isPortalBlock(cur) && !isPortalBlock(st) {
			return // the builder must NEVER destroy a standing portal sheet
		}
		w.SetBlock(x, y, z, st)
		s.hub.post(evBlock{x: x, y: y, z: z, dim: p.dim, state: st, by: 0})
		p.sendEv(blockSetEv(x, y, z, st))
	}
	// Carve a livable pocket first (a refuge in solid netherrack needs room),
	// then a generous platform — a lava-sea island must be a workable base,
	// not a doormat — and the frame (x-axis, interior 2x3 at bx..bx+1).
	for dx := -4; dx <= 5; dx++ {
		for dz := -3; dz <= 3; dz++ {
			for dy := 0; dy <= 3; dy++ {
				blk := w.At(bx+dx, by+dy, bz+dz)
				if blk == worldgen.Air || isPortalBlock(blk) || blk == worldgen.Obsidian {
					continue // never carve through someone's standing portal
				}
				set(bx+dx, by+dy, bz+dz, worldgen.Air)
			}
		}
	}
	for dx := -4; dx <= 5; dx++ {
		for dz := -3; dz <= 3; dz++ {
			set(bx+dx, by-1, bz+dz, worldgen.Obsidian)
		}
	}
	for i := -1; i <= 2; i++ { // top + bottom rails
		set(bx+i, by+3, bz, worldgen.Obsidian)
	}
	for j := 0; j < 3; j++ { // side pillars + clear interior
		set(bx-1, by+j, bz, worldgen.Obsidian)
		set(bx+2, by+j, bz, worldgen.Obsidian)
		set(bx, by+j, bz, portalX)
		set(bx+1, by+j, bz, portalX)
		set(bx, by+j, bz-1, worldgen.Air) // breathing room in front
		set(bx, by+j, bz+1, worldgen.Air)
	}
	// Step the player out of the fresh portal so they don't instantly bounce.
	p.x, p.z = float64(bx)+0.5, float64(bz)+1.5+0.0
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evMove{eid: p.eid, x: p.x, y: p.y, z: p.z, yaw: p.yaw, pitch: p.pitch, onGround: true})
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

// updatePortalBlock is the scheduled step for portal blocks (overworld sim):
// orphans pop, and the pop cascades through the rest of the sheet.
func (h *hub) updatePortalBlock(players map[int32]*tracked, pos blockPos, state uint32) {
	if !portalIntact(h.world, pos.x, pos.y, pos.z, state) {
		h.setBlock(players, pos, worldgen.Air)
		h.scheduleAround(pos, 1)
	}
}

// portalSpotSafe rejects arrival portals whose doorstep is a lava bath —
// early builds dropped portals into the lava sea; snapping to those cooked
// travelers. The doorstep cells (both sides) must be lava-free at feet and
// head height after our standard clearing.
func portalSpotSafe(w *world.World, px, py, pz int) bool {
	for _, dz := range []int{1, -1} {
		safe := true
		for j := 0; j < 2; j++ {
			if worldgen.IsLava(w.At(px, py+j, pz+dz)) || worldgen.IsLava(w.At(px+1, py+j, pz+dz)) {
				safe = false
			}
		}
		if worldgen.IsLava(w.At(px, py-1, pz+dz)) && worldgen.IsLava(w.At(px, py-2, pz+dz)) {
			safe = false // standing over deep lava
		}
		if safe {
			return true
		}
	}
	return false
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
