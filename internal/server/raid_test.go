package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestRaidWavesAndVictory(t *testing.T) {
	skipHeavy(t)
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	pl := testTracked()
	lx, lz := h.findLand(120, 120) // a spawnable spot so raiders can appear
	center := blockPos{lx, h.world.SurfaceFeet(lx, lz), lz}
	pl.x, pl.y, pl.z = float64(center.x), float64(center.y), float64(center.z)
	players[pl.p.eid] = pl
	h.rules.Difficulty = diffNormal

	h.startRaid(players, center)
	r := h.raids[center]
	if r == nil || r.numGroups != 5 {
		t.Fatalf("normal raid should have 5 waves, got %+v", r)
	}
	if len(r.alive) == 0 {
		t.Fatal("wave 1 should have spawned raiders")
	}
	// Clear every wave by killing all its raiders; the raid should end in victory.
	for i := 0; i < 20 && h.raids[center] != nil; i++ {
		for eid := range h.raids[center].alive {
			delete(h.mobs, eid) // simulate the raiders dying
		}
		h.updateRaids(players)
	}
	if h.raids[center] != nil {
		t.Fatalf("raid should have ended after clearing all %d waves", r.numGroups)
	}
}

func TestBadOmenTriggersRaidNearVillage(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	// Put the player ON a real generated village so villageNear resolves.
	gen := h.world.Gen()
	var v = gen.VillageIn(0, 0)
	if !v.Exists {
		t.Skip("no village near origin in this seed")
	}
	pl.x, pl.y, pl.z = float64(v.X), float64(v.Y), float64(v.Z)
	h.applyEffect(players, pl, effBadOmen, 0, 6000)
	h.checkRaidTrigger(players, pl)
	if pl.hasEffect(effBadOmen) != 0 {
		t.Fatal("reaching a village should consume Bad Omen")
	}
	if len(h.raids) == 0 {
		t.Fatal("Bad Omen at a village should start a raid")
	}
}

// Raid.getEnchantOdds and the two applyRaidBuffs overrides: from Raid Omen
// level 2 a raider may come out of the spawn armed better than the last wave's,
// and the wave thresholds are vanilla's NORMAL and EASY group counts whatever
// difficulty the raid runs at.
func TestRaidBuffsArmTheLaterWaves(t *testing.T) {
	for _, tc := range []struct {
		omen int
		want float64
	}{{1, 0}, {2, 0.10}, {3, 0.25}, {4, 0.50}, {5, 0.75}} {
		if got := raidEnchantOdds(tc.omen); got != tc.want {
			t.Errorf("omen %d gives odds %v, want %v", tc.omen, got, tc.want)
		}
	}

	h := newHub(world.New(59))
	players := map[int32]*tracked{}
	// Omen 5 makes the roll near-certain enough to see both outcomes over
	// many spawns; the wave decides the level.
	for _, tc := range []struct {
		etype  int
		wave   int
		id     int8
		wantLo int8
	}{
		{entityPillager, 7, enchQuickCharge, 2},
		{entityPillager, 4, enchQuickCharge, 1},
		{entityVindicator, 7, enchSharpness, 2},
		{entityVindicator, 1, enchSharpness, 1},
	} {
		r := &raid{omenLevel: 5, wave: tc.wave, numGroups: raidWaveCount(diffNormal)}
		seen := int8(0)
		for i := 0; i < 200 && seen == 0; i++ {
			m := h.spawnMob(players, tc.etype, float64(i), 70, 0)
			h.applyRaidBuffs(m, r)
			if lvl := m.heldStack().enchLvl(tc.id); lvl > 0 {
				seen = int8(lvl)
			}
		}
		if seen != tc.wantLo {
			t.Errorf("%s on wave %d got level %d, want %d",
				advEntityName[tc.etype], tc.wave, seen, tc.wantLo)
		}
	}

	// A pillager on the first waves gets nothing at all, however good the omen.
	r := &raid{omenLevel: 5, wave: 2, numGroups: raidWaveCount(diffNormal)}
	for i := 0; i < 100; i++ {
		m := h.spawnMob(players, entityPillager, float64(100+i), 70, 0)
		h.applyRaidBuffs(m, r)
		if m.heldStack().enchLvl(enchQuickCharge) > 0 {
			t.Fatal("an early-wave pillager's crossbow is plain")
		}
	}

	// Quick Charge shortens the draw, which is what makes the later waves bite.
	plain := h.spawnMob(players, entityPillager, 300, 70, 0)
	if got := mobCrossbowCharge(plain); got != crossbowChargeTicks {
		t.Errorf("a plain crossbow charges in %d ticks, want %d", got, crossbowChargeTicks)
	}
	plain.heldEnch = enchApplyList([]enchInstance{{id: enchQuickCharge, lvl: 2}})
	if got := mobCrossbowCharge(plain); got != crossbowChargeTicks-10 {
		t.Errorf("Quick Charge II charges in %d ticks, want %d", got, crossbowChargeTicks-10)
	}
}
