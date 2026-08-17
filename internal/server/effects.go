package server

import (
	"fmt"
	"math"
	"strconv"

	attachproto "github.com/tachyne/tachyne-common/attach"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Status effects: the framework every potion-shaped mechanic hangs off. The
// hub owns each player's active effects, ticks them at 1 Hz (regen heals,
// poison bites to the brink), pushes the client HUD via entity_effect, and
// lets the combat/movement paths query modifiers (strength, weakness, speed,
// fire resistance). Effect ids are stable across 770-26.2 (ViaVersion never
// remaps them), so no chain work is needed.

// The ids are the registry's own order — stable across every version we serve.
const (
	effSpeed          = 0
	effSlowness       = 1
	effHaste          = 2
	effMiningFatigue  = 3
	effStrength       = 4
	effInstantHealth  = 5
	effInstantDamage  = 6
	effJumpBoost      = 7
	effNausea         = 8
	effRegen          = 9
	effResistance     = 10
	effFireRes        = 11
	effWaterBreathing = 12
	effInvisibility   = 13
	effBlindness      = 14
	effNightVision    = 15
	effHunger         = 16
	effWeakness       = 17
	effPoison         = 18
	effWither         = 19
	effHealthBoost    = 20
	effAbsorption     = 21
	effSaturation     = 22
	effGlowing        = 23
	effLevitation     = 24
	effLuck           = 25
	effUnluck         = 26
	effSlowFalling    = 27
	effConduitPower   = 28
	effDolphinsGrace  = 29
	effBadOmen        = 30
	effHeroOfVillage  = 31 // raid-victory reward; discounts villager trades
	effDarkness       = 32
)

// effectModifier is one attribute modifier an effect contributes, straight
// from vanilla's MobEffects declarations. The amount scales with the level:
// vanilla's AttributeTemplate.create is amount × (amplifier + 1).
type effectModifier struct {
	id     attr.ID
	amount float64
	op     attr.Op
}

// effectModifiers is the whole of vanilla's addAttributeModifier set. Effects
// that only move a number are now nothing BUT this table — no special case in
// the reader, no per-effect branch in the damage or movement code.
var effectModifiers = map[int32][]effectModifier{
	effSpeed:         {{attr.MovementSpeed, 0.2, attr.AddMultipliedTotal}},
	effSlowness:      {{attr.MovementSpeed, -0.15, attr.AddMultipliedTotal}},
	effHaste:         {{attr.AttackSpeed, 0.1, attr.AddMultipliedTotal}},
	effMiningFatigue: {{attr.AttackSpeed, -0.1, attr.AddMultipliedTotal}},
	effStrength:      {{attr.AttackDamage, 3, attr.AddValue}},
	effWeakness:      {{attr.AttackDamage, -4, attr.AddValue}},
	effJumpBoost:     {{attr.SafeFallDistance, 1, attr.AddValue}},
	effHealthBoost:   {{attr.MaxHealth, 4, attr.AddValue}},
	effAbsorption:    {{attr.MaxAbsorption, 4, attr.AddValue}},
	effLuck:          {{attr.Luck, 1, attr.AddValue}},
	effUnluck:        {{attr.Luck, -1, attr.AddValue}},
}

// effectSource is the modifier source an effect owns, so applying it twice
// replaces rather than stacks and expiry removes exactly its own contribution.
func effectSource(id int32) string { return "effect:" + strconv.Itoa(int(id)) }

var (
	itemGoldenApple     = itemByName["golden_apple"]
	itemEnchGoldenApple = itemByName["enchanted_golden_apple"]
)

// activeEffect is one running effect: amplifier 0-based, TICKS remaining (the
// vanilla MobEffectInstance.duration — ticked down and consulted at 20 Hz so
// the regen/poison/wither application cadence is exact; see updateEffects).
type activeEffect struct {
	amp  int
	left int
}

// effectNames maps /effect arguments to ids.
var effectNames = map[string]int32{
	"speed": effSpeed, "slowness": effSlowness, "haste": effHaste, "strength": effStrength,
	"instant_health": effInstantHealth, "instant_damage": effInstantDamage,
	"jump_boost": effJumpBoost, "regeneration": effRegen,
	"fire_resistance": effFireRes, "night_vision": effNightVision,
	"weakness": effWeakness, "poison": effPoison,
	"wither": effWither, "levitation": effLevitation,
	"resistance": effResistance, "water_breathing": effWaterBreathing,
	"absorption": effAbsorption, "slow_falling": effSlowFalling,
	"bad_omen": effBadOmen, "hero_of_the_village": effHeroOfVillage,
	"mining_fatigue": effMiningFatigue, "nausea": effNausea,
	"invisibility": effInvisibility, "blindness": effBlindness,
	"health_boost": effHealthBoost, "saturation": effSaturation,
	"glowing": effGlowing, "luck": effLuck, "unluck": effUnluck,
	"conduit_power": effConduitPower, "dolphins_grace": effDolphinsGrace,
	"darkness":   effDarkness,
	"trial_omen": effTrialOmen, "raid_omen": effRaidOmen,
	"wind_charged": effWindCharged, "weaving": effWeaving,
	"oozing": effOozing, "infested": effInfested,
}

// hasEffect returns the 1-based level of an active effect (0 = none).
func (t *tracked) hasEffect(id int32) int {
	if e, ok := t.effects[id]; ok {
		return e.amp + 1
	}
	return 0
}

// applyEffect starts (or refreshes) an effect and shows it on the client HUD.
// Instant effects apply immediately and are never stored.
func (h *hub) applyEffect(players map[int32]*tracked, t *tracked, id int32, amp, secs int) {
	switch id {
	case effInstantHealth:
		heal := float32(4 * (int(1) << amp))
		t.health = float32(math.Min(float64(t.maxHP()), float64(t.health+heal)))
		h.sendHealth(t)
		return
	case effInstantDamage:
		h.damageOf(players, t, float32(6*(int(1)<<amp)), dtMagic)
		return
	case effSaturation:
		// SaturationMobEffect: 1 food + 2 saturation per level, every tick it
		// applies — but the whole point is that it fills you instantly, so the
		// grant tops both up here and applyEffectTick keeps them there.
		h.feedSaturation(t, amp)
	}
	if t.effects == nil {
		t.effects = map[int32]*activeEffect{}
	}
	if cur, ok := t.effects[id]; ok && (cur.amp > amp || (cur.amp == amp && cur.left > secs*20)) {
		return // a stronger/longer instance is already running (vanilla)
	}
	t.effects[id] = &activeEffect{amp: amp, left: secs * 20}
	t.applyEffectModifiers(id, amp)
	if id == effAbsorption {
		// The buffer fills to the new ceiling the moment the effect lands.
		t.absorption = float32(t.playerAttrs().Value(attr.MaxAbsorption))
	}
	if id == effInvisibility || id == effGlowing {
		h.broadcastPlayerFlags(players, t) // other players have to see it too
	}
	t.p.trySendEv(attachproto.Effect{EID: t.p.eid, ID: id, Amp: int32(amp), Ticks: int32(secs * 20)})
}

// applyEffectModifiers installs an effect's attribute modifiers at its level,
// replacing any the same effect already had (vanilla removes then re-adds).
func (t *tracked) applyEffectModifiers(id int32, amp int) {
	mods := effectModifiers[id]
	if len(mods) == 0 {
		return
	}
	src := effectSource(id)
	a := t.playerAttrs()
	for _, m := range mods {
		a.Get(m.id).AddModifier(attr.Modifier{
			Source: src, Amount: m.amount * float64(amp+1), Op: m.op,
		})
	}
}

// removeEffect ends one effect (expiry or /effect clear).
func (h *hub) removeEffect(t *tracked, id int32) {
	delete(t.effects, id)
	t.playerAttrs().RemoveSource(effectSource(id))
	if id == effAbsorption {
		t.absorption = 0 // the yellow hearts vanish when the effect lapses
	}
	if id == effHealthBoost && t.health > t.maxHP() {
		// The extra hearts go with the effect; vanilla leaves you on the ones
		// you have left rather than healing you into the smaller bar.
		t.health = t.maxHP()
		h.sendHealth(t)
	}
	if id == effInvisibility || id == effGlowing {
		h.broadcastPlayerFlags(h.playersRef, t)
	}
	t.p.trySendEv(attachproto.Effect{EID: t.p.eid, ID: id, Remove: true})
}

// feedSaturation is SaturationMobEffect.applyEffectTick: +1 food and +2
// saturation per level, both clamped to the maximum.
func (h *hub) feedSaturation(t *tracked, amp int) {
	if t.food >= maxFood && t.saturation >= float32(maxFood) {
		return
	}
	t.food = min(maxFood, t.food+amp+1)
	t.saturation = float32(math.Min(float64(t.saturation)+float64(2*(amp+1)), float64(t.food)))
	h.sendHealth(t)
}

// clearEffects drops everything (death does this; vanilla too).
func (h *hub) clearEffects(t *tracked) {
	for id := range t.effects {
		h.removeEffect(t, id)
	}
}

// updateEffects ticks every survival player's status effects once per game
// tick (20 Hz) and applies the periodic ones on vanilla's exact per-effect
// cadence. It runs from the hub loop, NOT the 1 Hz survival step, because the
// intervals are sub-second (Regeneration 50>>amp ticks, Poison 25>>amp,
// Wither 40>>amp) and cannot be represented at 1 Hz.
func (h *hub) updateEffects(players map[int32]*tracked) {
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead || t.health <= 0 || len(t.effects) == 0 {
			continue
		}
		for id, e := range t.effects {
			switch id {
			case effRegen:
				// RegenerationMobEffect: heal 1 HP every 50>>amp ticks.
				if applyEffectTickNow(e.left, 50, e.amp) && t.health < t.maxHP() {
					t.health = float32(math.Min(float64(t.maxHP()), float64(t.health)+1))
					h.sendHealth(t)
				}
			case effPoison:
				// PoisonMobEffect: 1 HP every 25>>amp ticks, never lethal
				// (stops at half a heart).
				if applyEffectTickNow(e.left, 25, e.amp) && t.health > 1 {
					t.health--
					h.sendHealth(t)
					t.p.trySendEv(attachproto.Hurt{EID: t.p.eid, Yaw: t.yaw})
				}
			case effWither:
				// WitherMobEffect: 1 HP every 40>>amp ticks — like poison but CAN
				// kill.
				if applyEffectTickNow(e.left, 40, e.amp) {
					h.damageOf(players, t, 1, dtWither)
				}
			case effHunger:
				// HungerMobEffect: 0.005 exhaustion EVERY TICK per level. This
				// pass runs at 1 Hz, so a second's worth goes on at once — the
				// effect was being applied (a husk's bite grants it) and then
				// costing its victim nothing at all.
				t.exhaustion += hungerExhaustionPerSec * float32(e.amp+1)
			case effSaturation:
				// SaturationMobEffect fires every tick it is active.
				h.feedSaturation(t, e.amp)
			case effRaidOmen:
				// RaidOmenMobEffect fires on its LAST tick, and that is the
				// raid horn.
				if e.left == 1 {
					h.raidOmenExpired(players, t)
				}
			}
			if t.dead { // a wither tick may have killed — stop touching effects
				break
			}
			if e.left--; e.left <= 0 {
				h.removeEffect(t, id)
			}
		}
	}
}

// applyEffectTickNow ports MobEffect.shouldApplyEffectTickThisTick: a periodic
// effect fires this tick when its remaining duration is a multiple of
// (base>>amp) ticks; a zero interval (very high amplifier) fires every tick.
func applyEffectTickNow(left, base, amp int) bool {
	if i := base >> amp; i > 0 {
		return left%i == 0
	}
	return true
}

// eatSpecial applies the food-item side effects beyond hunger (golden apples).
// Called from eat() after the normal restore.
func (h *hub) eatSpecial(players map[int32]*tracked, t *tracked, item int32) {
	switch item {
	case itemGoldenApple:
		h.applyEffect(players, t, effRegen, 1, 5)        // Regen II ×5 s
		h.applyEffect(players, t, effAbsorption, 0, 120) // Absorption I ×2 min (4 HP)
	case itemEnchGoldenApple:
		h.applyEffect(players, t, effRegen, 1, 20)       // Regen II ×20 s
		h.applyEffect(players, t, effFireRes, 0, 300)    // Fire Res ×5 min
		h.applyEffect(players, t, effAbsorption, 3, 120) // Absorption IV ×2 min (16 HP)
		h.applyEffect(players, t, effResistance, 0, 300) // Resistance I ×5 min
	}
}

// cmdEffect is the op command: /effect <give|clear> <player> <effect> [secs] [amp].
func (s *Server) cmdEffect(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to apply effects.")
		return
	}
	if len(args) < 2 {
		p.tell("Usage: /effect <give|clear> <player> [effect] [seconds] [amplifier]")
		return
	}
	ev := evEffect{target: args[1], clear: args[0] == "clear"}
	if !ev.clear {
		if len(args) < 3 {
			p.tell("Usage: /effect give <player> <effect> [seconds] [amplifier]")
			return
		}
		id, ok := effectNames[args[2]]
		if !ok {
			p.tell("Unknown effect: " + args[2])
			return
		}
		ev.id, ev.secs = id, 30
		if len(args) >= 4 {
			ev.secs, _ = strconv.Atoi(args[3])
		}
		if len(args) >= 5 {
			ev.amp, _ = strconv.Atoi(args[4])
		}
	}
	s.hub.post(ev)
	p.tell(fmt.Sprintf("Effect %s applied to %s", args[0], args[1]))
}

type evEffect struct {
	target string
	clear  bool
	id     int32
	secs   int
	amp    int
}

func (evEffect) isHubEvent() {}

// hungerExhaustionPerSec is HungerMobEffect's 0.005-per-tick exhaustion,
// gathered into the one-second step this engine ticks effects on.
const hungerExhaustionPerSec = 0.005 * 20
