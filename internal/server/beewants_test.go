package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bee.wantsToEnterHive, condition by condition. "Has nectar" is only one of
// four reasons to go home, and there are five reasons not to.

// wantsBee is a plain forager: home known, empty-handed, calm, mid-day.
func wantsBee(t *testing.T, h *hub, players map[int32]*tracked, hive blockPos) *mob {
	t.Helper()
	m := h.spawnAnimal(players, entityBee, 0, 0)
	if m == nil {
		t.Fatal("no bee")
	}
	m.x, m.y, m.z = 0.5, 72, 0.5
	m.beeHome, m.beeHasHome = hive, true
	return m
}

func TestBeeGoesHomeForEachVanillaReason(t *testing.T) {
	cases := []struct {
		name    string
		day     bool
		raining bool
		setup   func(m *mob)
		want    bool
	}{
		{name: "idle in the sun stays out", day: true, want: false},
		{name: "carrying nectar", day: true, setup: func(m *mob) { m.beeNectar = true }, want: true},
		{name: "night", day: false, want: true},
		{name: "rain", day: true, raining: true, want: true},
		{
			name: "tired of looking for nectar", day: true,
			setup: func(m *mob) { m.beeNoNectar = beeTiredSecs + 1 }, want: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, players, hive := beeTravelWorld(t)
			m := wantsBee(t, h, players, hive)
			if c.setup != nil {
				c.setup(m)
			}
			if got := h.beeWantsHive(m, c.day, c.raining); got != c.want {
				t.Errorf("wantsToEnterHive = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBeeRefusesToGoHomeWhenBusyOrHurt(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *mob)
	}{
		{"mid-pollination", func(m *mob) { m.beePollinate = 5 }},
		{"dying of its sting", func(m *mob) { m.beeStingDie = 30 }},
		{"angry at someone", func(m *mob) { m.anger = 100 }},
		{"barred after a sedated robbery", func(m *mob) { m.beeStayOut = 10 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, players, hive := beeTravelWorld(t)
			m := wantsBee(t, h, players, hive)
			m.beeNectar = true // every reason to go home…
			c.setup(m)         // …and one reason it cannot
			if h.beeWantsHive(m, true, false) {
				t.Error("bee went home while it should not have")
			}
		})
	}
}

// isHiveNearFire: a burning hive is not a refuge. Fire, not a campfire — a
// campfire sedates a hive, which is a different rule entirely.
func TestBeeWillNotEnterABurningHive(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	if !h.beeWantsHive(m, true, false) {
		t.Fatal("a bee with nectar should want to go home")
	}
	h.world.SetBlock(hive.x+1, hive.y, hive.z, worldgen.BlockBase("fire"))
	if h.beeWantsHive(m, true, false) {
		t.Error("bee tried to enter a hive with fire beside it")
	}
}

// A campfire under the hive must NOT read as fire — that would stop bees
// entering their own hive on every honey farm ever built.
func TestCampfireUnderAHiveIsNotFire(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	m.beeNectar = true
	h.world.SetBlock(hive.x, hive.y-1, hive.z, worldgen.BlockBase("campfire"))
	if !h.beeWantsHive(m, true, false) {
		t.Error("a campfire under the hive stopped its bees going in")
	}
}

// The forage clock runs while empty-handed and is reset by nectar, so a bee
// that keeps finding flowers never times out.
func TestForageClockRunsAndResets(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)

	for i := 0; i < 5; i++ {
		h.updateBee(players, m, true, false)
	}
	if m.beeNoNectar < 5 {
		t.Errorf("forage clock at %d after 5 empty-handed seconds, want 5", m.beeNoNectar)
	}
	m.beeNectar = true
	h.updateBee(players, m, true, false)
	if m.beeNoNectar != 0 {
		t.Errorf("forage clock at %d after finding nectar, want it reset", m.beeNoNectar)
	}
}

// The whole point of the tired rule: a bee somewhere with no flowers at all
// still goes home eventually. Nothing else in the condition would ever fire.
func TestFlowerlessBeeEventuallyGivesUpAndGoesHome(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := wantsBee(t, h, players, hive)
	for i := 0; i < beeTiredSecs+2 && m.beeGoalKind != beeGoalKindHive; i++ {
		h.updateBee(players, m, true, false)
	}
	if m.beeGoalKind != beeGoalKindHive {
		t.Errorf("after %d seconds with no flowers the bee is still foraging (goal %d)",
			beeTiredSecs+2, m.beeGoalKind)
	}
}
