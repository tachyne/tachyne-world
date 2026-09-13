package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Panda personalities (Panda's goals and tick): a worried panda sits out a
// thunderstorm and keeps eight blocks from players, a lazy one lies on its
// back now and then, a playful one — and every cub — rolls, off a ledge
// for certain and otherwise at its odds, and a weak cub sneezes far more.
// The four states ride the panda's flags byte, which the client animates.

const (
	pandaFlagSneeze byte = 2
	pandaFlagRoll   byte = 4
	pandaFlagSit    byte = 8
	pandaFlagOnBack byte = 16

	metaIndexPandaFlags = 22 // DATA_ID_FLAGS (byte); the ageable shift applies on 26.2

	pandaRollTicks   = 32  // handleRoll runs the tumble for 32 ticks
	pandaLieCooldown = 200 // ticks after getting up before it may lie down again
	pandaAvoidRange  = 8.0 // PandaAvoidGoal<Player>
)

// pandaFlagsMeta is the flags byte as metadata.
func pandaFlagsMeta(b []byte, m *mob) []byte {
	b = protocol.AppendU8(b, metaIndexPandaFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	return protocol.AppendU8(b, m.pandaFlags)
}

// setPandaFlag flips one flag and shows it.
func (h *hub) setPandaFlag(players map[int32]*tracked, m *mob, flag byte, on bool) {
	was := m.pandaFlags
	if on {
		m.pandaFlags |= flag
	} else {
		m.pandaFlags &^= flag
	}
	if m.pandaFlags != was {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(variantMeta(m)))
	}
}

// pandaScared is isScared: worried, and it is thundering.
func (h *hub) pandaScared(m *mob) bool {
	return pandaTrait(m.variant) == pandaWorried && h.thundering
}

// pandaCanAct is canPerformAction: no state has hold of it.
func (h *hub) pandaCanAct(m *mob) bool {
	return m.pandaFlags&(pandaFlagOnBack|pandaFlagRoll|pandaFlagSit) == 0 && !h.pandaScared(m)
}

// pandaStep runs a panda's personality each mob update (one vanilla goal
// tick). Returns whether a state holds the panda this update.
func (h *hub) pandaStep(players map[int32]*tracked, m *mob) bool {
	trait := pandaTrait(m.variant)
	now := h.tick.Load()
	if trait == pandaWorried {
		// Panda.tick: a thunderstorm sits it down (not in water); it stands
		// again when the storm passes.
		if h.thundering && !h.inWater(m.dim, m.x, m.y, m.z) {
			h.setPandaFlag(players, m, pandaFlagSit, true)
		} else if m.pandaFlags&pandaFlagSit != 0 {
			h.setPandaFlag(players, m, pandaFlagSit, false)
		}
		if m.pandaFlags&pandaFlagSit != 0 {
			m.vx, m.vz = 0, 0
			return true
		}
		// PandaAvoidGoal: a player within eight sends it off at 2.0× (the
		// panic branch's speed).
		if m.panic == 0 && m.kb == 0 && h.pandaCanAct(m) {
			if t := h.nearestHuntable(players, m.dim, m.x, m.z, pandaAvoidRange); t != nil {
				m.panic, m.fleeX, m.fleeZ = panicTicks/2, t.x, t.z
			}
		}
	}
	if m.pandaFlags&pandaFlagRoll != 0 {
		// handleRoll: the tumble carries it along its facing for 32 ticks.
		m.rollLeft--
		if m.rollLeft <= 0 {
			h.setPandaFlag(players, m, pandaFlagRoll, false)
			m.vx, m.vz = 0, 0
			return true
		}
		m.vx, m.vz = m.rollDX, m.rollDZ
		return true
	}
	if m.pandaFlags&pandaFlagOnBack != 0 {
		// PandaLieOnBackGoal.canContinueToUse: water ends it, a non-lazy
		// panda gets up at 1/300 a goal tick, any panda at 1/1000.
		up := h.inWater(m.dim, m.x, m.y, m.z) || (trait != pandaLazy && h.rng.Intn(300) == 0) || h.rng.Intn(1000) == 0
		if up {
			h.setPandaFlag(players, m, pandaFlagOnBack, false)
			m.lieCD = now + pandaLieCooldown
		}
		m.vx, m.vz = 0, 0
		return true
	}
	// PandaSitGoal (priority 7, ahead of lying and rolling): fetching,
	// sitting with, and eating bamboo or cake.
	if h.pandaSitEat(players, m, trait, now) {
		return true
	}
	if m.panic > 0 || m.kb > 0 || m.loveTicks > 0 {
		return false
	}
	// PandaLieOnBackGoal.canUse: lazy, past its cooldown, 1/200 a goal tick.
	if trait == pandaLazy && now >= m.lieCD && h.rng.Intn(200) == 0 {
		h.setPandaFlag(players, m, pandaFlagOnBack, true)
		return true
	}
	// PandaRollGoal.canUse: a cub or a playful adult on the ground rolls off
	// a ledge in front of it for certain, otherwise playful at 1/30 a goal
	// tick, any at 1/250.
	if (m.baby || trait == pandaPlayful) && m.grounded() {
		yaw := float64(m.yaw) * math.Pi / 180
		f2, f3 := -math.Sin(yaw), math.Cos(yaw)
		nx, nz := 0, 0
		if math.Abs(f2) > 0.5 {
			nx = int(math.Copysign(1, f2))
		}
		if math.Abs(f3) > 0.5 {
			nz = int(math.Copysign(1, f3))
		}
		w := h.worldFor(m.dim)
		edge := w.At(int(math.Floor(m.x))+nx, int(math.Floor(m.y))-1, int(math.Floor(m.z))+nz) == worldgen.Air
		if edge || (trait == pandaPlayful && h.rng.Intn(30) == 0) || h.rng.Intn(250) == 0 {
			speed := 0.2
			if m.baby {
				speed = 0.1
			}
			m.rollDX, m.rollDZ = f2*speed, f3*speed
			m.vx, m.vz = m.rollDX, m.rollDZ // rollCounter 1: the push that starts the tumble
			m.rollLeft = pandaRollTicks / mobMoveInterval
			h.setPandaFlag(players, m, pandaFlagRoll, true)
			return true
		}
	}
	return false
}
