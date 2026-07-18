package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Warden behaviour on top of generic hostile chase/melee: a Darkness aura, a
// piercing sonic boom, and digging away (despawn) after a spell with no target.
// The Warden is blind — it homes on the nearest huntable player as a stand-in
// for vibration/scent tracking (a shrieker summons it near whoever roused it).
// Full anger management, sniffing, and the emerge/dig animations are deferred.

const effDarkness = 32 // mob_effect id (the Warden's dread)

const (
	wardenDarknessR  = 20.0 // players this close get the Darkness effect
	wardenSonicR     = 15.0 // sonic-boom range
	wardenSonicDmg   = 10.0 // sonic boom bypasses armour AND shields
	wardenSonicCD    = 30   // mob-updates between booms (~6 s at 2 ticks/update)
	wardenDigAwayUpd = 300  // mob-updates with no target before it digs away (~60 s)
)

// wardenTick runs once per mob update (every mobMoveInterval ticks) for a Warden.
func (h *hub) wardenTick(players map[int32]*tracked, m *mob) {
	// Darkness dread for everyone nearby (refreshed so it never lapses in range).
	for _, t := range players {
		if t.dim != m.dim {
			continue
		}
		if dx, dz := t.x-m.x, t.z-m.z; dx*dx+dz*dz < wardenDarknessR*wardenDarknessR {
			h.applyEffect(players, t, effDarkness, 0, 12)
		}
	}

	t := h.nearestHuntable(players, m.dim, m.x, m.z, 24)
	if t == nil {
		if m.digClock++; m.digClock >= wardenDigAwayUpd {
			h.despawnMob(players, m) // no prey for a minute: burrow back underground
		}
		return
	}
	m.digClock = 0
	if m.sonicCD > 0 {
		m.sonicCD--
		return
	}
	dx, dz := t.x-m.x, t.z-m.z
	if d2 := dx*dx + dz*dz; d2 > 4 && d2 < wardenSonicR*wardenSonicR { // 2..15 blocks
		m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		m.sonicCD = wardenSonicCD
		h.wardenSonicBoom(players, m, t)
	}
}

// wardenSonicBoom fires the piercing shriek: fixed damage that ignores armour
// and shields, plus knockback.
func (h *hub) wardenSonicBoom(players map[int32]*tracked, m *mob, t *tracked) {
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSound(players, "minecraft:entity.warden.sonic_boom", sndHostile, m.x, m.y, m.z, 3, 1)
	h.damage(players, t, wardenSonicDmg) // no armorReduce, no shield check — it pierces
	h.knockback(t, m.x, m.z)
	if t.dead {
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
		h.incStat(t, attachproto.StatKilledBy, int32(m.etype), 1)
	}
}
