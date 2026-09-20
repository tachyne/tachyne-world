package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Water animals out of water, and a dolphin's breath.
//
// Vanilla's fish, squid and tadpoles (WaterAnimal / AgeableWaterCreature)
// carry three hundred ticks of air out of water and, once it is gone, take
// two drowning damage a second; an axolotl has six thousand ticks and dries
// out instead. A dolphin keeps its air on land but dries: two minutes of
// moisture, then a point of damage every tick. Under water a dolphin is the
// odd one: it cannot breathe there, has four minutes of air rather than
// fifteen seconds, and heads for the surface when it runs low
// (BreathAirGoal). tachyne drowned it in fifteen seconds like a zombie and
// let a fish live on dry stone for ever.

const (
	dolphinMaxAir    = 4800 // Dolphin.getMaxAirSupply
	axolotlMaxAir    = 6000 // Axolotl.getMaxAirSupply
	waterAnimalAir   = 300  // WaterAnimal.handleAirSupply: what a fish holds
	dolphinMoistness = 2400 // Dolphin.TOTAL_MOISTNESS_LEVEL
	breatheAirBelow  = 140  // BreathAirGoal.canUse
	envSecondTicks   = 20   // mobEnvironment runs once a second
	airColumnReach   = 8    // BreathAirGoal.findAirPosition looks 8 up
)

// waterAnimals are vanilla's WaterAnimal family and the axolotl: out of
// water they lose air and, past it, take damage.
var waterAnimals = map[int]bool{
	entityCod: true, entitySalmon: true, entityTropicalFish: true, entityPufferfish: true,
	entitySquid: true, entityGlowSquid: true, entityTadpole: true,
	entityDolphin: true, entityAxolotl: true,
}

// mobMaxAir is getMaxAirSupply: the ticks a mob lasts under water.
func mobMaxAir(m *mob) int {
	switch m.etype {
	case entityDolphin:
		return dolphinMaxAir
	case entityAxolotl:
		return axolotlMaxAir
	}
	return maxAir
}

// waterAnimalDry runs once a second for a water animal: in water it is
// fine; out of it a fish's air runs down and then it takes drowning damage,
// an axolotl dries out on the same clock, and a dolphin dries out on its
// moisture clock (rain keeps a dolphin or an axolotl wet).
func (h *hub) waterAnimalDry(players map[int32]*tracked, m *mob) {
	if !waterAnimals[m.etype] {
		return
	}
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	wet := h.inWater(m.dim, m.x, m.y, m.z)
	if m.etype == entityDolphin {
		// Dolphin.handleAirSupply is empty: no air lost on land; Dolphin.tick
		// runs the moisture down and hurts a point a tick once it is gone.
		if wet || (m.dim == 0 && h.raining && h.isRainingAt(fx, fy, fz)) {
			m.dryTicks = 0
			return
		}
		m.dryTicks += envSecondTicks
		if m.dryTicks > dolphinMoistness {
			h.hurtMobOf(players, m, envSecondTicks, dtDryOut)
		}
		return
	}
	if wet {
		m.dryTicks = 0
		return
	}
	m.dryTicks += envSecondTicks
	if m.dryTicks < mobMaxAir(m)+envSecondTicks {
		return // air left (vanilla hurts when it reaches -20)
	}
	if m.etype == entityAxolotl {
		h.hurtMobOf(players, m, drownDmgPerSec, dtDryOut)
	} else {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusDrown))
		h.hurtMobOf(players, m, drownDmgPerSec, dtDrown)
	}
}

// dolphinBreathe is BreathAirGoal: under 140 ticks of air the dolphin makes
// for the nearest cell in the surrounding columns (up to eight above) that
// holds no water and can be entered, or failing that straight up, at full
// pace. Returns whether it is doing so.
func (h *hub) dolphinBreathe(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityDolphin || dolphinMaxAir-m.submerged*envSecondTicks >= breatheAirBelow {
		return false
	}
	w := h.worldFor(m.dim)
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	tx, ty, tz := fx, fy+airColumnReach, fz
found:
	for y := fy; y <= fy+airColumnReach; y++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if b := w.At(fx+dx, y, fz+dz); !worldgen.HoldsWater(b) && !worldgen.Collides(b) {
					tx, ty, tz = fx+dx, y, fz+dz
					break found
				}
			}
		}
	}
	if tx == fx && tz == fz {
		m.vx, m.vz = 0, 0 // straight up
	} else {
		h.steerTo(m, float64(tx)+0.5, float64(tz)+0.5, 1.0)
	}
	if float64(ty) > m.y {
		m.vy = m.moveSpeed() // rise; swimMove keeps it inside the water
	}
	return true
}
