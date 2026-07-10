package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"log"
)

// Dimension switching. The connection owns the client-facing sequence (the
// Respawn packet, chunk restream, position sync — all connection-side state),
// then tells the hub via evDim so the authoritative record and everyone's
// entity views move between dimensions.

type evDim struct {
	eid     int32
	dim     int
	x, y, z float64
}

func (evDim) isHubEvent() {}

// switchDimension moves a player between the overworld (0) and nether (1),
// landing them at (x,z) scaled 8:1 the vanilla way.
func (s *Server) switchDimension(p *player, dim int) {
	if dim == p.dim {
		return
	}
	var x, z float64
	switch {
	case dim == 1 && p.dim == 0:
		x, z = p.x/8, p.z/8 // 8:1 only between overworld and nether
	case dim == 0 && p.dim == 1:
		x, z = p.x*8, p.z*8
	case dim == 0: // returning from the End: the overworld spawn
		x, z = 0.5, 0.5
	default:
		x, z = p.x, p.z
	}
	p.dim = dim
	log.Printf("portal: %q respawning into dim %d", p.name, dim)
	w := s.worldFor(p)
	var y float64
	switch dim {
	case 1: // pick real cavern floor near the scaled point (or a refuge spot)
		lx, ly, lz, ok := w.Gen().NetherLanding(int(x), int(z))
		x, y, z = float64(lx), float64(ly), float64(lz)
		if !ok {
			log.Printf("portal: no natural nether floor near (%.0f,%.0f) — carving a refuge", x, z)
		}
	case 2: // the vanilla End spawn platform
		x, y, z = 100.5, 49, 0.5
		s.buildEndPlatform(p)
	default:
		y = w.SurfaceY(int(x), int(z))
	}

	// Respawn into the new dimension (the session renders the respawn packet,
	// resets its entity view, and re-Wants chunks on the Teleport).
	p.sendEv(attachproto.Dimension{Dim: int32(dim), Gamemode: int32(s.modes.get(p.name))})
	p.x, p.y, p.z = x+0.5, y, z+0.5
	p.setHubPos(p.x, p.z) // server-initiated placement: open the stream gate here
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evDim{eid: p.eid, dim: dim, x: p.x, y: p.y, z: p.z})
}

// switchDimensionTo is switchDimension landing at a KNOWN portal base (the
// remembered half of a portal pair) instead of derived coordinates.
func (s *Server) switchDimensionTo(p *player, dim int, dest blockPos) {
	if dim == p.dim {
		return
	}
	p.dim = dim
	log.Printf("portal: %q respawning into dim %d at linked portal (%d,%d,%d)", p.name, dim, dest.x, dest.y, dest.z)
	p.sendEv(attachproto.Dimension{Dim: int32(dim), Gamemode: int32(s.modes.get(p.name))})
	p.x, p.y, p.z = float64(dest.x)+0.5, float64(dest.y), float64(dest.z)+1.5
	p.setHubPos(p.x, p.z)
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evDim{eid: p.eid, dim: dim, x: p.x, y: p.y, z: p.z})
}

// buildEndPlatform lays the vanilla 5x5 obsidian arrival pad at (100,48,0).
func (s *Server) buildEndPlatform(p *player) {
	w := s.end
	if w == nil {
		return
	}
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(100+dx, 48, dz, 2400) // obsidian
			for dy := 1; dy <= 3; dy++ {
				if w.At(100+dx, 48+dy, dz) != 0 {
					w.SetBlock(100+dx, 48+dy, dz, 0)
				}
			}
		}
	}
}

// onDimSwitch is the hub side: move the tracked record and swap entity
// visibility — the switcher disappears from the old dimension's players and
// appears to the new dimension's, and vice versa on their own screen.
func (h *hub) onDimSwitch(players map[int32]*tracked, t *tracked, e evDim) {
	old := t.dim
	t.dim, t.x, t.y, t.z = e.dim, e.x, e.y, e.z
	t.portalLatch, t.portalTicks = true, 0 // arrivals stand in the return portal
	if e.dim == 2 {
		h.enterEnd(players, t) // first End arrival stages the dragon fight
	}
	t.graceUntil = h.tick.Load() + 60 // vanilla-style arrival invulnerability
	t.fireSecs, t.peakY = 0, t.y      // and no burn/fall carries across the portal
	gone := entGone(t.p.eid)
	for _, o := range players {
		if o.p.eid == t.p.eid {
			continue
		}
		switch o.dim {
		case old: // they lose sight of the switcher, and the switcher of them
			o.p.trySendEv(gone)
			t.p.trySendEv(entGone(o.p.eid))
		case e.dim: // both sides gain sight (gear rides with the spawn)
			o.p.trySendEv(entAdd(t.p.eid, playerEntityType, t.p.uuid, t.x, t.y, t.z, t.yaw, t.pitch))
			o.p.trySendEv(equipEv(t.p.eid, heldStack(t), t.offhand, t.armor))
			// The switcher's queue is mid-chunk-flood: a trySend here silently
			// drops and the other player stays invisible until relog. Block.
			t.p.sendEv(entAdd(o.p.eid, playerEntityType, o.p.uuid, o.x, o.y, o.z, o.yaw, o.pitch))
			t.p.sendEv(equipEv(o.p.eid, heldStack(o), o.offhand, o.armor))
		}
	}
	// Swap entity views: hide the old dimension's mobs/items/projectiles,
	// show the new dimension's. Vehicles are overworld-only.
	for _, m := range h.mobs {
		switch m.dim {
		case old:
			t.p.sendEv(entGone(m.eid))
		case e.dim:
			t.p.sendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
			if m.size > 0 {
				t.p.sendEv(metaEv(slimeMeta(m.eid, m.size)))
			}
		}
	}
	for eid, c := range h.crystals {
		switch {
		case old == 2:
			t.p.sendEv(entGone(eid))
		case e.dim == 2:
			t.p.sendEv(entAdd(eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
		}
	}
	for eid, it := range h.items {
		switch it.dim {
		case old:
			t.p.sendEv(entGone(eid))
		case e.dim:
			t.p.sendEv(entAdd(eid, entityItem, it.uuid, it.x, it.y, it.z, 0, 0))
			t.p.sendEv(metaEv(itemMetadata(eid, it.item, it.count)))
		}
	}
	for eid, a := range h.arrows {
		switch a.dim {
		case old:
			t.p.trySendEv(entGone(eid))
		}
	}
	if e.dim != 0 {
		for eid := range h.vehicles {
			t.p.trySendEv(entGone(eid))
		}
	} else {
		h.sendVehiclesTo(t)
	}
}
