package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// Pufferfish (Pufferfish.tick + PufferfishPuffGoal + playerTouch): anything
// that scares it within two blocks starts it inflating — half way at once,
// fully after forty ticks — and it deflates in two steps once left alone.
// Puffed, it stings whatever touches it: one damage plus its puff state,
// poison for three seconds per state, and the sting flash for a player.

const (
	metaIndexPuffState   = 17 // Pufferfish PUFF_STATE (int, after AbstractFish's FROM_BUCKET at 16)
	gameEventPufferSting = 9  // ClientboundGameEventPacket PUFFER_FISH_STING
	pufferScareRange     = 2.0
	pufferStingCooldown  = 10 // ticks between stings on one target (the hurt invulnerability)
)

// notScaryForPufferfish is #not_scary_for_pufferfish.
var notScaryForPufferfish = map[int]bool{
	entityTurtle: true, entityGuardian: true, entityElderGuardian: true, entityCod: true,
	entityPufferfish: true, entitySalmon: true, entityTropicalFish: true, entityDolphin: true,
	entitySquid: true, entityGlowSquid: true, entityTadpole: true,
}

func puffMeta(eid int32, state int8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexPuffState)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(state))
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) setPuff(players map[int32]*tracked, m *mob, state int8, sound string) {
	m.puff = state
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(puffMeta(m.eid, state)))
	h.playSoundDim(players, m.dim, sound, sndNeutral, m.x, m.y, m.z, 1, 1)
}

// pufferScared is PufferfishPuffGoal.canUse: a non-creative player or a mob
// not on the calm list within two blocks of its box.
func (h *hub) pufferScared(players map[int32]*tracked, m *mob) bool {
	near := func(x, y, z float64) bool {
		return math.Abs(x-m.x) <= pufferScareRange+0.35 && math.Abs(z-m.z) <= pufferScareRange+0.35 && y > m.y-pufferScareRange && y < m.y+pufferScareRange+0.35
	}
	for _, t := range players {
		if t.dim == m.dim && !t.dead && t.gamemode != gmCreative && t.gamemode != gmSpectator && near(t.x, t.y, t.z) {
			return true
		}
	}
	for _, o := range h.mobs {
		if o != m && o.dim == m.dim && o.dying == 0 && !notScaryForPufferfish[o.etype] && near(o.x, o.y, o.z) {
			return true
		}
	}
	return false
}

// pufferStep runs one mob update (mobMoveInterval ticks) of the fish's clock
// and stings.
func (h *hub) pufferStep(players map[int32]*tracked, m *mob) {
	if h.pufferScared(players, m) {
		if m.inflate == 0 {
			m.inflate, m.deflate = 1, 0
		}
	} else {
		m.inflate = 0
	}
	if m.inflate > 0 {
		if m.puff == 0 {
			h.setPuff(players, m, 1, "minecraft:entity.puffer_fish.blow_up")
		} else if m.inflate > 40 && m.puff == 1 {
			h.setPuff(players, m, 2, "minecraft:entity.puffer_fish.blow_up")
		}
		m.inflate += mobMoveInterval
	} else if m.puff != 0 {
		if m.deflate > 60 && m.puff == 2 {
			h.setPuff(players, m, 1, "minecraft:entity.puffer_fish.blow_out")
		} else if m.deflate > 100 && m.puff == 1 {
			h.setPuff(players, m, 0, "minecraft:entity.puffer_fish.blow_out")
		}
		m.deflate += mobMoveInterval
	}
	if m.stingCD > 0 {
		m.stingCD -= mobMoveInterval
	}
	if m.puff == 0 || m.stingCD > 0 {
		return
	}
	touching := func(x, y, z, w float64) bool {
		return math.Abs(x-m.x) < w && math.Abs(z-m.z) < w && y < m.y+0.8 && y+1.8 > m.y
	}
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator || !touching(t.x, t.y, t.z, 1.0) {
			continue
		}
		if h.hurtFrom(players, t, float32(1+m.puff), mobMeleeDamage(m.etype), deathCause{key: causeMob, by: mobDisplayName(m.etype)}, from(m.x, m.z)) {
			t.p.trySendEv(attachproto.GameEvent{Event: gameEventPufferSting})
			h.applyEffect(players, t, effPoison, 0, 3*int(m.puff))
			m.stingCD = pufferStingCooldown
		}
	}
	for _, o := range h.mobs {
		if o == m || o.dim != m.dim || o.dying > 0 || notScaryForPufferfish[o.etype] || !touching(o.x, o.y, o.z, 0.7) {
			continue
		}
		h.hurtMobOf(players, o, float64(1+m.puff), mobMeleeDamage(m.etype))
		h.applyMobEffect(players, o, effPoison, 0, 3*int(m.puff))
		h.playSoundDim(players, m.dim, "minecraft:entity.puffer_fish.sting", sndNeutral, m.x, m.y, m.z, 1, 1)
		m.stingCD = pufferStingCooldown
	}
}
