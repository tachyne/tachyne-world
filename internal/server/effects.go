package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

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
	// 33-38: trial_omen, raid_omen, wind_charged, weaving, oozing, infested
	effBreathOfTheNautilus = 39 // riding a nautilus: breathes under water (MobEffectUtil.hasWaterBreathing)
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
	amp         int
	left        int           // ticks remaining, or effInfinite
	ambient     bool          // from a beacon or a conduit: fainter particles, as vanilla's flag
	noParticles bool          // !visible: /effect give … hideParticles
	hidden      *activeEffect // hiddenEffect: a weaker, longer instance waiting under this one
}

// effInfinite is MobEffectInstance.INFINITE_DURATION: an effect that never
// runs down (/effect give … infinite).
const effInfinite = -1

func (e *activeEffect) infinite() bool { return e.left == effInfinite }

// shorterThan is MobEffectInstance.isShorterDurationThan: nothing outlasts an
// infinite effect, and an infinite one is never the shorter.
func (e *activeEffect) shorterThan(o *activeEffect) bool {
	return !e.infinite() && (e.left < o.left || o.infinite())
}

// endsWithin is MobEffectInstance.endsWithin: never, for an infinite effect.
func (e *activeEffect) endsWithin(ticks int) bool { return !e.infinite() && e.left <= ticks }

// clock is the count a periodic effect's cadence reads (tickServer): its
// remaining duration, or for an infinite one the entity's own tick count.
func (e *activeEffect) clock(tick uint64) int {
	if e.infinite() {
		return int(tick & 0x7fffffff)
	}
	return e.left
}

// tickDown is tickDownDuration: the instance and its hidden stack each lose a
// tick, and an infinite one keeps it.
func (e *activeEffect) tickDown() {
	for x := e; x != nil; x = x.hidden {
		if !x.infinite() {
			x.left--
		}
	}
}

// expired is !hasRemainingDuration.
func (e *activeEffect) expired() bool { return !e.infinite() && e.left <= 0 }

// effectEv is the effect's frame for its holder's HUD and the entity's viewers.
func effectEv(eid, id int32, e *activeEffect) attachproto.Effect {
	return attachproto.Effect{EID: eid, ID: id, Amp: int32(e.amp), Ticks: int32(e.left),
		Ambient: e.ambient, NoParticles: e.noParticles}
}

// update is MobEffectInstance.update: a stronger instance takes over (the
// current one, if it would outlast it, is kept hidden underneath); an equal
// one extends the duration; a weaker but longer one goes into the hidden
// stack. Reports whether what is showing changed.
func (e *activeEffect) update(o activeEffect) bool {
	changed := false
	switch {
	case o.amp > e.amp:
		if o.shorterThan(e) {
			prev := *e
			e.hidden = &prev
		}
		e.amp, e.left, changed = o.amp, o.left, true
	case e.shorterThan(&o):
		if o.amp == e.amp {
			e.left, changed = o.left, true
		} else if e.hidden == nil {
			h := o
			h.hidden = nil
			e.hidden = &h
		} else {
			e.hidden.update(o)
		}
	}
	if (!o.ambient && e.ambient) || changed {
		e.ambient, changed = o.ambient, true
	}
	if o.noParticles != e.noParticles {
		e.noParticles, changed = o.noParticles, true
	}
	return changed
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
	"breath_of_the_nautilus": effBreathOfTheNautilus,
	"mining_fatigue":         effMiningFatigue, "nausea": effNausea,
	"invisibility": effInvisibility, "blindness": effBlindness,
	"health_boost": effHealthBoost, "saturation": effSaturation, "hunger": effHunger,
	"glowing": effGlowing, "luck": effLuck, "unluck": effUnluck,
	"conduit_power": effConduitPower, "dolphins_grace": effDolphinsGrace,
	"darkness":   effDarkness,
	"trial_omen": effTrialOmen, "raid_omen": effRaidOmen,
	"wind_charged": effWindCharged, "weaving": effWeaving,
	"oozing": effOozing, "infested": effInfested,
}

// effectTitles are the effects' English names (effect.minecraft.*), for
// command feedback.
var effectTitles = map[string]string{
	"speed":                  "Speed",
	"slowness":               "Slowness",
	"haste":                  "Haste",
	"strength":               "Strength",
	"instant_health":         "Instant Health",
	"instant_damage":         "Instant Damage",
	"jump_boost":             "Jump Boost",
	"regeneration":           "Regeneration",
	"fire_resistance":        "Fire Resistance",
	"night_vision":           "Night Vision",
	"weakness":               "Weakness",
	"poison":                 "Poison",
	"wither":                 "Wither",
	"levitation":             "Levitation",
	"resistance":             "Resistance",
	"water_breathing":        "Water Breathing",
	"absorption":             "Absorption",
	"slow_falling":           "Slow Falling",
	"bad_omen":               "Bad Omen",
	"hero_of_the_village":    "Hero of the Village",
	"breath_of_the_nautilus": "Breath of the Nautilus",
	"mining_fatigue":         "Mining Fatigue",
	"nausea":                 "Nausea",
	"invisibility":           "Invisibility",
	"blindness":              "Blindness",
	"health_boost":           "Health Boost",
	"saturation":             "Saturation",
	"hunger":                 "Hunger",
	"glowing":                "Glowing",
	"luck":                   "Luck",
	"unluck":                 "Bad Luck",
	"conduit_power":          "Conduit Power",
	"dolphins_grace":         "Dolphin's Grace",
	"darkness":               "Darkness",
	"trial_omen":             "Trial Omen",
	"raid_omen":              "Raid Omen",
	"wind_charged":           "Wind Charged",
	"weaving":                "Weaving",
	"oozing":                 "Oozing",
	"infested":               "Infested",
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
	h.applyEffectFrom(players, t, id, amp, secs, false)
}

// applyEffectTicks is applyEffect in the unit vanilla actually stores. A
// potion's duration is a tick count, and rounding it to whole seconds costs
// the two that are not a round number.
func (h *hub) applyEffectTicks(players map[int32]*tracked, t *tracked, id int32, amp, ticks int) {
	h.applyEffectFrom(players, t, id, amp, ticks, false, true)
}

// applyEffectFrom is applyEffect with MobEffectInstance's ambient flag: a
// beacon's and a conduit's effects are ambient, which the client draws with
// fainter particles and a blue-ringed icon.
func (h *hub) applyEffectFrom(players map[int32]*tracked, t *tracked, id int32, amp, dur int, ambient bool, inTicks ...bool) {
	ticks := dur * 20
	if (len(inTicks) > 0 && inTicks[0]) || dur == effInfinite {
		ticks = dur
	}
	h.addEffect(players, t, id, activeEffect{amp: amp, left: ticks, ambient: ambient})
}

// addEffect is LivingEntity.addEffect for a player: the instance is merged
// into any running one (MobEffectInstance.update) and shown. Reports whether
// anything took, which is what /effect's feedback counts.
func (h *hub) addEffect(players map[int32]*tracked, t *tracked, id int32, in activeEffect) bool {
	amp := in.amp
	switch id {
	case effInstantHealth:
		heal := float32(4 * (int(1) << amp))
		t.health = float32(math.Min(float64(t.maxHP()), float64(t.health+heal)))
		h.sendHealth(t)
		return true
	case effInstantDamage:
		h.damageOf(players, t, float32(6*(int(1)<<amp)), dtMagic)
		return true
	}
	if t.effects == nil {
		t.effects = map[int32]*activeEffect{}
	}
	cur, ok := t.effects[id]
	if ok {
		if !cur.update(in) {
			return false // nothing showing changed: a weaker one went into the hidden stack, if anywhere
		}
		amp = cur.amp
	} else {
		in.hidden = nil
		cur = &in
		t.effects[id] = cur
	}
	t.applyEffectModifiers(id, amp)
	if id == effAbsorption {
		// The buffer fills to the new ceiling the moment the effect lands.
		t.absorption = float32(t.playerAttrs().Value(attr.MaxAbsorption))
	}
	if id == effInvisibility || id == effGlowing {
		h.broadcastPlayerFlags(players, t) // other players have to see it too
	}
	ev := effectEv(t.p.eid, id, cur)
	ev.Blend = !ok // onEffectAdded blends a new effect in; onEffectUpdated does not
	t.p.trySendEv(ev)
	h.syncPlayerSwirls(players, t)
	if id == effHeroOfVillage {
		h.advance(players, t, "hero_of_the_village", advMatch{})
	}
	if id == effLevitation && !t.levitating {
		t.levStartY, t.levitating = t.y, true
	}
	active := make(map[int32]bool, len(t.effects))
	for eid := range t.effects {
		active[eid] = true
	}
	h.advance(players, t, "effects_changed", advMatch{effects: active})
	return true
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
	h.syncPlayerSwirls(h.playersRef, t)
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
		if t.dead || len(t.effects) == 0 {
			continue
		}
		// Effects tick in every game mode (vanilla's MobEffectInstance does),
		// but the periodic damage and healing are a survival concern — a
		// creative player's Night Vision still runs out, their Poison still
		// does nothing.
		survival := isSurvival(t.gamemode) && t.health > 0
		for id, e := range t.effects {
			switch id {
			case effRegen:
				// RegenerationMobEffect: heal 1 HP every 50>>amp ticks.
				if survival && applyEffectTickNow(e.clock(h.tick.Load()), 50, e.amp) && t.health < t.maxHP() {
					t.health = float32(math.Min(float64(t.maxHP()), float64(t.health)+1))
					h.sendHealth(t)
				}
			case effPoison:
				// PoisonMobEffect: 1 HP every 25>>amp ticks, never lethal
				// (stops at half a heart).
				if survival && applyEffectTickNow(e.clock(h.tick.Load()), 25, e.amp) && t.health > 1 {
					t.health--
					h.sendHealth(t)
					t.p.trySendEv(attachproto.Hurt{EID: t.p.eid, Yaw: t.yaw})
				}
			case effWither:
				// WitherMobEffect: 1 HP every 40>>amp ticks — like poison but CAN
				// kill.
				if survival && applyEffectTickNow(e.clock(h.tick.Load()), 40, e.amp) {
					h.damageOf(players, t, 1, dtWither)
				}
			case effLevitation:
				// LevitationTrigger: checked while the effect runs, against where it began.
				if t.levitating {
					h.advance(players, t, "levitation", advMatch{distY: t.y - t.levStartY})
					if e.endsWithin(1) {
						t.levitating = false
					}
				}
			case effHunger:
				// HungerMobEffect: 0.005 exhaustion every tick per level (this
				// pass runs every tick; a second's worth at once drained a
				// husk's victim twenty times too fast).
				if survival {
					t.exhaust(hungerExhaustionPerTick * float32(e.amp+1))
				}
			case effSaturation:
				// SaturationMobEffect fires every tick it is active.
				if survival {
					h.feedSaturation(t, e.amp)
				}
			case effAbsorption:
				// AbsorptionMobEffect.applyEffectTick: the effect lasts only as
				// long as the golden hearts do.
				if t.absorption <= 0 {
					h.removeEffect(t, id)
					continue
				}
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
			e.tickDown() // tickDownDuration: the hidden stack runs down too; an infinite one never does
			if e.expired() {
				if hid := e.hidden; hid != nil && !hid.expired() { // downgradeToHiddenEffect
					*e = *hid
					t.applyEffectModifiers(id, e.amp)
					t.p.trySendEv(effectEv(t.p.eid, id, e))
					h.syncPlayerSwirls(players, t)
					continue
				}
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
// eatSpecial is the food's on_consume effects (vanilla's Consumables table):
// apply_effects with their chances, the honey bottle's remove_effects, and
// the chorus fruit's teleport_randomly.
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
	case itemRawChicken:
		if h.rng.Float64() < 0.3 {
			h.applyEffect(players, t, effHunger, 0, 30)
		}
	case itemRottenFlesh:
		if h.rng.Float64() < 0.8 {
			h.applyEffect(players, t, effHunger, 0, 30)
		}
	case itemSpiderEye:
		h.applyEffect(players, t, effPoison, 0, 5)
	case itemPoisonousPotato:
		if h.rng.Float64() < 0.6 {
			h.applyEffect(players, t, effPoison, 0, 5)
		}
	case itemPufferfish:
		h.applyEffect(players, t, effPoison, 1, 60)
		h.applyEffect(players, t, effHunger, 2, 15)
		h.applyEffect(players, t, effNausea, 0, 15)
	case itemHoneyBottle:
		h.removeEffect(t, effPoison)
	case itemChorusFruit:
		h.chorusTeleport(players, t)
	}
}

var (
	itemPoisonousPotato = int32(itemByName["poisonous_potato"])
	itemPufferfish      = int32(itemByName["pufferfish"])
	itemChorusFruit     = int32(itemByName["chorus_fruit"])
)

// cmdEffect is EffectCommands: /effect give <targets> <effect>
// [<seconds>|infinite] [<amplifier>] [<hideParticles>] and /effect clear
// [<targets>] [<effect>].
func (s *Server) cmdEffect(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to apply effects.")
		return
	}
	effect := func(arg string) (int32, bool) {
		id, ok := effectNames[strings.TrimPrefix(arg, "minecraft:")]
		if !ok {
			p.tell("Unknown effect: " + arg)
		}
		return id, ok
	}
	if len(args) >= 1 && args[0] == "clear" {
		ev := evEffect{target: "@s", by: p.eid, clear: true}
		if len(args) >= 2 {
			ev.target = args[1]
		}
		if len(args) >= 3 {
			id, ok := effect(args[2])
			if !ok {
				return
			}
			ev.id, ev.one = id, true
		}
		s.hub.post(ev)
		return
	}
	usage := "Usage: /effect give <targets> <effect> [<seconds>|infinite] [<amplifier>] [<hideParticles>]"
	if len(args) < 3 || args[0] != "give" {
		p.tell(usage)
		return
	}
	id, ok := effect(args[2])
	if !ok {
		return
	}
	ev := evEffect{target: args[1], by: p.eid, id: id, secs: -2} // -2: no seconds given
	if len(args) >= 4 {
		if args[3] == "infinite" {
			ev.secs = effInfinite
		} else if n, err := strconv.Atoi(args[3]); err == nil && n >= 1 && n <= 1000000 {
			ev.secs = n
		} else {
			p.tell("Seconds must be between 1 and 1000000, or infinite")
			return
		}
	}
	if len(args) >= 5 {
		n, err := strconv.Atoi(args[4])
		if err != nil || n < 0 || n > 255 {
			p.tell("Amplifier must be between 0 and 255")
			return
		}
		ev.amp = n
	}
	if len(args) >= 6 {
		switch args[5] {
		case "true":
			ev.hide = true
		case "false":
		default:
			p.tell(usage)
			return
		}
	}
	s.hub.post(ev)
}

type evEffect struct {
	target string
	by     int32
	clear  bool
	one    bool // clear: just this effect, not every one
	id     int32
	secs   int // seconds, effInfinite, or -2 for the default
	amp    int
	hide   bool // hideParticles
}

// effectTicks is EffectCommands.computeDurationInTicks: thirty seconds by
// default (an instant effect's single tick), seconds × 20 otherwise, an
// instant effect's "seconds" taken as ticks, and infinite as it is.
func effectTicks(id int32, secs int) int {
	instant := id == effInstantHealth || id == effInstantDamage
	switch {
	case secs == -2 && instant:
		return 1
	case secs == -2:
		return 600
	case instant || secs == effInfinite:
		return secs
	}
	return secs * 20
}

// effectCommand runs /effect on the hub, against players and — through an
// entity selector — mobs, answering as vanilla's CommandResponseTracker does.
func (h *hub) effectCommand(players map[int32]*tracked, e evEffect) {
	caller := players[e.by]
	tell := func(msg string) {
		if caller != nil {
			caller.p.trySendEv(chatEv(msg))
		}
	}
	targets := h.commandTargets(players, e.by, e.target)
	mobs := h.commandMobs(players, e.by, e.target)
	total := len(targets) + len(mobs)
	if total == 0 {
		tell("No entity was found")
		return
	}
	done, name := 0, ""
	for _, t := range targets {
		if h.effectCommandOn(players, e, t, nil) {
			done, name = done+1, t.p.name
		}
	}
	for _, m := range mobs {
		if h.effectCommandOn(players, e, nil, m) {
			done, name = done+1, mobDisplayName(m.etype)
		}
	}
	title := ""
	for k, id := range effectNames {
		if id == e.id {
			title = effectTitles[k]
		}
	}
	switch {
	case done == 0 && !e.clear:
		tell("Unable to apply this effect (target is either immune to effects, or has something stronger)")
	case done == 0 && e.one:
		tell("Target doesn't have the requested effect")
	case done == 0:
		tell("Target has no effects to remove")
	case !e.clear && total == 1:
		h.cmdOK(players, e.by)(fmt.Sprintf("Applied effect %s to %s", title, name))
	case !e.clear:
		h.cmdOK(players, e.by)(fmt.Sprintf("Applied effect %s to %d targets", title, done))
	case e.one && total == 1:
		h.cmdOK(players, e.by)(fmt.Sprintf("Removed effect %s from %s", title, name))
	case e.one:
		h.cmdOK(players, e.by)(fmt.Sprintf("Removed effect %s from %d targets", title, done))
	case total == 1:
		h.cmdOK(players, e.by)("Removed every effect from " + name)
	default:
		h.cmdOK(players, e.by)(fmt.Sprintf("Removed every effect from %d targets", done))
	}
}

// effectCommandOn applies one /effect to a player or a mob, reporting
// whether it did anything (addEffect / removeEffect / removeAllEffects).
func (h *hub) effectCommandOn(players map[int32]*tracked, e evEffect, t *tracked, m *mob) bool {
	var effects map[int32]*activeEffect
	if t != nil {
		effects = t.effects
	} else {
		effects = m.effects
	}
	switch {
	case e.clear && e.one:
		if effects[e.id] == nil {
			return false
		}
		if t != nil {
			h.removeEffect(t, e.id)
		} else {
			h.removeMobEffect(players, m, e.id)
		}
		return true
	case e.clear:
		if len(effects) == 0 {
			return false
		}
		for id := range effects {
			if t != nil {
				h.removeEffect(t, id)
			} else {
				h.removeMobEffect(players, m, id)
			}
		}
		return true
	}
	in := activeEffect{amp: e.amp, left: effectTicks(e.id, e.secs), noParticles: e.hide}
	if t != nil {
		return h.addEffect(players, t, e.id, in)
	}
	return h.addMobEffect(players, m, e.id, in)
}

func (evEffect) isHubEvent() {}

// hungerExhaustionPerTick is HungerMobEffect's exhaustion per tick per level.
const hungerExhaustionPerTick = 0.005 // HungerMobEffect.applyEffectTick

// resendEffects pushes a restored player's active effects to their client.
// The hub keeps effects live; without this a relog left them running on the
// server with no icons, no particles and no way to see them run out.
func (h *hub) resendEffects(t *tracked) {
	for id, e := range t.effects {
		t.p.trySendEv(effectEv(t.p.eid, id, e))
	}
	if len(t.effects) > 0 {
		t.p.trySendEv(metaEv(effectSwirlMeta(t.p.eid, t.effects))) // its own swirls
	}
}
