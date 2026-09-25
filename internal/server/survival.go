package server

import (
	"log"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Survival mechanics: health, hunger, damage (fall/void/starve), regeneration,
// and death/respawn. Simulated only for players in survival mode; creative/
// adventure/spectator are unaffected. All of this runs on the hub goroutine
// (the per-second tick plus damage from movement events), so it shares the
// authoritative player records without locks.

const (
	playClientRespawn       = 0x4b // respawn
	playServerClientCommand = 0x0a // serverbound: actionId 0 = perform respawn

	maxHealth     = 20.0
	maxFood       = 20
	regenFood     = 18 // food at/above which health regenerates
	voidBelow     = worldgen.MinY - 64
	survivalTickN = 20 // run the per-second survival step every N ticks

	// Vanilla tuning (see docs/MECHANICS.md).
	regenPeriod         = 80   // ticks per 1 HP regen / 1 HP starvation step (4 s)
	regenExhaustion     = 6.0  // exhaustion added per HP regenerated
	maxExhaustion       = 40.0 // vanilla FoodData: exhaustion never banks past this
	waterExhaustion     = 0.01 // exhaustion per block swum, walked under or on water
	exhaustionThreshold = 4.0  // exhaustion units per 1 saturation/food drained
	voidDamagePerSec    = 8    // 4 HP every 0.5 s, applied as 8 HP once per second
	sprintExhaustion    = 0.1  // exhaustion per block sprinted (walking is free)
	attackExhaustion    = 0.1  // exhaustion per landed melee hit

	// Environmental contact damage, applied once per survival second (see
	// environmentDamage). Air is in ticks so the bubble HUD reads it directly.
	maxAir             = 300 // ~15 s of breath (10 bubbles)
	airDrainPerSec     = 20  // 1/tick submerged (vanilla)
	airRefillPerSec    = 80  // 4/tick out of water (vanilla)
	drownDamagePerSec  = 2   // 2 HP/s once the air supply is empty
	lavaHurtDamage     = 4   // Entity.lavaHurt: 4 a hit, two hits a second
	cactusDamagePerSec = 1   // approx (vanilla 1 HP / 0.5 s on contact)

	metaIndexAir = 1 // entity-metadata index of the air supply
	metaTypeInt  = 1 // metadata value type id for VarInt (1.21.5)
)

// initSurvival sets a freshly tracked player to full health/food.
func initSurvival(t *tracked) {
	t.health = t.maxHP()
	t.absorption = 0
	t.food = maxFood
	t.saturation = 5
	t.exhaustion = 0
	t.dead = false
	t.airborne = false
	t.air = maxAir
	t.eatingSlot = -1
	t.floatTicks = 0 // a respawn teleport must not inherit pre-death float time
	t.fireSecs = 0
	t.effects = map[int32]*activeEffect{} // death strips every effect (vanilla)
	t.inv = &inventory{}
}

// fastRegen is saturation regen at vanilla's true 10-tick cadence
// (vanilla FoodData.tick): full food + saturation left → heal
// min(saturation, 6)/6 HP, costing that many exhaustion points (the same
// 6.0/HP ratio) — so healing tapers as saturation drains, exactly like
// vanilla, instead of the old 2-HP-per-second chunks.
// exhaust adds food exhaustion, capped the way vanilla's FoodData caps it
// (40.0): a long sprint or a heal cannot bank more than ten food points of
// debt.
func (t *tracked) exhaust(f float32) {
	t.exhaustion = float32(math.Min(float64(t.exhaustion+f), maxExhaustion))
}

func (h *hub) fastRegen(players map[int32]*tracked) {
	for _, t := range players {
		if !isSurvival(t.gamemode) || t.dead || t.health <= 0 {
			continue
		}
		if !h.rules.NaturalRegen {
			continue // gamerule naturalRegeneration=false: only potions heal
		}
		if t.food == maxFood && t.saturation > 0 && t.health < t.maxHP() {
			f := float32(math.Min(float64(t.saturation), 6))
			heal := f / 6
			if t.health+heal > t.maxHP() {
				heal = t.maxHP() - t.health
			}
			t.health += heal
			t.exhaust(f)
			h.sendHealth(t)
		}
	}
}

// survivalTick runs once per second: void damage, regeneration, starvation, and
// converting exhaustion into hunger. Regen/starvation step only every regenPeriod
// ticks (vanilla: 1 HP per 80 ticks); the exhaustion→hunger drain runs each call.
func (h *hub) survivalTick(players map[int32]*tracked) {
	now := h.tick.Load()
	slow := now%regenPeriod == 0 // 80-tick (4s) regen/starve cadence
	for _, t := range players {
		if !isSurvival(t.gamemode) || t.dead {
			continue
		}
		// Peaceful (vanilla's tickRegeneration): with natural regeneration on,
		// a point of health and of saturation every second, a point of food
		// every half second, whatever the player has eaten.
		if h.rules.Difficulty == diffPeaceful && h.rules.NaturalRegen && t.health > 0 {
			changed := false
			if now%20 == 0 {
				if t.health < t.maxHP() {
					t.health = float32(math.Min(float64(t.maxHP()), float64(t.health)+1))
					changed = true
				}
				if t.saturation < maxFood {
					t.saturation = float32(math.Min(maxFood, float64(t.saturation)+1))
					changed = true
				}
			}
			if now%10 == 0 && t.food < maxFood {
				t.food++
				changed = true
			}
			if changed {
				h.sendHealth(t)
			}
		}
		if t.y < float64(voidBelow) {
			h.damageOf(players, t, voidDamagePerSec, dtOutOfWorld)
			continue
		}
		if now := h.tick.Load(); now >= t.graceUntil { // portal arrivals get 3s of peace
			h.environmentDamage(players, t) // drowning, lava, fire, cactus
			if t.dead {
				continue
			}
			h.tickBurning(players, t) // afterburn: 1 dmg/s until it runs out or water
			if t.dead {
				continue
			}
			h.suffocate(players, t) // a head buried in solid rock takes 2/s
			if t.dead {
				continue
			}
		}
		changed := false
		// (Fast saturation regen moved to fastRegen — vanilla's true 10-tick
		// cadence, run from the hub loop rather than this 1 Hz step.)
		// Slow regen is vanilla's ELSE-IF branch: it never fires while
		// saturation regen is active (full food + sat left) — the final oracle
		// fight caught us healing 1.5 HP in one tick by running both.
		fastActive := t.food == maxFood && t.saturation > 0
		if slow && !fastActive && h.rules.NaturalRegen && t.food >= regenFood && t.health < t.maxHP() && t.health > 0 {
			t.health = float32(math.Min(float64(t.maxHP()), float64(t.health)+1))
			t.exhaust(regenExhaustion) // vanilla: 6.0 exhaustion per HP healed
			changed = true
		}
		if slow && t.food == 0 && h.rules.Difficulty != diffPeaceful {
			// Starvation floor by difficulty (vanilla FoodData): easy stops
			// at 10 HP, normal at half a heart, hard starves to death.
			floor := float32(1)
			switch h.rules.Difficulty {
			case diffEasy:
				floor = 10
			case diffHard:
				floor = 0
			}
			if t.health > floor {
				h.damageOf(players, t, 1, dtStarve)
				changed = true
			}
		}
		for t.exhaustion >= exhaustionThreshold { // drains saturation, then food
			t.exhaustion -= exhaustionThreshold
			if t.saturation > 0 {
				t.saturation = float32(math.Max(0, float64(t.saturation)-1))
			} else if t.food > 0 && h.rules.Difficulty != diffPeaceful {
				// FoodData.tick drains saturation on any difficulty but only
				// takes from the food bar when it is not PEACEFUL. Without the
				// guard the bar emptied on peaceful too — you simply never
				// starved once it did.
				t.food--
				changed = true
			}
		}
		if changed {
			h.sendHealth(t)
		}
	}
}

// environmentDamage is the once-a-second part of the environment: the air
// supply drains while the head is submerged (drowning at 0). The contact
// hazards (lava, fire, cactus) hit twice a second in playerContactTick.
func (h *hub) environmentDamage(players map[int32]*tracked, t *tracked) {
	fx, fz := int(math.Floor(t.x)), int(math.Floor(t.z))
	eyeY := int(math.Floor(t.y + 1.5))

	// Drowning: eyes in water deplete the breath supply; empty → 2 HP/s.
	// Water Breathing holds the breath indefinitely (no drain).
	old := t.air
	// A bubble column is water you can breathe in (LivingEntity.baseTick
	// exempts it from the air drain).
	if eye := h.worldFor(t.dim).At(fx, eyeY, fz); worldgen.HoldsWater(eye) && !worldgen.IsBubbleColumn(eye) && !t.breathesUnderwater() {
		drain := airDrainPerSec
		if h.keepsAirThisTick(t) {
			drain = 0 // Respiration: a 1-in-(bonus+1) chance of losing a breath
		}
		if t.air -= drain; t.air <= 0 {
			t.air = 0
			if h.rules.DrownDamage {
				h.toNearbyEv(players, t.dim, t.x, t.z, entityStatus(t.p.eid, entityStatusDrown))
				h.hurtBy(players, t, drownDamagePerSec, dtDrown, deathCause{})
			}
		}
	} else if t.air < maxAir {
		t.air = min(maxAir, t.air+airRefillPerSec)
	}
	if t.air != old { // bubble HUD reads the air metadata on the player entity
		t.p.trySendEv(metaEv(airMetadata(t.p.eid, t.air)))
	}
	if t.dead {
		return
	}
	// Lava, fire, campfire and cactus hurt in playerContactTick.
}

// playerContactTick is the contact hazards' hits for players, at the 10-tick
// cadence the damage cooldown allows — two hits a second, as in vanilla —
// with each hazard's per-hit amount: lava 4 (Entity.lavaHurt), fire 1 or
// soul fire 2, a lit campfire 1 or 2, cactus 1.
func (h *hub) playerContactTick(players map[int32]*tracked) {
	for _, t := range players {
		if t.dead || !isSurvival(t.gamemode) {
			continue
		}
		h.contactDamage(players, t)
	}
}

func (h *hub) contactDamage(players map[int32]*tracked, t *tracked) {
	fx, fz := int(math.Floor(t.x)), int(math.Floor(t.z))
	feet := int(math.Floor(t.y))
	// Lava: standing in it (feet or body) burns fast — and leaves you alight.
	// Fire resistance makes lava a warm bath (vanilla).
	if t.hasEffect(effFireRes) == 0 &&
		(worldgen.IsLava(h.worldFor(t.dim).At(fx, feet, fz)) || worldgen.IsLava(h.worldFor(t.dim).At(fx, feet+1, fz))) {
		h.setBurning(players, t, lavaFireSecs)
		if h.hurtBy(players, t, lavaHurtDamage, dtLava, deathCause{}); t.dead {
			return
		}
	}
	// Fire blocks: contact damage + a shorter afterburn.
	dmg := fireContactDamage(h.worldFor(t.dim).At(fx, feet, fz), h.worldFor(t.dim).At(fx, feet+1, fz))
	if dmg > 0 && t.frozen > 0 { // BaseFireBlock.entityInside: CLEAR_FREEZE, whatever the fire_damage rule
		t.frozen = 0
		t.p.trySendEv(metaEv(frozenMetadata(t.p.eid, 0)))
	}
	if h.rules.FireDamage && dmg > 0 {
		h.setBurning(players, t, fireContactSecs)
		if h.hurtBy(players, t, float32(dmg), dtInFire, deathCause{}); t.dead {
			return
		}
	}
	// Lit campfires burn whoever stands in them (vanilla 1 HP, soul 2).
	if s := h.worldFor(t.dim).At(fx, feet, fz); isCampfireBlock(s) && boolProp(s, "lit") &&
		t.hasEffect(effFireRes) == 0 && h.rules.FireDamage {
		dmg := float32(1)
		if isSoulCampfire(s) {
			dmg = 2
		}
		if h.hurtBy(players, t, dmg, dtCampfire, deathCause{}); t.dead {
			return
		}
	}
	// Cactus: contact with an adjacent cactus at feet or body height.
	if h.touchingCactus(t.dim, fx, feet, fz) {
		h.hurtBy(players, t, cactusDamagePerSec, dtCactus, deathCause{})
	}
}

// cactusInset is the cactus's collision inset (a sixteenth on each side):
// what presses against it is inside its cell by that much.
const cactusInset = 1.0 / 16

// boxTouchesCactus reports whether an entity box (feet centre, half width,
// height) reaches into a cactus cell — up to the inset past each face.
func (h *hub) boxTouchesCactus(dim int, x, y, z, hw, ht float64) bool {
	w := h.worldFor(dim)
	r := hw + cactusInset
	for cx := int(math.Floor(x - r)); cx <= int(math.Floor(x+r-1e-7)); cx++ {
		for cz := int(math.Floor(z - r)); cz <= int(math.Floor(z+r-1e-7)); cz++ {
			for cy := int(math.Floor(y - cactusInset)); cy <= int(math.Floor(y+ht-1e-7)); cy++ {
				if s := w.At(cx, cy, cz); s >= cactusMin && s <= cactusMax {
					return true
				}
			}
		}
	}
	return false
}

// touchingCactus reports whether a cactus occupies any of the four horizontal
// neighbours at the player's feet or body height (approximates hitbox overlap).
func (h *hub) touchingCactus(dim, fx, feet, fz int) bool {
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		for dy := 0; dy <= 1; dy++ {
			if s := h.worldFor(dim).At(fx+d[0], feet+dy, fz+d[1]); s >= cactusMin && s <= cactusMax {
				return true
			}
		}
	}
	return false
}

// inWater reports whether the block at a position's feet is water — the vanilla
// test for cancelling fall distance (and thus fall damage).
func (h *hub) inWater(dim int, x, y, z float64) bool {
	w := h.worldFor(dim)
	return worldgen.HoldsWater(w.At(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))))
}

// onFallAndExhaust updates fall-damage tracking and walking exhaustion from a
// movement event (called from onMove before the position is advanced).
func (h *hub) onFallAndExhaust(players map[int32]*tracked, t *tracked, e evMove) {
	if !isSurvival(t.gamemode) || t.dead {
		return
	}
	if e.sprinting { // vanilla: only sprinting drains food from movement; walking is free
		t.exhaust(sprintExhaustion * float32(math.Hypot(e.x-t.x, e.z-t.z)))
	}
	if inWater := h.inWater(t.dim, e.x, e.y, e.z); inWater { // swimming, walking under or on water: 0.01 a block
		t.exhaust(waterExhaustion * float32(math.Hypot(e.x-t.x, e.z-t.z)))
		if !t.wasInWater && !t.p.sneaking {
			h.vibAt(t.dim, freqSplash, e.x, e.y, e.z, t.p.eid) // Entity.doWaterSplashEffect: SPLASH (sneaking is silent)
			// …and the splash itself, which had never been audible. The volume
			// is the speed going in, so a dive is loud and wading is not.
			h.playSplash(players, t.dim, e.x, e.y, e.z, e.x-t.x, e.y-t.y, e.z-t.z, true)
		}
		t.wasInWater = true
	} else {
		t.wasInWater = false
	}
	h.trackClimbable(t, e.x, e.y, e.z, e.onGround)
	if !e.onGround {
		// Touching water cancels accumulated fall distance (vanilla resets fall
		// distance each tick you are in a liquid), so falling THROUGH or INTO
		// water never deals fall damage — even onto ground below it.
		if h.inWater(t.dim, e.x, e.y, e.z) {
			t.airborne = false
			return
		}
		if !t.airborne {
			t.airborne, t.peakY = true, e.y
			if e.y > t.y { // leaving the ground UPWARD = a jump (vanilla 0.05 / sprint 0.2)
				if e.sprinting {
					t.exhaust(0.2)
				} else {
					t.exhaust(0.05)
				}
			}
		} else if e.y > t.peakY {
			t.peakY = e.y
		}
		return
	}
	if t.airborne {
		t.airborne = false
		// Slow Falling, and landing in water, negate fall damage (vanilla resets
		// fall distance each tick).
		if t.hasEffect(effSlowFalling) > 0 || h.inWater(t.dim, e.x, e.y, e.z) {
			return
		}
		dist := t.peakY - e.y
		if dist > 0 && !t.p.sneaking {
			h.vibAt(t.dim, freqHitGround, t.x, e.y, t.z, t.p.eid) // HIT_GROUND (sneaking is silent)
		}
		h.advance(players, t, "fall_from_height", advMatch{distY: dist, startY: t.peakY, endY: e.y})
		if t.launchCause != "" {
			h.advance(players, t, "fall_after_explosion", advMatch{distY: dist, cause: t.launchCause})
			t.launchCause = ""
		}
		// Trampling farmland happens on any hard landing — before the fall-damage
		// grace and regardless of the FallDamage gamerule (FarmBlock.fallOn). The
		// soil is the block just under the player's feet.
		if dist > 0 {
			h.tramplePlayer(players, t, int(math.Floor(e.x)), int(math.Floor(e.y))-1, int(math.Floor(e.z)), dist)
		}
		if h.rules.FallDamage && dist > 0 {
			// A stalagmite tip is the one block that makes a fall WORSE: it
			// counts the drop 2.5 blocks longer and doubles what it deals, so
			// it hurts even from heights that would otherwise be safe.
			lx, ly, lz := int(math.Floor(e.x)), int(math.Floor(e.y))-1, int(math.Floor(e.z))
			landed := h.worldFor(t.dim).At(lx, ly, lz)
			if isTurtleEgg(landed) && h.rng.Intn(3) == 0 { // TurtleEggBlock.fallOn
				h.crushTurtleEgg(players, t.dim, lx, ly, lz, landed)
			}
			hurt, impaled := stalagmiteFallExtra(landed, dist)
			if !impaled {
				// SAFE_FALL_DISTANCE (three blocks, plus one per Jump Boost
				// level) and FALL_DAMAGE_MULTIPLIER, as calculateFallDamage
				// reads them — /attribute moves either.
				a := t.playerAttrs()
				grace, mult := a.Value(attr.SafeFallDistance), a.Value(attr.FallDamageMultiplier)
				hurt = fallDamageOn(landed, dist, grace, mult, t.p.sneaking) // hay, honey, beds and slime soften; powder snow catches
				if hurt <= 0 {
					return
				}
			}
			if hurt > 0 {
				// Landing on a stalagmite is its own damage type, not a fall
				// with a different label: it pierces armour and a shield, and
				// it reads "was impaled on a stalagmite".
				dt := dtFall
				if impaled {
					dt = dtStalagmite
				}
				dmg := math.Floor(hurt)
				t.landingFall = dist // the combat log's fallDistance for this hit
				h.hurtBy(players, t, float32(dmg), dt, deathCause{})
				t.landingFall = 0
				h.playFallDamageSound(players, t.dim, t.x, t.y, t.z, dmg)
			}
		}
	}
}

// damageOf applies harm of one damage TYPE to a survival player, triggering
// death at 0 health. On death the player's inventory scatters as item entities
// (the survival stake), so callers pass the player registry for the drops to be
// shown to nearby players.
//
// Naming the type is the caller's ONLY job. Everything the type implies —
// whether armour absorbs the blow and wears from it, whether Resistance and the
// protection enchantments get a say, what it costs in hunger — is derived from
// the type's tags in hurtBy. It used to be the call site's job, and the result
// was that lava, fire, cacti, magma, berry bushes and lightning all ignored
// armour entirely while the dragon's blows never wore any.
func (h *hub) damageOf(players map[int32]*tracked, t *tracked, amount float32, dt dmgType) {
	h.hurtBy(players, t, amount, dt, deathCause{})
}

// difficultyScaled is Player.hurtServer's difficulty branch: Peaceful takes
// the blow away entirely, Easy softens it to min(f/2 + 1, f) — which leaves
// small hits alone rather than halving everything — and Hard adds half again.
// Normal is untouched. Whether it applies at all is the damage TYPE's
// business. In 1.21.11 four types scale `always` — explosion, player_explosion,
// bad_respawn_point and sonic_boom — and the other forty-six scale
// `when_caused_by_living_non_player`, which is why the caller has to say who
// dealt it: a skeleton's arrow scales and the same arrow from a player does
// not. Nothing is tagged `never`, so starving or falling on your own goes
// unscaled only because nothing living caused it.
func (h *hub) difficultyScaled(amount float32, dt dmgType, byMob bool) float32 {
	switch scaling := dmgTypeScaling[dt]; {
	case scaling == scaleAlways, scaling == scaleWhenLivingNonPlayer && byMob:
	default:
		return amount
	}
	switch h.rules.Difficulty {
	case diffPeaceful:
		return 0
	case diffEasy:
		return float32(math.Min(float64(amount)/2+1, float64(amount)))
	case diffHard:
		return amount * 3 / 2
	}
	return amount
}

// hurtBy is damageOf plus the CAUSE, which is what a death message is made of.
// The cause has to ride with the damage: by the time health reaches zero the
// thing that dealt it is long gone, so it is recorded as the last thing to
// hurt this player — which is also why walking out of lava and dying of the
// burns still credits the lava.
//
// Nothing that reaches here names a source position, so nothing that reaches
// here can be blocked by a shield — which is right for every environmental
// hazard and wrong for anything thrown, fired or swung. Those call hurtFrom.
func (h *hub) hurtBy(players map[int32]*tracked, t *tracked, amount float32, dt dmgType, cause deathCause) bool {
	return h.hurtFrom(players, t, amount, dt, cause, dmgFrom{})
}

// hurtFrom is the whole damage path, and src is what a shield is resolved
// against. It reports whether the hit LANDED — false when a shield ate all of
// it, which is vanilla's `success` and is what gates a blow's follow-on
// effects: a bite caught on a shield delivers no venom.
//
// The mitigation order is vanilla's: the shield first (before armour, since it
// decides how much there is to absorb), then the helmet's own share of a
// falling anvil, then armour absorption, then magic absorption (Resistance,
// then the protection enchantments), then the absorption buffer.
func (h *hub) hurtFrom(players map[int32]*tracked, t *tracked, amount float32, dt dmgType, cause deathCause, src dmgFrom) bool {
	if !isSurvival(t.gamemode) || t.dead || t.health <= 0 {
		return false
	}
	// LivingEntity.hurtServer: Fire Resistance turns away every is_fire blow
	// outright — a blaze's fireball included — before anything else runs.
	if dt.has(tagIsFire) && t.hasEffect(effFireRes) > 0 {
		return false
	}
	h.dropShoulderParrots(players, t) // hurtServer: any blow that gets this far shakes them off
	// Player.hurtServer scales the blow by the difficulty BEFORE anything
	// mitigates it, and only for the damage types whose `scaling` field says
	// so. This used to be a flat 0.5/1/1.5 applied at five mob-damage sites,
	// which both got Easy wrong and left every explosion, every projectile and
	// every environmental hazard unscaled.
	if amount = h.difficultyScaled(amount, dt, src.byMob); amount == 0 {
		return false
	}
	// The LAST thing to hurt them is what gets the credit, and the damage type
	// is what the message is built from, so stamp it on here rather than
	// asking twenty call sites to repeat themselves.
	cause.dt = dt
	if cause.by != "" {
		t.killCredit, t.killCreditAt = cause.by, h.tick.Load()
	} else if h.tick.Load() < t.killCreditAt+killCreditTicks {
		// LivingEntity.getKillCredit: still counts as dying in a fight.
		cause.credit = t.killCredit
	}
	if cause.byEID != 0 {
		t.pvpBy, t.pvpByTil = cause.byEID, h.tick.Load()+hurtByPlayerMemory
	}
	t.lastCause = cause
	h.recordCombat(t, cause, amount, t.landingFall)
	blocked := h.shieldBlocked(t, amount, dt, src)
	if blocked > 0 {
		h.shieldBlockFX(players, t, blocked)
		h.incCustom(t, "damage_blocked_by_shield", tenths(blocked))
		amount -= blocked
		if !dt.has(tagIsProjectile) { // blockUsingItem: a melee attacker's axe disables the shield
			if secs := weaponDisableSeconds(src.weapon); secs > 0 {
				h.disableShield(players, t, secs)
			}
		}
	}
	// A falling anvil batters the helmet specifically, then a quarter of the
	// blow is gone before the rest of the armour ever sees it.
	if dt.has(tagDamagesHelmet) && t.armor[0].count > 0 {
		h.wearArmorSlot(players, t, 0, helmetWear(amount), dt)
		amount *= 0.75
	}
	// LivingEntity.hurt's cooldown: for 10 ticks after a landed blow only a
	// bigger blow lands, and only its excess over the last one — so fire,
	// cactus, a crowd of zombies do not stack their hits every tick.
	if now := h.tick.Load(); now < t.hurtAt+10 {
		if amount <= t.lastHurt {
			return false
		}
		amount, t.lastHurt = amount-t.lastHurt, amount
	} else {
		t.lastHurt, t.hurtAt = amount, now
	}
	preMitigation := amount // what armour and magic will be measured against
	// Armour absorbs the blow and wears from it under ONE condition, as vanilla
	// does in getDamageAfterArmorAbsorb — hurtArmor is called there, with the
	// damage as it stands BEFORE the reduction. Keeping the two together is the
	// point: split across call sites, three of them wore no armour at all.
	if !dt.has(tagBypassesArmor) {
		h.wearArmor(players, t, amount, dt)
		amount = t.armorReduceBreach(amount, src.breach)
	}
	if !dt.has(tagBypassesEffects) { // starve: no effect or enchantment helps
		// Resistance: -20% per level (MobEffects.DAMAGE_RESISTANCE); level 5 =
		// immune. Vanilla's getDamageAfterMagicAbsorb applies resistance BEFORE
		// the enchantment protection, and the order shows: two 20% cuts in a
		// row are not the same as one 40% cut.
		if r := t.hasEffect(effResistance); r > 0 && !dt.has(tagBypassesResistance) {
			amount *= float32(math.Max(0, float64(25-r*5)) / 25)
		}
		if amount > 0 && !dt.has(tagBypassesEnchantments) { // sonic_boom shrugs off protection
			amount = t.enchantProtect(amount, dt)
		}
	}
	// Absorption soaks damage into its buffer before real health (yellow hearts).
	if beforeAbsorb := amount; beforeAbsorb < preMitigation { // armour + magic took the rest
		h.incCustom(t, "damage_resisted", tenths(preMitigation-beforeAbsorb))
	}
	if t.absorption > 0 && amount > 0 {
		soak := float32(math.Min(float64(t.absorption), float64(amount)))
		h.incCustom(t, "damage_absorbed", tenths(soak))
		t.absorption -= soak
		amount -= soak
	}
	if amount <= 0 {
		// Nothing got through. The hit still LANDED unless a shield is what
		// stopped it — vanilla's `!blocked || damage > 0`, and the distinction
		// is what makes blocking cancel a bite's venom while a lucky roll of
		// armour and Resistance does not.
		return blocked <= 0
	}
	h.wakePlayer(players, t)   // pain wakes (and stands the pose back up)
	t.exhaust(dt.exhaustion()) // vanilla: DamageType.exhaustion, per type
	h.infestOnHurt(players, t) // Infested: silverfish burst out on being hit
	h.incCustom(t, "damage_taken", tenths(amount))
	t.health -= amount
	h.vibAt(t.dim, freqEntityDamage, t.x, t.y, t.z, t.p.eid)
	if t.health <= 0 && h.totemSaves(players, t, dt) {
		return true // the blow landed; a totem of undying answered it
	}
	if t.health <= 0 {
		t.health = 0
		t.dead = true
		h.dropShoulderParrots(players, t) // ServerPlayer.die
		h.recordDeath(t)                  // ServerPlayer.die: the last death location
		h.deathForgiveness(players, t)
		h.ominousOnDeath(players, t) // wind burst / cobwebs / slimes, at the spot
		h.incCustom(t, "deaths", 1)
		h.resetCustom(t, "time_since_rest") // dying counts as a rest, in vanilla's book
		h.resetCustom(t, "time_since_death")
		h.sbCriteria(players, "deathCount", t.p.name, 1, false)
		h.creditPlayerDeath(players, t)
		log.Printf("%q died at (%.0f,%.0f,%.0f): %s", t.p.name, t.x, t.y, t.z,
			h.combatDeathMessage(t))
		if h.rules.ShowDeathMsgs { // gamerule showDeathMessages
			body := chatEv(h.combatDeathMessage(t))
			for _, o := range players {
				if h.deathMessageReaches(t.p.name, o.p.name) { // the team's deathMessageVisibility
					o.p.trySendEv(body)
				}
			}
		}
		if !h.rules.KeepInventory { // gamerule: keepInventory skips the stake
			h.dropInventory(players, t)
			h.dropDeathXP(players, t) // 7×level as an orb at the death spot, bar zeroed
		} else {
			h.catalystHears(players, t.dim, t.x, t.y, t.z, 0) // no experience to give, but a catalyst still blooms
		}
		t.p.trySendEv(attachproto.Death{EID: t.p.eid, Message: h.combatDeathMessage(t)})
		if h.rules.ImmediateResp { // gamerule doImmediateRespawn skips the death screen
			h.post(evRespawn{eid: t.p.eid})
		}
		h.playSoundDim(players, t.dim, "minecraft:entity.player.death", sndPlayer, t.x, t.y, t.z, 1, 1)
	} else {
		t.p.trySendEv(attachproto.Hurt{EID: t.p.eid, Yaw: t.yaw})
		h.playSoundDim(players, t.dim, hurtSoundFor(dt), sndPlayer, t.x, t.y, t.z, 1, h.hurtPitch())
	}
	h.sendHealth(t)
	return true
}

// hurtSoundFor is Player.getHurtSound: the sound belongs to the damage type,
// not to the player. Drowning, burning, freezing and a sweet berry bush each
// have their own; everything else uses the ordinary one.
func hurtSoundFor(dt dmgType) string {
	if int(dt) < len(dmgTypeHurtSound) && dmgTypeHurtSound[dt] != "" {
		return dmgTypeHurtSound[dt]
	}
	return "minecraft:entity.player.hurt"
}

// helmetWear is doHurtEquipment's durability cost — the same max(1, damage/4)
// the rest of the armour pays, charged to the helmet alone.
func helmetWear(dmg float32) int {
	if n := int(dmg) / 4; n > 1 {
		return n
	}
	return 1
}

// dropInventory scatters every held stack as an item entity at the player's feet
// and clears the inventory — vanilla death behaviour (keepInventory off).
func (h *hub) dropInventory(players map[int32]*tracked, t *tracked) {
	if t.inv == nil {
		return
	}
	stacks := make([]*invStack, 0, invSize+14)
	for i := range t.inv.slots {
		stacks = append(stacks, &t.inv.slots[i])
	}
	for i := range t.craft { // crafting grid, cursor, armor, offhand drop too
		stacks = append(stacks, &t.craft[i])
	}
	for i := range t.armor {
		stacks = append(stacks, &t.armor[i])
	}
	stacks = append(stacks, &t.cursor, &t.offhand)

	dropped := false
	for _, s := range stacks {
		if s.item == 0 || s.count == 0 {
			continue
		}
		if s.enchLvl(enchVanishingCurse) > 0 {
			// Curse of Vanishing (PREVENT_EQUIPMENT_DROP): the item is
			// destroyed with you rather than left on the ground.
			*s = invStack{}
			continue
		}
		// Small jitter so stacks don't perfectly overlap on one column.
		jx := t.x + (h.rng.Float64() - 0.5)
		jz := t.z + (h.rng.Float64() - 0.5)
		// In the player's OWN dimension: spawnItem defaults to the overworld,
		// which used to scatter a Nether or End death across the wrong world.
		if it := h.spawnItemIn(players, t.dim, s.item, s.count, jx, t.y, jz); it != nil {
			it.setFrom(*s)                 // the whole stack: wear, potions, contents, dye…
			h.refreshItemMeta(players, it) // the spawn broadcast went out bare
		}
		*s = invStack{}
		dropped = true
	}
	if dropped {
		h.sendInventory(t) // clear the client's inventory view
	}
}

// respawn resets a dead player and returns them to their bed (world spawn if
// they never slept or the bed is gone).
func (h *hub) respawn(t *tracked) {
	t.lastCause = deathCause{} // a fresh life owes its death to nothing yet
	t.combat = combatLog{}
	if !t.dead {
		return
	}
	initSurvival(t)
	sx, sy, sz, sdim := h.respawnPoint(h.playersRef, t)
	t.x, t.y, t.z = sx, sy, sz
	t.p.trySendEv(attachproto.Dimension{Dim: int32(t.dim), Gamemode: int32(t.gamemode), Death: h.deathOf(t.p.key())})
	t.p.trySendEv(teleportEv(sx, sy, sz, t.yaw, t.pitch))
	t.p.trySendEv(abilitiesFor(t.gamemode))
	h.sendHealth(t)
	h.sendInventory(t)  // clear the client's inventory view after the death drop
	h.sendExperience(t) // …and the (zeroed) XP bar
	if t.dim != sdim {
		// The respawn point decides the dimension — usually the overworld, but a
		// charged respawn anchor keeps you in the Nether. Without this, the
		// connection kept streaming the old dimension's chunks and the hub kept
		// the player in it for everyone else — phantom players and
		// blocks-in-thin-air. Route through the pending-switch machinery so the
		// connection resets its dimension, restreams chunks, and everyone's view
		// swaps.
		t.p.pendingFrom = dimPos{}
		t.p.pendingDest = blockPos{floorInt(sx), floorInt(sy), floorInt(sz) - 1}
		t.p.pendingDestOK = true
		t.p.pendingDim.Store(int32(sdim))
	}
}

const eatDuration = 32 // default ticks of eat-hold before the food applies (vanilla 1.6s)

var (
	itemDriedKelp   = itemByName["dried_kelp"]
	itemHoneyBottle = itemByName["honey_bottle"]
)

// alwaysEdible are the foods vanilla marks can_always_eat: eaten at full
// hunger for their other effect.
var alwaysEdible = map[int32]bool{
	int32(itemByName["golden_apple"]): true, int32(itemByName["enchanted_golden_apple"]): true,
	int32(itemByName["chorus_fruit"]): true, int32(itemByName["honey_bottle"]): true,
	int32(itemByName["suspicious_stew"]): true,
}

var (
	_ = alwaysEdible
)

// foodEatTicks is a food's consume time (vanilla Consumable consume_seconds ×
// 20). The default is 32 t (1.6 s); in 1.21.11 only dried kelp (16 t / 0.8 s)
// and honey bottle (40 t / 2.0 s) deviate.
func foodEatTicks(item int32) int {
	switch item {
	case itemDriedKelp:
		return 16
	case itemHoneyBottle:
		return 40
	}
	return eatDuration
}

// eatNearlyTicks is how close to the finish a release still counts as eaten (a
// client's own consume timer can race ours by a packet): 2 ticks short.
func eatNearlyTicks(item int32) int { return foodEatTicks(item) - 2 }

// startEating begins the eat-hold: use_item only STARTS eating; the food
// applies after eatDuration ticks (updateEating), and an early release or a
// hotbar switch cancels it. Validated here so an invalid start never ticks.
func (h *hub) startEating(t *tracked, slot int) {
	if !t.canConsume() || t.dead || t.handStack(slot) == nil {
		return
	}
	// Milk is a drink, not a food: it has no nutrition, so the "already full"
	// gate below must not stop it — carrying it to cure a poison is the whole
	// reason to have it.
	if st := t.handStack(slot); (st.item == itemMilkBucket || st.item == itemOminousBottle || st.item == itemPotion) && st.count > 0 {
		t.eatingSlot, t.eatingAt = slot, h.tick.Load() // drinks: no nutrition, so no "already full" gate
		return
	}
	st := t.handStack(slot)
	if isBundle(st.item) && st.count > 0 {
		if st.bundleID != 0 && len(h.bundles.get(st.bundleID)) > 0 {
			t.eatingSlot, t.eatingAt = slot, h.tick.Load() // BundleItem.use: startUsingItem, emptied by the tick
		}
		return
	}
	if _, ok := foodPoints[st.item]; !ok || st.count == 0 || !t.canEat(st.item) {
		return
	}
	t.eatingSlot, t.eatingAt = slot, h.tick.Load()
}

// canConsume is who may eat and drink at all: every game mode but
// spectator (adventure eats as survival does; creative can always eat).
func (t *tracked) canConsume() bool { return t.gamemode != gmSpectator }

// canEat is Player.canEat: invulnerable (creative) players and the
// can_always_eat foods ignore a full hunger bar.
func (t *tracked) canEat(item int32) bool {
	return t.gamemode == gmCreative || alwaysEdible[item] || t.food < maxFood
}

// stopEating handles a release_use_item or hotbar switch: cancel the hold — or
// apply it when it was effectively finished (the client's own 32-tick timer can
// race ours by a packet).
func (h *hub) stopEating(players map[int32]*tracked, t *tracked) {
	if t.eatingSlot < 0 {
		return
	}
	slot := t.eatingSlot
	elapsed := h.tick.Load() - t.eatingAt
	t.eatingSlot = -1
	if s := t.handStack(slot); s != nil && isBundle(s.item) {
		return // letting go of a bundle just stops the emptying
	}
	// handStack, not inv.slots: the offhand is slot 40 of a 36-slot array,
	// and reading it directly panicked the hub when an offhand eat was let go.
	if s := t.handStack(slot); s != nil && elapsed >= uint64(eatNearlyTicks(s.item)) {
		h.eat(players, t, slot)
	}
}

// updateEating applies finished eat-holds (runs every tick).
func (h *hub) updateEating(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, t := range players {
		if t.eatingSlot < 0 {
			continue
		}
		if t.dead || !t.canConsume() {
			t.eatingSlot = -1
			continue
		}
		if s := t.handStack(t.eatingSlot); s != nil && isBundle(s.item) {
			h.bundleUseTick(players, t, t.eatingSlot, now-t.eatingAt)
			continue
		}
		if now-t.eatingAt >= uint64(foodEatTicks(t.handStack(t.eatingSlot).item)) {
			slot := t.eatingSlot
			t.eatingSlot = -1
			h.eat(players, t, slot)
		}
	}
}

// eat consumes one food item from the player's held hotbar slot and restores
// hunger + saturation. Survival only, and only when not already full. Reached
// via the eat-hold state machine above (use_item starts, eatDuration applies).
func (h *hub) eat(players map[int32]*tracked, t *tracked, slot int) {
	if !t.canConsume() || t.dead || t.inv == nil || t.handStack(slot) == nil {
		return // either hand: a hotbar slot or the offhand, which eats as vanilla's does
	}
	s := t.handStack(slot)
	if s.item != itemPotion && s.count > 0 {
		h.vibAt(t.dim, freqEat, t.x, t.y, t.z, t.p.eid)
	}
	if s.item == itemPotion && s.count > 0 {
		h.drinkPotion(nil, t, slot)
		return
	}
	if s.item == itemMilkBucket && s.count > 0 {
		h.drinkMilk(t, slot)
		return
	}
	if s.item == itemOminousBottle && s.count > 0 {
		h.drinkOminousBottle(players, t, slot)
		return
	}
	s = t.handStack(slot)
	pts, ok := foodPoints[s.item]
	if !ok || s.count == 0 || !t.canEat(s.item) {
		return // vanilla canEat: full players may still eat the can_always_eat foods
	}
	h.advance(players, t, "consume_item", advMatch{item: s.item})
	h.incStat(t, attachproto.StatUsed, s.item, 1)
	t.food = min(maxFood, t.food+pts)
	h.eatSpecial(players, t, s.item) // the food's on_consume effects
	if s.item == itemSuspiciousStew {
		h.eatStew(players, t, s.stew) // the flower's hidden effect
	}
	// Saturation gained per the food's value, capped at the new food level (vanilla).
	t.saturation = float32(math.Min(float64(t.food), float64(t.saturation)+float64(foodSaturation[s.item])))
	eaten := s.item
	if t.gamemode != gmCreative { // ItemStack.consume: infinite materials keep the stack
		s.count--
		if s.count == 0 {
			s.item = 0
		}
		h.giveUseRemainder(t, slot, eaten)
	}
	h.sendHealth(t)
	h.sendHandSlot(t, slot)
	t.p.trySendEv(soundEv("minecraft:entity.player.burp", sndPlayer, t.x, t.y, t.z, 1, 1))
}

// useRemainder is Item.Properties.usingConvertsTo: what a food leaves behind
// when it is eaten. A stew leaves its bowl and a honey bottle its glass; both
// simply vanished, which is a quiet tax on every bowl a player owns.
var useRemainder = map[int32]int32{}

func init() {
	for food, left := range map[string]string{
		"mushroom_stew": "bowl", "rabbit_stew": "bowl", "beetroot_soup": "bowl",
		"suspicious_stew": "bowl", "honey_bottle": "glass_bottle",
	} {
		if f, ok := itemByName[food]; ok {
			if l, ok := itemByName[left]; ok {
				useRemainder[int32(f)] = int32(l)
			}
		}
	}
}

// giveUseRemainder hands back the bowl or bottle. Vanilla puts it in the slot
// the food came from when that slot is now empty, and otherwise into the
// inventory — dropping it at the player's feet when there is nowhere left.
func (h *hub) giveUseRemainder(t *tracked, slot int, eaten int32) {
	left, ok := useRemainder[eaten]
	if !ok {
		return
	}
	if s := t.handStack(slot); s != nil && s.item == 0 {
		*s = invStack{item: left, count: 1}
		return
	}
	changed, leftover := t.inv.addStack(invStack{item: left, count: 1})
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	if leftover > 0 {
		h.spawnItemIn(h.playersRef, t.dim, left, leftover, t.x, t.y, t.z)
	}
}

func (h *hub) sendHealth(t *tracked) {
	t.p.trySendEv(attachproto.Health{Health: t.health, Food: int32(t.food), Saturation: t.saturation})
}

// airMetadata builds set_entity_metadata (0x5c) setting a player's air supply
// (index 1, VarInt), which drives the client's bubble HUD.
func airMetadata(eid int32, air int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexAir)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(air))
	return protocol.AppendU8(b, 0xff) // metadata list terminator
}

// offhandSlot names the offhand where a hotbar index is expected: vanilla's
// inventory slot 40, the number its own menus give the offhand. The use-item
// dispatch passes it when the client's hand was OFF_HAND.
const offhandSlot = 40

// handStack points at the stack a use slot names — a hotbar slot or the
// offhand. nil when the slot names neither (no inventory, out of range).
func (t *tracked) handStack(slot int) *invStack {
	switch {
	case t.inv == nil:
		return nil
	case slot == offhandSlot:
		return &t.offhand
	case slot >= 0 && slot < 9:
		return &t.inv.slots[slot]
	}
	return nil
}
