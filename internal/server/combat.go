package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/plugin"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Mob combat: a player attacks a mob with the Interact Entity packet; the hub
// applies damage, knocks the mob back with a hurt flash, and on death plays the
// death animation before despawning it and dropping its loot (a survival food
// source — cows drop beef). Runs on the hub goroutine alongside the entity state.

const (
	fistDamage = 1  // bare-hand melee (vanilla; swords are craftable now)
	cowHealth  = 10 // 5 hearts

	playClientEntityStatus = 0x1e // entity_event/entity_status (death animation, etc.)
	entityStatusDeath      = 3    // living-entity death: tip over, redden, play sound
	deathAnimTicks         = 20   // vanilla death animation length (~1 s) before despawn
)

// meleeDamage is a held item's attack damage (vanilla values, halves rounded
// down for shovels). Item ids from minecraft-data items.json (1.21.5). Anything
// not listed hits like a fist.
var meleeDamage = itemIntMap(map[string]int{
	"wooden_sword": 4, "golden_sword": 4, "stone_sword": 5, "copper_sword": 5, "iron_sword": 6, "diamond_sword": 7, "netherite_sword": 8,
	"wooden_axe": 7, "golden_axe": 7, "stone_axe": 9, "copper_axe": 9, "iron_axe": 9, "diamond_axe": 9, "netherite_axe": 10,
	"wooden_pickaxe": 2, "golden_pickaxe": 2, "stone_pickaxe": 3, "copper_pickaxe": 3, "iron_pickaxe": 4, "diamond_pickaxe": 5, "netherite_pickaxe": 6,
	"wooden_shovel": 2, "golden_shovel": 2, "stone_shovel": 3, "copper_shovel": 3, "iron_shovel": 4, "diamond_shovel": 5, "netherite_shovel": 6,
	"mace":    6, // vanilla: +5 attack-damage modifier over the player's base 1
	"trident": 9, // TridentItem: +8 over the base 1, the same in melee as thrown
	// Spears (Item.Properties.spear): the base 1 plus the material's attack
	// bonus and nothing else — the jab is light; the charge is the weapon.
	"wooden_spear": 1, "golden_spear": 1, "stone_spear": 2, "copper_spear": 2, "iron_spear": 3, "diamond_spear": 4, "netherite_spear": 5,
})

// itemIntMap / itemFloatMap resolve name-keyed tables to id-keyed (version-independent).
func itemIntMap(m map[string]int) map[int32]int {
	r := make(map[int32]int, len(m))
	for n, v := range m {
		if id, ok := itemByName[n]; ok {
			r[id] = v
		}
	}
	return r
}

func itemFloatMap(m map[string]float64) map[int32]float64 {
	r := make(map[int32]float64, len(m))
	for n, v := range m {
		if id, ok := itemByName[n]; ok {
			r[id] = v
		}
	}
	return r
}

// mobHealth is a mob type's starting hit points.
func mobHealth(etype int) int {
	switch etype {
	case entityCow:
		return cowHealth
	case entityZombie:
		return zombieHealth
	case entitySkeleton:
		return skeletonHealth
	case entitySpider:
		return spiderHealth
	case entityCreeper:
		return creeperHealth
	case entityHusk, entityDrowned:
		return zombieHealth
	case entityStray:
		return skeletonHealth
	case entityEnderman:
		return endermanHealth
	case entityWitch:
		return witchHealth
	case entitySlime:
		return 16 // size 4²; splits carry their own
	case entityZombifiedPiglin:
		return piglinHealth
	case entityBlaze:
		return blazeHealth
	case entityMagmaCube:
		return 4 // resized on spawn (size²)
	case entityChicken:
		return chickenHealth
	case entityPig:
		return pigHealth
	case entitySheep:
		return sheepHealth
	case entityIronGolem:
		return ironGolemHealth
	}
	if d := speciesOf(etype); d != nil { // roster species: from the table
		return d.health
	}
	return cowHealth
}

// ironGolemHealth is IronGolem.createAttributes' MAX_HEALTH; the golem is
// not on the roster table (it belongs with the villagers), and without this
// it fell through to the cow's ten.
const ironGolemHealth = 100

// meleeDamageFor is a species' base ATTACK_DAMAGE, seeded at spawn. Slimes and
// magma cubes are absent: their damage is their size, set by applyCubeSize.
func meleeDamageFor(etype int) float64 {
	switch etype {
	case entitySpider:
		return spiderDamage
	case entityEnderman:
		return endermanDamage
	case entityZombifiedPiglin:
		return 5 // ZombifiedPiglin.createAttributes; the golden sword adds its +3
	case entityBlaze:
		return 6 // Blaze.createAttributes ATTACK_DAMAGE (it fell through to the zombie's 3)
	}
	if d := speciesOf(etype); d != nil { // roster species: from the table
		return float64(d.damage)
	}
	return zombieDamage
}

// hostileMelee is a hostile mob's bite damage (skeletons shoot, creepers
// explode — neither reaches here). It reads ATTACK_DAMAGE, so a modifier from
// a weapon, an effect or a plugin lands here without a new special case.
func hostileMelee(m *mob) float32 {
	switch m.etype {
	case entitySlime:
		if m.size <= 1 {
			return 0 // AbstractCubeMob.isDealsDamage: a tiny slime cannot hurt you
		}
	case entityMagmaCube:
		return float32(m.attackDamage()) + 2 // MagmaCube.getAttackDamage
	}
	return float32(m.attackDamage())
}

// maxMeleeReach is the server-side sanity cap on a melee hit's distance:
// vanilla survival reach is ~3 blocks, plus generous slack for latency and
// entity movement between the client's swing and our processing. AUTHORITY:
// beyond this, the claimed hit is a hacked client's kill-aura — ignored.
const maxMeleeReach = 6.0

// attackMob applies a player's melee hit to a mob, killing it at 0 health.
// swing is one melee swing's worked-out numbers, shared by every target a
// player can hit. Extracted when PvP arrived: the arithmetic is long (weapon
// base, attributes, cooldown scaling, crit, mace smash, enchantment bonuses)
// and a second copy of it for players would have drifted from the mob one
// within a release.
type swing struct {
	dmg        int
	base       float64
	charge     float64
	full       bool // attackStrengthScale > 0.9: a crit, the strong sound and a sweep need it
	crit       bool
	smash      bool
	fall       float64
	breachFrac float64
}

// meleeSwing works out what the attacker's swing is worth. familyBonus is the
// Smite / Bane of Arthropods contribution, which depends on the victim and so
// is the caller's to supply — against another player it is always zero.
func (h *hub) meleeSwing(t *tracked, familyBonus float64) swing {
	base := float64(fistDamage)
	sharpBonus := 0.0 // enchantment damage (Sharpness): added AFTER crit, not multiplied
	charge, scale, crit := 1.0, 1.0, false
	smash, fall := false, 0.0 // mace smash attack + its fall distance
	var breachFrac float64
	if t != nil {
		held := t.p.heldItem()
		if d, ok := meleeDamage[held]; ok {
			base = float64(d) // a crafted weapon hits harder than a fist
		}
		if lvl := heldStack(t).enchLvl(enchSharpness); lvl > 0 {
			sharpBonus = 0.5*float64(lvl) + 0.5 // vanilla: 1.0 + 0.5·(lvl-1); I=+1, V=+3
		}
		// Smite and Bane of Arthropods are the same effect component as
		// Sharpness with a condition on the target's family, so they add in
		// the same place — and only one of them can ever match.
		sharpBonus += familyBonus
		// The weapon sets the ATTACK_DAMAGE base and everything else — Strength,
		// Weakness, and whatever else lands on the attribute — is a modifier on
		// top, exactly as vanilla layers them.
		t.playerAttrs().SetBase(attr.AttackDamage, base)
		base = t.playerAttrs().Value(attr.AttackDamage)
		if base < 0 {
			base = 0
		}
		// Attack cooldown (1.9 combat): a swing before the weapon recovers is
		// scaled by 0.2 + 0.8×charge² — spam-clicking does a fifth of the damage.
		now := h.tick.Load()
		// ATTACK_SPEED scales how fast the weapon recovers, which is what Haste
		// and Mining Fatigue modify — the effects set the attribute and nothing
		// read it before, so neither changed a swing.
		period := t.attackPeriodTicks(attackPeriod(held))
		// getAttackStrengthScale(0.5): (ticks since the swing + 0.5) over the
		// weapon's delay, capped at one. The base damage takes 0.2 + 0.8×scale²,
		// the enchantments' bonus the scale itself.
		if dt := now - t.lastAttack; t.lastAttack != 0 && dt < uint64(period) {
			scale = math.Min(1, (float64(dt)+0.5)/float64(period))
			charge = 0.2 + 0.8*scale*scale
		}
		t.lastAttack = now
		// Critical: full-charge, falling, not sprinting, not in water, not blind
		// (vanilla Player.attack crit gate). ×1.5 on the base+strength portion.
		if scale > 0.9 && t.airborne && t.y < t.peakY && !t.sprinting &&
			!h.inWater(t.dim, t.x, t.y, t.z) && t.hasEffect(effBlindness) == 0 {
			crit = true
		}
		// Mace smash: falling past the threshold adds fall-distance bonus damage
		// (density scales it). Breach is armor_effectiveness −0.15 a level on
		// every blow the weapon deals, smash or not.
		smash, fall = maceSmashing(t)
		breachFrac = 0.15 * float64(heldStack(t).enchLvl(enchBreach))
		if t.gamemode == gmSurvival {
			t.exhaust(attackExhaustion)                 // vanilla: attacking burns food
			if n := attackWear(t.p.heldItem()); n > 0 { // Weapon.itemDamagePerAttack
				h.applyToolWear(t, t.p.heldSlot(), n)
			}
		}
	}
	dmgF := base * charge
	if smash { // vanilla adds the fall bonus after the cooldown scale, before the crit ×1.5
		dmgF += maceFallBonus(fall) + 0.5*float64(heldStack(t).enchLvl(enchDensity))*fall
	}
	if crit {
		dmgF *= 1.5
	}
	dmgF += sharpBonus * scale // magicBoost: the enchantments' bonus times the raw scale, added after crit
	dmg := int(math.Max(1, math.Round(dmgF)))
	return swing{dmg: dmg, base: base, charge: charge, full: scale > 0.9, crit: crit,
		smash: smash, fall: fall, breachFrac: breachFrac}
}

func (h *hub) attackMob(players map[int32]*tracked, attacker, target int32) {
	m := h.mobs[target]
	if m == nil || m.dying > 0 {
		return // not a mob (player/item), or already dying — ignore
	}
	if t := players[attacker]; t != nil {
		t.lastHitMob = target // their wolves join in (OwnerHurtTargetGoal)
		if t.dim != m.dim {
			return // cross-dimension hits are impossible
		}
		dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
		if dx*dx+dy*dy+dz*dz > maxMeleeReach*maxMeleeReach {
			return // hit claimed from across the map — not physically possible
		}
	}
	t := players[attacker]
	// A mob-on-mob hit has no attacker player, so there is no held stack to
	// read a Smite or Bane bonus off — the nil check has to happen HERE, not
	// inside the swing helper, because heldStack itself dereferences.
	family := 0.0
	if t != nil {
		family = familyMeleeBonus(heldStack(t), m.etype)
	}
	sw := h.meleeSwing(t, family)
	base, full, crit := sw.base, sw.full, sw.crit
	smash, fall, breachFrac := sw.smash, sw.fall, sw.breachFrac
	dmg := sw.dmg
	_ = fall

	// Plugin damage event: fires with the final amount, before any effect
	// (sound, knockback, hurt) — a cancel makes the swing a complete no-op.
	if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
		dev := &plugin.EntityDamageByEntityEvent{AttackerEID: attacker, VictimEID: target,
			AttackerIsPlayer: t != nil, Damage: float64(dmg)}
		if !h.plugins.Fire(dev) {
			return
		}
		dmg = int(math.Max(0, math.Round(dev.Damage)))
	}

	// A guardian's spikes bite back at whoever is close enough to punch it —
	// before its own damage lands, exactly as Guardian.hurtServer orders it.
	h.guardianThorns(players, m, attacker)

	if crit {
		h.spawnParticles(players, m.dim, particleCrit, m.x, m.y+1, m.z, 0.4, 0.2, 8)
		h.playSoundDim(players, m.dim, "minecraft:entity.player.attack.crit", sndPlayer, m.x, m.y, m.z, 1, 1)
	} else if full {
		h.playSoundDim(players, m.dim, "minecraft:entity.player.attack.strong", sndPlayer, m.x, m.y, m.z, 1, 1)
	} else {
		h.playSoundDim(players, m.dim, "minecraft:entity.player.attack.weak", sndPlayer, m.x, m.y, m.z, 1, 1)
	}

	// A sulfur cube carrying a block is a ball: the blow goes through its own
	// knockback model (and, mostly, its immunity) rather than the mob's.
	if m.hasBody() && t != nil {
		dt := dtPlayerAttack
		if smash {
			dt = dtMaceSmash
		}
		h.cubeStruckByPlayer(players, m, t, float64(dmg), dt)
		return
	}

	// Real knockback: shove the mob away from the attacker (server physics —
	// the impulse rides out uncapped for a few updates). Sprinting hits harder.
	if t != nil {
		if kdx, kdz := m.x-t.x, m.z-t.z; (kdx != 0 || kdz != 0) && m.kbScale() > 0 {
			d := math.Hypot(kdx, kdz)
			// Vanilla: base hurt-knockback 0.4, +0.5 sprint, +0.5 per Knockback level.
			power := 0.4
			if t.sprinting {
				power += 0.5
			}
			power += 0.5 * float64(heldStack(t).enchLvl(enchKnockback))
			power *= m.kbScale() // LivingEntity.knockback: power *= 1 − resistance
			// LivingEntity.knockback keeps HALF of what the mob already had and
			// takes the shove off that, so a zombie charging in is turned
			// rather than simply reset — its own momentum still counts against
			// it. Per mob update, not per tick: m.v* is the step.
			step := power * mobMoveInterval
			m.vx, m.vz = m.vx/2+kdx/d*step, m.vz/2+kdz/d*step
			m.kb, m.reroute = 3, 0
			h.mobKnockVelocity(players, m)
		}
		// Sweep: a full-charge, grounded, non-sprinting sword swing clips
		// everything beside the target. Vanilla damage = 1 + sweepingEdgeRatio·
		// base (ratio = lvl/(lvl+1)); without Sweeping Edge it is a flat 1.
		if _, sword := swordPeriod[t.p.heldItem()]; sword && full && !crit && t.onGround && !t.sprinting {
			sweepDmg := 1.0
			if se := heldStack(t).enchLvl(enchSweepingEdge); se > 0 {
				sweepDmg += float64(se) / float64(se+1) * base
			}
			sweep := int(math.Max(1, math.Round(sweepDmg)))
			for _, om := range h.mobs {
				if om == m || om.dying > 0 {
					continue
				}
				if dist3(om.x, om.y, om.z, m.x, m.y, m.z) > 1.5 {
					continue
				}
				om.hitByPlayer = true
				om.hurt(float64(sweep))
				h.incCustom(t, "damage_dealt", tenths(float32(sweep)))
				if om.health <= 0 {
					h.killMob(players, om)
					h.advance(players, t, "player_killed_entity", advMatch{entity: advEntityName[om.etype]})
					h.incStat(t, attachproto.StatKilled, int32(om.etype), 1)
					h.incCustom(t, "mob_kills", 1)
					h.sbCriteria(players, "totalKillCount", t.p.name, 1, false)
				}
			}
			// The sweep reaches every LIVING thing beside the target, which
			// includes other players — it had only ever touched mobs.
			if h.rules.PvP {
				for _, o := range players {
					if o == t || o.dead || o.dim != t.dim || o.gamemode != gmSurvival {
						continue
					}
					if dist3(o.x, o.y, o.z, m.x, m.y, m.z) > 1.5 {
						continue
					}
					h.hurtFrom(players, o, float32(sweep), dtPlayerAttack,
						deathCause{by: t.p.name}, from(t.x, t.z))
					h.incCustom(t, "damage_dealt", tenths(float32(sweep)))
				}
			}
			h.playSoundDim(players, t.dim, "minecraft:entity.player.attack.sweep", sndPlayer, t.x, t.y, t.z, 1, 1)
		}
	}

	m.hitByPlayer = true // its death now pays XP (vanilla: player-caused only)
	m.lastAttacker = attacker
	if t != nil {
		m.looting = heldStack(t).enchLvl(enchLooting)
	}
	hpBefore := m.health
	if m.etype == entityPiglin && t != nil {
		h.piglinHurtByPlayer(players, m) // PiglinAi.wasHurtBy: admiring stops, and stays off a while
	}
	if m.etype == entityArmadillo {
		h.armadilloHurtByLiving(players, m) // Armadillo.actuallyHurt: danger, and it rolls up
	}
	melee := float64(dmg)
	if m == h.dragon {
		// EnderDragon.hurtServer routes a blow with no part attached to the
		// BODY, which takes a quarter. A melee hit carries no part here — the
		// engine keeps the dragon as one entity on the wire, so a client can
		// only ever name the whole dragon — and the body is the honest answer.
		melee = dragonPartDamage("body", melee)
	}
	// A mace smash is its OWN damage type (MaceItem.getItemDamageSource →
	// damageSources().mace), which is what makes the death message read
	// "was smashed by" instead of the plain player attack.
	dt := dtPlayerAttack
	if smash {
		dt = dtMaceSmash
	}
	m.hurtOf(melee, breachFrac, dt) // through base armor (zombie family has 2), less breach
	if t != nil {
		h.incCustom(t, "damage_dealt", tenths(float32(dmg)))
		if taken := float64(hpBefore - m.health); taken >= 0 && float64(dmg) > taken {
			h.incCustom(t, "damage_dealt_resisted", tenths(float32(float64(dmg)-taken))) // armour, Resistance
		}
	}
	if t != nil {
		h.advance(players, t, "player_hurt_entity", advMatch{damageDirect: "player", mainhand: heldStack(t).item,
			damageTags: map[string]bool{"mace_smash": smash}, dealt: float64(dmg)})
	}
	if t != nil {
		h.applyFireAspect(players, t, m)              // Fire Aspect: 4 s alight per level
		h.applyBaneSlowness(players, heldStack(t), m) // Bane of Arthropods: Slowness IV
	}
	if smash { // shockwave, fall-damage negation, wind_burst launch
		h.smashEffects(players, t, m, fall)
	}
	h.mobStruck(players, m, t, dt)
}

// mobStruck is what follows a player's blow landing on a mob, whatever the
// weapon: the kin and reinforcements it alerts, the kill and its credit, or
// the hurt flash and the mob's answer (panic, a grudge, a retaliation). The
// melee swing and the spear's stab and charge all end here.
func (h *hub) mobStruck(players map[int32]*tracked, m *mob, t *tracked, dt dmgType) {
	h.alertKin(m, t)                 // HurtByTargetGoal.setAlertOthers: the neighbours join in
	h.zombieReinforce(players, m, t) // hard mode: a hurt zombie may call for backup
	if m.etype == entityZombifiedPiglin && t != nil {
		h.alertZombifiedPiglins(m, t)
	}
	if m.etype == entityVillager && t != nil {
		h.villagerHurtBy(players, m, t, m.health <= 0) // VILLAGER_HURT / VILLAGER_KILLED gossip
	}
	if m.health <= 0 {
		h.killMob(players, m)
		if t != nil {
			h.advance(players, t, "player_killed_entity", advMatch{entity: advEntityName[m.etype]})
			h.incStat(t, attachproto.StatKilled, int32(m.etype), 1)
			h.incCustom(t, "mob_kills", 1)
			h.sbCriteria(players, "totalKillCount", t.p.name, 1, false)
		}
		return
	}
	// Hurt flash. A passive mob bolts away in panic; a hostile one shrugs the hit
	// off and keeps hunting (it doesn't flee its prey).
	yaw := m.yaw
	if t != nil {
		h.traderLlamasDefend(m, t)
		if m.etype == entityIronGolem {
			// IronGolem's HurtByTargetGoal: it does not flee and it does not
			// need a grudge — hit it and it comes after you.
			m.golemGrudgeEID, m.golemGrudgeLeft = t.p.eid, golemGrudgeTicks
		} else if m.retaliates { // wolf/goat/bee/llama: a hit turns the herd hostile
			h.provoke(m, t)
		} else if !m.hostile && panicsAt(m, dt) { // the species' own PanicGoal, if it has one
			m.panic, m.fleeX, m.fleeZ, m.reroute = panicTicks, t.x, t.z, 0
		} else {
			if m.etype == entityWarden {
				h.wardenAngerAt(m, t.p.eid, wardenAngerHurt) // ANGRY + 20 at whoever struck
			}
			m.anger = spiderAnger                   // a hit spider/enderman retaliates
			m.targetEID, m.unseenTicks = t.p.eid, 0 // HurtByTargetGoal: the attacker, seen or not
			m.settled = 0                           // a new target restarts the enderman's daylight clock
			if m.etype == entityEnderman {
				h.endermanTeleport(players, m) // blinks away from the blow
			}
		}
		if dx, dz := m.x-t.x, m.z-t.z; dx != 0 || dz != 0 {
			yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		}
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: yaw})
	if hurt, _, _ := h.mobSoundsFor(m); hurt != "" {
		h.playSoundDim(players, m.dim, hurt, sndNeutral, m.x, m.y, m.z, 1, h.hurtPitch())
	}
}

// killMob begins a mob's death: it plays the death animation (Entity Status 3 —
// the mob tips over, reddens and sinks) and freezes in place; updateMobs despawns
// it and drops its loot when the animation finishes (deathAnimTicks later). This
// gives the vanilla death transition instead of the mob vanishing instantly.
func (h *hub) killMob(players map[int32]*tracked, m *mob) {
	if m.dying > 0 {
		return // already dying
	}
	m.dying = deathAnimTicks
	m.vx, m.vz, m.panic = 0, 0, 0   // stop moving while it dies
	h.ominousOnMobDeath(players, m) // wind-charged, weaving and oozing are LivingEntity-wide
	if m.patrolCaptain {            // entities/pillager: a raid captain's death drops its ominous bottle, however it died
		h.dropOminousBottle(players, m)
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusDeath))
	if _, death, _ := h.mobSoundsFor(m); death != "" {
		h.playSoundDim(players, m.dim, death, sndNeutral, m.x, m.y, m.z, 1, h.hurtPitch())
	}
}

// despawnMob removes a fully-dead mob and drops its loot (called from updateMobs
// once the death animation has played out).
func (h *hub) despawnMob(players map[int32]*tracked, m *mob) {
	delete(h.mobs, m.eid)
	h.gridDirty()
	if m.mount != 0 { // a mob rider died — detach it from its vehicle's view
		if v := h.mobs[m.mount]; v != nil {
			h.freeMobSeat(players, v, m.eid)
		}
	}
	if m.cart != 0 { // died in a cart or a boat: its seat is free, whoever else is aboard stays
		if v := h.vehicles[m.cart]; v != nil {
			v.mobRider, v.mobFirst = 0, false
			h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.passengers()...))
		}
	}
	h.spillHorse(players, m) // a mount's saddle/armor/chest drop with it
	h.entityGone(players, m.dim, m.eid)
	h.shadowGoneAll(m.eid) // retract any cross-seam shadow of it
	if m.etype == entityWither {
		h.clearBossBar(players, m) // pull the boss bar the instant it dies
	}
	if m.etype == entityEnderDragon {
		h.dragonDefeated(players)
		return
	}
	if m.etype == entitySlime || m.etype == entityMagmaCube {
		h.splitSlime(players, m) // halves pop out
	}
	if m.etype == entitySulfurCube {
		h.splitSulfurCube(players, m) // two babies, unless its fuse was burning
	}

	// Roll everything the death yields FIRST, so the plugin death event can
	// mutate the drop list and XP before anything hits the ground.
	// Gamerule doMobLoot=false silences the roll entirely.
	var drops []plugin.ItemStack
	if h.rules.DoMobLoot { // gamerule doMobLoot=false silences the roll
		if m.patrolCaptain { // the captain drops its ominous banner (raid trigger later)
			drops = append(drops, plugin.ItemStack{Item: itemByName["white_banner"], Count: 1})
		}
		// Picked-up gear drops in full (vanilla drops equipped loot at 100%);
		// gear issued at spawn (ominous trial mobs) never does.
		// An enchanted piece drops as itself (the plugin drop list carries
		// bare ids); a plain one goes through the list like any other drop.
		dropGear := func(st invStack) {
			if st.ench[0].id == 0 && st.ench[0].lvl == 0 && st.dmg == 0 {
				drops = append(drops, plugin.ItemStack{Item: st.item, Count: max(st.count, 1)})
				return
			}
			if it := h.spawnItemIn(players, m.dim, st.item, max(st.count, 1), m.x, m.y, m.z); it != nil {
				it.ench, it.dmg = st.ench, st.dmg
				h.refreshItemMeta(players, it)
			}
		}
		if m.held != 0 && h.rng.Float32() < h.gearDropChance(m, gearSlotHand) { // DropChances: picked up = certain, spawn gear = its roll
			dropGear(m.heldStack())
		}
		for slot, g := range m.gear {
			if slot == 0 && m.spawnGear && isPumpkinHead(g.item) {
				continue // a Halloween pumpkin has a drop chance of 0
			}
			if g.item != 0 && h.rng.Float32() < h.gearDropChance(m, slot) {
				dropGear(g)
			}
		}
		if m.etype != entityVillager { // a villager's pockets are lost with it (no dropCustomDeathLoot)
			for _, st := range m.hoard { // a piglin's gold, and whatever it was admiring
				drops = append(drops, plugin.ItemStack{Item: st.item, Count: st.count})
			}
		}
		if m.offhand.item != 0 {
			drops = append(drops, plugin.ItemStack{Item: m.offhand.item, Count: m.offhand.count})
		}
		if m.hasBody() { // setItemSlotAndDropWhenKilled: the swallowed block always comes back
			dropGear(m.cube.body)
		}
		if m.frogEaten > 0 {
			// Eaten by a frog (the magma_cube loot table's frog branch): a
			// magma cube becomes the froglight of the frog's variant, and
			// nothing else — no slime, no magma cream.
			if m.etype == entityMagmaCube {
				drops = append(drops, plugin.ItemStack{Item: froglightFor(int32(m.frogEaten - 1)), Count: 1})
			}
		} else if loot, _ := deathDropsAllowed(m); loot { // LivingEntity.shouldDropLoot
			// Data-driven entity table (looting, killed-by-player, cooked-on-fire)
			// when one is baked; else the legacy mobLoot roll.
			if ds, ok := h.evalEntityLoot(int32(m.etype), lootCtx{
				looting: m.looting, killedByPlayer: m.hitByPlayer, onFire: m.burning,
				direct: advEntityName[m.lastDirect], source: h.blowSourceName(players, m), dt: m.lastDT,
				rng: h.rng.Intn, randf: h.rng.Float64}); ok {
				if m.etype == entitySheep && m.sheared {
					ds = nil // no wool off a sheared sheep (handled outside the table)
				}
				for _, d := range ds {
					drops = append(drops, plugin.ItemStack{Item: d.item, Count: d.count})
				}
			} else {
				loot := h.mobLoot(m)
				if m.etype == entitySheep && m.sheared {
					loot = loot[1:]
				}
				for _, d := range loot {
					if m.looting > 0 && !d.fixed { // Looting: up to +level per roll (vanilla)
						d.count += h.rng.Intn(m.looting + 1)
					}
					// A roll may start BELOW zero — the magma cube's set_count
					// is uniform -2..1, which is what makes it stingy with
					// cream — so only a positive count is actually a drop.
					if d.count <= 0 {
						continue
					}
					drops = append(drops, plugin.ItemStack{Item: d.item, Count: d.count})
				}
			}
		}
	}
	xp := 0
	if _, pays := deathDropsAllowed(m); m.hitByPlayer && pays { // burn/blast deaths pay nothing
		xp = xpForMob(m, h.rng.Intn)
	}
	if plugin.Has[*plugin.MobDeathEvent](h.plugins) {
		dev := &plugin.MobDeathEvent{EID: m.eid, Type: m.etype, TypeName: entityNameByID[m.etype],
			X: m.x, Y: m.y, Z: m.z, Dim: m.dim, KillerEID: m.lastAttacker, Drops: drops, XP: xp}
		h.plugins.Fire(dev)
		drops, xp = dev.Drops, dev.XP
	}
	for _, d := range drops {
		h.spawnItemIn(players, m.dim, d.Item, d.Count, m.x, m.y, m.z) // no-ops on count 0
	}
	// A death is a frequency-15 vibration; a nearby sculk catalyst consumes the
	// XP into a bloom instead of dropping orbs.
	h.gameEvent(freqEntityDie, floorInt(m.x), floorInt(m.y), floorInt(m.z), m.eid)
	if xp > 0 && h.catalystConsume(players, m, xp) {
		if k := players[m.lastAttacker]; k != nil {
			h.advance(players, k, "kill_mob_near_sculk_catalyst", advMatch{})
		}
		return
	}
	if xp > 0 {
		h.spawnXPOrbIn(players, m.dim, xp, m.x, m.y, m.z)
	}
}

// entityStatus builds Entity Status (0x1e): an i32 entity id + a status byte.
func entityStatus(eid int32, status byte) attachproto.EntityStatus {
	return attachproto.EntityStatus{EID: eid, Status: int32(status)}
}

// mobLoot rolls what a killed mob drops (vanilla tables, simplified).
func (h *hub) mobLoot(m *mob) []drop {
	etype := m.etype
	switch etype {
	case entityCow:
		return []drop{{item: itemBeef, count: 1 + h.rng.Intn(3)}, {item: itemLeather, count: h.rng.Intn(3)}} // 1-3 beef, 0-2 leather
	case entityZombie:
		return []drop{{item: itemRottenFlesh, count: h.rng.Intn(3)}} // 0-2 rotten flesh
	case entitySkeleton:
		return []drop{{item: itemBone, count: h.rng.Intn(3)}, {item: itemArrowDrop, count: h.rng.Intn(3)}} // 0-2 bones + 0-2 arrows
	case entitySpider:
		l := []drop{{item: itemString, count: h.rng.Intn(3)}} // 0-2 string
		if h.rng.Intn(3) == 0 {
			l = append(l, drop{item: itemSpiderEye, count: 1})
		}
		return l
	case entityCreeper:
		l := []drop{{item: itemGunpowder, count: h.rng.Intn(3)}} // 0-2 gunpowder (killed BEFORE the bang)
		if k := h.mobs[m.lastAttacker]; k != nil && skeletonFamily(k.etype) {
			l = append(l, drop{item: creeperDiscs[h.rng.Intn(len(creeperDiscs))], count: 1}) // entities/creeper: #creeper_drop_music_discs for a skeleton's kill
		}
		return l
	case entityChicken:
		return []drop{{item: itemFeather, count: h.rng.Intn(3)}, {item: itemRawChicken, count: 1}}
	case entityPig:
		return []drop{{item: itemPorkchop, count: 1 + h.rng.Intn(3)}}
	case entitySheep:
		meat := itemMutton
		if m.burning {
			meat = itemCookedMutton // furnace_smelt: a burning death cooks the meat
		}
		// The fleece comes from the per-colour sub-table, which carries no
		// enchanted_count_increase: Looting gives more mutton, never more wool.
		return []drop{{item: sheepWool(m), count: 1, fixed: true}, {item: meat, count: 1 + h.rng.Intn(2)}}
	case entityHusk, entityDrowned:
		return []drop{{item: itemRottenFlesh, count: h.rng.Intn(3)}}
	case entityStray:
		return []drop{{item: itemBone, count: h.rng.Intn(3)}, {item: itemArrowDrop, count: h.rng.Intn(3)}}
	case entityEnderman:
		return []drop{{item: itemEnderPearl, count: h.rng.Intn(2)}} // 0-1 pearls
	case entityWitch:
		return []drop{{item: []int32{itemRedstone, itemGlowstone, itemSugar, itemStick}[h.rng.Intn(4)], count: 1 + h.rng.Intn(2)}}
	case entityZombifiedPiglin:
		d := []drop{{item: itemRottenFlesh, count: h.rng.Intn(2)}, {item: itemGoldNugget, count: h.rng.Intn(2)}}
		if h.rng.Intn(40) == 0 { // rare ingot (vanilla ~2.5%)
			d = append(d, drop{item: itemGoldIngot, count: 1})
		}
		return d
	case entitySlime:
		if m.size > 1 {
			return nil // only the smallest slime leaves a ball behind
		}
		return []drop{{item: itemSlimeball, count: h.rng.Intn(3)}} // entities/slime: uniform 0-2
	case entityMagmaCube:
		if m.size <= 1 {
			return nil // a small magma cube drops nothing at all
		}
		// entities/magma_cube: set_count is uniform -2..1, so the cream comes
		// off one kill in four — and Looting is added to that negative base,
		// which is what makes the enchantment matter here.
		return []drop{{item: itemMagmaCream, count: h.rng.Intn(4) - 2}} // brewing: fire resistance
	case entityBlaze:
		if !m.hitByPlayer {
			return nil // vanilla: rods only on player kills
		}
		return []drop{{item: itemBlazeRod, count: h.rng.Intn(2)}} // brewing: the fuel + powder
	case entityGuardian, entityElderGuardian:
		return h.guardianLoot(m)
	case entityTurtle:
		l := []drop{}
		if n := h.rng.Intn(3); n > 0 {
			l = append(l, drop{item: itemSeagrassItem, count: n}) // entities/turtle: 0-2 seagrass
		}
		if m.lastDT == dtLightningBolt {
			l = append(l, drop{item: itemBowlItem, count: 1}) // …and a bowl when lightning did it
		}
		return l
	}
	if d := speciesOf(etype); d != nil { // roster species: from the table
		return h.speciesLoot(d)
	}
	return nil
}

// Nether drop item ids (brewing feedstock).
var (
	itemGoldNugget = itemByName["gold_nugget"]
	itemGoldIngot  = itemByName["gold_ingot"]
	itemBlazeRod   = itemByName["blaze_rod"]
	itemMagmaCream = itemByName["magma_cream"]
)

// swordPeriod marks sword item ids (sweep + their attack period).
var swordPeriod = itemSet("wooden_sword", "stone_sword", "golden_sword",
	"iron_sword", "diamond_sword", "netherite_sword")

// itemSet resolves item names to a set of ids (version-independent lookup).
func itemSet(names ...string) map[int32]bool {
	m := make(map[int32]bool, len(names))
	for _, n := range names {
		if id, ok := itemByName[n]; ok {
			m[id] = true
		}
	}
	return m
}

// attackPeriod is a held weapon's full-charge recovery in whole ticks —
// ceil(20/attack_speed − 0.5), matching vanilla's getAttackStrengthScale
// sampling at ticker+0.5. Attack speeds from Mojang's items report: hand 4.0
// → 5t; swords 1.6 → 12t; axes are PER-TIER (wood/stone 0.8 → 25t, iron 0.9
// → 22t, gold/diamond/netherite 1.0 → 20t); pickaxes 1.2 → 17t; shovels 20t.
func attackPeriod(item int32) int {
	if sp := spearOf(item); sp != nil {
		return sp.period // attack speed 1/attack_duration
	}
	return int(math.Ceil(20/(attr.Defs[attr.AttackSpeed].Default+weaponAttackSpeed[item]) - 0.5))
}

// weaponAttackSpeed is the ATTACK_SPEED modifier each weapon's default
// attribute_modifiers adds in the main hand (Item.Properties.sword/axe/
// pickaxe/shovel/hoe/spear, MaceItem, TridentItem). The attack period is
// ceil(20 / speed − 0.5) ticks: a sword's 1.6 is twelve, a fist's 4 is five.
var weaponAttackSpeed = func() map[int32]float64 {
	out := map[int32]float64{itemByName["mace"]: -3.4, itemByName["trident"]: -2.9}
	mats := []string{"wooden", "stone", "copper", "iron", "golden", "diamond", "netherite"}
	per := map[string][7]float64{ // in mats order
		"sword":   {-2.4, -2.4, -2.4, -2.4, -2.4, -2.4, -2.4},
		"pickaxe": {-2.8, -2.8, -2.8, -2.8, -2.8, -2.8, -2.8},
		"shovel":  {-3.0, -3.0, -3.0, -3.0, -3.0, -3.0, -3.0},
		"axe":     {-3.2, -3.2, -3.2, -3.1, -3.0, -3.0, -3.0},
		"hoe":     {-3.0, -2.0, -2.0, -1.0, -3.0, 0, 0},
		"spear":   {1/0.65 - 4, 1/0.75 - 4, 1/0.85 - 4, 1/0.95 - 4, 1/0.95 - 4, 1/1.05 - 4, 1/1.15 - 4},
	}
	for kind, v := range per {
		for i, m := range mats {
			if id, ok := itemByName[m+"_"+kind]; ok && v[i] != 0 {
				out[id] = v[i]
			}
		}
	}
	return out
}()

// deathDropsAllowed is LivingEntity.shouldDropLoot / shouldDropExperience:
// a baby drops no loot and pays no experience — except that every Monster
// (the zombie family, piglins, zoglins) drops and pays whatever its age, a
// baby hoglin pays experience without loot, and a tadpole pays none.
func deathDropsAllowed(m *mob) (loot, xp bool) {
	switch {
	case m.etype == entityTadpole:
		return !m.baby, false
	case m.etype == entityHoglin:
		return !m.baby, true
	case !m.baby:
		return true, true
	case m.hostile, m.etype == entityPiglin, m.etype == entityZombifiedPiglin, m.etype == entityZoglin:
		return true, true // Monster overrides both
	}
	return false, false
}
