package server

import "github.com/tachyne/tachyne-common/protocol"

// Tadpoles grow up (Tadpole.aiStep / setAge / feed): a tadpole counts its
// age every tick and at twenty-four thousand becomes a frog of the biome's
// kind; a slime ball fed to it takes a tenth of the time left off. Vexes
// (Vex.tick / VexChargeAttackGoal): a summoned vex past its life takes a
// point of magic damage every twenty ticks rather than vanishing, and its
// charging flag is raised while it goes for a target.

const (
	tadpoleTicksToFrog = 24000 // Tadpole.ticksToBeFrog
	metaIndexVexFlags  = 16    // Vex DATA_FLAGS_ID: 1 = charging
	vexLifeHurtEvery   = 20
)

// tadpoleTick ages a tadpole; at the mark it is a frog.
func (h *hub) tadpoleTick(players map[int32]*tracked, m *mob) {
	m.tadpoleAge += mobMoveInterval
	if m.tadpoleAge >= tadpoleTicksToFrog {
		h.tadpoleGrowUp(players, m)
	}
}

// tadpoleGrowUp is Tadpole.ageUp: a frog in its place, the variant from
// the biome (applySpecies rolls it), the health carried across.
func (h *hub) tadpoleGrowUp(players map[int32]*tracked, m *mob) {
	x, y, z, dim := m.x, m.y, m.z, m.dim
	hp := m.health
	h.removeMob(players, m)
	if f := h.spawnSpecies(players, entityFrog, dim, x, y, z); f != nil && hp < f.health {
		f.health = hp
	}
}

// feedTadpole is Tadpole.feed: a slime ball takes a tenth of the time left
// off (AgeableMob.getSpeedUpSecondsWhenFeeding), and the item goes.
func (h *hub) feedTadpole(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityTadpole || heldStack(t).item != itemSlimeball {
		return false
	}
	left := tadpoleTicksToFrog - m.tadpoleAge
	secs := int(float64(left/20) * 0.1)
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.spawnParticles(players, particleHappyVillager, m.x, m.y+0.5, m.z, 0.5, 0, 1)
	m.tadpoleAge += secs * 20
	if m.tadpoleAge >= tadpoleTicksToFrog {
		h.tadpoleGrowUp(players, m)
	}
	return true
}

func vexFlagsMeta(m *mob) []byte {
	var f byte
	if m.vexCharging {
		f = 1
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexVexFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, f)
	return protocol.AppendU8(b, itemMetaEnd)
}

// vexChargeTick keeps the charging flag in step with the hunt.
func (h *hub) vexChargeTick(players map[int32]*tracked, m *mob) {
	charging := m.hasTarget
	if charging == m.vexCharging {
		return
	}
	m.vexCharging = charging
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(vexFlagsMeta(m)))
}
