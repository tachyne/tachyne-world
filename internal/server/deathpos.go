package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The last death location (ServerPlayer.lastDeathLocation): set when a
// player dies, carried on the login and respawn packets, and what the
// recovery compass points at. It lives in the inventory store so it
// survives restarts with the rest of the player's record.

// recordDeath notes where t died.
func (h *hub) recordDeath(t *tracked) {
	if h.invs == nil {
		return
	}
	h.invs.setDeath(t.p.name, attachproto.DeathPos{Dim: int32(t.dim),
		X: int32(math.Floor(t.x)), Y: int32(math.Floor(t.y)), Z: int32(math.Floor(t.z))})
}

// deathOf is name's last death location, nil if unknown.
func (h *hub) deathOf(name string) *attachproto.DeathPos {
	if h.invs == nil {
		return nil
	}
	return h.invs.death(name)
}

// deathOf on the server side (session goroutines): the store is mutexed.
func (s *Server) deathOf(name string) *attachproto.DeathPos {
	return s.hub.deathOf(name)
}
