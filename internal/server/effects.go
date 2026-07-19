package server

import (
	"fmt"
	"math"
	"strconv"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Status effects: the framework every potion-shaped mechanic hangs off. The
// hub owns each player's active effects, ticks them at 1 Hz (regen heals,
// poison bites to the brink), pushes the client HUD via entity_effect, and
// lets the combat/movement paths query modifiers (strength, weakness, speed,
// fire resistance). Effect ids are stable across 770-26.2 (ViaVersion never
// remaps them), so no chain work is needed.

const (
	effSpeed          = 0
	effSlowness       = 1
	effHaste          = 2
	effStrength       = 4
	effInstantHealth  = 5
	effInstantDamage  = 6
	effJumpBoost      = 7
	effRegen          = 9
	effResistance     = 10
	effFireRes        = 11
	effBlindness      = 14
	effWaterBreathing = 12
	effNightVision    = 15
	effHunger         = 16
	effWeakness       = 17
	effPoison         = 18
	effWither         = 19
	effAbsorption     = 21
	effLevitation     = 24
	effSlowFalling    = 27
	effBadOmen        = 30
)

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
	"bad_omen": effBadOmen,
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
		t.health = float32(math.Min(maxHealth, float64(t.health+heal)))
		h.sendHealth(t)
		return
	case effInstantDamage:
		h.damageExh(players, t, float32(6*(int(1)<<amp)), 0) // magic: no exhaustion
		return
	case effAbsorption:
		// Vanilla: the buffer is 4 HP per level, granted immediately.
		t.absorption = float32(4 * (amp + 1))
	}
	if t.effects == nil {
		t.effects = map[int32]*activeEffect{}
	}
	if cur, ok := t.effects[id]; ok && (cur.amp > amp || (cur.amp == amp && cur.left > secs*20)) {
		return // a stronger/longer instance is already running (vanilla)
	}
	t.effects[id] = &activeEffect{amp: amp, left: secs * 20}
	t.p.trySendEv(attachproto.Effect{EID: t.p.eid, ID: id, Amp: int32(amp), Ticks: int32(secs * 20)})
}

// removeEffect ends one effect (expiry or /effect clear).
func (h *hub) removeEffect(t *tracked, id int32) {
	delete(t.effects, id)
	if id == effAbsorption {
		t.absorption = 0 // the yellow hearts vanish when the effect lapses
	}
	t.p.trySendEv(attachproto.Effect{EID: t.p.eid, ID: id, Remove: true})
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
				if applyEffectTickNow(e.left, 50, e.amp) && t.health < maxHealth {
					t.health = float32(math.Min(maxHealth, float64(t.health)+1))
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
				// kill; the `wither` damage type costs no hunger exhaustion.
				if applyEffectTickNow(e.left, 40, e.amp) {
					h.damageExh(players, t, 1, 0)
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
