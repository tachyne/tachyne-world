package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/worldgen"
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
	skeletonKite = 5.0  // retreat when the target is closer than this
	skeletonHold = 10.0 // advance until inside this, then stand and shoot

	aggroRange     = 16.0 // default FOLLOW_RANGE (vanilla Mob base; species override via m.aggro)
	deaggroSlack   = 8.0  // keep chasing this far past aggro before giving up (edge hysteresis)
	attackReach    = 2.0  // horizontal distance at which a bite lands
	attackReachY   = 2.0  // vertical tolerance (can't hit a player up a cliff)
	attackCooldown = 9    // mob-updates between bites; +the biting update = 20 ticks
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

	// Night spawning + daylight burn.
	// Vanilla mob cap (MobCategory.MONSTER): 70 per 17×17 loaded chunks,
	// scaled by the chunks players actually have streamed.
	monsterCapPerChunk = 70
	spawnMinDist       = 24 // vanilla: never within 24 blocks of a player…
	spawnMaxDist       = 80 // …out to the streamed radius (vanilla: any loaded
	//                        chunk ≤128; ours spawn where players will meet them)
	burnDamagePerSec = 1 // vanilla fire: 1 HP/s — 20s of visible burning
	burnStaggerMax   = 8 // seconds of per-mob random ignition delay at dawn:
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
		return straightSteer(m, m.tx, m.tz, standoffDist) // airborne/aquatic: no ground path
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
	switch {
	case d < skeletonKite:
		return -dx / d * m.speed, -dz / d * m.speed // too close — back off
	case d > skeletonHold:
		return dx / d * m.speed, dz / d * m.speed // close in to bow range
	}
	// In the sweet spot: vanilla RangedBowAttackGoal STRAFES — circle the
	// target at 0.5× speed, flipping direction ~30% of the time each second.
	if h.rng.Intn(33) == 0 {
		m.strafeCW = !m.strafeCW
	}
	sx, sz := -dz/d, dx/d // perpendicular to the firing line
	if m.strafeCW {
		sx, sz = -sx, -sz
	}
	return sx * m.speed * 0.5, sz * m.speed * 0.5
}

// skeletonShoot fires an arrow at the nearest huntable player, on a cooldown.
func (h *hub) skeletonShoot(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, shootRange)
	if t == nil {
		return
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi) // face the shot
	h.spawnArrow(players, m, t)
	h.playSound(players, "minecraft:entity.skeleton.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
	// RangedBowAttackGoal cadence (vanilla behavior): 40 ticks on easy/normal,
	// 20 on hard. attackCD counts mob-updates (2 ticks) incl. this one.
	m.attackCD = 19
	if h.rules.Difficulty == diffHard {
		m.attackCD = 9
	}
}

// rollZombieBaby applies vanilla's getSpawnAsBabyOdds: 5% of zombie-family
// spawns are babies — half-size, 1.5× speed (SPEED_MODIFIER_BABY +0.5
// multiplied-base), never maturing, and worth 2.5× XP (already in xpForMob).
func (h *hub) rollZombieBaby(players map[int32]*tracked, m *mob) {
	if h.rng.Float64() >= 0.05 {
		return
	}
	m.baby = true
	m.speed *= 1.5
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
}

// rollReinforcements gives a fresh zombie its SPAWN_REINFORCEMENTS_CHANCE
// (vanilla Zombie.finalizeSpawn): random 0..0.1, and 5% are "leaders"
// carrying an extra 0.5..0.75 — the zombies that summon whole sieges.
func (h *hub) rollReinforcements() float64 {
	c := h.rng.Float64() * 0.1
	if h.rng.Float64() < 0.05 {
		c += 0.5 + h.rng.Float64()*0.25
	}
	return c
}

// zombieReinforce implements Zombie.hurtServer's reinforcement call
// (vanilla behavior): HARD difficulty only, doMobSpawning on, chance = the zombie's
// SPAWN_REINFORCEMENTS_CHANCE. On success a fresh same-species zombie appears
// 7-40 blocks away (never within 7 of a player), already hunting the
// attacker; caller and recruit each lose 0.05 chance.
func (h *hub) zombieReinforce(players map[int32]*tracked, m *mob, attacker *tracked) {
	if m.reinf <= 0 || m.dim != 0 || h.rules.Difficulty != diffHard || !h.rules.DoMobSpawning {
		return
	}
	if h.rng.Float64() >= m.reinf {
		return
	}
	for i := 0; i < 50; i++ {
		off := func() int { return (7 + h.rng.Intn(34)) * (h.rng.Intn(3) - 1) }
		sx, sz := int(m.x)+off(), int(m.z)+off()
		if !h.world.Spawnable(sx, sz) {
			continue
		}
		if h.nearestPlayer(players, float64(sx), float64(sz), 7) != nil {
			continue // vanilla: reinforcements never appear within 7 blocks
		}
		r := h.spawnHostile(players, m.etype, sx, sz)
		r.reinf = math.Max(0, m.reinf-0.05)
		if attacker != nil {
			r.hasTarget, r.tx, r.tz = true, attacker.x, attacker.z
		}
		m.reinf = math.Max(0, m.reinf-0.05)
		return
	}
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
	// Spiders are neutral in daylight (vanilla): they only hunt at night — or
	// while angry at whoever just hit them.
	if m.etype == entitySpider && h.isDayTime() {
		if m.anger == 0 {
			m.hasTarget = false
			return
		}
		m.anger--
	}
	// Endermen also aggro on a STARE (vanilla isBeingStaredBy): a player's
	// crosshair on their eyes provokes them; a carved pumpkin exempts.
	if m.etype == entityEnderman && m.anger == 0 && h.staredAt(players, m) {
		m.anger = 200 // hunts ~20 s per provocation (refreshed while stared at)
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
	reach := m.aggro
	if reach == 0 {
		reach = aggroRange
	}
	if m.hasTarget {
		reach += deaggroSlack
	}
	if tx, tz, ok := h.nearestQuarry(players, m.dim, m.x, m.z, reach); ok {
		m.hasTarget, m.tx, m.tz = true, tx, tz
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
		if se.kind != handover.KindPlayer || se.dim != dim || se.gamemode != gmSurvival {
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
		if t.gamemode != gmSurvival || t.dead || t.dim != dim {
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
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, attackReach)
	if t == nil || math.Abs(t.y-m.y) > attackReachY {
		return
	}
	// Swing the arm so the bite is visible (not just "walking into you"), deal the
	// hit, and knock the player back — which also unglues them so they can retaliate.
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	dmg := hostileMelee(m) * h.diffMult()
	// A raised shield facing the attacker catches the whole bite (damage +
	// on-hit venom), but the knockback still lands.
	if h.shieldBlocks(t, m.x, m.z) {
		h.shieldBlockFX(players, t)
		h.knockback(t, m.x, m.z)
		return
	}
	h.damage(players, t, t.armorReduce(dmg))
	if t.dead { // the bite was fatal: adventure/root's killed_by_something
		h.advance(players, t, "entity_killed_player", advMatch{entity: advEntityName[m.etype]})
	}
	h.wearArmor(players, t, dmg)
	h.knockback(t, m.x, m.z)
	// Species that envenom or wither on a bite (cave spider, bee, wither skeleton).
	if d := speciesOf(m.etype); d != nil {
		if secs := d.poisonFor(h.rules.Difficulty); secs > 0 {
			h.applyEffect(players, t, effPoison, 0, secs)
		}
		if d.wither > 0 {
			h.applyEffect(players, t, effWither, 0, d.wither)
		}
	}
	m.attackCD = attackCooldown
}

const itemBow = 841

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

// mobKnockVelocity animates a mob's knockback impulse client-side: vanilla
// sends set_entity_velocity on every hit so the client plays the shove (and
// hit-hop) between our relative moves. The moves stay authoritative — the
// client tracks the server position from deltas regardless of the velocity
// animation — so this is pure feel, no drift.
func (h *hub) mobKnockVelocity(players map[int32]*tracked, m *mob) {
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Velocity{
		EID: m.eid, VX: m.vx / mobMoveInterval, VY: 0.36, VZ: m.vz / mobMoveInterval})
}

// knockback shoves a player away from (fromX,fromZ) via Set Entity Velocity, the
// same way vanilla knockback is applied — server-sent velocity the client obeys.
func (h *hub) knockback(t *tracked, fromX, fromZ float64) {
	dx, dz := t.x-fromX, t.z-fromZ
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		dx, dz, d = 1, 0, 1
	}
	t.p.trySendEv(attachproto.Velocity{
		EID: t.p.eid, VX: dx / d * knockbackH, VY: knockbackV, VZ: dz / d * knockbackH})
}

// spawnZombie creates a hostile zombie at a column and returns it.
func (h *hub) spawnZombie(players map[int32]*tracked, x, z int) *mob {
	return h.spawnHostile(players, entityZombie, x, z)
}

// spawnHostile creates a night mob of the given type at a column, wiring its
// species-specific behavior, speed and daylight rules.
func (h *hub) spawnHostile(players map[int32]*tracked, etype, x, z int) *mob {
	return h.spawnHostileY(players, etype, float64(x)+0.5, float64(h.world.SurfaceFeet(x, z)), float64(z)+0.5)
}

// spawnHostileY spawns a configured hostile at an explicit position (dungeon
// spawners put mobs underground, not on the surface).
func (h *hub) spawnHostileY(players map[int32]*tracked, etype int, x, y, z float64) *mob {
	m := h.spawnMob(players, etype, x, y, z)
	m.hostile, m.behavior = true, Behavior(hostileBehavior{}) // speed from speedFor
	switch etype {
	case entityZombie, entitySkeleton:
		m.burns = true // the undead burn at dawn
		m.burnDelay = h.rng.Intn(burnStaggerMax)
		if etype == entityZombie {
			m.aggro = 35 // Zombie FOLLOW_RANGE override (vanilla 1.21.5)
			m.armor = 2  // Zombie base ARMOR attribute (vanilla 1.21.5)
			m.reinf = h.rollReinforcements()
			h.rollZombieBaby(players, m)
		}
		if etype == entitySkeleton {
			m.behavior = rangedBehavior{}
			// Show the bow (pure visual — the arrows are real either way).
			h.toNearbyEv(players, m.dim, m.x, m.z, skeletonEquip(m.eid))
		}
	case entitySpider:
		// (spider speed comes from speedFor: attr 0.30; they survive the day, neutral until dark)
	case entityCreeper:
		m.behavior = creeperBehavior{}
	default:
		if !h.configureHostile2(players, m) { // pack-2 species quirks…
			h.applySpecies(players, m) // …else a roster species from the table
		}
	}
	return m
}

// rollHostileType picks a species for a night spawn (vanilla-ish weighting).
func (h *hub) rollHostileType() int {
	switch r := h.rng.Intn(100); {
	case r < 45:
		return entityZombie
	case r < 70:
		return entitySkeleton
	case r < 88:
		return entitySpider
	}
	return entityCreeper
}

// updateHostiles runs once per second: spawn zombies at night near players (up to
// a cap) and burn sky-exposed hostiles in daylight so the night's horde clears at
// dawn. Attacks + chasing happen at the faster mob-update cadence, not here.
func (h *hub) updateHostiles(players map[int32]*tracked) {
	h.updateNetherMobs(players)
	if len(players) == 0 {
		return
	}
	day := h.dayTime.Load() % dayLength
	// Vanilla despawn rules (vanilla Mob.checkDespawn; hostiles only —
	// CREATURE-category animals are persistent and never despawn): instant
	// beyond 128 blocks of every same-dimension player; beyond 32 blocks an
	// idle clock runs and past 600 ticks each tick has a 1/800 chance —
	// ≈2.5%/s, our sweep is 1 Hz so Intn(40); within 32 the clock resets.
	for _, m := range h.mobs {
		if !m.hostile || m.dying > 0 || m == h.dragon {
			continue
		}
		best := math.Inf(1)
		for _, t := range players {
			if t.dim != m.dim {
				continue
			}
			if d := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z); d < best {
				best = d
			}
		}
		switch {
		case best > 128*128: // includes "no player in this dimension"
			h.removeMob(players, m)
		case best > 32*32:
			if m.idleSecs++; m.idleSecs > 30 && h.rng.Intn(40) == 0 {
				h.removeMob(players, m)
			}
		default:
			m.idleSecs = 0
		}
	}
	if h.rules.Difficulty == diffPeaceful { // peaceful: hostiles never linger
		for _, m := range h.mobs {
			if m.hostile && m.dying == 0 {
				h.removeMob(players, m)
			}
		}
	}
	// Rained-on endermen warp away (vanilla water phobia).
	if h.raining {
		for _, m := range h.mobs {
			if m.etype == entityEnderman && m.dim == 0 && m.dying == 0 && h.rng.Intn(4) == 0 && h.skyExposed(m) {
				h.endermanTeleport(players, m)
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
			if !m.hostile || !m.burns || m.dim != 0 {
				continue
			}
			if h.skyExposed(m) {
				if m.burnDelay > 0 { // still in this mob's slice of the dawn ramp
					m.burnDelay--
					continue
				}
				m.ignite(8)
			}
		}
	} else {
		for _, m := range h.mobs { // night fell mid-burn: put survivors out
			if m.burning {
				m.burning = false
				h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
			}
		}
	}
	// Night spawning: one PACK attempt per second near a random player, under
	// the vanilla mob cap.
	if !h.rules.DoMobSpawning || h.rules.Difficulty == diffPeaceful {
		return
	}
	if day < nightStart || day >= dayStart {
		return
	}
	// A rare phantom swoops in at night (vanilla: the sleepless-player harrier;
	// ours is a low-odds overhead spawn).
	if h.rng.Intn(30) == 0 {
		for _, t := range players {
			if t.dim == 0 && t.gamemode == gmSurvival && !t.dead {
				h.spawnPhantom(players, t)
				break
			}
		}
	}
	// Vanilla cap (NaturalSpawner): MONSTER 70 × spawnableChunks / 17² —
	// spawnableChunks = the union of chunks the overworld players actually
	// have streamed (vanilla counts a 17×17 window per player; ours is the
	// per-player view radius, so one radius-6 player caps at 70×169/289 ≈ 40).
	chunks := map[[2]int32]bool{}
	hostiles := 0
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		r := t.p.radius()
		cx, cz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for x := cx - r; x <= cx+r; x++ {
			for z := cz - r; z <= cz+r; z++ {
				chunks[[2]int32{x, z}] = true
			}
		}
	}
	for _, m := range h.mobs {
		if m.hostile && m.dim == 0 {
			hostiles++
		}
	}
	mobCap := monsterCapPerChunk * len(chunks) / 289
	if hostiles >= mobCap {
		return
	}
	var pick *tracked
	for _, t := range players { // any player; map order is effectively random
		if t.dim == 0 {
			pick = t
			break
		}
	}
	if pick == nil {
		return
	}
	ang := h.rng.Float64() * 2 * math.Pi
	dist := spawnMinDist + h.rng.Intn(spawnMaxDist-spawnMinDist)
	sx := int(pick.x) + int(math.Cos(ang)*float64(dist))
	sz := int(pick.z) + int(math.Sin(ang)*float64(dist))
	if !h.ownedBlock(sx, sz) {
		return // don't spawn outside this pod's region
	}
	// Vanilla spawns in packs of up to 4 of one species around the point.
	etype := h.rollHostileFor(sx, sz)
	pack := 1 + h.rng.Intn(4)
	for i := 0; i < pack && hostiles < mobCap; i++ {
		px, pz := sx, sz
		if i > 0 { // pack-mates scatter a few blocks off the anchor
			px += h.rng.Intn(9) - 4
			pz += h.rng.Intn(9) - 4
		}
		if !h.world.Spawnable(px, pz) {
			continue
		}
		// Hostiles only spawn in the dark (vanilla: light level 0). Sky light
		// is night everywhere now, so torch light is what keeps a room safe.
		if h.world.BlockLightAt(px, h.world.MobFeet(px, pz), pz) > 0 {
			continue
		}
		h.spawnHostile(players, etype, px, pz)
		hostiles++
	}
}

// skyExposed reports whether open sky sits above the mob (nothing opaque within a
// few blocks over its head) — the daylight-burn test.
func (h *hub) skyExposed(m *mob) bool {
	return h.skyExposedColumn(int(math.Floor(m.x)), int(math.Floor(m.z)))
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
