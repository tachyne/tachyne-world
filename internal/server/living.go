package server

import (
	"github.com/tachyne/tachyne-world/internal/attribute"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// living is the state vanilla puts on LivingEntity and every kind of entity
// therefore shares: the attribute map and the running status effects.
//
// It is EMBEDDED in both tracked and mob, so `t.attrs` and `m.effects` keep
// reading the way they always did while the behaviour lives in one place. That
// matters more than it sounds: effects were player-only, which meant nothing
// could poison a mob and half of vanilla's potion surface had nowhere to land.
type living struct {
	attrs   *attribute.Map          // entity attributes (base + modifiers)
	effects map[int32]*activeEffect // active status effects, ticked by the hub
}

// hasEffect returns the 1-based level of an active effect (0 = none).
func (l *living) hasEffect(id int32) int {
	if e, ok := l.effects[id]; ok {
		return e.amp + 1
	}
	return 0
}

// effectLeft is the ticks remaining on an effect (0 = not running).
func (l *living) effectLeft(id int32) int {
	if e, ok := l.effects[id]; ok {
		return e.left
	}
	return 0
}

// startEffect records an effect, refusing to weaken one already running —
// vanilla's MobEffectInstance.update keeps the stronger, then longer, instance.
// Reports whether it took.
func (l *living) startEffect(id int32, amp, secs int) bool {
	if l.effects == nil {
		l.effects = map[int32]*activeEffect{}
	}
	if cur, ok := l.effects[id]; ok && (cur.amp > amp || (cur.amp == amp && cur.left > secs*20)) {
		return false
	}
	l.effects[id] = &activeEffect{amp: amp, left: secs * 20}
	return true
}

// installEffectModifiers applies an effect's attribute modifiers at its level,
// replacing any the same effect already had (vanilla removes then re-adds).
//
// This is where migrating the mob stats onto the pipeline pays off: Strength,
// Speed and Health Boost on a MOB need no per-effect code at all, because the
// mob's damage, pace and health ceiling already read their attributes.
func (l *living) installEffectModifiers(id int32, amp int) {
	mods := effectModifiers[id]
	if len(mods) == 0 {
		return
	}
	src := effectSource(id)
	for _, m := range mods {
		l.attrMap().Get(m.id).AddModifier(attr.Modifier{
			Source: src, Amount: m.amount * float64(amp+1), Op: m.op,
		})
	}
}

// dropEffectModifiers removes everything one effect contributed.
func (l *living) dropEffectModifiers(id int32) { l.attrMap().RemoveSource(effectSource(id)) }

// attrMap is the attribute map, created empty if the entity has none. The
// typed accessors (playerAttrs, mobAttrs) seed species/player baselines on top;
// this is the raw fallback the shared effect code uses.
func (l *living) attrMap() *attribute.Map {
	if l.attrs == nil {
		l.attrs = attribute.NewMap()
	}
	return l.attrs
}

// ignoresPoisonAndRegen is #minecraft:ignores_poison_and_regen — the undead,
// which poison and regeneration do nothing to (and which instant damage heals).
func ignoresPoisonAndRegen(etype int) bool { return undeadTypes[etype] }
