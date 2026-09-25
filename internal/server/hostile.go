package server

import (
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
)

// Clientbound play packets used for hostile combat feedback.
const (
	playClientAnimation = 0x02 // entity animation (arm swing)
)

// Hostile mobs — zombies that hunt and attack survival players, the first mobs
// that fight back. They chase the nearest player in aggro range, bite on a
// cooldown, spawn at night near players, and burn away in daylight (so they don't
// accumulate). They share the mob struct + movement/collision (so fences pen them
// too); only the steering, the melee, and the spawn/cleanup rules are new.

const (
	zombieHealth   = 20 // 10 hearts (vanilla)
	zombieDamage   = 3  // normal-difficulty melee (easy 2 / hard 4)
	skeletonHealth = 20
	spiderHealth   = 16
	spiderDamage   = 2
	spiderAnger    = 100 // mob-updates a daytime spider stays vengeful after a hit

	// Skeleton kiting: approach to shooting range, back off if crowded.
	shootRange   = 15.0 // fire at a target inside this range
	tridentRange = 10.0 // DrownedTridentAttackGoal's attack radius
	// RangedBowAttackGoal(this, 1.0, 20, 15.0f): the goal closes while the
	// target is outside the attack radius, then stops and strafes — drifting
	// backwards inside a quarter of the radius squared (7.5 blocks) and
	// forwards again past three quarters of it (about 13).
	bowRadius    = 15.0
	skeletonKite = bowRadius * 0.5   // √0.25 · radius: back off inside this
	skeletonHold = bowRadius * 0.866 // √0.75 · radius: close in again past this

	aggroRange       = 16.0 // default FOLLOW_RANGE (vanilla Mob base; species override via m.aggro)
	deaggroSlack     = 8.0  // keep chasing this far past aggro before giving up (edge hysteresis)
	attackReach      = 2.0  // horizontal distance at which a bite lands
	attackReachY     = 2.0  // vertical tolerance (can't hit a player up a cliff)
	attackCooldown   = 9    // mob-updates between bites; +the biting update = 20 ticks
	creakingAttackCD = 19   // the creaking's 40 ticks, counted the same way
	//                          (vanilla-measured 995 ms cadence; 10 gave 1.1 s)
	standoffDist = 1.1 // stop closing here so it bites from the front, not buried
	//                       inside the player (where the player couldn't click it)
	// Knockback velocities MEASURED off vanilla's wire (oracle combat
	// experiment 2026-07-05: an unsprinting zombie's every hit sent the
	// player h≈0.22-0.24, v=0.275 blocks/tick — we previously shoved ~2×
	// too hard at 0.42/0.36).
	knockbackH = 0.23  // horizontal player knockback per bite (blocks/tick)
	knockbackV = 0.275 // upward component
	velUnit    = 8000  // Set Entity Velocity unit: 1/8000 block per tick

	// Daylight burn (natural spawning lives in spawn.go now).
	spawnMinDist     = 24 // vanilla: mobs never spawn within 24 blocks of a player
	burnDamagePerSec = 1  // vanilla fire: 1 HP/s — 20s of visible burning
	burnStaggerMax   = 8  // seconds of per-mob random ignition delay at dawn:
	//                          real dawn light ramps up, so the horde catches
	//                          fire (and dies) spread out, not on one tick
	dayStart   = 23000 // dawn: hostiles start burning (ticks into the MC day)
	nightStart = 13000 // dusk: hostiles may spawn
	dayLength  = 24000
)

var (
	entityZombie    = entityID("zombie") // minecraft:entity_type ordinals (1.21.5)
	entitySkeleton  = entityID("skeleton")
	entitySpider    = entityID("spider")
	itemRottenFlesh = itemByName["rotten_flesh"]
	itemArrowDrop   = itemByName["arrow"]
	itemString      = itemByName["string"]
	itemSpiderEye   = itemByName["spider_eye"]
)

// hostileBehavior steers a mob straight at its acquired target (set each update by
// acquireTarget); with no target in range it falls back to idle wandering, like a
// zombie milling about until a player comes near.
type hostileBehavior struct{}

func (hostileBehavior) name() string { return "hostile" }
func (hostileBehavior) steer(h *hub, m *mob) (float64, float64) {
	if !m.hasTarget {
		m.path = nil // drop any stale route when we lose the target
		return wanderBehavior{}.steer(h, m)
	}
	if m.flies || m.swims {
		stop := standoffDist
		if m.tamed {
			stop = 0.1 // a pet flier following its owner flies right up to them
		}
		return straightSteer(m, m.tx, m.tz, stop) // airborne/aquatic: no ground path
	}
	// A* around obstacles toward the target instead of walking straight into
	// walls, water and cliffs (which just made the mob jitter in place).
	return h.pathSteer(m, m.tx, m.tz)
}

// rangedBehavior is skeleton steering: keep the target at bow range — advance
// when far, retreat when crowded, stand and shoot in the sweet spot.
type rangedBehavior struct{}

func (rangedBehavior) name() string { return "ranged" }
func (rangedBehavior) steer(h *hub, m *mob) (float64, float64) {
	if !m.hasTarget {
		return wanderBehavior{}.steer(h, m)
	}
	dx, dz := m.tx-m.x, m.tz-m.z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return 0, 0
	}
	if d > bowRadius {
		return dx / d * m.moveSpeed(), dz / d * m.moveSpeed() // outside bow range: close in
	}
	// Inside it the goal strafes: a sideways half-speed circle plus a
	// forward/back half-speed drift, both flipping about 30% of the time each
	// second, held to the band between a quarter and three quarters of the
	// radius.
	if h.rng.Intn(33) == 0 {
		m.strafeCW = !m.strafeCW
	}
	if h.rng.Intn(33) == 0 {
		m.strafeBack = !m.strafeBack
	}
	switch {
	case d > skeletonHold:
		m.strafeBack = false
	case d < skeletonKite:
		m.strafeBack = true
	}
	sx, sz := -dz/d, dx/d // perpendicular to the firing line
	if m.strafeCW {
		sx, sz = -sx, -sz
	}
	fx, fz := dx/d, dz/d
	if m.strafeBack {
		fx, fz = -fx, -fz
	}
	sp := m.moveSpeed() * 0.5
	return (sx + fx) * sp, (sz + fz) * sp
}

// blazeBehavior is BlazeAttackGoal's movement: a blaze that can see its
// target holds its ground and fires from wherever it is, closing only to
// within two blocks for a punch and, for a few ticks after losing sight, on
// the spot it last saw the target. It never backs off or strafes.
type blazeBehavior struct{}

func (blazeBehavior) name() string { return "blaze" }
func (blazeBehavior) steer(h *hub, m *mob) (float64, float64) {
	if !m.hasTarget {
		return wanderBehavior{}.steer(h, m)
	}
	dx, dz := m.tx-m.x, m.tz-m.z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return 0, 0
	}
	lost := m.unseenTicks > 0 && m.unseenTicks < 5 // lastSeen < 5
	if d >= 2 && !lost {
		return 0, 0
	}
	return dx / d * m.moveSpeed(), dz / d * m.moveSpeed()
}

// holdRangedBehavior is vanilla's plain RangedAttackGoal: walk in until the
// target is inside the attack radius, then stand and throw. A trident drowned
// uses it — it does not kite or strafe the way a skeleton does.
type holdRangedBehavior struct{ radius float64 }

func (holdRangedBehavior) name() string { return "hold-ranged" }
func (b holdRangedBehavior) steer(h *hub, m *mob) (float64, float64) {
	if !m.hasTarget {
		return wanderBehavior{}.steer(h, m)
	}
	dx, dz := m.tx-m.x, m.tz-m.z
	d := math.Hypot(dx, dz)
	if d < 1e-6 || d <= b.radius {
		return 0, 0 // in range: stand and throw
	}
	return dx / d * m.moveSpeed(), dz / d * m.moveSpeed()
}

// skeletonShoot fires an arrow at the nearest huntable player, on a cooldown.
func (h *hub) skeletonShoot(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	q, ok := h.rangedQuarry(players, m, shootRange)
	if !ok {
		return
	}
	m.yaw = float32(math.Atan2(-(q.x-m.x), q.z-m.z) * 180 / math.Pi) // face the shot
	if !h.seesQuarry(m, q, true) {
		return // RangedBowAttackGoal: the shot needs line of sight
	}
	h.spawnArrowAt(players, m, q.x, q.aimY, q.z)
	h.playSoundDim(players, m.dim, "minecraft:entity.skeleton.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
	// RangedBowAttackGoal cadence (AbstractSkeleton.getAttackInterval): 40
	// ticks on easy/normal, 20 on hard; a parched or a bogged draws slower,
	// 70 and 50 (Bogged/Parched.getAttackInterval, getHardAttackInterval).
	// attackCD counts mob-updates (2 ticks) incl. this one.
	m.attackCD = 19
	if h.rules.Difficulty == diffHard {
		m.attackCD = 9
	}
	if m.etype == entityIllusioner {
		m.attackCD = 9 // Illusioner's RangedBowAttackGoal(this, 0.5, 20, 15): twenty ticks, any difficulty
	}
	if m.etype == entityParched || m.etype == entityBogged {
		m.attackCD = 34
		if h.rules.Difficulty == diffHard {
			m.attackCD = 24
		}
	}
}

// burnsInDaylight is 26.3's #burn_in_daylight (Mob.isSunBurnTick's gate):
// the zombie horse and the zombie nautilus burn too, the parched and the husk
// do not. It replaced a per-species flag that only hostiles carried.
var burnsInDaylight = entitySet("skeleton", "stray", "wither_skeleton", "bogged", "zombie", "zombie_horse",
	"zombie_villager", "drowned", "zombie_nautilus", "phantom")

// rollZombieBaby applies vanilla's getSpawnAsBabyOdds: 5% of zombie-family
// spawns are babies — half-size, 1.5× speed (SPEED_MODIFIER_BABY +0.5
// multiplied-base), never maturing, and worth 2.5× XP (already in xpForMob).
func (h *hub) rollZombieBaby(players map[int32]*tracked, m *mob) {
	if h.rng.Float64() >= 0.05 {
		return
	}
	m.baby = true
	m.setBabySpeed(true)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
}

// The SPAWN_REINFORCEMENTS_CHANCE modifiers Zombie keeps: the leader's
// bonus, the charge a caller pays for each recruit it summons, and the one
// every recruit starts with.
const (
	leaderZombieBonusSource  = "minecraft:leader_zombie_bonus"
	reinforceCallerSource    = "minecraft:reinforcement_caller_charge"
	reinforceCalleeSource    = "minecraft:reinforcement_callee_charge"
	reinforcementCharge      = -0.05
	zombieLeaderChance       = 0.05
	reinforcementAttempts    = 50
	reinforcementRangeMin    = 7
	reinforcementRangeSpread = 34 // 7..40 inclusive
)

// reinforcementChance is the zombie's SPAWN_REINFORCEMENTS_CHANCE (0 for
// every species that never rolled one).
func (m *mob) reinforcementChance() float64 {
	return m.mobAttrs().Value(attr.SpawnReinforcements)
}

// rollReinforcements is the reinforcement half of Zombie.handleAttributes
// at spawn: a base chance of 0..0.1 (randomizeReinforcementsChance), and a
// leader roll at 5% of the special multiplier. A leader carries 0.5..0.75
// extra chance — the zombies that summon whole sieges — 1..4 times more
// MAX_HEALTH on top of its own, and can break doors. It runs after the
// door roll, as vanilla's does, so a leader always breaks doors.
func (h *hub) rollReinforcements(m *mob) {
	a := m.mobAttrs()
	a.SetBase(attr.SpawnReinforcements, h.rng.Float64()*0.1)
	if h.rng.Float64() >= h.specialMultiplier()*zombieLeaderChance {
		return
	}
	a.Get(attr.SpawnReinforcements).AddModifier(attr.Modifier{Source: leaderZombieBonusSource,
		Amount: h.rng.Float64()*0.25 + 0.5, Op: attr.AddValue})
	a.Get(attr.MaxHealth).AddModifier(attr.Modifier{Source: leaderZombieBonusSource,
		Amount: h.rng.Float64()*3 + 1, Op: attr.AddMultipliedTotal})
	if !h.reloading { // a reload keeps the health it was saved with
		m.health = m.maxHP()
	}
	m.breaksDoors = true
}

// zombieReinforce implements Zombie.hurtServer's reinforcement call
// (vanilla behavior): HARD difficulty only, doMobSpawning on, chance = the
// zombie's SPAWN_REINFORCEMENTS_CHANCE. On success a fresh same-species
// zombie appears 7-40 blocks away (never within 7 of a player), already
// hunting the attacker. The recruit rolls its own chance as any new zombie
// does and starts 0.05 down on it; the caller's charge grows by 0.05 with
// every recruit it summons.
func (h *hub) zombieReinforce(players map[int32]*tracked, m *mob, attacker *tracked) {
	chance := m.reinforcementChance()
	if chance <= 0 || h.rules.Difficulty != diffHard || !h.rules.DoMobSpawning {
		return
	}
	if h.rng.Float64() >= chance {
		return
	}
	for i := 0; i < reinforcementAttempts; i++ {
		off := func() int {
			return (reinforcementRangeMin + h.rng.Intn(reinforcementRangeSpread)) * (h.rng.Intn(3) - 1)
		}
		sx, sz := int(m.x)+off(), int(m.z)+off()
		if !h.worldFor(m.dim).Spawnable(sx, sz) {
			continue
		}
		if h.nearestPlayer(players, float64(sx), float64(sz), 7) != nil {
			continue // vanilla: reinforcements never appear within 7 blocks
		}
		r := h.spawnHostileIn(players, m.etype, m.dim, sx, sz)
		if r == nil {
			return // plugin-cancelled spawn
		}
		if attacker != nil {
			r.hasTarget, r.tx, r.tz = true, attacker.x, attacker.z
		}
		r.mobAttrs().Get(attr.SpawnReinforcements).AddModifier(attr.Modifier{Source: reinforceCalleeSource,
			Amount: reinforcementCharge, Op: attr.AddValue})
		in := m.mobAttrs().Get(attr.SpawnReinforcements)
		charge := reinforcementCharge
		for _, mod := range in.Modifiers() {
			if mod.Source == reinforceCallerSource {
				charge += mod.Amount
			}
		}
		in.RemoveModifier(reinforceCallerSource)
		in.AddModifier(attr.Modifier{Source: reinforceCallerSource, Amount: charge, Op: attr.AddValue})
		return
	}
}

// How bright is bright enough to put a spider off: vanilla compares the
// light-dependent "magic value" (the brightness ramp) against 0.5, which on
// the overworld's ramp is light level 12.
const (
	spiderLightNeutral = 0.5
	spiderDropOdds     = 100 / mobMoveInterval // 1-in-100 per TICK, and we run every other one
)

// lightMagic is Entity.getLightLevelDependentMagicValue: the brightness ramp
// (l/15)/(4-3·l/15) over the effective light at the mob's eyes.
func (h *hub) lightMagic(m *mob) float64 {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0
	}
	sky, block := w.LightAt(floorInt(m.x), floorInt(m.y+mobEyeHeight(m)), floorInt(m.z))
	f := float64(h.rawBrightness(sky, block, -1)) / 15
	return f / (4 - 3*f)
}

// isDayTime reports whether the world clock is in the daylight window (the
// same boundary the burn/spawn rules use).
func (h *hub) isDayTime() bool {
	day := h.dayTime.Load() % dayLength
	return day < nightStart || day >= dayStart
}

// acquireTarget latches the nearest huntable player, with aggro/de-aggro
// hysteresis: an idle mob only wakes to a player within aggroRange, but once
// hunting it keeps chasing out to deaggroRange before giving up.
func (h *hub) acquireTarget(players map[int32]*tracked, m *mob) {
	// Spiders go neutral in the LIGHT, not by the clock (SpiderTargetGoal):
	// a spider in a dark cave hunts at noon, one standing in a lit base does
	// not hunt at midnight, and one already chasing gives up now and then
	// once it is in the light (SpiderAttackGoal's one-in-a-hundred).
	if m.etype == entitySpider || m.etype == entityCaveSpider {
		if h.lightMagic(m) >= spiderLightNeutral && m.anger == 0 {
			if !m.hasTarget || h.rng.Intn(spiderDropOdds) == 0 {
				m.hasTarget = false
				return
			}
		} else if m.anger > 0 {
			m.anger--
		}
	}
	// Endermen also aggro on a STARE (vanilla isBeingStaredBy): a player's
	// crosshair on their eyes provokes them; a carved pumpkin exempts.
	if m.etype == entityEnderman && m.anger == 0 && h.staredAt(players, m) {
		m.anger = 200 // hunts ~20 s per provocation (refreshed while stared at)
		m.settled = 0 // targetChangeTime: the daylight flight waits 600 ticks from here
	}
	// Neutral species (endermen) never START a fight — anger from a hit (or
	// the stare above) does.
	if m.neutral {
		if m.anger == 0 {
			m.hasTarget = false
			return
		}
		m.anger--
	}
	// FOLLOW_RANGE is per-species in vanilla (vanilla 1.21.5: Mob default
	// 16, Zombie family 35, Blaze 48, EnderMan 64); +8 hysteresis to de-aggro.
	reach := m.followRange()
	if m.hasTarget {
		reach += deaggroSlack
	}
	if m.etype == entityWarden {
		// The warden walks only after its ATTACK_TARGET (WardenAi's FIGHT
		// SetWalkTargetFromAttackTargetIfTargetOutOfReach), which it has
		// once it has roared at somebody it is angry at.
		m.preyTarget = 0
		if t := players[m.wardenTarget]; t != nil && isSurvival(t.gamemode) && !t.dead && t.dim == m.dim {
			m.hasTarget, m.tx, m.tz = true, t.x, t.z
		} else {
			m.hasTarget = false
		}
		return
	}
	if m.etype == entityPiglin {
		// PiglinAi: a player in a piece of gold armour is left alone, and an
		// admiring piglin has eyes only for its gold.
		// StartAttacking is gated on isAdult: a baby piglin hunts nobody.
		m.piglinCoolDown()
		if t := h.piglinTarget(players, m, reach); t != nil && m.admireUntil == 0 && !m.baby {
			m.hasTarget, m.tx, m.tz = true, t.x, t.z
		} else {
			m.hasTarget = false
		}
		return
	}
	if t := h.huntTarget(players, m, reach); t != nil { // mustSee: a player it can see, or one remembered
		m.hasTarget, m.tx, m.tz = true, t.x, t.z
		if m.flies {
			m.ty = t.y // a flier aims at the target's height, not the ground
		}
		m.preyTarget = 0
	} else if tx, tz, ok := h.nearestQuarry(noPlayers, m.dim, m.x, m.z, reach); ok { // a shadow over the seam: no blocks are known across it to see through
		m.hasTarget, m.tx, m.tz = true, tx, tz
		m.preyTarget = 0
	} else if huntsPrey(m.etype) {
		// No player: whatever else this species hunts — the villagers and
		// iron golems a zombie or a raider goes for, an enderman's
		// endermite, a guardian's squid, a fox's chickens (vanilla's other
		// NearestAttackableTargetGoals, which sit below the player one).
		if v := h.nearestPrey(m, preyReach(m)); v != nil {
			m.hasTarget, m.tx, m.tz, m.preyTarget = true, v.x, v.z, v.eid
		} else {
			m.hasTarget, m.preyTarget = false, 0
		}
	} else {
		m.hasTarget = false
	}
}

// nearestQuarry is the aggro-acquisition candidate set: the nearest huntable
// REAL player on this pod, or the nearest huntable cross-seam SHADOW of one (a
// survival player standing just over the border). A shadow is a chaseable
// position, nothing more — the chase itself carries the mob across the seam
// (migrateMobAcross), where it becomes real on the player's pod and the normal
// targeting/melee take over. Melee never bites a shadow (mobMelee scans real
// players only), so no damage routing is needed here.
func (h *hub) nearestQuarry(players map[int32]*tracked, dim int, x, z, maxDist float64) (float64, float64, bool) {
	bestD2 := maxDist * maxDist
	var bx, bz float64
	found := false
	if t := h.nearestHuntable(players, dim, x, z, maxDist); t != nil {
		bx, bz, found = t.x, t.z, true
		bestD2 = (t.x-x)*(t.x-x) + (t.z-z)*(t.z-z)
	}
	for _, se := range h.shadowIn {
		if se.kind != handover.KindPlayer || se.dim != dim || !isSurvival(se.gamemode) {
			continue // only survival players are hunted (dead ones cast no shadow at all)
		}
		if d2 := (se.x-x)*(se.x-x) + (se.z-z)*(se.z-z); d2 < bestD2 {
			bx, bz, bestD2, found = se.x, se.z, d2, true
		}
	}
	return bx, bz, found
}

// nearestHuntable is nearestPlayer restricted to living survival players (creative/
// spectator/dead players are ignored — nothing to hunt or hurt).
func (h *hub) nearestHuntable(players map[int32]*tracked, dim int, x, z, maxDist float64) *tracked {
	var best *tracked
	bestD2 := maxDist * maxDist
	for _, t := range players {
		if !isSurvival(t.gamemode) || t.dead || t.dim != dim {
			continue // hunt only in the mob's own dimension
		}
		if d2 := (t.x-x)*(t.x-x) + (t.z-z)*(t.z-z); d2 < bestD2 {
			best, bestD2 = t, d2
		}
	}
	return best
}

// mobMelee bites a survival player standing within reach, on an attack cooldown.
// Damage flows through the normal player-damage path (hurt flash, death drops).
func (h *hub) mobMelee(players map[int32]*tracked, m *mob) {
	if m.etype == entityBee && m.beeStingDie > 0 {
		return // vanilla: a bee that has stung attacks no more (it dies of it)
	}
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, attackReach)
	if m.etype == entityPiglin {
		// A piglin swings only at its own target (PiglinAi's attack target):
		// never at a bystander in gold who happens to be standing close.
		t = h.piglinTarget(players, m, attackReach)
	}
	if m.etype == entityWarden {
		// MeleeAttack hits the warden's ATTACK_TARGET and nobody else.
		t = nil
		if w := players[m.wardenTarget]; w != nil && isSurvival(w.gamemode) && !w.dead && w.dim == m.dim &&
			(w.x-m.x)*(w.x-m.x)+(w.z-m.z)*(w.z-m.z) < attackReach*attackReach {
			t = w
		}
	}
	if t == nil || math.Abs(t.y-m.y) > attackReachY {
		if t == nil && m.preyTarget != 0 {
			h.mobBitesPrey(players, m) // no player in reach: the creature it hunts
		}
		return
	}
	// Mob.doHurtTarget: ATTACK_DAMAGE (the weapon's included), then the
	// weapon's Sharpness (EnchantmentHelper.modifyDamage). Smite and Bane never
	// match a player.
	dmg := hostileMelee(m) + mobHeldBonus(m) + mobSharpness(m)
	if m.etype == entityHoglin || m.etype == entityZoglin {
		dmg = h.hoglinBiteDamage(m) // hurtAndThrowTarget: half plus a roll
	}
	// Plugin damage event (mob → player), before the swing so a cancel makes
	// the whole bite invisible.
	if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
		dev := &plugin.EntityDamageByEntityEvent{AttackerEID: m.eid, VictimEID: t.p.eid,
			VictimIsPlayer: true, Damage: float64(dmg)}
		if !h.plugins.Fire(dev) {
			return
		}
		dmg = float32(dev.Damage)
	}
	// Swing the arm so the bite is visible (not just "walking into you"), deal the
	// hit, and knock the player back — which also unglues them so they can retaliate.
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	if m.etype == entityRavager {
		h.ravagerBite(players, m) // doHurtTarget: the ten-tick pause and the bite animation
	}
	if m.etype == entityHoglin || m.etype == entityZoglin {
		h.hoglinBiteStart(players, m) // doHurtTarget: the animation and the grunt
	}
	if m.etype == entityWarden {
		// Warden.doHurtTarget: the attack animation, the impact, and the
		// sonic boom put back 40 ticks.
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusAttack))
		h.playSoundDim(players, m.dim, "minecraft:entity.warden.attack_impact", sndHostile, m.x, m.y, m.z, 10, h.voicePitch(m))
		m.sonicCD = wardenSonicCoolUpd
		defer func() { m.attackCD = wardenMeleeCD }()
	}
	landed := h.hurtFrom(players, t, dmg, mobMeleeDamage(m.etype),
		deathCause{by: mobDisplayName(m.etype)}, fromMobWeapon(m.x, m.z, m.held))
	// A caught bite still shoves them. Mob.getKnockback is ATTACK_KNOCKBACK
	// plus the weapon's Knockback, halved, on top of the 0.4 the hurt itself
	// gives — and 0.4 is what a scale of 1 means here, so each unit of the
	// attribute is worth 1.25 of scale.
	extraKB := m.mobAttrs().Value(attr.AttackKnockback) + float64(m.heldStack().enchLvl(enchKnockback))
	h.knockbackScaled(t, m.x, m.z, 1+1.25*extraKB)
	if landed {
		t.lastHurtByMob = m.eid // the owner's wolves take note
		if lvl := m.heldStack().enchLvl(enchFireAspect); lvl > 0 {
			h.setBurning(players, t, 4*lvl) // Fire Aspect: 4 s per level
		}
	}
	if landed && (m.etype == entityHoglin || m.etype == entityZoglin) && !m.baby {
		h.hoglinThrow(t, m) // HoglinBase.throwTarget: tossed up and away
	}
	if landed && m.etype == entityHoglin {
		h.hoglinBroadcastTarget(players, m, t) // onHitTarget: the pack joins in
	}
	if m.etype == entityHoglin || m.etype == entityZoglin {
		defer func() { m.attackCD = hoglinAttackCD(m) }() // ATTACK_INTERVAL 40 (15 for a piglet)
	}
	if m.etype == entityCreaking {
		defer func() { m.attackCD = creakingAttackCD }() // Creaking.ATTACK_INTERVAL: 40 ticks between blows
	}
	if !landed {
		// A raised shield facing the attacker catches the whole bite, and with
		// it everything the bite would have delivered.
		m.attackCD = attackCooldown
		if m.etype == entityRavager {
			h.ravagerBlocked(players, m, t) // blockedByItem: a stun, or a hurl
		}
		return
	}
	h.thornsRetaliate(players, t, m) // armour that bites back
	if t.dead {                      // the bite was fatal: adventure/root's killed_by_something
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
		h.incStat(t, attachproto.StatKilledBy, int32(m.etype), 1)
	}
	// A bee spends its sting: one hit, then sixty seconds to live.
	if m.etype == entityBee && m.beeStingDie == 0 {
		m.beeStingDie = beeStingDieSecs
	}
	// Species that envenom or wither on a bite (cave spider, bee, wither skeleton).
	if d := speciesOf(m.etype); d != nil {
		if secs := d.poisonFor(h.rules.Difficulty); secs > 0 {
			h.applyEffect(players, t, effPoison, 0, secs)
		}
		if d.wither > 0 {
			h.applyEffect(players, t, effWither, 0, d.wither)
		}
	}
	// A husk's bite inflicts Hunger (vanilla Husk: 140 ticks × effective
	// difficulty, ≈7 s per level; peaceful never reaches melee).
	if m.etype == entityHusk {
		if secs := 7 * int(h.rules.Difficulty); secs > 0 {
			h.applyEffect(players, t, effHunger, 0, secs)
		}
	}
	m.attackCD = attackCooldown
}

// itemBow resolves from the generated canonical registry — a hardcoded 841
// survived the 1.21.5→1.21.11 item id migration and became light_blue_harness
// (the happy-ghast goggles), which every skeleton then proudly held.
var itemBow = int32(itemByName["bow"])

// skeletonEquip builds the set_equipment putting a bow in a skeleton's hand.
func skeletonEquip(eid int32) attachproto.Equipment {
	return equipEv(eid, invStack{item: itemBow, count: 1}, invStack{}, [4]invStack{})
}

// fireMetadata builds set_entity_data toggling the shared entity-flags "on
// fire" bit — what makes the client actually render flames on a burning mob.
func fireMetadata(eid int32, on bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, 0)     // index 0: shared entity flags
	b = protocol.AppendVarInt(b, 0) // type 0: byte
	var flags byte
	if on {
		flags = 0x01
	}
	b = protocol.AppendU8(b, flags)
	return protocol.AppendU8(b, itemMetaEnd)
}

// swingArm builds Entity Animation (0x02): swing the main arm (animation 0).
func swingArm(eid int32) attachproto.Swing {
	return attachproto.Swing{EID: eid}
}

// evArmSwing is a player's own swing, from the client (at air, a block, a
// mob): shown to everyone watching them, as LivingEntity.swing broadcasts
// it. The swinger's client has drawn it already.
type evArmSwing struct {
	eid  int32
	hand int32
}

func (evArmSwing) isHubEvent() {}

func (h *hub) onArmSwing(players map[int32]*tracked, e evArmSwing) {
	t := players[e.eid]
	if t == nil || t.dead {
		return
	}
	h.toOthersNear(players, e.eid, t.dim, t.x, t.z, attachproto.Swing{EID: e.eid, Hand: e.hand})
}

// mobKnockVelocity animates a mob's knockback impulse client-side: vanilla
// sends set_entity_velocity on every hit so the client plays the shove (and
// hit-hop) between our relative moves. The moves stay authoritative — the
// client tracks the server position from deltas regardless of the velocity
// animation — so this is pure feel, no drift.
func (h *hub) mobKnockVelocity(players map[int32]*tracked, m *mob) {
	// The hop is vanilla's min(0.4, vy/2 + strength) on a grounded victim.
	// Any real hit already clears 0.4, so in practice it is the ceiling — the
	// point of computing it rather than hardcoding is the SMALL shoves, where
	// a light nudge should lift a mob less than a mace does. It was a flat
	// 0.36 before, just under vanilla's ceiling for every hit. The strength is
	// read back off the impulse, so every knockback source gets it for free.
	vx, vz := m.vx/mobMoveInterval, m.vz/mobMoveInterval
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Velocity{
		EID: m.eid, VX: vx, VY: math.Min(0.4, math.Hypot(vx, vz)), VZ: vz})
}

// knockback shoves a player away from (fromX,fromZ) via Set Entity Velocity, the
// same way vanilla knockback is applied — server-sent velocity the client obeys.
func (h *hub) knockback(t *tracked, fromX, fromZ float64) {
	h.knockbackScaled(t, fromX, fromZ, 1)
}

// knockbackScaled is knockback with a fraction of the shove getting through —
// what Blast Protection's EXPLOSION_KNOCKBACK_RESISTANCE buys.
func (h *hub) knockbackScaled(t *tracked, fromX, fromZ, scale float64) {
	if scale <= 0 {
		return
	}
	dx, dz := t.x-fromX, t.z-fromZ
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		dx, dz, d = 1, 0, 1
	}
	// LivingEntity.knockback: KNOCKBACK_RESISTANCE eats its share of the
	// shove, which is what a netherite set is for.
	if r := t.playerAttrs().Value(attr.KnockbackResistance); r > 0 {
		if scale *= 1 - r; scale <= 0 {
			return
		}
	}
	t.p.trySendEv(attachproto.Velocity{
		EID: t.p.eid,
		VX:  dx / d * knockbackH * scale,
		VY:  knockbackV * scale,
		VZ:  dz / d * knockbackH * scale,
	})
}

// spawnZombie creates a hostile zombie at a column and returns it.
func (h *hub) spawnZombie(players map[int32]*tracked, x, z int) *mob {
	return h.spawnHostileIn(players, entityZombie, dimOverworld, x, z)
}

// spawnHostileIn stands a hostile on the surface of column (x, z) in dim.
func (h *hub) spawnHostileIn(players map[int32]*tracked, etype, dim, x, z int) *mob {
	w := h.worldFor(dim)
	if w == nil {
		return nil
	}
	return h.spawnHostileYIn(players, etype, dim, float64(x)+0.5, float64(w.SurfaceFeet(x, z)), float64(z)+0.5)
}

// spawnHostileYIn spawns a hostile at an exact position in a dimension.
func (h *hub) spawnHostileYIn(players map[int32]*tracked, etype, dim int, x, y, z float64) *mob {
	m := h.spawnMobIn(players, etype, dim, x, y, z)
	if m == nil {
		return nil // plugin-cancelled spawn
	}
	m.hostile, m.behavior = true, Behavior(hostileBehavior{}) // speed from speedFor
	switch etype {
	case entityZombie, entitySkeleton:
		m.burnDelay = h.rng.Intn(burnStaggerMax)
		if etype == entityZombie {
			m.setFollowRange(35) // Zombie FOLLOW_RANGE override (vanilla 1.21.5)
			m.setBaseArmor(2)    // Zombie base ARMOR attribute (vanilla 1.21.5)
			h.rollZombieBaby(players, m)
			if !h.reloading { // finalizeSpawn extras roll once, never on a chunk reload
				h.rollChickenJockey(players, m)
				m.breaksDoors = h.rng.Float64() < h.specialMultiplier()*0.1 // setCanBreakDoors(random < f × 0.1)
			}
			h.rollReinforcements(m) // handleAttributes, after the door roll
		}
		if etype == entitySkeleton {
			m.behavior = rangedBehavior{}
			// Show the bow (pure visual — the arrows are real either way).
			h.toTracking(players, m.eid, m.dim, m.x, m.z, skeletonEquip(m.eid))
		}
	case entitySpider:
		// (spider speed comes from speedFor: attr 0.30; they survive the day, neutral until dark)
		if !h.reloading {
			h.rollSpiderJockey(players, m)
			h.rollSpiderEffect(players, m)
		}
	case entityCreeper:
		m.behavior = creeperBehavior{}
	default:
		if !h.configureHostile2(players, m) { // pack-2 species quirks…
			h.applySpecies(players, m) // …else a roster species from the table
		}
	}
	if !h.reloading { // a reloaded mob brings its own gear and flags back
		h.spawnGear(players, m) // armour and weapons by regional difficulty
		h.rollCanPickup(m)      // some hostiles spawn able to grab dropped gear
	}
	return m
}

// updateHostiles runs once per second: despawn far mobs, burn sky-exposed
// hostiles in daylight so the night's horde clears at dawn, and run the
// insomnia check that summons phantoms. Natural SPAWNING happens per tick in
// spawn.go; attacks + chasing happen at the faster mob-update cadence.
func (h *hub) updateHostiles(players map[int32]*tracked) {
	if len(players) == 0 {
		return
	}
	day := h.dayTime.Load() % dayLength
	h.despawnSweep(players)                 // vanilla checkDespawn for every non-persistent category
	h.tickSkeletonTraps(players)            // armed skeleton horses spring on approach
	if h.rules.Difficulty == diffPeaceful { // peaceful: hostiles never linger
		for _, m := range h.mobs {
			if m.hostile && m.dying == 0 {
				h.removeMob(players, m)
			}
		}
	}
	// Daylight burn: sky-exposed UNDEAD hostiles catch fire in the open. This
	// just relights the 8-second afterburn clock each second they're exposed
	// (like vanilla setSecondsOnFire(8)); the unified burn ticker in
	// mobEnvironment renders the flame, deals the damage, and puts them out a
	// few seconds after they reach cover. Spiders/creepers don't burn.
	if day < nightStart && !h.raining { // rain shields the undead (vanilla)
		for _, m := range h.mobs {
			if !burnsInDaylight[m.etype] || fireImmune[m.etype] || m.dim != 0 {
				continue
			}
			if h.skyExposed(m) {
				if m.burnDelay > 0 { // still in this mob's slice of the dawn ramp
					m.burnDelay--
					continue
				}
				if h.sunHelmetTakesIt(players, m) {
					continue // a helmet takes the sun (and wears away under it)
				}
				m.ignite(8)
			}
		}
	} else {
		for _, m := range h.mobs { // night fell mid-burn: put survivors out
			if m.burning {
				m.burning = false
				h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
			}
		}
	}
	if day >= nightStart && day < dayStart {
		h.phantomSpawner(players)
	}
}

// skyExposed reports whether open sky sits above the mob — the daylight-burn
// and rained-on test. Checked from the MOB'S OWN HEIGHT: a cave mob has rock
// between it and the sky and a penned mob has its roof, even though the
// column above the surface is open. (The old surface-based scan set cave
// zombies on fire through thirty blocks of stone.)
func (h *hub) skyExposed(m *mob) bool {
	return h.skyExposedAt(int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z)))
}

// skyExposedAt scans from a body's height to just above the column surface —
// any opaque cell in between (cave roof, built roof, overhang) blocks the sky.
func (h *hub) skyExposedAt(x, y, z int) bool {
	top := h.world.SurfaceFeet(x, z)
	if y > top {
		top = y
	}
	for yy := y; yy < top+6; yy++ {
		if worldgen.SkyOpacity(h.world.At(x, yy, z)) == worldgen.Opaque {
			return false
		}
	}
	return true
}

func (h *hub) skyExposedColumn(x, z int) bool {
	top := h.world.SurfaceFeet(x, z)
	for y := top; y < top+6; y++ {
		if worldgen.SkyOpacity(h.world.At(x, y, z)) == worldgen.Opaque {
			return false
		}
	}
	return true
}

// mobMeleeDamage is the damage type a species' melee deals. Almost everything
// bites with mob_attack; a bee stings, which is its own type in vanilla
// (Bee.doHurtTarget uses damageSources().sting) and reads as such in the death
// message.
func mobMeleeDamage(etype int) dmgType {
	if etype == entityBee {
		return dtSting
	}
	return dtMobAttack
}
