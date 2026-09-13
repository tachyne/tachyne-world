package server

import "math"

// Helmets against the sun (Zombie/AbstractSkeleton.aiStep isSunBurnTick):
// an undead wearing anything on its head does not burn — the helmet takes
// the sun instead, a point or none each tick, and burns away to nothing
// before the wearer catches fire. Phantoms fear cats (PhantomSweepAttack-
// Goal): a phantom about to swoop checks every twenty ticks for a cat
// within sixteen blocks; any it finds hisses, and the swoop is called off.

const (
	sunHelmetRolls  = 20 // one nextInt(2) per tick, and the burn loop runs once a second
	phantomCatRange = 16.0
	phantomCatEvery = 20
)

// sunHelmetTakesIt is the helmet branch: true when a head item absorbed
// this second's sun (and may have burnt away doing so).
func (h *hub) sunHelmetTakesIt(players map[int32]*tracked, m *mob) bool {
	head := &m.gear[0]
	if head.item == 0 {
		return false
	}
	if max, ok := itemMaxDurability[head.item]; ok {
		for i := 0; i < sunHelmetRolls; i++ {
			head.dmg += h.rng.Intn(2)
		}
		if head.dmg >= max { // breakItem: gone, and the next second the sun finds skin
			*head = invStack{}
			m.refreshGearArmor()
			h.playSoundDim(players, m.dim, "minecraft:entity.item.break", sndNeutral, m.x, m.y+1.5, m.z, 0.8, 0.8+h.rng.Float32()*0.4)
			h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
		}
	}
	return true
}

// phantomFearsCats is the sweep goal's cat check, once every twenty ticks.
func (h *hub) phantomFearsCats(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if now < m.phantomCatAt {
		return m.phantomScared
	}
	m.phantomCatAt = now + phantomCatEvery
	m.phantomScared = false
	h.grid().nearby(m.dim, m.x, m.z, phantomCatRange, func(o *mob) {
		if o.etype != entityCat || o.dying > 0 || math.Abs(o.y-m.y) > phantomCatRange {
			return
		}
		m.phantomScared = true
		h.playSoundDim(players, o.dim, "minecraft:entity.cat.hiss", sndNeutral, o.x, o.y, o.z, 1, 1) // cat.hiss()
	})
	return m.phantomScared
}
