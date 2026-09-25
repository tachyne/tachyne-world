package server

import "math"

// The walk to a mate. BreedGoal (and the brains' AnimalMakeLove) moves a
// courting animal to the nearest courting animal of its kind at the goal's
// own speed; the breeding sweep (animals.go) then counts the time the two
// spend together. Until 2026-09-25 the sweep only wrote the partner into the
// hunt target, which a wandering animal never reads, so two fed cows at
// opposite ends of a pen milled about at full pace and met by chance.

// breedSpeeds are the goals' speed modifiers where they are not 1.0.
var breedSpeeds = map[int]float64{
	entityCat: 0.8, entityOcelot: 0.8, entityRabbit: 0.8, // BreedGoal(this, 0.8)
	entityAxolotl: 0.2, entityHoglin: 0.6, // AnimalMakeLove(…, 0.2F / 0.6F, 2)
}

// breedClose is AnimalMakeLove's closeEnoughDistance for the brain-driven
// breeders; a BreedGoal walks right up to its partner.
var breedClose = map[int]float64{
	entityGoat: 2, entityFrog: 2, entitySniffer: 2, entityCamel: 2, entityAxolotl: 2,
	entityHoglin: 2, entityArmadillo: 1,
}

func breedSpeed(etype int) float64 {
	if sp, ok := breedSpeeds[etype]; ok {
		return sp
	}
	return 1
}

// breedApproachStep walks a courting animal toward its nearest courting
// partner. It yields to FollowOwnerGoal (a pet already after its owner) and
// to a hunt, as both outrank breeding. Returns whether it moved the mob.
func (h *hub) breedApproachStep(m *mob) bool {
	if m.loveTicks <= 0 || m.hasTarget && (m.hostile || m.tamed) {
		return false
	}
	switch m.etype {
	case entityNautilus, entityVillager:
		return false // their own courting steps
	}
	var mate *mob
	best := breedRange * breedRange
	h.grid().nearby(m.dim, m.x, m.z, breedRange, func(o *mob) {
		if o == m || o.etype != m.etype || o.loveTicks <= 0 || o.dying > 0 || o.panic > 0 {
			return
		}
		if m.etype == entitySniffer && (!snifferMates(m) || !snifferMates(o)) {
			return
		}
		if d2 := sq(o.x-m.x) + sq(o.z-m.z); d2 < best {
			mate, best = o, d2
		}
	})
	if mate == nil {
		return false // no partner: BreedGoal cannot start, the animal strolls
	}
	if m.etype == entityPanda && !h.pandaFindsBamboo(m) {
		return false // PandaBreedGoal.canUse: no bamboo about, it sulks instead
	}
	m.rest = 0
	m.yaw = yawToward(m.x, m.z, mate.x, mate.z)
	m.headYaw = m.yaw
	stop := 1.0
	if c, ok := breedClose[m.etype]; ok {
		stop = c
	}
	if math.Sqrt(best) <= stop {
		m.vx, m.vz = 0, 0
		return true
	}
	sp := breedSpeed(m.etype)
	switch {
	case m.swims:
		h.swimToward(m, mate.x, mate.y, mate.z, sp)
		return true
	case m.flies:
		m.ty = mate.y
		vx, vz := straightSteer(m, mate.x, mate.z, stop)
		m.vx, m.vz = vx*sp, vz*sp
	default:
		vx, vz := h.pathSteer(m, mate.x, mate.z)
		m.vx, m.vz = vx*sp, vz*sp
	}
	return true
}
