package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// LivingEntity.hurt's cooldown: for ten ticks after a landed blow only a
// bigger blow lands, and only its excess; after that a blow lands in full.
func TestDamageCooldownWindow(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.tick.Store(100)
	pl.health = 20
	if !h.hurtBy(players, pl, 4, dtMobAttack, deathCause{}) || pl.health != 16 {
		t.Fatalf("first blow lands in full: health %v", pl.health)
	}
	if h.hurtBy(players, pl, 3, dtMobAttack, deathCause{}) || pl.health != 16 {
		t.Fatalf("a smaller blow inside the window does nothing: health %v", pl.health)
	}
	if !h.hurtBy(players, pl, 6, dtMobAttack, deathCause{}) || pl.health != 14 {
		t.Fatalf("a bigger blow lands only its excess (6-4=2): health %v", pl.health)
	}
	h.tick.Store(111)
	if !h.hurtBy(players, pl, 4, dtMobAttack, deathCause{}) || pl.health != 10 {
		t.Fatalf("past the window a blow lands in full: health %v", pl.health)
	}

	m := &mob{etype: entityCow, health: 20}
	m.hurt(4)
	m.hurt(3)
	m.hurt(6)
	if m.health != 14 {
		t.Fatalf("a mob's window works the same: health %v", m.health)
	}
	m.invulnTicks = 0
	m.hurt(4)
	if m.health != 10 {
		t.Fatalf("a mob past its window takes the full blow: health %v", m.health)
	}
}

// Unbreaking on armour spares a piece 2·lvl/(5·lvl+5) of the time — about
// a fifth at level one — not the tool rule's half.
func TestArmourUnbreakingRate(t *testing.T) {
	h := newHub(world.New(1))
	spared := 0
	for i := 0; i < 20000; i++ {
		if h.armourUnbreakingSpares(1) {
			spared++
		}
	}
	if spared < 3600 || spared > 4400 {
		t.Fatalf("level-one armour should be spared ~20%% of the time, got %d/20000", spared)
	}
	if h.armourUnbreakingSpares(0) {
		t.Fatal("no enchantment, no sparing")
	}
}

// TestUnbreakingRollsPerPoint: remove_binomial removes each point of wear on
// its own (tools lvl/(lvl+1)), so over many two-point events the kept total
// averages 2/(lvl+1) a hit and single points are kept too — not all or none.
func TestUnbreakingRollsPerPoint(t *testing.T) {
	h := newHub(world.New(1))
	h.rng.Seed(7)
	ones := 0
	total := 0
	for i := 0; i < 20000; i++ {
		k := h.unbreakingKept(2, 3.0/4.0) // Unbreaking III on a tool
		if k == 1 {
			ones++
		}
		total += k
	}
	if ones == 0 {
		t.Fatal("a two-point hit is never half spared: the roll is per event, not per point")
	}
	if avg := float64(total) / 20000; avg < 0.45 || avg > 0.55 {
		t.Fatalf("kept %.3f points a hit, want about 0.5 (2 × 1/4)", avg)
	}
}
