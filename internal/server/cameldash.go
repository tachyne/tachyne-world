package server

// Camel dash (Camel.handleStartJump): the riding client sends
// START_RIDING_JUMP when the jump key is released and applies the dash
// impulse itself (executeRidersJump in tickRidden); the server's half is the
// dash sound, the DASH synced flag the animation keys off, and the cooldown
// that gates the next one. The flag drops once the camel is back on the
// ground with the cooldown under fifty (Camel.tick).

const (
	metaIndexCamelDash = 18 // Camel DASH (bool); the ageable shift applies on 26.2
	camelDashCooldown  = 55 // DASH_COOLDOWN_TICKS
	camelDashFlagOff   = 50 // the flag drops once the cooldown is under this (and the camel is down)
)

type evRidingJump struct{ eid int32 }

func (evRidingJump) isHubEvent() {}

// camelDashStart is handleStartJump for the camel the player rides.
func (h *hub) camelDashStart(players map[int32]*tracked, t *tracked) {
	m := h.mobs[t.ridingEID]
	if m == nil || m.etype != entityCamel || m.dying > 0 || !m.saddled || m.dashCD > 0 {
		return
	}
	m.dashCD, m.dashing = camelDashCooldown, true
	h.playSoundDim(players, m.dim, "minecraft:entity.camel.dash", sndNeutral, m.x, m.y, m.z, 1, 1)
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexCamelDash, true)))
}

// camelDashTick runs the cooldown (one mob update = mobMoveInterval ticks)
// and drops the flag once it is under fifty.
func (h *hub) camelDashTick(players map[int32]*tracked, m *mob) {
	if m.dashCD == 0 {
		return
	}
	if m.dashCD -= mobMoveInterval; m.dashCD < 0 {
		m.dashCD = 0
	}
	if m.dashing && m.dashCD < camelDashFlagOff {
		m.dashing = false
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexCamelDash, false)))
	}
}
