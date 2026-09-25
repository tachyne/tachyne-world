package server

import (
	"fmt"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /spectate and the spectator camera (ServerPlayer.setCamera). A spectator
// who left-clicks an entity, or is put there by /spectate, looks through its
// eyes; shift, or the entity going away, brings the camera home. While the
// camera is elsewhere the player's own position rides along with the entity
// (ServerPlayer.tick's absSnapTo), which is what keeps its chunks loaded.

// setCamera points a player's camera at an entity, or home (eid 0).
func (h *hub) setCamera(t *tracked, eid int32) {
	if eid == t.p.eid {
		eid = 0
	}
	if t.camera == eid {
		return
	}
	t.camera = eid
	shown := eid
	if eid == 0 {
		shown = t.p.eid
	}
	t.p.trySendEv(attachproto.Camera{EID: shown})
}

// cameraEntity is where a camera target stands, if it is still there.
func (h *hub) cameraEntity(players map[int32]*tracked, eid int32) (x, y, z float64, dim int, ok bool) {
	if o := players[eid]; o != nil && !o.dead {
		return o.x, o.y, o.z, o.dim, true
	}
	if m := h.mobs[eid]; m != nil && m.dying == 0 {
		return m.x, m.y, m.z, m.dim, true
	}
	return 0, 0, 0, 0, false
}

// updateCameras keeps each spectating player with their camera entity, and
// brings the camera home when that entity is gone or the player stopped
// being a spectator.
func (h *hub) updateCameras(players map[int32]*tracked) {
	for _, t := range players {
		if t.camera == 0 {
			continue
		}
		x, y, z, dim, ok := h.cameraEntity(players, t.camera)
		if !ok || t.gamemode != gmSpectator || dim != t.dim {
			h.setCamera(t, 0)
			continue
		}
		if sq(t.x-x)+sq(t.z-z) > 4 { // keep the view's chunks under the entity
			h.teleportPlayer(players, t, x, y, z)
		}
	}
}

func (s *Server) cmdSpectate(p *player, args []string) {
	if !s.isOp(p.name) { // SpectateCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		h := s.hub
		caller := players[p.eid]
		if caller == nil {
			return
		}
		who := caller
		if len(args) >= 2 {
			ts := h.commandTargets(players, p.eid, args[1])
			if len(ts) != 1 {
				cmdFail(p, "No player was found")
				return
			}
			who = ts[0]
		}
		if len(args) == 0 {
			h.setCamera(who, 0)
			h.cmdSuccess(players, p, "No longer spectating an entity", true)
			return
		}
		es := h.commandEntities(players, p.eid, args[0])
		if len(es) != 1 {
			cmdFail(p, "No entity was found")
			return
		}
		target := es[0]
		if target.eid() == who.p.eid {
			cmdFail(p, "Cannot spectate yourself")
			return
		}
		if who.gamemode != gmSpectator {
			cmdFail(p, who.p.name+" is not in spectator mode")
			return
		}
		if target.dim() != who.dim {
			cmdFail(p, target.name()+" cannot be spectated")
			return
		}
		x, y, z := target.pos()
		h.teleportPlayer(players, who, x, y, z)
		h.setCamera(who, target.eid())
		h.cmdSuccess(players, p, "Now spectating "+target.name(), true)
	})
}

// spectatorAttack is ServerPlayer.attack for a spectator: the camera goes
// to what was clicked. Reports whether it handled the click.
func (h *hub) spectatorAttack(players map[int32]*tracked, t *tracked, target int32) bool {
	if t == nil || t.gamemode != gmSpectator {
		return false
	}
	if _, _, _, dim, ok := h.cameraEntity(players, target); ok && dim == t.dim {
		h.setCamera(t, target)
	}
	return true
}

// teleportToEntity is handleTeleportToEntity: a spectator picks someone from
// the hotbar menu and goes there.
func (h *hub) teleportToEntity(players map[int32]*tracked, t *tracked, u [16]byte) {
	if t == nil || t.gamemode != gmSpectator {
		return
	}
	for _, o := range players {
		if o.p.uuid == u && o.dim == t.dim {
			h.teleportPlayer(players, t, o.x, o.y, o.z)
			return
		}
	}
	for _, m := range h.mobs {
		if m.uuid == u && m.dim == t.dim {
			h.teleportPlayer(players, t, m.x, m.y, m.z)
			return
		}
	}
}

type evTeleportToEntity struct {
	eid  int32
	uuid [16]byte
}

func (evTeleportToEntity) isHubEvent() {}

// cmdTransfer is /transfer <hostname> [<port>] [<players>]: the named players
// are sent to another server (ClientboundTransferPacket).
func (s *Server) cmdTransfer(p *player, args []string) {
	if !s.isOp(p.name) { // TransferCommand: LEVEL_ADMINS
		p.tell("You don't have permission.")
		return
	}
	if len(args) == 0 {
		p.tell("Usage: /transfer <hostname> [<port>] [<players>]")
		return
	}
	host, port := args[0], 25565
	if len(args) >= 2 {
		v, err := strconv.Atoi(args[1])
		if err != nil || v < 1 || v > 65535 {
			cmdFail(p, "Integer must be between 1 and 65535, found "+args[1])
			return
		}
		port = v
	}
	s.onHub(func(players map[int32]*tracked) {
		h := s.hub
		var ts []*tracked
		if len(args) >= 3 {
			ts = h.commandTargets(players, p.eid, strings.Join(args[2:], " "))
		} else if t := players[p.eid]; t != nil {
			ts = []*tracked{t}
		}
		if len(ts) == 0 {
			cmdFail(p, "Must specify at least one player to transfer")
			return
		}
		for _, t := range ts {
			t.p.sendEv(attachproto.Transfer{Host: host, Port: int32(port)})
		}
		if len(ts) == 1 {
			h.cmdSuccess(players, p, fmt.Sprintf("Transferring %s to %s:%d", ts[0].p.name, host, port), true)
		} else {
			h.cmdSuccess(players, p, fmt.Sprintf("Transferring %d players to %s:%d", len(ts), host, port), true)
		}
	})
}
