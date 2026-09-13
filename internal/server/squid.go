package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Squid (Squid.hurtServer + spawnInk + SquidFleeGoal, GlowSquid dark
// ticks) and endermites (ThrownEnderpearl.onHit, Endermite.tick): a squid
// hurt by something squirts a cloud of thirty ink particles and jets away
// from its attacker at up to three blocks a second for as long as it is
// within ten; a glow squid also goes dark for a hundred ticks. One ender
// pearl in twenty lands an endermite, which lives two minutes unless
// something keeps it.

const (
	particleSquidInk     = 63 // canonical 770 ids; the chain renumbers per version
	particleGlowSquidInk = 96
	metaIndexGlowDark    = 17 // GlowSquid DATA_DARK_TICKS_REMAINING (1.21.5; 18 on 26.2)
	glowDarkTicks        = 100
	squidFleeSpeed       = 3.0  // SQUID_FLEE_SPEED, per second
	squidFleeMin         = 5.0  // full speed inside five
	squidFleeMax         = 10.0 // the goal runs while the attacker is within ten
	squidInkCount        = 30
	endermitePearlOdds   = 0.05 // ThrownEnderpearl.onHit: nextFloat() < 0.05
	endermiteLife        = 2400 // Endermite MAX_LIFE
)

func glowDarkMeta(m *mob) []byte {
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexGlowDark)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(m.glowDark))
	return protocol.AppendU8(b, itemMetaEnd)
}

// squidInk is spawnInk: the squirt and the cloud, downward from the body.
func (h *hub) squidInk(players map[int32]*tracked, m *mob) {
	pid := int32(particleSquidInk)
	sound := "minecraft:entity.squid.squirt"
	if m.etype == entityGlowSquid {
		pid, sound = particleGlowSquidInk, "minecraft:entity.glow_squid.squirt"
	}
	h.playSoundDim(players, m.dim, sound, sndNeutral, m.x, m.y, m.z, 1, 1)
	spread := float32(0.3)
	if m.baby {
		spread = 0.1
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Particles{PID: pid, X: m.x, Y: m.y + 0.5, Z: m.z, Spread: spread, Speed: 0.6, Count: squidInkCount})
}

// squidStep runs each mob update. Returns whether it holds the squid.
func (h *hub) squidStep(players map[int32]*tracked, m *mob) bool {
	if m.glowDark > 0 {
		m.glowDark -= mobMoveInterval
		if m.glowDark <= 0 {
			m.glowDark = 0
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(glowDarkMeta(m)))
		}
	}
	if m.squidHurt {
		m.squidHurt = false
		h.squidInk(players, m)
		if m.etype == entityGlowSquid {
			m.glowDark = glowDarkTicks
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(glowDarkMeta(m)))
		}
	}
	// SquidFleeGoal: away from whoever hurt it last while they are within ten.
	if m.lastAttacker == 0 || !h.inWater(m.dim, m.x, m.y, m.z) {
		return false
	}
	var ax, ay, az float64
	if t := players[m.lastAttacker]; t != nil && t.dim == m.dim {
		ax, ay, az = t.x, t.y, t.z
	} else if o := h.mobs[m.lastAttacker]; o != nil && o.dim == m.dim {
		ax, ay, az = o.x, o.y, o.z
	} else {
		return false
	}
	dx, dy, dz := m.x-ax, m.y-ay, m.z-az
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d*d >= squidFleeMax*squidFleeMax || d < 1e-6 {
		return false
	}
	w := h.worldFor(m.dim)
	ahead := w.At(int(math.Floor(m.x+dx)), int(math.Floor(m.y+dy)), int(math.Floor(m.z+dz)))
	if !worldgen.IsWater(ahead) && ahead != worldgen.Air {
		return false
	}
	d2 := squidFleeSpeed
	if d > squidFleeMin {
		d2 -= (d - squidFleeMin) / squidFleeMin
	}
	if d2 <= 0 {
		return false
	}
	dx, dy, dz = dx/d*d2, dy/d*d2, dz/d*d2
	if ahead == worldgen.Air {
		dy = 0
	}
	// movementVector = vec / 20 per tick, two ticks an update.
	m.vx, m.vy, m.vz = dx/20*mobMoveInterval, dy/20*mobMoveInterval, dz/20*mobMoveInterval
	m.rest = 0
	if h.tick.Load()%10 == 5 {
		h.spawnParticles(players, particleBubble, m.x, m.y, m.z, 0, 0, 1)
	}
	return true
}

// endermiteTick is Endermite.tick's life: two minutes unless kept.
func (h *hub) endermiteTick(players map[int32]*tracked, m *mob) {
	if m.persistent {
		return
	}
	m.endermiteLife += mobMoveInterval
	if m.endermiteLife >= endermiteLife {
		h.removeMob(players, m)
	}
}

// pearlEndermite is the one-in-twenty endermite where a pearl lands.
func (h *hub) pearlEndermite(players map[int32]*tracked, a *arrowEntity) {
	if !h.rules.DoMobSpawning || h.rng.Float64() >= endermitePearlOdds {
		return
	}
	h.spawnMobIn(players, entityEndermite, a.dim, a.x, float64(h.worldFor(a.dim).DropY(int(math.Floor(a.x)), int(math.Ceil(a.y)), int(math.Floor(a.z)))), a.z)
}
