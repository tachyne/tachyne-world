package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// countEffectPeriod ticks updateEffects for `n` ticks starting from a fresh
// effect of `secs` seconds and returns how many ticks passed between the first
// two applications — the effect's real cadence.
func effectInterval(t *testing.T, id int32, amp, secs, ticks int) int {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	pl.health = 15 // below max so regen heals; above the poison floor so it bites
	players := map[int32]*tracked{pl.p.eid: pl}
	h.applyEffect(players, pl, id, amp, secs)

	prev := pl.health
	first, second := -1, -1
	for i := 0; i < ticks; i++ {
		h.updateEffects(players)
		if pl.health != prev {
			if first < 0 {
				first = i
			} else if second < 0 {
				second = i
				break
			}
			prev = pl.health
		}
	}
	if first < 0 || second < 0 {
		t.Fatalf("effect %d amp %d never applied twice in %d ticks (first=%d second=%d)", id, amp, ticks, first, second)
	}
	return second - first
}

// TestEffectCadenceMatchesVanilla pins the per-effect application intervals to
// vanilla's MobEffect.shouldApplyEffectTickThisTick (base>>amp ticks).
func TestEffectCadenceMatchesVanilla(t *testing.T) {
	cases := []struct {
		name      string
		id        int32
		amp, want int
	}{
		{"regen I", effRegen, 0, 50},
		{"regen II", effRegen, 1, 25},
		{"poison I", effPoison, 0, 25},
		{"poison II", effPoison, 1, 12},
		{"wither I", effWither, 0, 40},
		{"wither II", effWither, 1, 20},
	}
	for _, c := range cases {
		// Poison caps at half a heart and wither can kill, so start full and give
		// plenty of headroom / duration.
		if got := effectInterval(t, c.id, c.amp, 300, 400); got != c.want {
			t.Errorf("%s: interval %d ticks, want %d", c.name, got, c.want)
		}
	}
}

// TestPoisonNeverKills — vanilla poison stops at half a heart.
func TestPoisonNeverKills(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.health = 2
	players := map[int32]*tracked{pl.p.eid: pl}
	h.applyEffect(players, pl, effPoison, 0, 60)
	for i := 0; i < 1200; i++ {
		h.updateEffects(players)
	}
	if pl.dead || pl.health < 1 {
		t.Fatalf("poison must not drop below 1 HP: health=%v dead=%v", pl.health, pl.dead)
	}
}

// TestWitherCanKill — unlike poison, wither is lethal.
func TestWitherCanKill(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.health = 2
	players := map[int32]*tracked{pl.p.eid: pl}
	h.applyEffect(players, pl, effWither, 0, 60)
	for i := 0; i < 1200 && !pl.dead; i++ {
		h.updateEffects(players)
	}
	if !pl.dead {
		t.Fatalf("wither should be able to kill: health=%v", pl.health)
	}
}

// TestDamageExhaustionPerType — environmental damage types with vanilla
// exhaustion 0.0 must not drain hunger; attacks/contact charge 0.1.
func TestDamageExhaustionPerType(t *testing.T) {
	h := newHub(world.New(1))

	// Zero-exhaustion sources.
	for _, amt := range []float32{2, 4} {
		pl := testTracked()
		pl.health = maxHealth
		h.damageExh(nil, pl, amt, 0)
		if pl.exhaustion != 0 {
			t.Fatalf("zero-exhaustion damage drained hunger: exhaustion=%v", pl.exhaustion)
		}
	}
	// Default attack/contact source: 0.1 per hit.
	pl := testTracked()
	pl.health = maxHealth
	h.damage(nil, pl, 2)
	if pl.exhaustion < 0.099 || pl.exhaustion > 0.101 {
		t.Fatalf("attack damage should cost 0.1 exhaustion: %v", pl.exhaustion)
	}
}

// TestSlowRegenGatedByGamerule — with naturalRegeneration off the slow (fed)
// regen branch must not heal.
func TestSlowRegenGatedByGamerule(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.NaturalRegen = false
	pl := testTracked()
	pl.health, pl.food, pl.saturation, pl.exhaustion = 10, 18, 0, 0
	players := map[int32]*tracked{pl.p.eid: pl}
	// Advance to a slow-regen boundary tick and step it.
	for h.tick.Load()%regenPeriod != 0 {
		h.tick.Add(1)
	}
	h.survivalTick(players)
	if pl.health != 10 {
		t.Fatalf("naturalRegeneration=false must block slow regen: health=%v", pl.health)
	}
	// …and with the gamerule on it heals.
	h.rules.NaturalRegen = true
	h.survivalTick(players)
	if pl.health <= 10 {
		t.Fatalf("naturalRegeneration=true should slow-regen: health=%v", pl.health)
	}
}

// TestFoodEatTicks pins the per-item consume time (vanilla Consumable
// consume_seconds × 20): default 32, dried kelp 16, honey bottle 40.
func TestFoodEatTicks(t *testing.T) {
	if got := foodEatTicks(itemByName["cooked_beef"]); got != 32 {
		t.Errorf("default food eat time %d, want 32", got)
	}
	if got := foodEatTicks(itemDriedKelp); got != 16 {
		t.Errorf("dried kelp eat time %d, want 16", got)
	}
	if got := foodEatTicks(itemHoneyBottle); got != 40 {
		t.Errorf("honey bottle eat time %d, want 40", got)
	}
}

// TestOreMiningXP pins the mining-XP ranges (vanilla Blocks.java UniformInt) for
// every ore that awards experience, including the newly-covered ones.
func TestOreMiningXP(t *testing.T) {
	// A deterministic rng that returns its argument-1 (the max of the range) so
	// we can read the upper bound, then 0 for the lower.
	max := func(n int) int { return n - 1 }
	min := func(int) int { return 0 }

	cases := []struct {
		name   string
		state  uint32
		lo, hi int
	}{
		{"coal_ore", worldgen.CoalOre, 0, 2},
		{"diamond_ore", worldgen.DiamondOre, 3, 7},
		{"emerald_ore", worldgen.EmeraldOre, 3, 7},
		{"deepslate_emerald_ore", worldgen.DeepslateEmeraldOre, 3, 7},
		{"lapis_ore", worldgen.LapisOre, 2, 5},
		{"nether_quartz_ore", worldgen.NetherQuartzOre, 2, 5},
		{"redstone_ore_unlit", worldgen.RedstoneOre, 1, 5},
		{"redstone_ore_lit", worldgen.RedstoneOre - 1, 1, 5},
		{"deepslate_redstone_ore", worldgen.DeepslateRedstoneOre, 1, 5},
		{"nether_gold_ore", worldgen.NetherGoldOre, 0, 1},
	}
	for _, c := range cases {
		if got := xpForBlock(c.state, min); got != c.lo {
			t.Errorf("%s: min XP %d, want %d", c.name, got, c.lo)
		}
		if got := xpForBlock(c.state, max); got != c.hi {
			t.Errorf("%s: max XP %d, want %d", c.name, got, c.hi)
		}
	}
	// Plain stone yields nothing.
	if got := xpForBlock(worldgen.Stone, min); got != 0 {
		t.Errorf("stone XP %d, want 0", got)
	}
}
