package server

import "math"

// Who a mob hunts besides the player. Vanilla hangs a
// NearestAttackableTargetGoal on a species for every class it goes after —
// a zombie for villagers, iron golems and baby turtles caught on land, an
// enderman for endermites, a guardian for squid, a fox for chickens and
// fish — and until these were here the engine's hostiles saw players and
// (for the zombie family) villagers, and nothing else in the world was
// ever attacked.

// preyOf reports whether hunter attacks o: vanilla's target classes, with
// their selectors (a turtle only while it is a baby out of the water).
func preyOf(hunter, o *mob) bool {
	if o == nil || o.eid == hunter.eid || o.dying > 0 || o.dim != hunter.dim {
		return false
	}
	babyTurtleOnLand := o.etype == entityTurtle && o.baby
	switch {
	case zombieKind(hunter.etype):
		// Zombie: AbstractVillager, IronGolem, and a baby turtle on land;
		// a drowned adds the axolotl.
		return o.etype == entityVillager || o.etype == entityWanderingTrader ||
			o.etype == entityIronGolem || babyTurtleOnLand ||
			(hunter.etype == entityDrowned && o.etype == entityAxolotl)
	case illagerKind(hunter.etype):
		// Raider: the village's people and its guardian.
		return o.etype == entityVillager || o.etype == entityWanderingTrader || o.etype == entityIronGolem
	case hunter.etype == entitySlime || hunter.etype == entityMagmaCube:
		return o.etype == entityIronGolem
	case hunter.etype == entityEnderman:
		return o.etype == entityEndermite
	case hunter.etype == entityGuardian || hunter.etype == entityElderGuardian:
		return o.etype == entitySquid || o.etype == entityGlowSquid // GuardianAttackSelector
	case skeletonKind(hunter.etype):
		return babyTurtleOnLand
	case hunter.etype == entityFox:
		return o.etype == entityChicken || o.etype == entityRabbit || babyTurtleOnLand ||
			o.etype == entityCod || o.etype == entitySalmon || o.etype == entityTropicalFish
	case hunter.etype == entityOcelot:
		return o.etype == entityChicken || babyTurtleOnLand
	case hunter.etype == entityPolarBear:
		return o.etype == entityFox
	}
	return false
}

// illagerKind is the raider family, which all carry the villager and
// iron-golem target goals.
func illagerKind(etype int) bool {
	switch etype {
	case entityPillager, entityVindicator, entityEvoker, entityIllusioner, entityRavager, entityWitch:
		return true
	}
	return false
}

// huntsPrey reports whether a species hunts anything but players at all —
// the cheap gate before the grid search.
func huntsPrey(etype int) bool {
	return zombieKind(etype) || illagerKind(etype) || skeletonKind(etype) ||
		etype == entitySlime || etype == entityMagmaCube || etype == entityEnderman ||
		etype == entityGuardian || etype == entityElderGuardian ||
		etype == entityFox || etype == entityOcelot || etype == entityPolarBear
}

// nearestPrey is the closest mob this one hunts, within r. Villagers that
// are NPCs are left out — they are the server's own, not the world's.
func (h *hub) nearestPrey(m *mob, r float64) *mob {
	var best *mob
	bestD := r
	h.grid().nearby(m.dim, m.x, m.z, r, func(c *mob) {
		if !preyOf(m, c) {
			return
		}
		if h.npcs != nil && h.npcs[c.eid] != nil {
			return
		}
		if d := dist3(c.x, c.y, c.z, m.x, m.y, m.z); d < bestD {
			best, bestD = c, d
		}
	})
	return best
}

// mobBitesPrey is the mob-vs-mob bite: the hunter's melee on the creature
// it has been chasing, with the zombie's infection of a villager kept as
// the special case vanilla makes of it.
func (h *hub) mobBitesPrey(players map[int32]*tracked, m *mob) bool {
	if m.preyTarget == 0 {
		return false
	}
	v := h.mobs[m.preyTarget]
	if v == nil || v.dying > 0 || !preyOf(m, v) {
		m.preyTarget = 0
		return false
	}
	if dist3(v.x, v.y, v.z, m.x, m.y, m.z) > attackReach+0.5 {
		return false
	}
	if zombieKind(m.etype) && v.etype == entityVillager {
		return h.zombieBitesVillager(players, m, v)
	}
	m.attackCD = attackCooldown
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	v.hurtKind(float64((hostileMelee(m)+mobHeldBonus(m))*h.diffMult()), dtMobAttack)
	h.mobKnockFrom(players, v, m.x, m.z)
	if v.health <= 0 {
		h.killMob(players, v)
		m.preyTarget = 0
		return true
	}
	if !v.hostile { // whatever was bitten bolts, as a hurt passive mob does
		v.panic, v.fleeX, v.fleeZ, v.reroute = panicTicks, m.x, m.z, 0
	}
	return true
}

// preyReach is how far a hunter looks for prey when no player is about:
// its own FOLLOW_RANGE, as the target goal uses.
func preyReach(m *mob) float64 { return math.Max(m.followRange(), 16) }
