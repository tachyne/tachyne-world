package server

import (
	"log"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/worldgen"
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
	regenPeriod         = 80  // ticks per 1 HP regen / 1 HP starvation step (4 s)
	regenExhaustion     = 6.0 // exhaustion added per HP regenerated
	exhaustionThreshold = 4.0 // exhaustion units per 1 saturation/food drained
	voidDamagePerSec    = 8   // 4 HP every 0.5 s, applied as 8 HP once per second
	sprintExhaustion    = 0.1 // exhaustion per block sprinted (walking is free)
	attackExhaustion    = 0.1 // exhaustion per landed melee hit

	// Environmental contact damage, applied once per survival second (see
	// environmentDamage). Air is in ticks so the bubble HUD reads it directly.
	maxAir             = 300 // ~15 s of breath (10 bubbles)
	airDrainPerSec     = 20  // 1/tick submerged (vanilla)
	airRefillPerSec    = 80  // 4/tick out of water (vanilla)
	drownDamagePerSec  = 2   // 2 HP/s once the air supply is empty
	lavaDamagePerSec   = 8   // vanilla lava ≈ 4 HP / 0.5 s (fire-after-exit not modelled)
	cactusDamagePerSec = 1   // approx (vanilla 1 HP / 0.5 s on contact)

	metaIndexAir = 1 // entity-metadata index of the air supply
	metaTypeInt  = 1 // metadata value type id for VarInt (1.21.5)
)

// initSurvival sets a freshly tracked player to full health/food.
func initSurvival(t *tracked) {
	t.health = maxHealth
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
func (h *hub) fastRegen(players map[int32]*tracked) {
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead || t.health <= 0 {
			continue
		}
		if t.food == maxFood && t.saturation > 0 && t.health < maxHealth {
			f := float32(math.Min(float64(t.saturation), 6))
			heal := f / 6
			if t.health+heal > maxHealth {
				heal = maxHealth - t.health
			}
			t.health += heal
			t.exhaustion += f
			h.sendHealth(t)
		}
	}
}

// survivalTick runs once per second: void damage, regeneration, starvation, and
// converting exhaustion into hunger. Regen/starvation step only every regenPeriod
// ticks (vanilla: 1 HP per 80 ticks); the exhaustion→hunger drain runs each call.
func (h *hub) survivalTick(players map[int32]*tracked) {
	slow := h.tick.Load()%regenPeriod == 0 // 80-tick (4s) regen/starve cadence
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead {
			continue
		}
		if t.y < float64(voidBelow) {
			h.damage(players, t, voidDamagePerSec) // ≈ vanilla 4 HP / 0.5 s = 8 HP/s
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
		h.tickEffects(players, t) // regen heals, poison bites, timers run out
		if t.dead {
			continue
		}
		changed := false
		// (Fast saturation regen moved to fastRegen — vanilla's true 10-tick
		// cadence, run from the hub loop rather than this 1 Hz step.)
		// Slow regen is vanilla's ELSE-IF branch: it never fires while
		// saturation regen is active (full food + sat left) — the final oracle
		// fight caught us healing 1.5 HP in one tick by running both.
		fastActive := t.food == maxFood && t.saturation > 0
		if slow && !fastActive && t.food >= regenFood && t.health < maxHealth && t.health > 0 {
			t.health = float32(math.Min(maxHealth, float64(t.health)+1))
			t.exhaustion += regenExhaustion // vanilla: 6.0 exhaustion per HP healed
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
				h.damage(players, t, 1) // starve damage: death drops on hard
				changed = true
			}
		}
		for t.exhaustion >= exhaustionThreshold { // drains saturation, then food
			t.exhaustion -= exhaustionThreshold
			if t.saturation > 0 {
				t.saturation = float32(math.Max(0, float64(t.saturation)-1))
			} else if t.food > 0 {
				t.food--
				changed = true
			}
		}
		if changed {
			h.sendHealth(t)
		}
	}
}

// environmentDamage applies contact hazards once per survival second: the air
// supply drains while the head is submerged (drowning at 0), lava burns fast, and
// touching an adjacent cactus stings. Reads blocks through the edit overlay so
// player-built hazards count. Each h.damage may kill, so callers re-check t.dead.
func (h *hub) environmentDamage(players map[int32]*tracked, t *tracked) {
	fx, fz := int(math.Floor(t.x)), int(math.Floor(t.z))
	feet := int(math.Floor(t.y))
	eyeY := int(math.Floor(t.y + 1.5))

	// Drowning: eyes in water deplete the breath supply; empty → 2 HP/s.
	// Water Breathing holds the breath indefinitely (no drain).
	old := t.air
	if worldgen.IsWater(h.worldFor(t.dim).At(fx, eyeY, fz)) && t.hasEffect(effWaterBreathing) == 0 {
		if t.air -= airDrainPerSec; t.air <= 0 {
			t.air = 0
			h.damage(players, t, drownDamagePerSec)
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
	// Lava: standing in it (feet or body) burns fast — and leaves you alight.
	// Fire resistance makes lava a warm bath (vanilla).
	if t.hasEffect(effFireRes) == 0 &&
		(worldgen.IsLava(h.worldFor(t.dim).At(fx, feet, fz)) || worldgen.IsLava(h.worldFor(t.dim).At(fx, feet+1, fz))) {
		h.setBurning(players, t, lavaFireSecs)
		if h.damage(players, t, lavaDamagePerSec); t.dead {
			return
		}
	}
	// Fire blocks: contact damage + a shorter afterburn.
	if isFire(h.worldFor(t.dim).At(fx, feet, fz)) || isFire(h.worldFor(t.dim).At(fx, feet+1, fz)) {
		h.setBurning(players, t, fireContactSecs)
		if h.damage(players, t, fireDamagePerSec); t.dead {
			return
		}
	}
	// Cactus: contact with an adjacent cactus at feet or body height.
	if h.touchingCactus(t.dim, fx, feet, fz) {
		h.damage(players, t, cactusDamagePerSec)
	}
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
	return worldgen.IsWater(w.At(int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))))
}

// onFallAndExhaust updates fall-damage tracking and walking exhaustion from a
// movement event (called from onMove before the position is advanced).
func (h *hub) onFallAndExhaust(players map[int32]*tracked, t *tracked, e evMove) {
	if t.gamemode != gmSurvival || t.dead {
		return
	}
	if e.sprinting { // vanilla: only sprinting drains food from movement; walking is free
		t.exhaustion += sprintExhaustion * float32(math.Hypot(e.x-t.x, e.z-t.z))
	}
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
					t.exhaustion += 0.2
				} else {
					t.exhaustion += 0.05
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
		if dist := t.peakY - e.y; dist > 3 { // 3-block grace, then 1 dmg/block
			h.damage(players, t, float32(math.Floor(dist-3)))
		}
	}
}

// damage applies harm to a survival player, triggering death at 0 health. On
// death the player's inventory scatters as item entities (the survival stake), so
// callers pass the player registry for the drops to be shown to nearby players.
func (h *hub) damage(players map[int32]*tracked, t *tracked, amount float32) {
	if t.gamemode != gmSurvival || t.dead || t.health <= 0 {
		return
	}
	// Resistance: -20% per level (MobEffects.DAMAGE_RESISTANCE); level 5 = immune.
	if r := t.hasEffect(effResistance); r > 0 {
		amount *= float32(math.Max(0, float64(25-r*5)) / 25)
	}
	// Absorption soaks damage into its buffer before real health (yellow hearts).
	if t.absorption > 0 && amount > 0 {
		soak := float32(math.Min(float64(t.absorption), float64(amount)))
		t.absorption -= soak
		amount -= soak
	}
	if amount <= 0 {
		return // fully resisted / absorbed — no health lost, no hurt flash
	}
	h.wakePlayer(players, t) // pain wakes (and stands the pose back up)
	t.exhaustion += 0.1      // vanilla: taking damage costs food
	t.health -= amount
	if t.health <= 0 {
		t.health = 0
		t.dead = true
		h.incCustom(t, "deaths", 1)
		log.Printf("%q died at (%.0f,%.0f,%.0f)", t.p.name, t.x, t.y, t.z)
		if !h.rules.KeepInventory { // gamerule: keepInventory skips the stake
			h.dropInventory(players, t)
			h.dropDeathXP(players, t) // 7×level as an orb at the death spot, bar zeroed
		}
		t.p.trySendEv(attachproto.Death{EID: t.p.eid, Message: "You died"})
		h.playSound(players, "minecraft:entity.player.death", sndPlayer, t.x, t.y, t.z, 1, 1)
	} else {
		t.p.trySendEv(attachproto.Hurt{EID: t.p.eid, Yaw: t.yaw})
		h.playSound(players, "minecraft:entity.player.hurt", sndPlayer, t.x, t.y, t.z, 1, h.hurtPitch())
	}
	h.sendHealth(t)
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
		// Small jitter so stacks don't perfectly overlap on one column.
		jx := t.x + (h.rng.Float64() - 0.5)
		jz := t.z + (h.rng.Float64() - 0.5)
		if it := h.spawnItem(players, s.item, s.count, jx, t.y, jz); it != nil {
			it.dmg = s.dmg // worn tools keep their wear through the death drop
			it.ench = s.ench
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
	if !t.dead {
		return
	}
	initSurvival(t)
	sx, sy, sz := h.respawnPoint(t)
	t.x, t.y, t.z = sx, sy, sz
	t.p.trySendEv(attachproto.Dimension{Dim: int32(t.dim), Gamemode: int32(t.gamemode)})
	t.p.trySendEv(teleportEv(sx, sy, sz, t.yaw, t.pitch))
	t.p.trySendEv(abilitiesFor(t.gamemode))
	h.sendHealth(t)
	h.sendInventory(t)  // clear the client's inventory view after the death drop
	h.sendExperience(t) // …and the (zeroed) XP bar
	if t.dim != 0 {
		// Death respawns to the OVERWORLD: without this, the connection kept
		// streaming the old dimension's chunks and the hub kept the player in
		// it for everyone else — phantom players and blocks-in-thin-air. Route
		// through the pending-switch machinery so the connection resets its
		// dimension, restreams overworld chunks, and everyone's view swaps.
		t.p.pendingFrom = dimPos{}
		t.p.pendingDest = blockPos{floorInt(sx), floorInt(sy), floorInt(sz) - 1}
		t.p.pendingDestOK = true
		t.p.pendingDim.Store(0)
	}
}

const (
	eatDuration   = 32 // ticks of eat-hold before the food applies (vanilla 1.6s)
	eatNearlyDone = 30 // a release this close to done counts as finished (packet race)
)

// startEating begins the eat-hold: use_item only STARTS eating; the food
// applies after eatDuration ticks (updateEating), and an early release or a
// hotbar switch cancels it. Validated here so an invalid start never ticks.
func (h *hub) startEating(t *tracked, slot int) {
	if t.gamemode != gmSurvival || t.dead || t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	if _, ok := foodPoints[t.inv.slots[slot].item]; !ok || t.inv.slots[slot].count == 0 || t.food >= maxFood {
		return
	}
	t.eatingSlot, t.eatingAt = slot, h.tick.Load()
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
	if elapsed >= eatNearlyDone {
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
		if t.dead || t.gamemode != gmSurvival {
			t.eatingSlot = -1
			continue
		}
		if now-t.eatingAt >= eatDuration {
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
	if t.gamemode != gmSurvival || t.dead || t.inv == nil || slot < 0 || slot >= invSize {
		return
	}
	s := &t.inv.slots[slot]
	if s.item == itemPotion && s.count > 0 {
		h.drinkPotion(nil, t, slot)
		return
	}
	s = &t.inv.slots[slot]
	pts, ok := foodPoints[s.item]
	if !ok || s.count == 0 || t.food >= maxFood {
		return
	}
	h.advance(players, t, "consume_item", advMatch{item: s.item})
	h.incStat(t, attachproto.StatUsed, s.item, 1)
	t.food = min(maxFood, t.food+pts)
	h.eatSpecial(nil, t, s.item) // golden apples carry regen/fire-res
	// Saturation gained per the food's value, capped at the new food level (vanilla).
	t.saturation = float32(math.Min(float64(t.food), float64(t.saturation)+float64(foodSaturation[s.item])))
	s.count--
	if s.count == 0 {
		s.item = 0
	}
	h.sendHealth(t)
	h.sendSlot(t, slot)
	t.p.trySendEv(soundEv("minecraft:entity.player.burp", sndPlayer, t.x, t.y, t.z, 1, 1))
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
