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

// isTameFood reports whether item tames the species: a bone for wolves,
// #cat_food / #ocelot_food (cod or salmon) for cats and ocelots, any
// #parrot_food seed for parrots.
func isTameFood(etype int, item int32) bool {
	switch etype {
	case entityWolf:
		return item == itemBone
	case entityCat, entityOcelot, entityParrot:
		return isLoveFood(etype, item)
	}
	return false
}

// tameable reports whether the species can be tamed by hand at all.
func tameable(etype int) bool {
	switch etype {
	case entityWolf, entityCat, entityOcelot, entityParrot:
		return true
	}
	return false
}

// wolfTamedHealth is a tamed wolf's MAX_HEALTH (a wild one has 8).
const wolfTamedHealth = 40

// tameOdds is the 1-in-N chance a single feeding tames the animal (vanilla 1/3).
const tameOdds = 3

// tryTame handles a right-click on a tameable mob. Returns true if consumed.
func (h *hub) tryTame(players map[int32]*tracked, t *tracked, m *mob) bool {
	if !tameable(m.etype) || m.dying > 0 {
		return false
	}
	if m.tamed {
		// A dye in the owner's hand recolours the collar (Wolf/Cat.mobInteract);
		// the same colour again does nothing.
		if dye, ok := dyeOrdinalByItem[heldStack(t).item]; ok && m.owner == t.p.eid && (m.etype == entityWolf || m.etype == entityCat) {
			if dye == m.collar {
				return false
			}
			m.collar = dye
			if t.gamemode == gmSurvival {
				h.consumeHeld(t)
			}
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(variantMeta(m)))
			return true
		}
		// The owner toggles sit/stand with an empty hand; anyone else is ignored.
		if m.owner != t.p.eid || heldStack(t).item != 0 {
			return false
		}
		m.sitting = !m.sitting
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(petMeta(m)))
		return true
	}
	if !isTameFood(m.etype, heldStack(t).item) {
		return false
	}
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	if h.rng.Intn(tameOdds) != 0 { // didn't take this time
		h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameFail))
		return true
	}
	m.tamed, m.owner, m.ownerUUID = true, t.p.eid, t.p.uuid
	m.collar = collarDefault
	if m.etype == entityWolf { // Wolf.applyTamingSideEffects: 8 → 40 max, healed to full
		m.setMaxHP(wolfTamedHealth)
		m.health = wolfTamedHealth
	}
	m.hostile, m.neutral, m.retaliates = false, false, false // a pet no longer hunts on its own
	m.behavior = Behavior(hostileBehavior{})                 // …it "hunts" the owner to follow
	m.setFollowRange(petFollowStart)
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameOK))
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(petMeta(m)))
	if vm := variantMeta(m); vm != nil {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(vm)) // the collar appears with the tame
	}
	h.advance(players, t, "tame_animal", advMatch{entity: advEntityName[m.etype]})
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
		h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		m.hasTarget = false
	case d > petFollowStart:
		m.hasTarget, m.tx, m.tz = true, owner.x, owner.z
	case d < petFollowStop:
		m.hasTarget = false // close enough — mill around
	}
	return false
}

// petMeta picks the right taming metadata for the species: ocelots are NOT
// TamableAnimal in vanilla — their index-17 field is the Boolean TRUSTING
// (writing the tamable flags BYTE there is a type-mismatch disconnect); every
// real pet gets the TamableAnimal flags byte.
func petMeta(m *mob) []byte {
	if m.etype == entityOcelot {
		return boolMeta(m.eid, 17, true)
	}
	return petFlagsMeta(m.eid, true, m.sitting)
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
