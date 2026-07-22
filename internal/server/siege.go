package server

import (
	"log"
	"math"
)

// Zombie siege — a port of vanilla's VillageSiege. On a random night a horde of
// zombies materialises at the edge of a village and attacks it. Vanilla runs one
// siege state machine per level: at dusk it rolls once (1-in-10), and if it wins
// it picks a village, then releases 20 zombies around its rim. Dawn clears the
// state so the next dusk rolls afresh.

const (
	siegeZombies      = 20 // vanilla numSpawned: how many zombies a siege releases
	siegeChance       = 10 // 1-in-N nightly roll (vanilla nextInt(10) != 0 → no siege)
	siegeRadius       = 24 // zombies appear within this of the village centre
	siegeVillageRange = 64 // how far from a player a besiegeable village may sit
	siegeMinVillagers = 1  // an empty village isn't worth besieging
	siegePerStep      = 2  // zombies released per siege step (survivalTick cadence)
	siegeFindTries    = 10 // attempts to find a valid spawn column per zombie
	siegeDuskWindow   = 1000
)

// Village-siege states (vanilla VillageSiege.State).
const (
	siegeDone    = iota // nothing running: daytime, or tonight's siege declined/finished
	siegeTonight        // the roll succeeded — still looking for a village to besiege
	siegeActive         // releasing zombies around the village
)

// updateVillageSiege advances the siege state machine once per survival tick.
func (h *hub) updateVillageSiege(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful {
		return
	}
	if h.isDayTime() { // dawn wipes the night's state; the next dusk rolls again
		h.siegeState, h.siegeRolled, h.siegeLeft = siegeDone, false, 0
		return
	}
	switch h.siegeState {
	case siegeDone:
		if h.siegeRolled {
			return // tonight is already decided
		}
		if d := h.dayTime.Load() % dayLength; d < nightStart || d >= nightStart+siegeDuskWindow {
			return // vanilla only rolls in the dusk window
		}
		h.siegeRolled = true
		if h.rng.Intn(siegeChance) != 0 {
			return // quiet night
		}
		h.siegeState = siegeTonight
		fallthrough
	case siegeTonight:
		center, ok := h.findSiegeVillage(players)
		if !ok {
			h.siegeState = siegeDone // nowhere to besiege — tonight passes
			return
		}
		h.siegeCenter, h.siegeLeft, h.siegeState = center, siegeZombies, siegeActive
		log.Printf("zombie siege at village (%d,%d): %d zombies", center.x, center.z, siegeZombies)
	case siegeActive:
		for i := 0; i < siegePerStep && h.siegeLeft > 0; i++ {
			if !h.spawnSiegeZombie(players) {
				break // no room this step — try again on the next
			}
			h.siegeLeft--
		}
		if h.siegeLeft <= 0 {
			h.siegeState = siegeDone
		}
	}
}

// findSiegeVillage picks a village near some player that still has villagers in
// it. Go's map iteration is random, so this is vanilla's "random player" pick.
func (h *hub) findSiegeVillage(players map[int32]*tracked) (blockPos, bool) {
	for _, t := range players {
		if t.dim != 0 || t.dead {
			continue
		}
		center, ok := h.villageNear(int(t.x), int(t.z), siegeVillageRange)
		if !ok {
			continue
		}
		if h.countVillagersNear(center, siegeRadius*2) < siegeMinVillagers {
			continue
		}
		return center, true
	}
	return blockPos{}, false
}

// countVillagersNear counts the live villagers around a village centre.
func (h *hub) countVillagersNear(c blockPos, r int) int {
	n, rr := 0, float64(r*r)
	for _, m := range h.mobs {
		if m.etype != entityVillager || m.dying != 0 || m.dim != 0 {
			continue
		}
		dx, dz := m.x-float64(c.x), m.z-float64(c.z)
		if dx*dx+dz*dz <= rr {
			n++
		}
	}
	return n
}

// spawnSiegeZombie releases one zombie on the village rim, reporting whether it
// found a spot. Vanilla picks a random bearing out from the centre and drops the
// zombie on the first stand-able column it finds.
func (h *hub) spawnSiegeZombie(players map[int32]*tracked) bool {
	c := h.siegeCenter
	for i := 0; i < siegeFindTries; i++ {
		ang := h.rng.Float64() * 2 * math.Pi
		d := float64(siegeRadius)/2 + h.rng.Float64()*float64(siegeRadius)/2
		x := c.x + int(math.Cos(ang)*d)
		z := c.z + int(math.Sin(ang)*d)
		y := h.world.SurfaceFeet(x, z)
		if !h.spawnPositionOK(catMonster, entityZombie, x, y, z) {
			continue
		}
		return h.spawnHostileY(players, entityZombie, float64(x)+0.5, float64(y), float64(z)+0.5) != nil
	}
	return false
}
