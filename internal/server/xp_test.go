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

	h.spawnXPOrb(players, 10, pl.x, pl.y, pl.z)
	if len(h.orbs) != 1 {
		t.Fatal("orb should exist")
	}
	h.updateOrbs(players)
	if len(h.orbs) != 0 || pl.xpLevel != 1 || pl.xpPoints != 3 {
		t.Fatalf("orb pickup: orbs=%d level=%d points=%d", len(h.orbs), pl.xpLevel, pl.xpPoints)
	}

	// Death scatters 7×level (capped) at the spot and zeroes the bar.
	pl.xpLevel, pl.xpPoints = 10, 4
	h.damage(players, pl, 1000)
	if !pl.dead || pl.xpLevel != 0 || pl.xpPoints != 0 {
		t.Fatalf("death must zero XP: dead=%v level=%d", pl.dead, pl.xpLevel)
	}
	if len(h.orbs) != 1 {
		t.Fatalf("death must drop one orb, got %d", len(h.orbs))
	}
	for _, o := range h.orbs {
		if o.value != 70 {
			t.Fatalf("death orb should hold 7×level=70, got %d", o.value)
		}
	}
	// A dead player can't hoover the orb back up.
	h.updateOrbs(players)
	if len(h.orbs) != 1 {
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
	if len(h.orbs) != 1 {
		t.Fatal("a player-hit mob's death must drop an XP orb")
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
