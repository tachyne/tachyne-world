package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Tameable companions. Wolves (bones), cats (fish) and parrots (seeds) can be
// tamed; a tamed pet follows its owner, sits on an empty-handed right-click,
// and teleports to the owner when left too far behind. Taming reuses the mob
// targeting + steering pipeline: a following pet simply hunts the owner's
// position (hostileBehavior toward tx/tz) at a friendly stand-off.

const (
	entityStatusTameFail = 6 // smoke puff: taming didn't take
	entityStatusTameOK   = 7 // hearts: tamed!

	petFollowStart = 10.0 // start walking to the owner past this…
	petFollowStop  = 3.0  // …and stop within this
	petTeleport    = 20.0 // farther than this: blink to the owner (vanilla pets do)

	metaIndexTameFlags = 17 // TamableAnimal flags byte: 0x01 sitting, 0x04 tamed
)

// tameFood maps a tameable species to the item that tames it.
func tameFood(etype int) int32 {
	switch etype {
	case entityWolf:
		return itemBone
	case entityCat, entityOcelot:
		return itemByName["cod"]
	case entityParrot:
		return itemWheatSeeds
	}
	return 0
}

// tameOdds is the 1-in-N chance a single feeding tames the animal (vanilla 1/3).
const tameOdds = 3

// tryTame handles a right-click on a tameable mob. Returns true if consumed.
func (h *hub) tryTame(players map[int32]*tracked, t *tracked, m *mob) bool {
	food := tameFood(m.etype)
	if food == 0 || m.dying > 0 {
		return false
	}
	if m.tamed {
		// The owner toggles sit/stand with an empty hand; anyone else is ignored.
		if m.owner != t.p.eid || heldStack(t).item != 0 {
			return false
		}
		m.sitting = !m.sitting
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(petFlagsMeta(m.eid, true, m.sitting)))
		return true
	}
	if heldStack(t).item != food {
		return false
	}
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	if h.rng.Intn(tameOdds) != 0 { // didn't take this time
		h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameFail))
		return true
	}
	m.tamed, m.owner = true, t.p.eid
	m.hostile, m.neutral, m.retaliates = false, false, false // a pet no longer hunts on its own
	m.behavior = Behavior(hostileBehavior{})                 // …it "hunts" the owner to follow
	m.aggro = petFollowStart
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameOK))
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(petFlagsMeta(m.eid, true, false)))
	return true
}

// petAcquire steers a tamed pet toward its owner: it targets the owner's
// position when they wander off, stops when close, and teleports to them if
// left too far behind. Returns true if the pet is sitting (caller holds it
// still).
func (h *hub) petAcquire(players map[int32]*tracked, m *mob) bool {
	if m.sitting {
		m.hasTarget = false
		return true
	}
	owner := players[m.owner]
	if owner == nil || owner.dim != m.dim {
		m.hasTarget = false
		return false
	}
	d := math.Hypot(owner.x-m.x, owner.z-m.z)
	switch {
	case d > petTeleport: // blink to the owner's side (vanilla pet teleport)
		m.x, m.z = owner.x+1, owner.z
		m.y = float64(h.worldFor(m.dim).MobFeet(int(math.Floor(m.x)), int(math.Floor(m.z))))
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, true))
		m.hasTarget = false
	case d > petFollowStart:
		m.hasTarget, m.tx, m.tz = true, owner.x, owner.z
	case d < petFollowStop:
		m.hasTarget = false // close enough — mill around
	}
	return false
}

// petFlagsMeta builds the TamableAnimal flags byte (tamed + sitting bits).
func petFlagsMeta(eid int32, tamed, sitting bool) []byte {
	var flags byte
	if sitting {
		flags |= 0x01
	}
	if tamed {
		flags |= 0x04
	}
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexTameFlags)
	b = protocol.AppendVarInt(b, 0) // type 0: byte
	b = protocol.AppendU8(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}
