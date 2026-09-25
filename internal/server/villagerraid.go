package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A villager's raid activities (VillagerGoalPackages: PRE_RAID and RAID,
// switched by SetRaidStatus once in twenty ticks for a raid within 96
// blocks). Before the first wave and between waves it hurries to the
// meeting point (the bell it rings, raidextra.go). While a wave is on it
// hides (LocateHidingPlace(24, ×1.4, 1)): to a bed it stands beside, else a
// random bed within twenty-four, else its own, and stays there. After a
// victory it goes out under the open sky (MoveToSkySeeingSpot) and, for its
// thirty seconds of CelebrateVillagersSurvivedRaid, cheers and now and then
// sends up a firework of one random colour.

const (
	raidNearRange       = 96.0 // Raids.getNearbyRaid: 9216 = 96²
	villagerPreRaidMod  = 1.5
	villagerHideMod     = 1.4
	villagerHideRadius  = 24
	villagerMeetClose   = 2
	villagerMeetTooFar  = 150
	villagerCelebrateTk = 600
)

// raidAt is Level.getRaidAt: the raid centred within 96 blocks.
func (h *hub) raidAt(m *mob) *raid {
	if m.dim != dimOverworld {
		return nil
	}
	for c, r := range h.raids {
		if math.Hypot(float64(c.x)+0.5-m.x, float64(c.z)+0.5-m.z) < raidNearRange &&
			math.Abs(float64(c.y)-m.y) < raidNearRange {
			return r
		}
	}
	return nil
}

// villagerRaidStep runs a villager's raid activity. It reports whether it
// took the villager's move.
func (h *hub) villagerRaidStep(players map[int32]*tracked, m *mob) bool {
	r := h.raidAt(m)
	if r == nil || m.sleeping || r.lostLeft > 0 {
		m.vRaidHideSet, m.vCelebrate = false, 0
		return false
	}
	now := h.tick.Load()
	switch {
	case r.wonLeft > 0:
		return h.villagerRaidVictory(players, m, now)
	case r.wave > 0 && r.cooldown <= 0: // RAID: a wave is on
		return h.villagerHide(m)
	default: // PRE_RAID: to the meeting point
		m.vRaidHideSet = false
		if m.meet == (blockPos{}) {
			return false
		}
		d := math.Hypot(float64(m.meet.x)+0.5-m.x, float64(m.meet.z)+0.5-m.z)
		if d <= villagerMeetClose || d > villagerMeetTooFar {
			return false
		}
		vx, vz := h.pathSteer(m, float64(m.meet.x)+0.5, float64(m.meet.z)+0.5)
		m.vx, m.vz = vx*villagerPreRaidMod, vz*villagerPreRaidMod
		m.rest = 0
		return true
	}
}

// villagerHide is LocateHidingPlace: the hiding bed, walked to and stayed at.
func (h *hub) villagerHide(m *mob) bool {
	if !m.vRaidHideSet {
		p, ok := h.villagerHidingPlace(m)
		if !ok {
			return false
		}
		m.vRaidHide, m.vRaidHideSet = p, true
	}
	p := m.vRaidHide
	if math.Hypot(float64(p.x)+0.5-m.x, float64(p.z)+0.5-m.z) <= 1 {
		m.vx, m.vz = 0, 0 // hidden: it stays put while the wave lasts
		return true
	}
	vx, vz := h.pathSteer(m, float64(p.x)+0.5, float64(p.z)+0.5)
	m.vx, m.vz = vx*villagerHideMod, vz*villagerHideMod
	m.rest = 0
	return true
}

// villagerHidingPlace: a bed within two it is already beside, else a random
// bed within twenty-four, else its own home.
func (h *hub) villagerHidingPlace(m *mob) (blockPos, bool) {
	w := h.poiWorld(m.dim)
	if w != nil {
		isHome := func(p world.POI) bool { return p.Kind == poiKindHome }
		bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
		for _, p := range w.POIsNear(bx, by, bz, 2, isHome) {
			if dist3(float64(p.X)+0.5, float64(p.Y)+0.5, float64(p.Z)+0.5, m.x, m.y, m.z) < 1 {
				return blockPos{p.X, p.Y, p.Z}, true
			}
		}
		if homes := w.POIsNear(bx, by, bz, villagerHideRadius, isHome); len(homes) > 0 {
			p := homes[h.rng.Intn(len(homes))]
			return blockPos{p.X, p.Y, p.Z}, true
		}
	}
	if m.home != (blockPos{}) {
		return m.home, true
	}
	return blockPos{}, false
}

// villagerRaidVictory is the won raid: out under the sky, and the cheering.
func (h *hub) villagerRaidVictory(players map[int32]*tracked, m *mob, now uint64) bool {
	m.vRaidHideSet = false
	under := h.skyExposedAt(floorInt(m.x), floorInt(m.y), floorInt(m.z))
	if under {
		if m.vCelebrate == 0 {
			m.vCelebrate = now + villagerCelebrateTk // CelebrateVillagersSurvivedRaid(600, 600)
		}
		if now < m.vCelebrate {
			if h.rng.Intn(100/mobMoveInterval) == 0 {
				h.playSoundDim(players, m.dim, "minecraft:entity.villager.celebrate", sndNeutral, m.x, m.y, m.z, 1, 1)
			}
			if h.rng.Intn(200/mobMoveInterval) == 0 {
				h.villagerFirework(players, m)
			}
		}
		return false // it strolls about the village as it cheers
	}
	// MoveToSkySeeingSpot: ten tries within ten across and three up or down.
	for i := 0; i < 10; i++ {
		x := floorInt(m.x) + h.rng.Intn(20) - 10
		y := floorInt(m.y) + h.rng.Intn(6) - 3
		z := floorInt(m.z) + h.rng.Intn(20) - 10
		if h.skyExposedAt(x, y, z) && h.world.SurfaceFeet(x, z) <= floorInt(m.y)+1 {
			m.vx, m.vz = h.pathSteer(m, float64(x)+0.5, float64(z)+0.5)
			m.rest = 0
			return true
		}
	}
	return false
}

// villagerFirework is the celebration's rocket: one burst of one random
// dye colour, flying for up to two.
func (h *hub) villagerFirework(players map[int32]*tracked, m *mob) {
	if h.stars == nil {
		h.stars = newStarStore()
	}
	c := dyeFireworkColor[h.rng.Intn(len(dyeFireworkColor))]
	st := invStack{item: itemFireworkRocket, count: 1, flight: int8(h.rng.Intn(3)),
		starID: h.stars.intern([]fireworkBurst{{Shape: burstBurst, Colors: []int32{c}}})}
	h.spawnRocket(players, m.dim, m.x, m.y+mobEyeHeight(m), m.z, 0, st)
}
