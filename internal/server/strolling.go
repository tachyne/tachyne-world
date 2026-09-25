package server

import "math"

// What a mob does when it has nowhere to be: how fast it ambles, and — for
// a monster — which way. Vanilla gives each species' stroll goal its own
// speed modifier on the movement-speed attribute, and monsters score a
// candidate walk target by how dark it is, so they drift into the shade
// rather than wandering out into the light.

// strollSpeeds are the vanilla WaterAvoidingRandomStrollGoal (and
// RandomStrollGoal) modifiers. A species that is absent strolls at 1.0,
// which is what the goal is given almost everywhere.
var strollSpeeds = func() map[int]float64 {
	m := map[int]float64{}
	for name, mul := range map[string]float64{
		"horse": 0.7, "donkey": 0.7, "mule": 0.7, "skeleton_horse": 0.7, "zombie_horse": 0.7,
		"llama": 0.7, "trader_llama": 0.7, "camel": 2.0, "camel_husk": 2.0, // CamelAi: RandomStroll.stroll(2.0F)
		"creeper": 0.8, "spider": 0.8, "cave_spider": 0.8,
		"rabbit": 0.6, "ravager": 0.4, "wandering_trader": 0.35,
		"evoker": 0.6, "illusioner": 0.6, "pillager": 0.6, "vindicator": 0.6,
		"creaking": 0.3, // CreakingAi idle: RandomStroll.stroll(SPEED_MULTIPLIER_WHEN_IDLING)
		"zoglin":   0.4, // Zoglin idle: RandomStroll.stroll(0.4F)
		"warden":   0.5, // WardenAi idle: RandomStroll.stroll(SPEED_MULTIPLIER_WHEN_IDLING)
		// Cat and Ocelot: WaterAvoidingRandomStrollGoal(this, 0.8, …);
		// TadpoleAi: RandomStroll.swim(0.5F).
		"cat": 0.8, "ocelot": 0.8, "tadpole": 0.5,
	} {
		if id, ok := entityByName[name]; ok {
			m[id] = mul
		}
	}
	return m
}()

// strollSpeed is the multiplier on a mob's movement speed while it is
// ambling with nothing else to do.
func strollSpeed(m *mob) float64 {
	if mul, ok := strollSpeeds[m.etype]; ok {
		return mul
	}
	return 1
}

// strollSpeedFor is strollSpeed with the axolotl's two idle strolls:
// AxolotlAi swims at 0.5 and, ashore, crawls at 0.15.
func (h *hub) strollSpeedFor(m *mob) float64 {
	if m.etype == entityIronGolem && m.golemStrolling {
		return golemStrollSpeed // GolemRandomStrollInVillageGoal / MoveBackToVillageGoal
	}
	if m.etype == entityFrog && h.inWater(m.dim, m.x, m.y, m.z) {
		return 0.75 // FrogAi SWIM: RandomStroll.swim(0.75F); ashore it strolls at 1.0
	}
	if m.etype == entityAxolotl {
		if h.inWater(m.dim, m.x, m.y, m.z) {
			return 0.5
		}
		return 0.15
	}
	return strollSpeed(m)
}

// strollAngle picks the heading a fresh amble sets off on. Vanilla's
// RandomPos draws several candidate targets and keeps the one its walk
// target scores highest; for a monster that score is the negated
// path-finding cost of the light there (Monster.getWalkTargetValue), so it
// prefers the dark. Everything else has no preference and takes the first
// draw.
func (h *hub) strollAngle(m *mob) float64 {
	a := h.rng.Float64() * 2 * math.Pi
	if !m.hostile || m.hasTarget {
		return a
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return a
	}
	const reach = 6 // about as far as a stroll target lands
	best, bestLight := a, 16
	for i := 0; ; i++ {
		x := int(math.Floor(m.x + math.Cos(a)*reach))
		z := int(math.Floor(m.z + math.Sin(a)*reach))
		sky, block := w.LightAt(x, int(math.Floor(m.y)), z)
		if l := h.rawBrightness(sky, block, -1); l < bestLight {
			best, bestLight = a, l
		}
		if i == 2 || bestLight == 0 { // three draws, or a spot that cannot be darker
			return best
		}
		a = h.rng.Float64() * 2 * math.Pi
	}
}
