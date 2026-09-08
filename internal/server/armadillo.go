package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Armadillos (vanilla Armadillo + ArmadilloAi.ArmadilloBallUp): every four
// seconds an armadillo looks for a threat within seven blocks sideways and
// two up or down — an undead mob, whoever last hurt it, or a player
// sprinting or riding — and rolls up when it finds one: ten ticks of
// rolling, then scared for as long as the danger memory (four seconds per
// sighting) lasts, then thirty ticks of unrolling. Rolled up it holds
// still, cannot breed, and a blow loses a point and halves. A blow from a
// living thing is danger in itself. A grown one drops a scute every five to
// ten minutes.

const (
	armIdle      = 0
	armRolling   = 1
	armScared    = 2
	armUnrolling = 3

	armRollingTicks   = 10 // ArmadilloState.ROLLING.animationDuration
	armUnrollingTicks = 30 // …UNROLLING
	armScareInterval  = 80 // SCARE_CHECK_INTERVAL
	armDangerTicks    = 80 // DANGER_DETECTED_RECENTLY
	armScareXZ        = 7.0
	armScareY         = 2.0
	metaIndexArmState = 17 // DATA_STATE on 1.21.5 (the gateway shifts it for 26.2)
	armScuteMinTicks  = 20 * 60 * 5
)

// armadilloStateMeta is one DATA_STATE entry (ARMADILLO_STATE serializer).
func armadilloStateMeta(eid int32, state int8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexArmState)
	b = protocol.AppendVarInt(b, protocol.ArmadilloStateSerializer770)
	b = protocol.AppendVarInt(b, int32(state))
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) armadilloSetState(players map[int32]*tracked, m *mob, state int8) {
	if m.armState == state {
		return
	}
	m.armState, m.armStateAt = state, h.tick.Load()
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(armadilloStateMeta(m.eid, state)))
	switch state {
	case armRolling:
		h.playSoundDim(players, m.dim, "minecraft:entity.armadillo.roll", sndNeutral, m.x, m.y, m.z, 1, 1)
	case armScared:
		h.playSoundDim(players, m.dim, "minecraft:entity.armadillo.land", sndNeutral, m.x, m.y, m.z, 1, 1)
	case armUnrolling:
		h.playSoundDim(players, m.dim, "minecraft:entity.armadillo.unroll_start", sndNeutral, m.x, m.y, m.z, 1, 1)
	case armIdle:
		h.playSoundDim(players, m.dim, "minecraft:entity.armadillo.unroll_finish", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
}

// armadilloScaredBy is Armadillo.isScaredBy for players.
func armadilloScaredBy(m *mob, t *tracked) bool {
	if t.gamemode == gmSpectator || t.dead || t.dim != m.dim {
		return false
	}
	if abs64(t.x-m.x) > armScareXZ || abs64(t.z-m.z) > armScareXZ || abs64(t.y-m.y) > armScareY {
		return false
	}
	return t.sprinting || t.ridingEID != 0 || m.lastAttacker == t.p.eid
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// armadilloDanger is the scare check: any threat within reach.
func (h *hub) armadilloDanger(players map[int32]*tracked, m *mob) bool {
	for _, t := range players {
		if armadilloScaredBy(m, t) {
			return true
		}
	}
	danger := false
	h.grid().nearby(m.dim, m.x, m.z, armScareXZ, func(o *mob) {
		if danger || o == m || o.dying > 0 || abs64(o.y-m.y) > armScareY {
			return
		}
		if undeadTypes[o.etype] || o.eid == m.lastAttacker {
			danger = true
		}
	})
	return danger
}

// armadilloTick runs the state machine one update.
func (h *hub) armadilloTick(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	// Scutes: a grown armadillo sheds one every five to ten minutes.
	if !m.baby {
		if m.armScuteAt == 0 {
			m.armScuteAt = now + armScuteMinTicks + uint64(h.rng.Intn(armScuteMinTicks))
		} else if now >= m.armScuteAt {
			m.armScuteAt = now + armScuteMinTicks + uint64(h.rng.Intn(armScuteMinTicks))
			if h.rules.DoMobLoot {
				h.spawnItemIn(players, m.dim, itemArmadilloScute, 1, m.x, m.y+0.5, m.z)
				h.playSoundDim(players, m.dim, "minecraft:entity.armadillo.scute_drop", sndNeutral, m.x, m.y, m.z, 1, 1)
			}
		}
	}
	if now%armScareInterval < uint64(mobMoveInterval) && h.armadilloDanger(players, m) {
		m.armDangerUntil = now + armDangerTicks
		if m.armState == armIdle && !m.baby {
			h.armadilloSetState(players, m, armRolling)
		}
	}
	switch m.armState {
	case armRolling:
		if now-m.armStateAt >= armRollingTicks {
			h.armadilloSetState(players, m, armScared)
		}
	case armScared:
		if m.armDangerUntil < now+armUnrollingTicks {
			h.armadilloSetState(players, m, armUnrolling)
		}
	case armUnrolling:
		if m.armDangerUntil >= now+armUnrollingTicks {
			h.armadilloSetState(players, m, armScared) // danger again: back in
		} else if now-m.armStateAt >= armUnrollingTicks {
			h.armadilloSetState(players, m, armIdle)
		}
	}
}

// armadilloHurtByLiving is the roll-up on a blow from a living thing.
func (h *hub) armadilloHurtByLiving(players map[int32]*tracked, m *mob) {
	m.armDangerUntil = h.tick.Load() + armDangerTicks
	if !m.baby && m.armState == armIdle {
		h.armadilloSetState(players, m, armRolling)
	}
}
