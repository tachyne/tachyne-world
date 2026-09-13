package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Turtles (Turtle.TurtleBreedGoal / TurtleLayEggGoal, TurtleEggBlock):
// breeding gives one parent an egg rather than a hatchling; she carries it
// home — the beach she came from — and lays one to four eggs on sand there
// after ten seconds of digging; the eggs crack twice and hatch on random
// ticks, mostly in the hour before dawn, into hatchlings whose home is the
// nest.

const (
	turtleEggBase   = 14888 // turtle_egg: base + (eggs-1)*3 + hatch
	turtleHomeReach = 9.0   // closerToCenterThan(homePos, 9)
	turtleLayTicks  = 200   // layEggCounter > adjustedTickDelay(200)
	turtleEggHatchP = 500   // shouldUpdateHatchLevel: 1 in 500 outside the dusk window
	turtleLoveAfter = 600   // setInLoveTime(600) after laying — a fresh courtship cooldown
	turtleHomeSpeed = 1.0   // MoveToBlockGoal speed for the egg goal
	turtleEggSpread = 0.2
)

// turtleEggState builds an egg block state.
func turtleEggState(eggs, hatch int) uint32 {
	return uint32(turtleEggBase + (eggs-1)*3 + hatch)
}

// turtleEggOf decodes an egg block state.
func turtleEggOf(s uint32) (eggs, hatch int, ok bool) {
	if s < turtleEggBase || s > turtleEggBase+11 {
		return 0, 0, false
	}
	n := int(s - turtleEggBase)
	return n/3 + 1, n % 3, true
}

// isSandFloor is TurtleEggBlock.isSand: the #sand tag.
func isSandFloor(s uint32) bool {
	return s == worldgen.Sand || s == worldgen.RedSand || s == worldgen.BlockBase("suspicious_sand")
}

// turtleStep runs a turtle's egg errand: with an egg, head home; at home and
// on sand, dig for two hundred ticks and lay. Returns whether the errand
// holds the turtle this update.
func (h *hub) turtleStep(players map[int32]*tracked, m *mob) bool {
	if m.home == (blockPos{}) {
		m.home = blockPos{int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))} // where it started is its beach
	}
	if !m.hasEgg || m.baby {
		m.layCounter = 0
		return false
	}
	hx, hz := float64(m.home.x)+0.5, float64(m.home.z)+0.5
	dx, dz := hx-m.x, hz-m.z
	if d := math.Hypot(dx, dz); d > turtleHomeReach {
		sp := m.moveSpeed() * turtleHomeSpeed
		m.vx, m.vz = dx/d*sp, dz/d*sp
		m.rest, m.layCounter = 0, 0
		return true
	}
	w := h.worldFor(m.dim)
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	if worldgen.HoldsWater(w.At(fx, fy, fz)) || !isSandFloor(w.At(fx, fy-1, fz)) || w.At(fx, fy, fz) != worldgen.Air {
		m.layCounter = 0
		return false // not a nest spot: wander until it is
	}
	m.vx, m.vz = 0, 0
	if m.layCounter == 0 {
		m.layCounter = mobMoveInterval
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(turtleMeta(m))) // setLayingEgg(true): the digging pose
		return true
	}
	m.layCounter += mobMoveInterval
	if m.layCounter <= turtleLayTicks {
		return true
	}
	h.setBlockLive(players, m.dim, fx, fy, fz, turtleEggState(1+h.rng.Intn(4), 0))
	h.playSoundDim(players, m.dim, "minecraft:entity.turtle.lay_egg", sndBlock, float64(fx)+0.5, float64(fy), float64(fz)+0.5, 0.3, 0.9+h.rng.Float32()*0.2)
	m.hasEgg, m.layCounter = false, 0
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(turtleMeta(m)))
	m.breedCD = turtleLoveAfter
	return true
}

// turtleEggRandomTick is TurtleEggBlock.randomTick: on sand, in the hour
// before dawn (or one tick in five hundred), the clutch cracks a stage;
// past the second crack it hatches its count of hatchlings.
func (h *hub) turtleEggRandomTick(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	eggs, hatch, ok := turtleEggOf(state)
	if !ok {
		return false
	}
	w := h.worldFor(dim)
	if !isSandFloor(w.At(x, y-1, z)) {
		return true
	}
	// getTimeOfDay: the day fraction with dawn at 0; the window 0.65–0.69 is
	// dayTime 21600–22560.
	f := math.Mod(float64(h.dayTime.Load()%24000)/24000-0.25+1, 1)
	if !(f < 0.69 && f > 0.65) && h.rng.Intn(turtleEggHatchP) != 0 {
		return true
	}
	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	if hatch < 2 {
		h.playSoundDim(players, dim, "minecraft:entity.turtle.egg_crack", sndBlock, cx, cy, cz, 0.7, 0.9+h.rng.Float32()*0.2)
		h.setBlockLive(players, dim, x, y, z, turtleEggState(eggs, hatch+1))
		return true
	}
	h.playSoundDim(players, dim, "minecraft:entity.turtle.egg_hatch", sndBlock, cx, cy, cz, 0.7, 0.9+h.rng.Float32()*0.2)
	h.setBlockLive(players, dim, x, y, z, worldgen.Air)
	for i := 0; i < eggs; i++ {
		baby := h.spawnMobIn(players, entityTurtle, dim, float64(x)+0.3+float64(i)*turtleEggSpread, float64(y), float64(z)+0.3)
		if baby == nil {
			continue
		}
		baby.baby, baby.growLeft = true, growUpTicks
		baby.home = blockPos{x, y, z}
		h.applySpecies(players, baby)
		h.toNearbyEv(players, dim, baby.x, baby.z, metaEv(babyMeta(baby.eid, true)))
	}
	return true
}
