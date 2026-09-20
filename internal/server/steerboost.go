package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// Boosting a food-on-a-stick mount. Holding a carrot on a stick aims a pig and
// a warped fungus on a stick a strider (tempt.go); USING one is the sprint.
// FoodOnAStickItem.use hands the mount's ItemBasedSteering a fresh boost — a
// rolled number of ticks over which the ridden speed swells and falls away
// again — and the stick pays for it in durability, turning back into the
// fishing rod it was built on once it is spent.
//
// The swell itself is the client's to draw: a ridden mount is client-driven
// (applyMountMove), and the client starts its own copy of the steering the
// moment DATA_BOOST_TIME arrives. The engine keeps the same clock because a
// sprint already running refuses a second one, and that refusal is the whole
// reason a rider cannot spend the stick twice in a row.

const (
	// metaIndexBoostTime is Pig/Strider DATA_BOOST_TIME (VarInt), the first
	// species field after AgeableMob's baby flag. Both are animals, so the
	// gateway's ageable shift moves it to 18 for a 26.2 client.
	metaIndexBoostTime = 17

	// ItemBasedSteering.boost rolls rng.nextInt(841) + 140 — seven seconds to
	// forty-nine, whatever the stick.
	boostTimeMin  = 140
	boostTimeSpan = 841

	// boostPeak is the height of the swell: ridden speed reaches 2.15× at the
	// halfway point of the roll.
	boostPeak = 1.15
)

// steerWear is FoodOnAStickItem's consumeItemDamage. A carrot on a stick
// spends seven of its twenty-five points per boost, a warped fungus one of its
// hundred — three sprints out of a pig's stick, a hundred out of a strider's.
var steerWear = map[int32]int{itemCarrotOnStick: 7, itemWarpedFungusStick: 1}

// evSteerBoost: a rider used the food on a stick their mount is steered by.
type evSteerBoost struct {
	eid  int32
	slot int
}

func (evSteerBoost) isHubEvent() {}

// steerBoost is FoodOnAStickItem.use: riding the species this stick was made
// for, and with the mount not already sprinting, roll a boost and wear the
// stick for it.
func (h *hub) steerBoost(players map[int32]*tracked, t *tracked, slot int) {
	s := t.handStack(slot)
	if s == nil || s.count == 0 || t.dead || t.ridingEID == 0 {
		return
	}
	m := h.mobs[t.ridingEID]
	if m == nil || m.rider != t.p.eid {
		return
	}
	// A stick only works on the species it names (getType() == canInteractWith):
	// waving a carrot from a strider's back does nothing at all.
	if _, steers := rideable(m.etype); steers == 0 || steers != s.item {
		return
	}
	if !h.startBoost(players, m) {
		return // already sprinting: vanilla's use PASSes and the stick is spared
	}
	h.wearSteerStick(players, t, slot, m)
}

// startBoost is ItemBasedSteering.boost: one sprint at a time, its length
// rolled once and published as DATA_BOOST_TIME. That metadata is what makes
// the sprint happen at all — the client owns a ridden mount's physics and
// starts its own steering when the value lands — so it is sent once and never
// re-asserted the way a saddle or a goat's horns are: a second copy would
// restart the swell from zero.
func (h *hub) startBoost(players map[int32]*tracked, m *mob) bool {
	if m.boosting {
		return false
	}
	m.boosting = true
	m.boostTick = 0
	m.boostTotal = boostTimeMin + h.rng.Intn(boostTimeSpan)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(boostTimeMeta(m.eid, int32(m.boostTotal))))
	return true
}

// tickBoosts is ItemBasedSteering.tickBoost, which vanilla runs from
// tickRidden: the clock only turns while somebody is aboard, so a mount left
// mid-sprint picks up where it was when the next rider mounts.
func (h *hub) tickBoosts(players map[int32]*tracked) {
	for _, t := range players {
		if t.ridingEID == 0 {
			continue
		}
		if m := h.mobs[t.ridingEID]; m != nil && m.boosting && m.rider == t.p.eid {
			if m.boostTick++; m.boostTick > m.boostTotal {
				m.boosting = false
			}
		}
	}
}

// boostFactor is the multiplier the sprint puts on the mount's ridden speed:
// one at the roll's two ends and 2.15 at its middle, a half-sine over the
// whole boost. The client applies it (it moves the mount); the engine carries
// the same curve so its model of the sprint is the one the rider is seeing.
func (m *mob) boostFactor() float64 {
	if !m.boosting || m.boostTotal <= 0 {
		return 1
	}
	return 1 + boostPeak*math.Sin(math.Pi*float64(m.boostTick)/float64(m.boostTotal))
}

// wearSteerStick is hurtAndConvertOnBreak(consumeItemDamage, FISHING_ROD): the
// stick takes its wear and, when that spends it, becomes the undamaged fishing
// rod it was built on rather than snapping to nothing like a tool.
func (h *hub) wearSteerStick(players map[int32]*tracked, t *tracked, slot int, m *mob) {
	if t.gamemode != gmSurvival {
		return // hasInfiniteMaterials: no wear, and so no durability criterion
	}
	s := t.handStack(slot)
	if s == nil {
		return
	}
	// "This Boat Has Legs" is a durability change on the stick WHILE ABOARD,
	// so the mount rides along with the criterion.
	h.advance(players, t, "item_durability_changed", advMatch{item: s.item, vehicle: advEntityName[m.etype]})
	// The sticks sit in #enchantable/durability but not in the armour branch,
	// so Unbreaking spares them at the tool rate, lvl/(lvl+1).
	if lvl := s.enchLvl(enchUnbreaking); lvl > 0 && h.rng.Intn(lvl+1) > 0 {
		return
	}
	max, ok := itemMaxDurability[s.item]
	if !ok {
		return
	}
	if s.dmg += steerWear[s.item]; s.dmg >= max {
		h.incStat(t, attachproto.StatBroken, s.item, 1)
		h.playSound(players, "minecraft:entity.item.break", sndPlayer, t.x, t.y, t.z, 0.8, 0.8+h.rng.Float32()*0.4)
		*s = invStack{item: itemFishingRod, count: 1}
	}
	if slot == offhandSlot {
		h.sendOffhand(t)
		return
	}
	h.sendSlot(t, slot)
}

// boostTimeMeta publishes a mount's rolled boost length.
func boostTimeMeta(eid int32, ticks int32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexBoostTime)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, ticks)
	return protocol.AppendU8(b, itemMetaEnd)
}
