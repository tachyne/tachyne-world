package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Dolphins play with what floats. Vanilla's PlayWithItemsGoal: an item in
// the water within eight blocks draws a dolphin over at 1.2 (the play call
// as it sets off); the dolphin scoops it up when it reaches it and, the next
// moment, tosses it ahead of itself with a forty-tick hold before anyone can
// pick it up again — and then goes after it again. Dolphin.pickUpItem takes
// it into the mouth slot; here the dolphin takes one from the stack, the way
// tachyne's fox does, so the rest keeps floating.

const (
	dolphinPlayRange  = 8.0 // the goal's inflate(8, 8, 8)
	dolphinPlaySpeed  = 1.2
	dolphinPlayReach  = 1.5 // Mob.aiStep's pickup reach (bbox + 1)
	dolphinTossHold   = 40  // the thrown item's pickup delay
	dolphinTossReach  = 1.5 // where the toss lands (0.3/tick, no item drag here)
	dolphinTossLift   = 0.2 // a little hop out of the water
	dolphinPlayGiveUp = 200 // ticks chasing one item before it loses interest
)

// itemInWater is ItemEntity.isInWater for a floating item: in a water cell,
// or settled on the top face of one.
func (h *hub) itemInWater(it *itemEntity) bool {
	w := h.worldFor(it.dim)
	if w == nil {
		return false
	}
	fx, fy, fz := int(math.Floor(it.x)), int(math.Floor(it.y)), int(math.Floor(it.z))
	return worldgen.IsWater(w.At(fx, fy, fz)) || (it.y == float64(fy) && worldgen.IsWater(w.At(fx, fy-1, fz)))
}

// dolphinPlay runs the goal for one dolphin each mob update; returns whether
// it holds the dolphin.
func (h *hub) dolphinPlay(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityDolphin || m.dying > 0 || !h.rules.MobGriefing {
		return false
	}
	now := h.tick.Load()
	if m.held != 0 { // tick(): something in its mouth goes straight back in the water
		h.dolphinToss(players, m, now)
		return true
	}
	var it *itemEntity
	bestD := dolphinPlayRange
	for _, cand := range h.items {
		if cand.dim != m.dim || cand.count <= 0 || now < cand.noPickupUntil || !h.itemInWater(cand) {
			continue
		}
		if math.Abs(cand.x-m.x) > dolphinPlayRange || math.Abs(cand.y-m.y) > dolphinPlayRange || math.Abs(cand.z-m.z) > dolphinPlayRange {
			continue
		}
		if d := dist3(cand.x, cand.y, cand.z, m.x, m.y, m.z); d < bestD {
			it, bestD = cand, d
		}
	}
	if it == nil {
		m.dolphinPlayEID = 0
		return false
	}
	if m.dolphinPlayEID != it.eid { // start(): the play call
		m.dolphinPlayEID = it.eid
		h.playSoundDim(players, m.dim, "minecraft:entity.dolphin.play", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
	if bestD <= dolphinPlayReach { // Dolphin.pickUpItem
		one := it.stack()
		one.count = 1
		m.setHeld(one)
		if it.count--; it.count <= 0 {
			delete(h.items, it.eid)
			h.entityGone(players, it.dim, it.eid)
		} else {
			h.refreshItemMeta(players, it)
		}
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
		h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
		m.dolphinPlayEID = 0
		return true
	}
	h.steerTo(m, it.x, it.z, dolphinPlaySpeed)
	if it.y > m.y+0.5 {
		m.vy = m.moveSpeed() // it floats above: rise to it
	}
	return true
}

// dolphinToss is PlayWithItemsGoal.drop: the item leaves the mouth ahead of
// the dolphin with a forty-tick pickup hold, so it can chase it again.
func (h *hub) dolphinToss(players map[int32]*tracked, m *mob, now uint64) {
	yaw := float64(m.yaw) * math.Pi / 180
	tx := m.x - math.Sin(yaw)*dolphinTossReach
	tz := m.z + math.Cos(yaw)*dolphinTossReach
	st := m.heldStack()
	m.held = 0
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
	if it := h.spawnItemIn(players, m.dim, st.item, 1, tx, m.y+0.5, tz); it != nil {
		it.setFrom(st)
		h.refreshItemMeta(players, it)
		it.noPickupUntil = now + dolphinTossHold
		it.vy = dolphinTossLift
	}
}
