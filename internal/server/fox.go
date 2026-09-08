package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Foxes (vanilla Fox): by day, sheltered from the sky and with nobody
// about, a fox lies down and sleeps; it hunts chickens, rabbits, baby
// turtles on land and schooling fish, crouching in as it stalks; and it
// picks up whatever is lying around, carrying it in its mouth — food it
// eats after half a minute, the rest it keeps (and drops when it dies).

const (
	foxFlagCrouching  = 0x04
	foxFlagInterested = 0x08
	foxFlagSleeping   = 0x20
	metaIndexFoxFlags = 18 // DATA_FLAGS_ID, after the variant at 17
	foxLandRange      = 10.0
	foxFishRange      = 20.0
	foxItemRange      = 8.0
	foxBiteRange      = 2.0
	foxStalkRange     = 6.0 // beyond this it creeps
	foxEatAfter       = 600 // MIN_TICKS_BEFORE_EAT
	foxSleepWait      = 140 // WAIT_TIME_BEFORE_SLEEP
	foxAlertRange     = 12.0
	foxHuntSpeed      = 1.2
	foxBiteEvery      = 20 // a bite a second, the melee cadence
	metaTypeByteFox   = 0  // metadata value type: byte
)

func foxFlagsMeta(eid int32, flags int8) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexFoxFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = append(b, byte(flags))
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) foxSetFlags(players map[int32]*tracked, m *mob, flags int8) {
	if m.foxFlags == flags {
		return
	}
	m.foxFlags = flags
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(foxFlagsMeta(m.eid, flags)))
}

// foxPrey: what a fox hunts, and from how far.
func foxPreyRange(o *mob) float64 {
	switch o.etype {
	case entityChicken, entityRabbit:
		return foxLandRange
	case entityCod, entitySalmon, entityTropicalFish:
		return foxFishRange
	case entityTurtle:
		if o.baby {
			return foxLandRange
		}
	}
	return 0
}

// foxAlertable: a survival player about, not sneaking (Fox.alertable).
func (h *hub) foxAlertable(players map[int32]*tracked, m *mob) bool {
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator || t.p.sneaking {
			continue
		}
		if math.Hypot(t.x-m.x, t.z-m.z) <= foxAlertRange {
			return true
		}
	}
	return false
}

// foxStep is a fox's own AI. Returns true while it owns the movement.
func (h *hub) foxStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	sleeping := m.foxFlags&foxFlagSleeping != 0
	sheltered := !h.skyOpen(m.dim, m.x, m.y+1, m.z)
	quiet := h.isDayTime() && sheltered && !h.foxAlertable(players, m) && m.panic == 0 && m.loveTicks == 0
	if sleeping {
		if !quiet {
			h.foxSetFlags(players, m, m.foxFlags&^foxFlagSleeping) // Fox.wakeUp
			m.foxSleepIn = h.rng.Intn(foxSleepWait)
		}
		m.vx, m.vz = 0, 0
		return true
	}
	// Eating what it carries.
	if m.held != 0 && foodPoints[m.held] > 0 {
		m.foxEatTicks += mobMoveInterval
		if m.foxEatTicks > foxEatAfter {
			m.held, m.foxEatTicks = 0, 0
			h.playSoundDim(players, m.dim, "minecraft:entity.fox.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
		} else if m.foxEatTicks > foxEatAfter-40 && h.rng.Intn(10) == 0 {
			h.playSoundDim(players, m.dim, "minecraft:entity.fox.eat", sndNeutral, m.x, m.y, m.z, 1, 1)
		}
	}
	// Prey within reach: stalk, then bite.
	var prey *mob
	best := foxFishRange
	h.grid().nearby(m.dim, m.x, m.z, foxFishRange, func(o *mob) {
		if o == m || o.dying > 0 {
			return
		}
		r := foxPreyRange(o)
		if r == 0 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d <= r && d < best {
			prey, best = o, d
		}
	})
	if prey != nil && !m.baby {
		if best <= foxBiteRange {
			h.foxSetFlags(players, m, m.foxFlags&^(foxFlagCrouching|foxFlagInterested))
			if now%foxBiteEvery < uint64(mobMoveInterval) {
				prey.lastAttacker = m.eid
				prey.hurtKind(float64(m.attackDamage()), dtMobAttack)
				h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
				if prey.health <= 0 {
					h.killMob(players, prey)
				}
			}
			m.vx, m.vz = 0, 0
			return true
		}
		flags := m.foxFlags &^ (foxFlagCrouching | foxFlagInterested)
		if best > foxStalkRange {
			flags |= foxFlagCrouching | foxFlagInterested // StalkPreyGoal: creeping in
		}
		h.foxSetFlags(players, m, flags)
		h.steerTo(m, prey.x, prey.z, foxHuntSpeed)
		return true
	}
	h.foxSetFlags(players, m, m.foxFlags&^(foxFlagCrouching|foxFlagInterested))
	// Nothing in its mouth: anything lying about within eight blocks.
	if m.held == 0 {
		var it *itemEntity
		bestD := foxItemRange
		for _, cand := range h.items {
			if cand.dim != m.dim || cand.count <= 0 || now < cand.noPickupUntil {
				continue
			}
			if d := dist3(cand.x, cand.y, cand.z, m.x, m.y, m.z); d < bestD {
				it, bestD = cand, d
			}
		}
		if it != nil {
			if bestD <= 1.5 {
				m.held, m.foxEatTicks = it.item, 0
				if it.count--; it.count <= 0 {
					delete(h.items, it.eid)
					h.toNearbyEv(players, it.dim, it.x, it.z, entGone(it.eid))
				} else {
					h.refreshItemMeta(players, it)
				}
				h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: 1}, invStack{}, m.gear))
				h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
				m.persistent = true
				return true
			}
			h.steerTo(m, it.x, it.z, 1)
			return true
		}
	}
	// Sleep: a quiet, sheltered day.
	if quiet {
		if m.foxSleepIn > 0 {
			m.foxSleepIn -= mobMoveInterval
			return false
		}
		h.foxSetFlags(players, m, m.foxFlags|foxFlagSleeping)
		m.vx, m.vz = 0, 0
		return true
	}
	m.foxSleepIn = h.rng.Intn(foxSleepWait)
	return false
}

// steerTo sets a walker's velocity straight at (x, z) at a speed multiple.
func (h *hub) steerTo(m *mob, x, z float64, speed float64) {
	dx, dz := x-m.x, z-m.z
	if hd := math.Hypot(dx, dz); hd > 1e-6 {
		sp := m.moveSpeed() * speed
		m.vx, m.vz = dx/hd*sp, dz/hd*sp
	}
	m.rest = 0
}
