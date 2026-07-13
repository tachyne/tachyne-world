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
	effWaterBreathing = 12
	effNightVision    = 15
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

// activeEffect is one running effect: amplifier 0-based, seconds remaining.
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
		h.damage(players, t, float32(6*(int(1)<<amp)))
		return
	case effAbsorption:
		// Vanilla: the buffer is 4 HP per level, granted immediately.
		t.absorption = float32(4 * (amp + 1))
	}
	if t.effects == nil {
		t.effects = map[int32]*activeEffect{}
	}
	if cur, ok := t.effects[id]; ok && (cur.amp > amp || (cur.amp == amp && cur.left > secs)) {
		return // a stronger/longer instance is already running (vanilla)
	}
	t.effects[id] = &activeEffect{amp: amp, left: secs}
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

// tickEffects runs at 1 Hz inside the survival step.
func (h *hub) tickEffects(players map[int32]*tracked, t *tracked) {
	for id, e := range t.effects {
		switch id {
		case effRegen:
			// Vanilla: 1 HP per 50>>amp ticks — at 1 Hz, level I lands every
			// 3rd second, level II+ every second.
			if e.amp >= 1 || e.left%3 == 0 {
				if t.health < maxHealth {
					t.health = float32(math.Min(maxHealth, float64(t.health)+1))
					h.sendHealth(t)
				}
			}
		case effPoison:
			// Vanilla: 1 HP per 25>>amp ticks, never lethal (stops at half a heart).
			if t.health > 1 {
				t.health--
				h.sendHealth(t)
				t.p.trySendEv(attachproto.Hurt{EID: t.p.eid, Yaw: t.yaw})
			}
		case effWither:
			// Vanilla: 1 HP per 40>>amp ticks — like poison but CAN kill.
			h.damage(players, t, 1)
		}
		if e.left--; e.left <= 0 {
			h.removeEffect(t, id)
		}
	}
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
