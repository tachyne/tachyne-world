package attribute

import (
	"math"
	"testing"

	api "github.com/tachyne/tachyne-world/plugin/attribute"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// An untouched attribute reads its registry default.
func TestDefaults(t *testing.T) {
	m := NewMap()
	if got := m.Value(api.MaxHealth); !near(got, 20) {
		t.Errorf("max_health default %v, want 20", got)
	}
	if got := m.Value(api.AttackDamage); !near(got, 2) {
		t.Errorf("attack_damage default %v, want 2", got)
	}
	if got := m.Value(api.FollowRange); !near(got, 32) {
		t.Errorf("follow_range default %v, want 32", got)
	}
}

// The operation order is the part that is easy to get subtly wrong:
// AddMultipliedBase multiplies the base AFTER additions, not the running
// total, so two of them stack additively while AddMultipliedTotal compounds.
func TestModifierOrder(t *testing.T) {
	in := NewInstance(api.MaxHealth)
	in.SetBase(10)

	in.AddModifier(api.Modifier{Source: "a", Amount: 10, Op: api.AddValue})
	if got := in.Value(); !near(got, 20) {
		t.Fatalf("after +10: %v, want 20", got)
	}

	// base is now 20; +0.5 base = +10  ->  30
	in.AddModifier(api.Modifier{Source: "b", Amount: 0.5, Op: api.AddMultipliedBase})
	if got := in.Value(); !near(got, 30) {
		t.Fatalf("after x0.5 base: %v, want 30", got)
	}

	// A second AddMultipliedBase adds another 0.5 x 20 = 10, NOT half of 30.
	in.AddModifier(api.Modifier{Source: "c", Amount: 0.5, Op: api.AddMultipliedBase})
	if got := in.Value(); !near(got, 40) {
		t.Fatalf("two x0.5 base should stack additively: %v, want 40", got)
	}

	// AddMultipliedTotal compounds against everything so far: 40 x 1.5 = 60.
	in.AddModifier(api.Modifier{Source: "d", Amount: 0.5, Op: api.AddMultipliedTotal})
	if got := in.Value(); !near(got, 60) {
		t.Fatalf("after x1.5 total: %v, want 60", got)
	}

	// And two of those DO compound: 40 x 1.5 x 1.5 = 90.
	in.AddModifier(api.Modifier{Source: "e", Amount: 0.5, Op: api.AddMultipliedTotal})
	if got := in.Value(); !near(got, 90) {
		t.Fatalf("two x1.5 total should compound: %v, want 90", got)
	}
}

// The same source applied twice replaces rather than stacks, so re-asserting
// equipment modifiers is safe.
func TestSameSourceDoesNotStack(t *testing.T) {
	in := NewInstance(api.MaxHealth)
	in.SetBase(20)
	for i := 0; i < 5; i++ {
		in.AddModifier(api.Modifier{Source: "boots", Amount: 4, Op: api.AddValue})
	}
	if got := in.Value(); !near(got, 24) {
		t.Errorf("re-applied modifier stacked: %v, want 24", got)
	}
	if n := len(in.Modifiers()); n != 1 {
		t.Errorf("%d modifiers held, want 1", n)
	}
}

func TestRemoveModifier(t *testing.T) {
	in := NewInstance(api.MaxHealth)
	in.SetBase(20)
	in.AddModifier(api.Modifier{Source: "ring", Amount: 10, Op: api.AddValue})
	if !in.HasModifier("ring") {
		t.Fatal("modifier not registered")
	}
	in.RemoveModifier("ring")
	if in.HasModifier("ring") {
		t.Error("modifier still present after removal")
	}
	if got := in.Value(); !near(got, 20) {
		t.Errorf("after removal %v, want the base 20", got)
	}
}

// One source can touch several attributes; dropping it clears them all.
func TestRemoveSourceAcrossAttributes(t *testing.T) {
	m := NewMap()
	m.Get(api.MaxHealth).AddModifier(api.Modifier{Source: "armour", Amount: 4, Op: api.AddValue})
	m.Get(api.Armor).AddModifier(api.Modifier{Source: "armour", Amount: 8, Op: api.AddValue})
	m.Get(api.MovementSpeed).AddModifier(api.Modifier{Source: "potion", Amount: 0.2, Op: api.AddMultipliedTotal})

	m.RemoveSource("armour")
	if got := m.Value(api.MaxHealth); !near(got, 20) {
		t.Errorf("max_health %v after dropping the source, want the default 20", got)
	}
	if got := m.Value(api.Armor); !near(got, 0) {
		t.Errorf("armor %v after dropping the source, want 0", got)
	}
	// The unrelated source survives.
	if !m.Get(api.MovementSpeed).HasModifier("potion") {
		t.Error("removing one source dropped another")
	}
}

// Values clamp to the registry range rather than running away.
func TestValueClampsToRange(t *testing.T) {
	// armor is capped at 30.
	in := NewInstance(api.Armor)
	in.AddModifier(api.Modifier{Source: "huge", Amount: 1000, Op: api.AddValue})
	if got := in.Value(); !near(got, 30) {
		t.Errorf("armor %v, want the cap 30", got)
	}
	// max_health has a floor of 1.
	h := NewInstance(api.MaxHealth)
	h.AddModifier(api.Modifier{Source: "drain", Amount: -1000, Op: api.AddValue})
	def := api.Defs[api.MaxHealth]
	if got := h.Value(); !near(got, def.Min) {
		t.Errorf("max_health %v, want the floor %v", got, def.Min)
	}
}

// The cached value must follow every kind of mutation.
func TestCacheInvalidates(t *testing.T) {
	in := NewInstance(api.MaxHealth)
	in.SetBase(20)
	_ = in.Value()

	in.AddModifier(api.Modifier{Source: "x", Amount: 5, Op: api.AddValue})
	if got := in.Value(); !near(got, 25) {
		t.Errorf("stale after AddModifier: %v", got)
	}
	in.SetBase(30)
	if got := in.Value(); !near(got, 35) {
		t.Errorf("stale after SetBase: %v", got)
	}
	in.RemoveModifier("x")
	if got := in.Value(); !near(got, 30) {
		t.Errorf("stale after RemoveModifier: %v", got)
	}
}

// Every id constant in the public package must exist in the generated table,
// or a plugin could name an attribute the engine has no definition for.
func TestEveryPublicIDHasADefinition(t *testing.T) {
	ids := []api.ID{
		api.MaxHealth, api.MovementSpeed, api.AttackDamage, api.AttackSpeed,
		api.AttackKnockback, api.Armor, api.ArmorToughness, api.KnockbackResistance,
		api.FollowRange, api.JumpStrength, api.Luck, api.MaxAbsorption, api.Scale,
		api.StepHeight, api.Gravity, api.SafeFallDistance, api.FallDamageMultiplier,
		api.BlockBreakSpeed, api.BlockInteractionRange, api.EntityInteractionRange,
		api.SweepingDamageRatio, api.BurningTime, api.ExplosionKnockbackResistance,
		api.MiningEfficiency, api.MovementEfficiency, api.OxygenBonus,
		api.SneakingSpeed, api.SubmergedMiningSpeed, api.WaterMovementEfficiency,
		api.SpawnReinforcements, api.FlyingSpeed, api.TemptRange,
	}
	for _, id := range ids {
		if _, ok := api.Defs[id]; !ok {
			t.Errorf("public id %q has no definition in the generated table", id)
		}
	}
	if len(api.Defs) != 40 {
		t.Errorf("%d definitions, want the registry's 40", len(api.Defs))
	}
}
