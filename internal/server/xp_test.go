package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestXPCurveMatchesVanilla(t *testing.T) {
	// Spot checks against the wiki's leveling table.
	for _, c := range [][2]int{{0, 7}, {15, 37}, {16, 42}, {30, 112}, {31, 121}, {40, 202}} {
		if got := xpToNext(c[0]); got != c[1] {
			t.Fatalf("xpToNext(%d) = %d, want %d", c[0], got, c[1])
		}
	}
	for _, c := range [][2]int{{16, 352}, {31, 1507}} {
		if got := totalXP(c[0], 0); got != c[1] {
			t.Fatalf("totalXP(%d) = %d, want %d", c[0], got, c[1])
		}
	}
}

func TestAddXPRollsLevels(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.addXP(pl, 7) // exactly level 0's cost
	if pl.xpLevel != 1 || pl.xpPoints != 0 {
		t.Fatalf("after 7 points: level=%d points=%d", pl.xpLevel, pl.xpPoints)
	}
	h.addXP(pl, 8) // level 1 needs 9
	if pl.xpLevel != 1 || pl.xpPoints != 8 {
		t.Fatalf("after +8: level=%d points=%d", pl.xpLevel, pl.xpPoints)
	}
	h.addXP(pl, 1)
	if pl.xpLevel != 2 || pl.xpPoints != 0 {
		t.Fatalf("after +1: level=%d points=%d", pl.xpLevel, pl.xpPoints)
	}
}

func TestOrbPickupAndDeathScatter(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	pl.y = float64(h.world.SurfaceFeet(0, 0))
	players := map[int32]*tracked{1: pl}

	// An award is paid in ladder denominations, so ten points is a 7 and a 3.
	h.spawnXPOrb(players, 10, pl.x, pl.y, pl.z)
	if len(h.orbs) != 2 {
		t.Fatalf("ten points should split into two orbs, got %d", len(h.orbs))
	}
	for i := 0; i < 20 && len(h.orbs) > 0; i++ {
		h.updateOrbs(players) // one orb every other tick (takeXpDelay)
	}
	if len(h.orbs) != 0 || pl.xpLevel != 1 || pl.xpPoints != 3 {
		t.Fatalf("orb pickup: orbs=%d level=%d points=%d", len(h.orbs), pl.xpLevel, pl.xpPoints)
	}

	// Death scatters 7×level (capped) at the spot and zeroes the bar.
	pl.xpLevel, pl.xpPoints = 10, 4
	h.damageOf(players, pl, 1000, dtGeneric)
	if !pl.dead || pl.xpLevel != 0 || pl.xpPoints != 0 {
		t.Fatalf("death must zero XP: dead=%v level=%d", pl.dead, pl.xpLevel)
	}
	total := 0
	for _, o := range h.orbs {
		total += o.value * o.count
	}
	if total != 70 {
		t.Fatalf("death must drop 7×level=70 across its orbs, got %d", total)
	}
	// A dead player can't hoover the orbs back up.
	was := len(h.orbs)
	h.updateOrbs(players)
	if len(h.orbs) != was {
		t.Fatal("a dead player must not pick up orbs")
	}
}

func TestMobXPOnlyForPlayerKills(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	burned := h.spawnHostile(players, entityZombie, 30, 30)
	h.despawnMob(players, burned) // died to daylight — nobody earned anything
	if len(h.orbs) != 0 {
		t.Fatal("environment deaths must not pay XP")
	}

	pl.x, pl.z = 40.5, 40.5
	fought := h.spawnHostile(players, entityZombie, 41, 40)
	fought.y = pl.y
	h.attackMob(players, 1, fought.eid)
	h.despawnMob(players, fought)
	// A zombie is worth 5, which the ladder pays as a 3 and two 1s.
	total := 0
	for _, o := range h.orbs {
		total += o.value * o.count
	}
	if total != 5 {
		t.Fatalf("a player-hit mob's death must drop its 5 XP, got %d", total)
	}
}

func TestOreXPGatedToSurvivalMiner(t *testing.T) {
	h := newHub(world.New(1))
	if xpForBlock(worldgen.CoalOre, func(int) int { return 1 }) != 1 {
		t.Fatal("coal ore must pay XP")
	}
	if xpForBlock(worldgen.DiamondOre, func(int) int { return 0 }) != 3 {
		t.Fatal("diamond ore must pay at least 3")
	}
	if xpForBlock(worldgen.IronOre, func(int) int { return 1 }) != 0 {
		t.Fatal("iron pays at the furnace, not the pick (vanilla)")
	}
	if xpForBlock(worldgen.Stone, func(int) int { return 1 }) != 0 {
		t.Fatal("plain stone pays nothing")
	}
	_ = h
}

// An award is paid out in the ladder denominations, largest first, the way
// ExperienceOrb.awardWithDirection walks getExperienceValue down.
func TestOrbAwardSplitsDownTheLadder(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.spawnXPOrb(players, 100, 0.5, float64(h.world.SurfaceFeet(0, 0)), 0.5)
	got, total := map[int]int{}, 0
	for _, o := range h.orbs {
		got[o.value] += o.count
		total += o.value * o.count
	}
	if total != 100 {
		t.Fatalf("the split must preserve the award, got %d of 100", total)
	}
	// 100 = 73 + 17 + 7 + 3.
	for _, want := range []int{73, 17, 7, 3} {
		if got[want] == 0 {
			t.Fatalf("no orb of value %d in %v", want, got)
		}
	}
	if got[1] != 0 {
		t.Fatalf("the ladder should not need any 1s for 100: %v", got)
	}
}

// Orbs of the same value lying in the same spot collapse into one entity that
// stands for several, so a mob farm does not fill the world with orbs.
func TestOrbsMergeInPlace(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	y := float64(h.world.SurfaceFeet(0, 0))
	// Two orbs of equal value in the same group merge; force the group by
	// giving the second one the id the first's modulus asks for.
	h.spawnXPOrb(players, 7, 0.5, y, 0.5)
	var first *xpOrb
	for _, o := range h.orbs {
		first = o
	}
	second := &xpOrb{eid: first.eid + orbMergeGroups, dim: first.dim,
		x: first.x, y: first.y, z: first.z, value: first.value, count: 1, born: h.tick.Load()}
	h.orbs[second.eid] = second
	h.scanForOrbMerges(players, first)
	if len(h.orbs) != 1 {
		t.Fatalf("the two orbs should have merged, %d left", len(h.orbs))
	}
	if first.count != 2 {
		t.Fatalf("the survivor should stand for two orbs, got %d", first.count)
	}
	// It pays out both, one touch at a time.
	pl := testTracked()
	pl.x, pl.y, pl.z = first.x, first.y, first.z
	players[1] = pl
	for i := 0; i < 10 && len(h.orbs) > 0; i++ {
		h.updateOrbs(players)
	}
	if got := totalXP(pl.xpLevel, pl.xpPoints); got != 14 {
		t.Fatalf("a merged pair of 7s is worth 14, got %d", got)
	}
}
