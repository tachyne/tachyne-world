package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/plugin"
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
	"mace": 6, // vanilla: +5 attack-damage modifier over the player's base 1
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
	}
	if d := speciesOf(etype); d != nil { // roster species: from the table
		return d.health
	}
	return cowHealth
}

// hostileMelee is a hostile mob's bite damage (skeletons shoot, creepers
// explode — neither reaches here).
func hostileMelee(m *mob) float32 {
	if m.ovrDamage > 0 { // plugin attribute override wins over every species rule
		return float32(m.ovrDamage)
	}
	switch m.etype {
	case entitySpider:
		return spiderDamage
	case entityEnderman:
		return endermanDamage
	case entitySlime, entityMagmaCube:
		return float32(m.size) // big 4 / medium 2 / small 1 (vanilla small is 0)
	case entityZombifiedPiglin:
		return 5 // gold sword swing
	}
	if d := speciesOf(m.etype); d != nil { // roster species: from the table
		return float32(d.damage)
	}
	return zombieDamage
}

// maxMeleeReach is the server-side sanity cap on a melee hit's distance:
// vanilla survival reach is ~3 blocks, plus generous slack for latency and
// entity movement between the client's swing and our processing. AUTHORITY:
// beyond this, the claimed hit is a hacked client's kill-aura — ignored.
const maxMeleeReach = 6.0

// attackMob applies a player's melee hit to a mob, killing it at 0 health.
func (h *hub) attackMob(players map[int32]*tracked, attacker, target int32) {
	m := h.mobs[target]
	if m == nil || m.dying > 0 {
		return // not a mob (player/item), or already dying — ignore
	}
	if t := players[attacker]; t != nil {
		if t.dim != m.dim {
			return // cross-dimension hits are impossible
		}
		dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
		if dx*dx+dy*dy+dz*dz > maxMeleeReach*maxMeleeReach {
			return // hit claimed from across the map — not physically possible
		}
	}
	base := float64(fistDamage)
	charge, crit := 1.0, false
	smash, fall := false, 0.0 // mace smash attack + its fall distance
	var breachFrac float64
	t := players[attacker]
	if t != nil {
		held := t.p.heldItem()
		if d, ok := meleeDamage[held]; ok {
			base = float64(d) // a crafted weapon hits harder than a fist
		}
		if lvl := heldStack(t).enchLvl(enchSharpness); lvl > 0 {
			base += float64(lvl) // sharpness: vanilla ~0.5+0.5×lvl, rounded up
		}
		base += 3 * float64(t.hasEffect(effStrength)) // vanilla: +3/level
		base -= 4 * float64(t.hasEffect(effWeakness)) // vanilla: -4/level
		if base < 0 {
			base = 0
		}
		// Attack cooldown (1.9 combat): a swing before the weapon recovers is
		// scaled by 0.2 + 0.8×charge² — spam-clicking does a fifth of the damage.
		now := h.tick.Load()
		if dt := now - t.lastAttack; t.lastAttack != 0 && dt < uint64(attackPeriod(held)) {
			c := float64(dt) / float64(attackPeriod(held))
			charge = 0.2 + 0.8*c*c
		}
		t.lastAttack = now
		// Critical: a full-charge hit while falling (jump-crit), ×1.5.
		if charge >= 0.9 && t.airborne && t.y < t.peakY && !t.sprinting {
			crit = true
		}
		// Mace smash: falling past the threshold adds fall-distance bonus damage
		// (density scales it), and breach lets the hit ignore some armour.
		if smash, fall = maceSmashing(t); smash {
			breachFrac = 0.15 * float64(heldStack(t).enchLvl(enchBreach))
		}
		if t.gamemode == gmSurvival {
			t.exhaustion += attackExhaustion // vanilla: attacking burns food
			h.applyToolWear(t, t.p.heldSlot(), 1)
		}
	}
	dmgF := base * charge
	if smash { // vanilla adds the fall bonus after the cooldown scale, before the crit ×1.5
		dmgF += maceFallBonus(fall) + 0.5*float64(heldStack(t).enchLvl(enchDensity))*fall
	}
	if crit {
		dmgF *= 1.5
	}
	dmg := int(math.Max(1, math.Round(dmgF)))

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

	if crit {
		h.spawnParticles(players, particleCrit, m.x, m.y+1, m.z, 0.4, 0.2, 8)
		h.playSound(players, "minecraft:entity.player.attack.crit", sndPlayer, m.x, m.y, m.z, 1, 1)
	} else if charge >= 0.9 {
		h.playSound(players, "minecraft:entity.player.attack.strong", sndPlayer, m.x, m.y, m.z, 1, 1)
	} else {
		h.playSound(players, "minecraft:entity.player.attack.weak", sndPlayer, m.x, m.y, m.z, 1, 1)
	}

	// Real knockback: shove the mob away from the attacker (server physics —
	// the impulse rides out uncapped for a few updates). Sprinting hits harder.
	if t != nil {
		if kdx, kdz := m.x-t.x, m.z-t.z; (kdx != 0 || kdz != 0) && !m.noKB {
			d := math.Hypot(kdx, kdz)
			power := 0.5
			if t.sprinting {
				power = 1.0
			}
			m.vx, m.vz = kdx/d*power, kdz/d*power
			m.kb, m.reroute = 3, 0
			h.mobKnockVelocity(players, m)
		}
		// Sweep: a full-charge grounded sword swing clips everything beside the
		// target for 1 + sharpness (vanilla sweeping edge, sans the enchant).
		if _, sword := swordPeriod[t.p.heldItem()]; sword && charge >= 0.9 && !crit && t.onGround {
			sweep := 1 + heldStack(t).enchLvl(enchSharpness)
			for _, om := range h.mobs {
				if om == m || om.dying > 0 {
					continue
				}
				if dist3(om.x, om.y, om.z, m.x, m.y, m.z) > 1.5 {
					continue
				}
				om.hitByPlayer = true
				om.hurt(float64(sweep))
				if om.health <= 0 {
					h.killMob(players, om)
					h.advance(players, t, "player_killed_entity", advMatch{entity: advEntityName[om.etype]})
					h.incStat(t, attachproto.StatKilled, int32(om.etype), 1)
					h.incCustom(t, "mob_kills", 1)
					h.sbCriteria(players, "totalKillCount", t.p.name, 1, false)
				}
			}
			h.playSound(players, "minecraft:entity.player.attack.sweep", sndPlayer, t.x, t.y, t.z, 1, 1)
		}
	}

	m.hitByPlayer = true // its death now pays XP (vanilla: player-caused only)
	m.lastAttacker = attacker
	if t != nil {
		m.looting = heldStack(t).enchLvl(enchLooting)
	}
	m.hurtBreach(float64(dmg), breachFrac) // through base armor (zombie family has 2), less breach
	if smash {                             // shockwave, fall-damage negation, wind_burst launch
		h.smashEffects(players, t, m, fall)
	}
	h.zombieReinforce(players, m, t)      // hard mode: a hurt zombie may call for backup
	if m.etype == entityZombifiedPiglin { // vanilla: one hit angers the pack
		for _, o := range h.mobs {
			if o.etype == entityZombifiedPiglin && o.dim == m.dim &&
				dist3(o.x, o.y, o.z, m.x, m.y, m.z) < 16 {
				o.anger = spiderAnger * 4 // piglins hold a long grudge
			}
		}
	}
	if m.health <= 0 {
		if m.patrolCaptain && t != nil { // a slain raid captain curses its killer
			lvl := t.hasEffect(effBadOmen) // 0-based next level, capped at Bad Omen V
			if lvl > 4 {
				lvl = 4
			}
			h.applyEffect(players, t, effBadOmen, lvl, badOmenSecs)
			t.p.trySendEv(chatEv("Bad Omen"))
		}
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
		if m.retaliates { // wolf/goat/bee/llama: a hit turns the herd hostile
			h.provoke(m, t)
		} else if !m.hostile {
			m.panic, m.fleeX, m.fleeZ, m.reroute = panicTicks, t.x, t.z, 0
		} else {
			m.anger = spiderAnger // a hit spider/enderman retaliates
			if m.etype == entityEnderman {
				h.endermanTeleport(players, m) // blinks away from the blow
			}
		}
		if dx, dz := m.x-t.x, m.z-t.z; dx != 0 || dz != 0 {
			yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
		}
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.Hurt{EID: m.eid, Yaw: yaw})
	if hurt, _, _ := mobSounds(m.etype); hurt != "" {
		h.playSound(players, hurt, sndNeutral, m.x, m.y, m.z, 1, h.hurtPitch())
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
	m.vx, m.vz, m.panic = 0, 0, 0 // stop moving while it dies
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusDeath))
	if _, death, _ := mobSounds(m.etype); death != "" {
		h.playSound(players, death, sndNeutral, m.x, m.y, m.z, 1, h.hurtPitch())
	}
}

// despawnMob removes a fully-dead mob and drops its loot (called from updateMobs
// once the death animation has played out).
func (h *hub) despawnMob(players map[int32]*tracked, m *mob) {
	delete(h.mobs, m.eid)
	h.spillHorse(players, m) // a mount's saddle/armor/chest drop with it
	h.toNearbyEv(players, m.dim, m.x, m.z, entGone(m.eid))
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

	// Roll everything the death yields FIRST, so the plugin death event can
	// mutate the drop list and XP before anything hits the ground.
	// Gamerule doMobLoot=false silences the roll entirely.
	var drops []plugin.ItemStack
	if h.rules.DoMobLoot { // gamerule doMobLoot=false silences the roll
		if (m.etype == entitySlime || m.etype == entityMagmaCube) && m.size <= 1 {
			drops = append(drops, plugin.ItemStack{Item: itemSlimeball, Count: h.rng.Intn(3)})
		}
		if m.patrolCaptain { // the captain drops its ominous banner (raid trigger later)
			drops = append(drops, plugin.ItemStack{Item: itemByName["white_banner"], Count: 1})
		}
		// Picked-up gear drops in full (vanilla drops equipped loot at 100%).
		if m.held != 0 {
			drops = append(drops, plugin.ItemStack{Item: m.held, Count: 1})
		}
		for _, g := range m.gear {
			if g.item != 0 {
				drops = append(drops, plugin.ItemStack{Item: g.item, Count: 1})
			}
		}
		if !m.baby { // babies drop nothing (vanilla)
			// Data-driven entity table (looting, killed-by-player, cooked-on-fire)
			// when one is baked; else the legacy mobLoot roll.
			if ds, ok := h.evalEntityLoot(int32(m.etype), lootCtx{
				looting: m.looting, killedByPlayer: m.hitByPlayer, onFire: m.burning,
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
					if m.looting > 0 { // Looting: up to +level per roll (vanilla)
						d.count += h.rng.Intn(m.looting + 1)
					}
					drops = append(drops, plugin.ItemStack{Item: d.item, Count: d.count})
				}
			}
		}
	}
	xp := 0
	if m.hitByPlayer && !m.baby { // burn/blast/baby deaths pay nothing
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
		return []drop{{itemBeef, 1 + h.rng.Intn(3)}, {itemLeather, h.rng.Intn(3)}} // 1-3 beef, 0-2 leather
	case entityZombie:
		return []drop{{itemRottenFlesh, h.rng.Intn(3)}} // 0-2 rotten flesh
	case entitySkeleton:
		return []drop{{itemBone, h.rng.Intn(3)}, {itemArrowDrop, h.rng.Intn(3)}} // 0-2 bones + 0-2 arrows
	case entitySpider:
		l := []drop{{itemString, h.rng.Intn(3)}} // 0-2 string
		if h.rng.Intn(3) == 0 {
			l = append(l, drop{itemSpiderEye, 1})
		}
		return l
	case entityCreeper:
		return []drop{{itemGunpowder, h.rng.Intn(3)}} // 0-2 gunpowder (killed BEFORE the bang)
	case entityChicken:
		return []drop{{itemFeather, h.rng.Intn(3)}, {itemRawChicken, 1}}
	case entityPig:
		return []drop{{itemPorkchop, 1 + h.rng.Intn(3)}}
	case entitySheep:
		return []drop{{itemWhiteWool, 1}, {itemMutton, 1 + h.rng.Intn(2)}}
	case entityHusk, entityDrowned:
		return []drop{{itemRottenFlesh, h.rng.Intn(3)}}
	case entityStray:
		return []drop{{itemBone, h.rng.Intn(3)}, {itemArrowDrop, h.rng.Intn(3)}}
	case entityEnderman:
		return []drop{{itemEnderPearl, h.rng.Intn(2)}} // 0-1 pearls
	case entityWitch:
		return []drop{{[]int32{itemRedstone, itemGlowstone, itemSugar, itemStick}[h.rng.Intn(4)], 1 + h.rng.Intn(2)}}
	case entityZombifiedPiglin:
		d := []drop{{itemRottenFlesh, h.rng.Intn(2)}, {itemGoldNugget, h.rng.Intn(2)}}
		if h.rng.Intn(40) == 0 { // rare ingot (vanilla ~2.5%)
			d = append(d, drop{itemGoldIngot, 1})
		}
		return d
	case entityMagmaCube:
		if m.size <= 1 {
			return nil
		}
		return []drop{{itemMagmaCream, h.rng.Intn(2)}} // brewing: fire resistance
	case entityBlaze:
		if !m.hitByPlayer {
			return nil // vanilla: rods only on player kills
		}
		return []drop{{itemBlazeRod, h.rng.Intn(2)}} // brewing: the fuel + powder
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
	if swordPeriod[item] {
		return 12
	}
	switch item {
	case itemByName["wooden_axe"], itemByName["stone_axe"]:
		return 25
	case itemByName["iron_axe"]:
		return 22
	case itemByName["golden_axe"], itemByName["diamond_axe"], itemByName["netherite_axe"]:
		return 20
	case itemByName["wooden_pickaxe"], itemByName["stone_pickaxe"], itemByName["iron_pickaxe"],
		itemByName["golden_pickaxe"], itemByName["diamond_pickaxe"], itemByName["netherite_pickaxe"]:
		return 17
	case itemByName["wooden_shovel"], itemByName["stone_shovel"], itemByName["iron_shovel"],
		itemByName["golden_shovel"], itemByName["diamond_shovel"], itemByName["netherite_shovel"]:
		return 20
	case itemByName["mace"]: // attack speed 0.6 (heavy, slow) → ceil(20/0.6 − 0.5)
		return 33
	case itemByName["wooden_hoe"], itemByName["golden_hoe"]: // attack speed 1.0
		return 20
	case itemByName["stone_hoe"]: // 2.0
		return 10
	case itemByName["iron_hoe"]: // 3.0
		return 7
	case itemByName["diamond_hoe"], itemByName["netherite_hoe"]: // 4.0 — as fast as a fist
		return 5
	}
	return 5 // bare hand
}
