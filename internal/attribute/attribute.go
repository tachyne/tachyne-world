// Package attribute computes entity attribute values from a base and a set of
// modifiers, following vanilla's AttributeInstance.
//
// It is a leaf: pure arithmetic over the vocabulary in plugin/attribute, with
// no world, no hub and no goroutines. The hub owns the Map and mutates it on
// its own goroutine, so nothing here locks.
package attribute

import (
	api "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Instance is one attribute on one entity: a base value plus the modifiers
// currently applied to it.
//
// The computed value is cached and invalidated on change, because it is read
// far more often than it is written — movement and combat ask every tick.
type Instance struct {
	id    api.ID
	def   api.Def
	base  float64
	mods  []api.Modifier
	total float64
	dirty bool
}

// NewInstance starts an attribute at its registry default.
func NewInstance(id api.ID) *Instance {
	def, ok := api.Defs[id]
	if !ok {
		def = api.Def{Default: 0, Min: -1e9, Max: 1e9}
	}
	return &Instance{id: id, def: def, base: def.Default, dirty: true}
}

// ID reports which attribute this is.
func (in *Instance) ID() api.ID { return in.id }

// Base is the unmodified value.
func (in *Instance) Base() float64 { return in.base }

// SetBase replaces the unmodified value — a mob's own speed or health, before
// any equipment or effect has a say.
func (in *Instance) SetBase(v float64) {
	if in.base != v {
		in.base = v
		in.dirty = true
	}
}

// AddModifier applies a modifier, replacing any existing one from the same
// source. Re-applying the same source does not stack, which is what makes it
// safe to re-assert a piece of equipment's modifiers without tracking whether
// they were already there.
func (in *Instance) AddModifier(m api.Modifier) {
	for i := range in.mods {
		if in.mods[i].Source == m.Source {
			in.mods[i] = m
			in.dirty = true
			return
		}
	}
	in.mods = append(in.mods, m)
	in.dirty = true
}

// RemoveModifier drops the modifier from a source, if present.
func (in *Instance) RemoveModifier(source string) {
	for i := range in.mods {
		if in.mods[i].Source == source {
			in.mods = append(in.mods[:i], in.mods[i+1:]...)
			in.dirty = true
			return
		}
	}
}

// HasModifier reports whether a source currently contributes.
func (in *Instance) HasModifier(source string) bool {
	for _, m := range in.mods {
		if m.Source == source {
			return true
		}
	}
	return false
}

// Modifiers returns a copy of the applied modifiers.
func (in *Instance) Modifiers() []api.Modifier {
	out := make([]api.Modifier, len(in.mods))
	copy(out, in.mods)
	return out
}

// Value is the attribute after every modifier, clamped to the registry range.
//
// The order is fixed and matters: all AddValue first, then AddMultipliedBase
// against the POST-ADDITION base (so two of those stack additively rather than
// compounding), then AddMultipliedTotal against the running total (which do
// compound with one another).
func (in *Instance) Value() float64 {
	if !in.dirty {
		return in.total
	}
	base := in.base
	for _, m := range in.mods {
		if m.Op == api.AddValue {
			base += m.Amount
		}
	}
	total := base
	for _, m := range in.mods {
		if m.Op == api.AddMultipliedBase {
			total += base * m.Amount
		}
	}
	for _, m := range in.mods {
		if m.Op == api.AddMultipliedTotal {
			total *= 1 + m.Amount
		}
	}
	if total < in.def.Min {
		total = in.def.Min
	}
	if total > in.def.Max {
		total = in.def.Max
	}
	in.total, in.dirty = total, false
	return total
}

// Map is an entity's attributes. Instances are created on first use, so an
// entity only carries the attributes something has actually asked about.
type Map struct {
	m map[api.ID]*Instance
}

// NewMap returns an empty attribute map.
func NewMap() *Map { return &Map{m: map[api.ID]*Instance{}} }

// Get returns the instance for an attribute, creating it at its default.
func (a *Map) Get(id api.ID) *Instance {
	if a.m == nil {
		a.m = map[api.ID]*Instance{}
	}
	in, ok := a.m[id]
	if !ok {
		in = NewInstance(id)
		a.m[id] = in
	}
	return in
}

// Value is the computed value of an attribute, or its default if untouched.
func (a *Map) Value(id api.ID) float64 { return a.Get(id).Value() }

// SetBase sets an attribute's base value.
func (a *Map) SetBase(id api.ID, v float64) { a.Get(id).SetBase(v) }

// RemoveSource drops every modifier from one source across ALL attributes —
// what unequipping an item or expiring an effect needs, without the caller
// having to remember which attributes it touched.
func (a *Map) RemoveSource(source string) {
	for _, in := range a.m {
		in.RemoveModifier(source)
	}
}
