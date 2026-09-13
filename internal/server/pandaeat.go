package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Pandas sit and eat (PandaSitGoal + Panda.handleEating): an adult panda
// that spots bamboo or a cake lying within eight blocks trots over at 1.2,
// picks it up, sits down with it, and after a while starts chewing — the
// eat counter climbing, a munch every five counts — until, past a hundred
// counts, the food is gone and it gets up. A panda that loses interest
// (one chance in two thousand a tick, a non-lazy one also one in six
// hundred) or is hurt drops what it holds and waits ten seconds to a few
// minutes before it eats again.

const (
	metaIndexPandaEat = 19  // EAT_COUNTER (int); the ageable shift applies on 26.2
	pandaEatSearch    = 8.0 // start(): items within 8 (canUse looks 6)
	pandaEatSpeed     = 1.2
	pandaEatReach     = 1.5
	pandaEatBored     = 2000 // canContinueToUse: 1/2000 a tick ends it
	pandaEatBoredBusy = 600  // a non-lazy panda: 1/600 too
)

// pandaEatsFromGround is #panda_eats_from_ground.
func pandaEatsFromGround(item int32) bool {
	return item != 0 && (item == itemByName["bamboo"] || item == itemByName["cake"])
}

// pandaEatMeta is the EAT_COUNTER entry.
func pandaEatMeta(m *mob) []byte {
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexPandaEat)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(m.pandaEat))
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) setPandaEat(players map[int32]*tracked, m *mob, n int) {
	was := m.pandaEat
	m.pandaEat = n
	if (was == 0) != (n == 0) || n%10 == 0 {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(pandaEatMeta(m)))
	}
}

// pandaSitEat is the goal's tick, run from pandaStep. Returns whether it
// holds the panda this update.
func (h *hub) pandaSitEat(players map[int32]*tracked, m *mob, trait int32, now uint64) bool {
	sitting := m.pandaFlags&pandaFlagSit != 0
	if m.held != 0 && pandaEatsFromGround(m.held) {
		// Hurt, in water, or bored: stop() drops the food and sets the
		// cooldown; a panda not yet sat down sits (tryToSit).
		bored := h.rng.Intn(pandaEatBored/mobMoveInterval) == 0 || (trait != pandaLazy && h.rng.Intn(pandaEatBoredBusy/mobMoveInterval) == 0)
		if m.kb > 0 || h.inWater(m.dim, m.x, m.y, m.z) || bored {
			h.pandaEatStop(players, m, trait, now)
			return false
		}
		if !sitting {
			h.setPandaFlag(players, m, pandaFlagSit, true)
			m.vx, m.vz = 0, 0
			return true
		}
		// handleEating.
		if m.pandaEat == 0 {
			if !h.pandaScared(m) && h.rng.Intn(80/mobMoveInterval) == 0 {
				h.setPandaEat(players, m, 1)
			}
		} else {
			if m.pandaEat%5 < mobMoveInterval {
				h.playSoundDim(players, m.dim, "minecraft:entity.panda.eat", sndNeutral, m.x, m.y, m.z, 0.5+0.5*float32(h.rng.Intn(2)), (h.rng.Float32()-h.rng.Float32())*0.2+1)
			}
			if m.pandaEat > 80 && h.rng.Intn(20/mobMoveInterval) == 0 {
				if m.pandaEat > 100 {
					m.held = 0 // eaten
					h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
					h.setPandaFlag(players, m, pandaFlagSit, false)
				}
				h.setPandaEat(players, m, 0)
			} else {
				h.setPandaEat(players, m, m.pandaEat+mobMoveInterval)
			}
		}
		m.vx, m.vz = 0, 0
		return true
	}
	if m.pandaEat != 0 {
		h.setPandaEat(players, m, 0) // eat(false): nothing in hand
	}
	// canUse: past the cooldown, an adult, dry, free to act, food about.
	if now < m.pandaSitCD || m.baby || m.held != 0 || h.inWater(m.dim, m.x, m.y, m.z) || !h.pandaCanAct(m) {
		return false
	}
	var it *itemEntity
	bestD := pandaEatSearch
	for _, cand := range h.items {
		if cand.dim != m.dim || cand.count <= 0 || now < cand.noPickupUntil || !pandaEatsFromGround(cand.item) {
			continue
		}
		if d := dist3(cand.x, cand.y, cand.z, m.x, m.y, m.z); d < bestD {
			it, bestD = cand, d
		}
	}
	if it == nil {
		return false
	}
	if bestD > pandaEatReach {
		h.steerTo(m, it.x, it.z, pandaEatSpeed)
		m.rest = 0
		return true
	}
	// pickUpItem: the whole stack goes into the mouth (a guaranteed drop).
	m.held = it.item
	delete(h.items, it.eid)
	h.toNearbyEv(players, it.dim, it.x, it.z, entGone(it.eid))
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: 1}, invStack{}, m.gear))
	h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
	m.persistent = true
	h.setPandaFlag(players, m, pandaFlagSit, true)
	m.vx, m.vz = 0, 0
	return true
}

// pandaEatStop is stop(): the food is dropped, the cooldown set, the
// panda stands.
func (h *hub) pandaEatStop(players map[int32]*tracked, m *mob, trait int32, now uint64) {
	if m.held != 0 {
		h.spawnItemIn(players, m.dim, m.held, 1, m.x, m.y+0.5, m.z)
		m.held = 0
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
		wait := h.rng.Intn(150) + 10
		if trait == pandaLazy {
			wait = h.rng.Intn(50) + 10
		}
		m.pandaSitCD = now + uint64(wait*20)
	}
	h.setPandaEat(players, m, 0)
	h.setPandaFlag(players, m, pandaFlagSit, false)
}
