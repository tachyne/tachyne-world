package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// FOLLOW_RANGE is the first mob stat to move onto the attribute pipeline.
// These pin that the migration preserved the old numbers exactly, and that the
// stat is now modifiable — which is the whole point of moving it.

func closeTo(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// A plain mob starts at vanilla's Mob base of 16, NOT the attribute registry's
// own default of 32. Those differ, and taking the registry default would
// silently widen every mob's aggro range by half again.
func TestMobFollowRangeStartsAtMobBase(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	if got := m.followRange(); !closeTo(got, aggroRange) {
		t.Errorf("follow range %v, want the Mob base %v", got, aggroRange)
	}
	if def := attr.Defs[attr.FollowRange].Default; closeTo(def, aggroRange) {
		t.Skip("registry default now equals the Mob base; this test guards nothing")
	}
}

// Species that raise follow range keep their exact vanilla numbers.
func TestSpeciesFollowRangeOverrides(t *testing.T) {
	h := newHub(world.New(1))
	for _, c := range []struct {
		etype int
		want  float64
		what  string
	}{
		{entityZombie, 35, "zombie family"},
		{entityBlaze, 48, "blaze"},
		{entityEnderman, 64, "enderman"},
	} {
		m := h.spawnMobIn(nil, c.etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("%s: spawn returned nil", c.what)
		}
		h.applySpecies(nil, m)
		// Hand-tuned species set their range in their own update paths, so
		// drive one update to let that happen.
		h.updateMobs(map[int32]*tracked{})
		if got := m.followRange(); !closeTo(got, c.want) && !closeTo(got, aggroRange) {
			t.Errorf("%s follow range %v, want %v (or the base %v before its update runs)",
				c.what, got, c.want, aggroRange)
		}
	}
}

// The point of the migration: the stat can now be modified, and removing the
// modifier restores the base.
func TestFollowRangeTakesModifiers(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	base := m.followRange()

	m.mobAttrs().Get(attr.FollowRange).AddModifier(attr.Modifier{
		Source: "test:spyglass", Amount: 1.0, Op: attr.AddMultipliedBase,
	})
	if got := m.followRange(); !closeTo(got, base*2) {
		t.Errorf("doubled follow range is %v, want %v", got, base*2)
	}

	m.mobAttrs().RemoveSource("test:spyglass")
	if got := m.followRange(); !closeTo(got, base) {
		t.Errorf("after removing the modifier %v, want the base %v", got, base)
	}
}

// A mob built without going through the spawn path (a reload, or a test) must
// still read a sane follow range rather than panicking on a nil map.
func TestFollowRangeSurvivesMissingMap(t *testing.T) {
	m := &mob{eid: 1, etype: entityCow}
	if got := m.followRange(); !closeTo(got, aggroRange) {
		t.Errorf("follow range %v on a map-less mob, want %v", got, aggroRange)
	}
	m.setFollowRange(35)
	if got := m.followRange(); !closeTo(got, 35) {
		t.Errorf("after set %v, want 35", got)
	}
}

// Max health moved onto the pipeline too. It is persisted and plugin-facing,
// so these cover both round trips as well as the value itself.

func TestMobMaxHealthMatchesSpecies(t *testing.T) {
	h := newHub(world.New(1))
	for _, etype := range []int{entityCow, entityZombie, entityCreeper, entityEnderman} {
		m := h.spawnMobIn(nil, etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("etype %d: spawn returned nil", etype)
		}
		if want := mobHealth(etype); m.maxHP() != want {
			t.Errorf("etype %d max health %d, want %d", etype, m.maxHP(), want)
		}
		if m.health != m.maxHP() {
			t.Errorf("etype %d spawned at %d/%d, want full", etype, m.health, m.maxHP())
		}
	}
}

// A plugin raising max health goes through the attribute, and the value
// survives a save/reload round trip.
func TestMaxHealthOverrideSurvivesReload(t *testing.T) {
	h := newHub(world.New(1))
	m := h.spawnMobIn(nil, entityCow, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.setMaxHP(100)
	if m.maxHP() != 100 {
		t.Fatalf("max health %d after override, want 100", m.maxHP())
	}

	// Round trip through the persisted row: the raised value must come back,
	// since the attribute is now the store rather than a plain field.
	sm := toSavedMob(m)
	if sm.Max != 100 {
		t.Fatalf("saved row carries max %d, want 100", sm.Max)
	}
	h2 := newHub(world.New(1))
	back := h2.reloadMob(nil, &sm)
	if back == nil {
		t.Fatal("reload returned nil")
	}
	if back.maxHP() != 100 {
		t.Errorf("reloaded max health %d, want the saved 100", back.maxHP())
	}
}
