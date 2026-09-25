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
func (h *hub) preyOf(hunter, o *mob) bool {
	if o == nil || o.eid == hunter.eid || o.dying > 0 || o.dim != hunter.dim {
		return false
	}
	// Turtle.BABY_ON_LAND_SELECTOR: a hatchling is prey only out of the water.
	babyTurtleOnLand := o.etype == entityTurtle && o.baby && !h.inWater(o.dim, o.x, o.y, o.z)
	switch {
	case zombieKind(hunter.etype):
		// Zombie: AbstractVillager, IronGolem, and a baby turtle on land;
		// a drowned adds the axolotl.
		return o.etype == entityVillager || o.etype == entityWanderingTrader ||
			o.etype == entityIronGolem || babyTurtleOnLand ||
			(hunter.etype == entityDrowned && o.etype == entityAxolotl)
	case illagerKind(hunter.etype):
		// Raider: the village's people and its guardian. The ravager's
		// villager goal takes adults only (Ravager.registerGoals).
		if hunter.etype == entityRavager && o.baby && o.etype == entityVillager {
			return false
		}
		return o.etype == entityVillager || o.etype == entityWanderingTrader || o.etype == entityIronGolem
	case hunter.etype == entitySlime || hunter.etype == entityMagmaCube:
		return o.etype == entityIronGolem
	case hunter.etype == entityWither:
		// WitherBoss's target goal takes any living thing that is not undead:
		// a wither loose in a village fights everything it meets.
		return !undeadTypes[o.etype] && o.etype != entityWither
	case hunter.etype == entityEnderman:
		return o.etype == entityEndermite
	case hunter.etype == entityGuardian || hunter.etype == entityElderGuardian:
		// GuardianAttackSelector: squid and axolotls, besides players.
		return o.etype == entitySquid || o.etype == entityGlowSquid || o.etype == entityAxolotl
	case skeletonKind(hunter.etype) || hunter.etype == entityWitherSkeleton:
		// AbstractSkeleton: iron golems and a baby turtle on land; a wither
		// skeleton adds the piglins (WitherSkeleton.registerGoals).
		if hunter.etype == entityWitherSkeleton && (o.etype == entityPiglin || o.etype == entityPiglinBrute) {
			return true
		}
		return o.etype == entityIronGolem || babyTurtleOnLand
	case hunter.etype == entitySpider || hunter.etype == entityCaveSpider:
		// SpiderTargetGoal<IronGolem>: only while the spider stands in the dark.
		return o.etype == entityIronGolem && h.lightMagic(hunter) < spiderLightNeutral
	case hunter.etype == entityFox:
		return o.etype == entityChicken || o.etype == entityRabbit || babyTurtleOnLand ||
			o.etype == entityCod || o.etype == entitySalmon || o.etype == entityTropicalFish
	case hunter.etype == entityOcelot:
		return o.etype == entityChicken || babyTurtleOnLand
	case hunter.etype == entityPolarBear:
		return o.etype == entityFox
	case hunter.etype == entityPiglin:
		// The piglin's brain picks its own (piglinFoe): the hoglin it hunts,
		// its nemesis, or the mob it is angry at.
		return o.eid == hunter.preyTarget
	case hunter.etype == entityPiglinBrute:
		// PiglinBruteAi: NEAREST_VISIBLE_NEMESIS, after the players.
		return isPiglinNemesis(o)
	case hunter.etype == entityHoglin:
		// HoglinAi.wasHurtBy: whatever hit it, once it fights back.
		return o.eid == hunter.fightBack
	}
	return false
}

// illagerKind is the raiders that carry the villager and iron-golem target
// goals. The witch is a raider but not one of them: Witch.registerGoals
// targets players only (and raiders, to heal them).
func illagerKind(etype int) bool {
	switch etype {
	case entityPillager, entityVindicator, entityEvoker, entityIllusioner, entityRavager:
		return true
	}
	return false
}

// huntsPrey reports whether a species hunts anything but players at all —
// the cheap gate before the grid search.
func huntsPrey(etype int) bool {
	return zombieKind(etype) || illagerKind(etype) || skeletonKind(etype) || etype == entityWither ||
		etype == entityWitherSkeleton || etype == entitySpider || etype == entityCaveSpider ||
		etype == entitySlime || etype == entityMagmaCube || etype == entityEnderman ||
		etype == entityGuardian || etype == entityElderGuardian ||
		etype == entityFox || etype == entityOcelot || etype == entityPolarBear ||
		etype == entityPiglinBrute || etype == entityHoglin
}

// preyMustSee is the mustSee flag of the NearestAttackableTargetGoal that
// makes o this hunter's prey. Most are registered mustSee; the villager
// goals of the zombie family, the pillager, evoker and illusioner, and the
// evoker's and illusioner's golem goals are not, nor are the wither's, the
// fox's or the ocelot's. The piglin brute reads NEAREST_VISIBLE_NEMESIS.
// The piglin's and hoglin's brains pick their own target and are left be.
func preyMustSee(hunter, o *mob) bool {
	villagerKind := o.etype == entityVillager || o.etype == entityWanderingTrader
	switch {
	case zombieKind(hunter.etype):
		return !villagerKind
	case illagerKind(hunter.etype):
		switch hunter.etype {
		case entityEvoker, entityIllusioner:
			return false
		case entityPillager:
			return !villagerKind
		}
		return true
	case hunter.etype == entityWither || hunter.etype == entityFox || hunter.etype == entityOcelot ||
		hunter.etype == entityPiglin || hunter.etype == entityHoglin:
		return false
	}
	return true
}

// nearestPrey is the mob this one hunts, within r: the one it holds, kept as
// TargetGoal.canContinueToUse keeps it (in range, and for a mustSee goal
// seen within the last targetUnseenMemory ticks), else the closest one it
// may acquire — which for a mustSee goal is one it has line of sight to.
// Villagers that are NPCs are left out — they are the server's own, not the
// world's.
func (h *hub) nearestPrey(m *mob, r float64) *mob {
	if cur := h.mobs[m.preyTarget]; cur != nil && h.preyOf(m, cur) && (h.npcs == nil || h.npcs[cur.eid] == nil) &&
		dist3(cur.x, cur.y, cur.z, m.x, m.y, m.z) <= r {
		if !preyMustSee(m, cur) || h.mobSeesMob(m, cur) {
			m.preyUnseen = 0
			return cur
		}
		m.preyUnseen += mobMoveInterval
		if m.preyUnseen <= targetUnseenMemory {
			return cur
		}
	}
	m.preyUnseen = 0
	var best *mob
	bestD := r
	h.grid().nearby(m.dim, m.x, m.z, r, func(c *mob) {
		if !h.preyOf(m, c) {
			return
		}
		if h.npcs != nil && h.npcs[c.eid] != nil {
			return
		}
		if d := dist3(c.x, c.y, c.z, m.x, m.y, m.z); d < bestD && (!preyMustSee(m, c) || h.mobSeesMob(m, c)) {
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
	if v == nil || v.dying > 0 || !h.preyOf(m, v) {
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
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	dmg := float64(hostileMelee(m) + mobHeldBonus(m))
	if m.etype == entityHoglin {
		dmg = float64(h.hoglinBiteDamage(m)) // hurtAndThrowTarget: half plus a roll
		h.hoglinBiteStart(players, m)
		m.attackCD = hoglinAttackCD(m)
	}
	v.hurtKind(dmg, dtMobAttack)
	v.lastAttacker = m.eid
	h.mobKnockFrom(players, v, m.x, m.z)
	if v.health <= 0 {
		h.killMob(players, v)
		m.preyTarget = 0
		if m.fightBack == v.eid {
			m.fightBack = 0
		}
		return true
	}
	// A piglin or a hoglin answers the blow.
	h.mobHurtByMob(players, v, m)
	if !v.hostile && panicsAt(v, dtMobAttack) { // whatever was bitten bolts, if its kind panics
		v.panic, v.fleeX, v.fleeZ, v.reroute = h.panicFor(v), m.x, m.z, 0
	}
	return true
}

// preyReach is how far a hunter looks for prey when no player is about:
// its own FOLLOW_RANGE, as the target goal uses.
func preyReach(m *mob) float64 { return math.Max(m.followRange(), 16) }
