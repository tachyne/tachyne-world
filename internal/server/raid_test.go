package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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
	raidVillage(h, center)

	h.startRaid(players, center)
	r := h.raids[center]
	if r == nil || r.numGroups != 5 {
		t.Fatalf("normal raid should have 5 waves, got %+v", r)
	}
	// Raid(): the first wave waits out the 300-tick cooldown, the bar
	// filling meanwhile.
	if len(r.alive) != 0 {
		t.Fatal("wave 1 came before the cooldown")
	}
	for i := 0; i < raidCooldownSecs-1; i++ {
		h.updateRaids(players)
	}
	if p := h.raidProgress(r, 0); len(r.alive) != 0 || p < 0.9 {
		t.Fatalf("near the end of the cooldown: %d raiders, bar %.2f", len(r.alive), p)
	}
	h.updateRaids(players)
	if len(r.alive) == 0 {
		t.Fatal("wave 1 should have spawned raiders")
	}
	// The bar is the raiders' health over the wave's.
	if p := h.raidProgress(r, len(r.alive)); p != 1 {
		t.Fatalf("a fresh wave's bar reads %.2f, want 1", p)
	}
	for eid := range r.alive {
		h.mobs[eid].health /= 2
	}
	if p := h.raidProgress(r, len(r.alive)); p < 0.4 || p > 0.6 {
		t.Fatalf("half-health raiders read %.2f", p)
	}
	// Clear every wave by killing all its raiders; the raid ends in victory:
	// the bar says so for the celebration, then the raid is gone.
	sawVictory := false
	for i := 0; i < 400 && h.raids[center] != nil; i++ {
		for eid := range h.raids[center].alive {
			delete(h.mobs, eid) // simulate the raiders dying
		}
		h.updateRaids(players)
		if rr := h.raids[center]; rr != nil && rr.title == "Raid - Victory" {
			sawVictory = true
		}
	}
	if h.raids[center] != nil || !sawVictory {
		t.Fatalf("raid over %v, victory shown %v", h.raids[center] == nil, sawVictory)
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
	// A spectator carries the omen through a village untouched
	// (BadOmenMobEffect.applyEffectTick: !isSpectator).
	pl.gamemode = gmSpectator
	h.checkRaidTrigger(players, pl)
	if pl.hasEffect(effBadOmen) == 0 || len(h.raids) != 0 {
		t.Fatal("a spectator's Bad Omen started a raid")
	}
	pl.gamemode = gmSurvival
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

// raidVillage gives a test raid its village: a bell (a meeting point of
// interest) over the centre, so ServerLevel.isVillage holds.
func raidVillage(h *hub, center blockPos) {
	h.world.ForceLoad(center.x, center.z, 2)
	h.poiWorld(dimOverworld)
	h.world.SetBlock(center.x, center.y+3, center.z, worldgen.BlockBase("bell"))
}

// A raid whose village is gone is lost once a wave has come: the bar reads
// "Raid - Defeat" for thirty seconds and the raid ends.
func TestRaidLostWhenTheVillageIsGone(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	center := blockPos{64, 100, 64}
	h.world.ForceLoad(center.x, center.z, 2)
	h.poiWorld(dimOverworld)
	h.raids[center] = &raid{center: center, uuid: raidUUID(center), wave: 1, numGroups: 5,
		alive: map[int32]bool{}, shown: map[int32]bool{}}
	h.updateRaids(players)
	r := h.raids[center]
	if r == nil || r.lostLeft != raidDefeatSecs || r.title != "Raid - Defeat" {
		t.Fatalf("a raid with no village: %+v, want it lost", r)
	}
	for i := 0; i < raidDefeatSecs; i++ {
		h.updateRaids(players)
	}
	if h.raids[center] != nil {
		t.Fatal("the lost raid never ended")
	}
}

// With its village standing the raid goes on, and a raid never outlasts
// 48000 ticks.
func TestRaidKeepsItsVillageAndTimesOut(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	center := blockPos{64, 100, 64}
	raidVillage(h, center)
	h.raids[center] = &raid{center: center, uuid: raidUUID(center), wave: 1, numGroups: 5,
		alive: map[int32]bool{}, shown: map[int32]bool{}, pending: 1, secsActive: raidTimeoutSecs - 2}
	h.updateRaids(players)
	if r := h.raids[center]; r == nil || r.lostLeft != 0 {
		t.Fatal("a raid with its village was lost")
	}
	h.updateRaids(players)
	if h.raids[center] != nil {
		t.Fatal("the raid outlasted 48000 ticks")
	}
}

// A raid whose centre stops being a village moves to the nearest village
// section around it instead of being lost (moveRaidCenterToNearbyVillageSection).
func TestRaidRecentresOnNearbyVillage(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	center := blockPos{64, 100, 64}
	near := blockPos{center.x + 32, center.y, center.z} // two sections east
	h.world.ForceLoad(near.x, near.z, 3)
	h.poiWorld(dimOverworld)
	h.world.SetBlock(near.x, near.y, near.z, worldgen.BlockBase("bell"))
	h.raids[center] = &raid{center: center, uuid: raidUUID(center), wave: 1, numGroups: 5,
		alive: map[int32]bool{}, shown: map[int32]bool{}, pending: 1}
	h.updateRaids(players)
	if h.raids[center] != nil {
		t.Fatal("the raid stayed on its old centre")
	}
	var r *raid
	for c, rr := range h.raids {
		if h.isVillage(c) && rr.center == c {
			r = rr
		}
	}
	if len(h.raids) != 1 || r == nil || r.lostLeft != 0 {
		t.Fatalf("the raid did not move to the village section beside it (raids %v)", h.raids)
	}
}
