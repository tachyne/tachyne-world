package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// PanicGoal's speed modifier, per species. Vanilla registers the goal
// separately on each animal with its own number — a cow bolts at twice its
// walking speed, a llama barely picks up the pace, a wandering trader
// actually slows down — and the engine used to run everything but a chicken
// at a flat 2.0.
var panicSpeeds = func() map[int]float64 {
	out := map[int]float64{}
	for name, sp := range map[string]float64{
		"cow": 2.0, "mooshroom": 2.0, "trader_llama": 2.0, "panda": 2.0, "rabbit": 2.2,
		"sheep": 1.25, "pig": 1.25, "cod": 1.25, "salmon": 1.25, "tropical_fish": 1.25,
		"pufferfish": 1.25, "tadpole": 2.0, "chicken": 1.4, "llama": 1.2, "turtle": 1.2,
		"strider": 1.65, "wandering_trader": 0.5, "camel": 4.0, "camel_husk": 4.0, "allay": 2.5,
		"wolf": 1.5, "polar_bear": 2.0, "goat": 2.0, "cat": 1.5, "parrot": 1.25, "fox": 2.2,
		"horse": 1.2, "donkey": 1.2, "mule": 1.2, // AbstractHorse's MountPanicGoal
		"copper_golem": 1.5, "nautilus": 1.6, "happy_ghast": 2.0,
	} {
		if id, ok := entityByName[name]; ok {
			out[id] = sp
		}
	}
	return out
}()

// panicSpeed is the multiplier a species bolts at (2.0 by default, as most
// of the roster's goals use).
func panicSpeed(etype int) float64 {
	if sp, ok := panicSpeeds[etype]; ok {
		return sp
	}
	return 2.0
}

// panicsAt reports whether a hurt of this kind sets an animal running.
// Vanilla's PanicGoal takes #panic_causes (everything but starvation, a
// fall, drowning and the rest of the quiet deaths); the species that panic
// only at the environment — wolves, polar bears, armadillos — take
// #panic_environmental_causes instead, which is fire, lava, a cactus, a hot
// floor, freezing and lightning.
func panicsAt(m *mob, dt dmgType) bool {
	if panicNever[m.etype] {
		return false // a zombie or skeleton horse plods
	}
	if m.etype == entityArmadillo {
		return dt.has(tagPanicEnvironmentalCauses) // ArmadilloPanic: a blow rolls it up instead
	}
	if m.etype == entityCamelHusk && m.mobRider != 0 {
		return false // CamelPanic: never while a mob holds the reins
	}
	// PolarBear's goal takes the environmental tag for an adult and the full
	// one for a cub, which is why a mother stands her ground and a cub bolts.
	if panicEnvironmentalOnly[m.etype] && !m.baby {
		return dt.has(tagPanicEnvironmentalCauses)
	}
	return dt.has(tagPanicCauses)
}

// brainPanics are the species whose panic is the brain's AnimalPanic rather
// than PanicGoal: it runs a fixed 100-120 ticks from its start, and a
// fresh hurt while it runs does not stretch it.
var brainPanics = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"allay", "goat", "sniffer", "armadillo", "happy_ghast", "tadpole",
		"copper_golem", "nautilus", "frog", "camel", "camel_husk"} {
		if id, ok := entityByName[n]; ok {
			out[id] = true
		}
	}
	return out
}()

// panicFor is how long (in mob updates) a panic-causing hurt sets m
// panicking: PanicGoal re-picks spots while the hurt is under forty ticks
// old, so every hurt restarts that clock; AnimalPanic runs 100-120 ticks.
func (h *hub) panicFor(m *mob) int {
	if brainPanics[m.etype] {
		if m.panic > 0 {
			return m.panic
		}
		return (100 + h.rng.Intn(21)) / mobMoveInterval
	}
	return panicTicks
}

// panicNever is the roster with no PanicGoal at all. A blow or an arrow
// consults it too: until 2026-09-24 any struck non-hostile bolted, so a hit
// ocelot, snow golem or zombie horse ran. (The armadillo has a panic goal,
// but for the environment only: a blow rolls it up. ZombieNautilusAi's core
// has no AnimalPanic: a struck zombie nautilus turns on you instead. A squid
// only has its own flee, which jets away from the attacker while it is near;
// a bat has no goals at all. A villager's brain has its own panic package,
// villagerpanic.go.)
var panicNever = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"zombie_horse", "skeleton_horse", "ocelot", "snow_golem", "zombie_nautilus",
		"squid", "glow_squid", "bat", "villager"} {
		if id, ok := entityByName[n]; ok {
			out[id] = true
		}
	}
	return out
}()

// panicEnvironmentalOnly is the PANIC_ENVIRONMENTAL_CAUSES set: an animal
// that fights back rather than fleeing when something hits it, but still
// runs from the fire it is standing in.
var panicEnvironmentalOnly = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"wolf", "polar_bear"} {
		if id, ok := entityByName[n]; ok {
			out[id] = true
		}
	}
	return out
}()

// panicTarget is PanicGoal.findRandomPosition → DefaultRandomPos.getPos(mob,
// 5, 4): ten random tries within five blocks sideways and four up or down,
// each landing on a spot the mob could stand, keeping the one it values
// most (PathfinderMob.getWalkTargetValue: an animal favours grass, 10 over
// the brightness it otherwise weighs). The panic used to run in a straight
// line away from the attacker, which vanilla never does.
func (h *hub) panicTarget(m *mob) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, 0, false
	}
	// PanicGoal.canUse: a mob on fire makes for water within five blocks
	// (lookForWater) before any random spot.
	if x, z, ok := h.panicWaterNear(m); ok {
		return x, z, true
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	if m.swims || m.flies {
		return h.panicTargetFree(m, bx, by, bz)
	}
	best, found := -1e9, false
	var tx, tz float64
	for i := 0; i < 10; i++ {
		x := bx + h.rng.Intn(11) - 5
		y := by + h.rng.Intn(9) - 4
		z := bz + h.rng.Intn(11) - 5
		if !w.Loaded(int32(x>>4), int32(z>>4)) {
			continue
		}
		// Settle onto the ground within the vertical window.
		for dy := 0; dy < 8 && y > by-4 && !worldgen.Collides(w.At(x, y-1, z)); dy++ {
			y--
		}
		below := w.At(x, y-1, z)
		if !worldgen.Collides(below) || worldgen.Collides(w.At(x, y, z)) || worldgen.Collides(w.At(x, y+1, z)) ||
			worldgen.IsLava(w.At(x, y, z)) {
			continue
		}
		v := 0.0
		if !m.hostile && below == worldgen.GrassBlock {
			v = 10
		}
		if v > best {
			best, found = v, true
			tx, tz = float64(x)+0.5, float64(z)+0.5
		}
	}
	return tx, tz, found
}

// panicTargetFree is the random panic spot for a mob whose navigation is not
// the ground's: a swimmer takes any water cell (WaterBoundPathNavigation's
// isStableDestination), a flier any open cell with something under it
// (FlyingPathNavigation's). Requiring a floor froze fish in open water and
// fliers in the air for the whole panic.
func (h *hub) panicTargetFree(m *mob, bx, by, bz int) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	for i := 0; i < 10; i++ {
		x := bx + h.rng.Intn(11) - 5
		y := by + h.rng.Intn(9) - 4
		z := bz + h.rng.Intn(11) - 5
		if !w.Loaded(int32(x>>4), int32(z>>4)) {
			continue
		}
		at := w.At(x, y, z)
		if worldgen.Collides(at) {
			continue
		}
		if m.swims && worldgen.IsWater(at) {
			return float64(x) + 0.5, float64(z) + 0.5, true
		}
		if m.flies && !worldgen.IsFluid(at) && w.At(x, y-1, z) != worldgen.Air {
			return float64(x) + 0.5, float64(z) + 0.5, true
		}
	}
	return 0, 0, false
}
