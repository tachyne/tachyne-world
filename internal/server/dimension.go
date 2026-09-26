package server

import (
	"log"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Dimension switching. The connection owns the client-facing sequence (the
// Respawn packet, chunk restream, position sync — all connection-side state),
// then tells the hub via evDim so the authoritative record and everyone's
// entity views move between dimensions.

// The three dimensions, by the index every dim-carrying field uses.
const (
	dimOverworld = 0
	dimNether    = 1
	dimEnd       = 2
)

// bedWorks and anchorWorks are DimensionType.bedWorks / respawnAnchorWorks: the
// two respawn blocks each work in exactly one dimension and detonate in the
// others. Anywhere they do not work, using one is an explosion, not a refusal.
func bedWorks(dim int) bool    { return dim == dimOverworld }
func anchorWorks(dim int) bool { return dim == dimNether }

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
	p.sendEv(attachproto.Dimension{Dim: int32(dim), Gamemode: int32(s.modes.get(p.key())), Death: s.deathOf(p.key())})
	p.x, p.y, p.z = x+0.5, y, z+0.5
	p.setHubPos(p.x, p.z) // server-initiated placement: open the stream gate here
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evDim{eid: p.eid, dim: dim, x: p.x, y: p.y, z: p.z})
}

// switchDimensionTo is switchDimension landing beside a KNOWN cell (a bed,
// the spawn, a teleport target) instead of derived coordinates.
func (s *Server) switchDimensionTo(p *player, dim int, dest blockPos) {
	if dim == p.dim {
		return
	}
	p.dim = dim
	log.Printf("portal: %q respawning into dim %d at linked portal (%d,%d,%d)", p.name, dim, dest.x, dest.y, dest.z)
	p.sendEv(attachproto.Dimension{Dim: int32(dim), Gamemode: int32(s.modes.get(p.key())), Death: s.deathOf(p.key())})
	p.x, p.y, p.z = float64(dest.x)+0.5, float64(dest.y), float64(dest.z)+1.5
	p.setHubPos(p.x, p.z)
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evDim{eid: p.eid, dim: dim, x: p.x, y: p.y, z: p.z})
}

// switchDimensionAt is switchDimension landing at a nether portal's exact
// arrival spot and heading, as the hub worked them out (portalforcer.go).
func (s *Server) switchDimensionAt(p *player, dim int, x, y, z float64, yaw float32) {
	if dim == p.dim {
		return
	}
	p.dim = dim
	log.Printf("portal: %q respawning into dim %d at portal (%.1f,%.1f,%.1f)", p.name, dim, x, y, z)
	p.sendEv(attachproto.Dimension{Dim: int32(dim), Gamemode: int32(s.modes.get(p.key())), Death: s.deathOf(p.key())})
	p.x, p.y, p.z, p.yaw = x, y, z, yaw
	p.setHubPos(p.x, p.z)
	p.sendEv(teleportEv(p.x, p.y, p.z, p.yaw, p.pitch))
	s.hub.post(evDim{eid: p.eid, dim: dim, x: p.x, y: p.y, z: p.z})
}

// buildEndPlatform lays the vanilla 5x5 obsidian arrival pad at (100,48,0).
func (s *Server) buildEndPlatform(p *player) { endPlatform(s.end) }

// endPlatform is EndPlatformFeature.createEndPlatform under END_SPAWN_POINT
// (100,50,0): obsidian at y 48, three cells of air above it.
func endPlatform(w *world.World) {
	if w == nil {
		return
	}
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(100+dx, 48, dz, obsidianBlock)
			for dy := 1; dy <= 3; dy++ {
				if w.At(100+dx, 48+dy, dz) != worldgen.Air {
					w.SetBlock(100+dx, 48+dy, dz, worldgen.Air)
				}
			}
		}
	}
}

// obsidianBlock is obsidian's state (the pad was once written as a raw id,
// which the 26.3 renumbering turned into piston heads).
var obsidianBlock = worldgen.BlockBase("obsidian")

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
	// The old dimension's viewers lose the switcher's body now; the switcher's
	// own view (other players included) is dropped below, and the tracking
	// pass spawns whoever is in range in the new dimension, both ways.
	h.untrackPlayer(players, t.p.eid)
	// Swap entity views: hide the old dimension's mobs/items/projectiles,
	// show the new dimension's. Vehicles are overworld-only.
	// Mobs, items and orbs are the tracker's: drop the whole view the
	// client holds, and the next pass spawns the new dimension's.
	// PlayerList.sendAllPlayerInfo + sendLevelInfo, which vanilla's
	// changeDimension runs after the respawn: the respawn clears what the
	// client is holding, so all of it has to go again. Without this the hotbar
	// came up empty on every portal — most visibly in creative, where the
	// server otherwise never pushes an inventory at all (sendInventory is
	// gated on survival at join, because in creative the client keeps its own
	// copy — a copy the respawn had just thrown away).
	h.sendInventory(t)
	h.sendHealth(t)
	h.sendExperience(t)
	h.resendEffects(t)
	if t.shoulderOccupied() { // the respawn rebuilt their own player entity too
		t.p.sendEv(metaEv(shoulderMeta(t)))
	}

	h.dropTracked(t)
	for eid, c := range h.crystals {
		switch {
		case c.dim == old:
			t.p.sendEv(entGone(eid))
		case c.dim == e.dim:
			t.p.sendEv(entAdd(eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
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
		h.sendItemSpawnersTo(t)
	}
	// The respawn gave the client a new level with no entities in it: the
	// fixed furniture of the dimension it arrived in goes again, as at join.
	// Without this a trip through a portal left every armour stand, painting,
	// item frame and leash knot invisible until a relog.
	h.sendStandsTo(t)
	h.sendPaintingsTo(t)
	h.sendFramesTo(t)
	h.sendLeashesTo(t)
}
