package server

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
		"pufferfish": 1.25, "tadpole": 1.25, "chicken": 1.4, "llama": 1.2, "turtle": 1.2,
		"strider": 1.65, "wandering_trader": 0.5, "camel": 4.0, "allay": 2.5,
		"wolf": 1.5, "polar_bear": 2.0,
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
		return false // a goat rams, an armadillo rolls up, a zombie horse plods
	}
	// PolarBear's goal takes the environmental tag for an adult and the full
	// one for a cub, which is why a mother stands her ground and a cub bolts.
	if panicEnvironmentalOnly[m.etype] && !m.baby {
		return dt.has(tagPanicEnvironmentalCauses)
	}
	return dt.has(tagPanicCauses)
}

// panicNever is the roster with no PanicGoal at all.
var panicNever = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"goat", "armadillo", "zombie_horse"} {
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
