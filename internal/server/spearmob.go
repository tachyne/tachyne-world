package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/plugin"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Mobs with spears (SpearUseGoal, on Zombie — and so on the husk and the
// zombie villager — and on ZombifiedPiglin; and the piglin brain's
// SpearApproach, SpearAttack and SpearRetreat, which run the same three
// phases with the same distances and timings). With a target and a spear in
// hand the goal outranks the melee goal: the mob walks up to within ten
// blocks, lowers the spear for the weapon's charge window (the delay plus the
// damage window), runs at its target, and on getting within two blocks picks
// a spot six to seven blocks past it and runs there, then comes round again.
// When the window closes it raises the spear and falls back nine to eleven
// blocks for up to five seconds before the next pass. Its hits are the
// charge's (spearmob's mobSpearTick): speed thresholds a fifth of a
// player's, half the reach, and its own base attack damage plus the closing
// speed.

const (
	spearApproachDist = 10.0 // SpearUseGoal approachDistance
	spearInRangeDist  = 2.0  // …targetInRangeRadius
	spearFleeUpdates  = 50   // MAX_FLEEING_TIME: reducedTickDelay(100), in goal updates
	spearGoalSpeed    = 1.0  // both speed modifiers the zombies register
)

// spearGoalState is SpearUseGoal.SpearUseState.
type spearGoalState struct {
	engage, flee int // goal updates left lowered / spent falling back; -1 before either began
	awayX, awayZ float64
	hasAway      bool
	done         bool
}

// spearWielder is whether the mob runs the spear behaviour and holds a
// spear now.
func spearWielder(m *mob) bool {
	switch m.etype {
	case entityZombie, entityHusk, entityZombieVillager, entityZombifiedPiglin:
		return spearOf(m.held) != nil
	case entityPiglin:
		// PiglinAi's fight activity: only an adult ever has an attack target.
		return !m.baby && spearOf(m.held) != nil
	}
	return false
}

// spearQuarry is the goal's target (getTarget): the hunted player, or the
// villager, golem or turtle it is after when no player is about. A
// piglin's is the one its brain would pick — nobody in gold, and nobody at
// all while it admires an ingot.
func (h *hub) spearQuarry(players map[int32]*tracked, m *mob) (quarry, bool) {
	if !m.hasTarget || m.dying > 0 {
		return quarry{}, false
	}
	if m.etype == entityPiglin {
		if m.admireUntil != 0 {
			return quarry{}, false
		}
		return h.piglinFoe(players, m, m.followRange()) // a player, or the hoglin or nemesis it fights
	}
	return h.rangedQuarry(players, m, m.followRange())
}

// quarryEyeY is the height the mob looks at on its target: a player's eyes,
// or a mob's.
func quarryEyeY(q quarry) float64 {
	if q.t != nil {
		return playerEyeY(q.t)
	}
	return q.o.y + mobEyeHeight(q.o)
}

// spearGoalStep runs SpearUseGoal for one mob update. Returns whether the
// goal holds the mob's movement this update.
func (h *hub) spearGoalStep(players map[int32]*tracked, m *mob) bool {
	target, ok := h.spearQuarry(players, m)
	sp := spearOf(m.held)
	if !ok || sp == nil {
		h.spearGoalStop(players, m)
		return false
	}
	g := m.spearGoal
	if g == nil {
		g = &spearGoalState{engage: -1, flee: -1}
		m.spearGoal = g
	}
	mount := 0.0
	if m.mount != 0 {
		mount = 2 // a mounted spearman keeps two blocks further off
	}
	speed := spearGoalSpeed * h.chargeSpeedModifier(m) // a mounted charge takes its vehicle's modifier
	d := dist3(m.x, m.y, m.z, target.x, target.y, target.z)
	m.headYaw = float32(math.Atan2(-(target.x-m.x), target.z-m.z) * 180 / math.Pi)
	if g.engage < 0 {
		if d > spearApproachDist {
			h.steerTo(m, target.x, target.z, speed)
			return true
		}
		g.engage = (sp.useTicks() + 1) / 2 // reducedTickDelay: goals tick every other tick
		m.spearUseAt, m.spearHits = h.tick.Load(), map[int32]uint64{}
		h.setHandActive(players, m, true)
	}
	if g.engage > 0 {
		if g.engage--; g.engage == 0 {
			h.mobSpearRaise(players, m)
			g.awayX, g.awayZ, g.hasAway = h.spearPosAway(m, target.x, target.z, math.Max(0, 9+mount-d), math.Max(1, 11+mount-d))
			g.flee = 1
		}
	}
	timedOut := false
	if g.flee > 0 {
		if g.flee++; g.flee > spearFleeUpdates {
			g.done, timedOut = true, true
		}
	}
	if !timedOut {
		switch {
		case g.hasAway:
			h.steerTo(m, g.awayX, g.awayZ, speed)
			if math.Hypot(g.awayX-m.x, g.awayZ-m.z) < 1 { // the path is done
				if g.flee > 0 {
					g.done = true
				} else {
					g.hasAway = false
				}
			}
		default:
			h.steerTo(m, target.x, target.z, speed)
			if d < spearInRangeDist {
				g.awayX, g.awayZ, g.hasAway = h.spearPosAway(m, target.x, target.z, 6+mount-d, 7+mount-d)
			}
		}
	}
	if g.done {
		h.spearGoalStop(players, m) // canContinueToUse fails; canUse starts a fresh pass
	}
	return true
}

// spearGoalStop is SpearUseGoal.stop: the spear comes up and the state goes.
func (h *hub) spearGoalStop(players map[int32]*tracked, m *mob) {
	if m.spearGoal == nil && m.spearUseAt == 0 {
		return
	}
	m.spearGoal = nil
	h.mobSpearRaise(players, m)
}

// mobSpearRaise is stopUsingItem for a mob's lowered spear.
func (h *hub) mobSpearRaise(players map[int32]*tracked, m *mob) {
	m.spearUseAt, m.spearHits = 0, nil
	h.setHandActive(players, m, false)
}

// spearPosAway is LandRandomPos.getPosAway: a standable spot between minD and
// maxD blocks from the mob, within a quarter turn of straight away from the
// target.
func (h *hub) spearPosAway(m *mob, tx, tz, minD, maxD float64) (float64, float64, bool) {
	if maxD < minD {
		maxD = minD
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, 0, false
	}
	ax, az := m.x-tx, m.z-tz
	if n := math.Hypot(ax, az); n > 1e-6 {
		ax, az = ax/n, az/n
	} else {
		ax, az = h.rng.Float64()-0.5, h.rng.Float64()-0.5
		n := math.Hypot(ax, az)
		ax, az = ax/n, az/n
	}
	for i := 0; i < 10; i++ {
		ang := (h.rng.Float64() - 0.5) * math.Pi / 2
		dist := minD + h.rng.Float64()*(maxD-minD)
		fx := m.x + (ax*math.Cos(ang)-az*math.Sin(ang))*dist
		fz := m.z + (ax*math.Sin(ang)+az*math.Cos(ang))*dist
		bx, bz, by := int(math.Floor(fx)), int(math.Floor(fz)), int(math.Floor(m.y))
		if feet := w.MobFeetFrom(bx, bz, by); feet-by > 7 || by-feet > 7 || !w.Walkable(bx, bz) {
			continue
		}
		return fx, fz, true
	}
	return 0, 0, false
}

// mobSpearTick is KineticWeapon.damageEntities for a mob's lowered spear, run
// every mob update while it is lowered. A mob's charge strikes players.
func (h *hub) mobSpearTick(players map[int32]*tracked, m *mob) {
	if m.spearUseAt == 0 {
		return
	}
	sp := spearOf(m.held)
	if sp == nil || m.dying > 0 {
		h.mobSpearRaise(players, m)
		return
	}
	now := h.tick.Load()
	used := int(now - m.spearUseAt)
	if used < sp.delay {
		return
	}
	if used -= sp.delay; used > sp.window() {
		return
	}
	ey := m.y + mobEyeHeight(m)
	// The mob looks at its target (the goal's lookAt); with none, ahead.
	lx, ly, lz := lookVector(m.yaw, 0)
	if q, ok := h.spearQuarry(players, m); ok {
		dx, dy, dz := q.x-m.x, quarryEyeY(q)-ey, q.z-m.z
		if n := math.Sqrt(dx*dx + dy*dy + dz*dz); n > 1e-6 {
			lx, ly, lz = dx/n, dy/n, dz/n
		}
	}
	mx, my, mz := h.mobMotion(m)
	along := mx*lx + my*ly + mz*lz
	attacker := along * 20
	base := m.mobAttrs().Get(attr.AttackDamage).Base()
	affected := false
	for _, tg := range h.spearLine(players, m.dim, m.x, ey, m.z, lx, ly, lz,
		spearMinReach*spearMobReach, spearMaxReach*spearMobReach, along, m.eid, m.mount, true) {
		// Players, and the creatures its kind hunts (villagers, golems,
		// baby turtles); its own kind and bystanders are left alone.
		if tg.m != nil && !h.preyOf(m, tg.m) {
			continue
		}
		if at, ok := m.spearHits[tg.eid()]; ok && now-at < spearContactCooldown {
			continue
		}
		m.spearHits[tg.eid()] = now
		var tx, ty, tz float64
		if tg.p != nil {
			tx, ty, tz = h.knownMove(tg.p)
		} else {
			tx, ty, tz = h.mobMotion(tg.m)
		}
		rel := math.Max(0, attacker-(tx*lx+ty*ly+tz*lz)*20)
		b := spearBlow{
			dismount: sp.dismount.test(used, attacker, rel, spearMobAction),
			knock:    sp.knock.test(used, attacker, rel, spearMobAction),
			damage:   sp.damage.test(used, attacker, rel, spearMobAction),
		}
		if !b.dismount && !b.knock && !b.damage {
			continue
		}
		b.dmg = base + math.Floor(rel*sp.mult)
		if tg.p != nil && h.stabPlayerByMob(players, m, tg.p, b) {
			affected = true
		}
		if tg.m != nil && h.stabMobByMob(players, m, tg.m, b) {
			affected = true
		}
	}
	if affected {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusKineticHit))
	}
}

// stabMobByMob is LivingEntity.stabAttack from a mob onto a creature it
// hunts: the blow, the shove and the dismount, and a zombie's kill of a
// villager infects it as a bite's would (Zombie.killedEntity).
func (h *hub) stabMobByMob(players map[int32]*tracked, m, v *mob, b spearBlow) bool {
	if v.dying > 0 {
		return false
	}
	landed := false
	if b.damage {
		dmg := b.dmg + float64(mobSharpness(m))
		if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
			dev := &plugin.EntityDamageByEntityEvent{AttackerEID: m.eid, VictimEID: v.eid, Damage: dmg}
			if !h.plugins.Fire(dev) {
				return false
			}
			dmg = math.Max(0, dev.Damage)
		}
		hp := v.health
		v.hurtKind(dmg, dtSpear)
		v.lastAttacker = m.eid
		landed = v.health < hp
	}
	if b.knock && v.kbScale() > 0 && v.health > 0 {
		h.mobKnockFrom(players, v, m.x, m.z)
	}
	dismounted := b.dismount && h.unseatMob(players, v)
	if !landed && !b.knock && !dismounted {
		return false
	}
	if v.health <= 0 {
		if zombieKind(m.etype) && v.etype == entityVillager {
			h.zombieKilledVillager(players, m, v)
		} else {
			h.killMob(players, v)
		}
		if m.preyTarget == v.eid {
			m.preyTarget = 0
		}
	} else if !v.hostile && panicsAt(v, dtMobAttack) {
		v.panic, v.fleeX, v.fleeZ, v.reroute = h.panicFor(v), m.x, m.z, 0
	} else if landed {
		h.mobHurtByMob(players, v, m) // a hoglin run through turns on the spearman
	}
	return true
}

// stabPlayerByMob is LivingEntity.stabAttack from a mob onto a player.
func (h *hub) stabPlayerByMob(players map[int32]*tracked, m *mob, v *tracked, b spearBlow) bool {
	if v.dead || (!isSurvival(v.gamemode) && v.gamemode != gmAdventure) {
		return false
	}
	landed := false
	if b.damage {
		dmg := float32(b.dmg) + mobSharpness(m) // EnchantmentHelper.modifyDamage
		if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
			dev := &plugin.EntityDamageByEntityEvent{AttackerEID: m.eid, VictimEID: v.p.eid,
				VictimIsPlayer: true, Damage: float64(dmg)}
			if !h.plugins.Fire(dev) {
				return false
			}
			dmg = float32(math.Max(0, dev.Damage))
		}
		landed = h.hurtFrom(players, v, dmg, dtSpear, deathCause{by: mobDisplayName(m.etype)},
			fromMobWeapon(m.x, m.z, m.held))
		if landed {
			v.lastHurtByMob = m.eid
			if lvl := m.heldStack().enchLvl(enchFireAspect); lvl > 0 {
				h.setBurning(players, v, 4*lvl)
			}
		}
	}
	if b.knock {
		extra := m.mobAttrs().Value(attr.AttackKnockback) + float64(m.heldStack().enchLvl(enchKnockback))
		h.knockbackScaled(v, m.x, m.z, (spearStabKnock+extra/2)/0.4)
	}
	dismounted := false
	if b.dismount && v.ridingEID != 0 {
		if !h.dismountMob(players, v) {
			h.dismount(players, v)
		}
		dismounted = true
	}
	return landed || b.knock || dismounted
}

// lunge is the Lunge enchantment's POST_PIERCING_ATTACK effect, run after
// every jab: afoot (not riding, not gliding, not in water) and — for a player
// out of creative — with more than three drumsticks, the wielder is thrown
// forward along the horizontal of their look, 0.458 blocks a tick per level,
// at the cost of a point of durability and 4 exhaustion per level.
//
// Lunge is not yet in the enchantment registry the gateways declare (it has
// to be APPENDED there, at id 42, so no saved enchantment moves); until it is,
// no stack can carry it and this never fires.
func (h *hub) lunge(players map[int32]*tracked, t *tracked) {
	lvl := heldStack(t).enchLvl(enchLunge)
	if lvl <= 0 || t.ridingEID != 0 || t.fallFlying || h.inWater(t.dim, t.x, t.y, t.z) {
		return
	}
	if t.gamemode != gmCreative && t.food < lungeMinFood {
		return
	}
	h.applyToolWear(t, t.p.heldSlot(), 1)
	if isSurvival(t.gamemode) {
		t.exhaust(lungeExhaustion * float32(lvl))
	}
	lx, _, lz := lookVector(t.yaw, t.pitch)
	mag := lungeImpulse * float64(lvl)
	kx, ky, kz := h.knownMove(t)
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: kx + lx*mag, VY: ky, VZ: kz + lz*mag})
	h.playSoundDim(players, t.dim, lungeSound(lvl), sndPlayer, t.x, t.y, t.z, 1, 1)
}

const (
	// enchLunge is Lunge's id: appended after the 1.21.5 registry's 42
	// (tachyne-common's extra26xEntries), so 42 everywhere.
	enchLunge       int8 = 42
	lungeImpulse         = 0.458 // ApplyEntityImpulse magnitude per level
	lungeExhaustion      = 4.0   // ApplyExhaustion per level
	lungeMinFood         = 7     // food level at least floor(6)+1
)

// lungeSound is the level's sound (lunge_1…lunge_3).
func lungeSound(lvl int) string {
	switch {
	case lvl >= 3:
		return "minecraft:item.spear.lunge_3"
	case lvl == 2:
		return "minecraft:item.spear.lunge_2"
	}
	return "minecraft:item.spear.lunge_1"
}
