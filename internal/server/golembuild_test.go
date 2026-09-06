package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func golemHub(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := riderAt(1, 12.5, 70, 10.5)
	pl.adv = advState{}
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	// Clear a working volume so natural terrain cannot fake or break a pattern.
	for dx := -3; dx <= 3; dx++ {
		for dy := -1; dy <= 4; dy++ {
			for dz := -3; dz <= 3; dz++ {
				h.world.SetBlock(10+dx, 70+dy, 10+dz, worldgen.Air)
			}
		}
	}
	return h, pl, players
}

func countSpecies(h *hub, etype int) int {
	n := 0
	for _, m := range h.mobs {
		if m.etype == etype {
			n++
		}
	}
	return n
}

// Pumpkin on two snow blocks: a snow golem, the three blocks gone, Hired
// Help NOT granted (that is the iron golem's).
func TestSnowGolemBuild(t *testing.T) {
	h, pl, players := golemHub(t)
	h.world.SetBlock(10, 70, 10, snowBlockState)
	h.world.SetBlock(10, 71, 10, snowBlockState)
	h.world.SetBlock(10, 72, 10, carvedPumpkinBase)

	if !h.checkGolemBuild(players, 0, 10, 72, 10, carvedPumpkinBase) {
		t.Fatal("a pumpkin on two snow blocks should build a snow golem")
	}
	if countSpecies(h, entitySnowGolem) != 1 {
		t.Fatalf("%d snow golems, want 1", countSpecies(h, entitySnowGolem))
	}
	for _, y := range []int{70, 71, 72} {
		if h.world.At(10, y, 10) != worldgen.Air {
			t.Errorf("pattern block at y=%d survived", y)
		}
	}
	if pl.adv.done(advByID["minecraft:adventure/summon_iron_golem"]) {
		t.Error("a snow golem must not grant summon_iron_golem")
	}
}

// The iron T with air at its shoulders and feet, arms along z: an iron golem
// standing on the foot block, and Hired Help for the nearby player.
func TestIronGolemBuild(t *testing.T) {
	h, pl, players := golemHub(t)
	h.world.SetBlock(10, 70, 10, ironBlockState)
	h.world.SetBlock(10, 71, 10, ironBlockState)
	h.world.SetBlock(10, 71, 9, ironBlockState)
	h.world.SetBlock(10, 71, 11, ironBlockState)
	h.world.SetBlock(10, 72, 10, jackLanternBase) // a jack o'lantern works too

	if !h.checkGolemBuild(players, 0, 10, 72, 10, jackLanternBase) {
		t.Fatal("the iron T under a pumpkin should build an iron golem")
	}
	if countSpecies(h, entityIronGolem) != 1 {
		t.Fatalf("%d iron golems, want 1", countSpecies(h, entityIronGolem))
	}
	var g *mob
	for _, m := range h.mobs {
		if m.etype == entityIronGolem {
			g = m
		}
	}
	if g.x != 10.5 || g.z != 10.5 || g.y < 70 || g.y > 70.1 {
		t.Errorf("golem at (%.2f,%.2f,%.2f), want on the foot block (10.5, 70.05, 10.5)", g.x, g.y, g.z)
	}
	if g.health != 100 {
		t.Errorf("golem health %v, want 100", g.health)
	}
	if !pl.adv.done(advByID["minecraft:adventure/summon_iron_golem"]) {
		t.Error("summon_iron_golem should be granted to the builder standing beside it")
	}
}

// A block where the pattern demands air is not a golem — the shoulders and
// the space beside the foot must be empty.
func TestIronGolemNeedsAirAtShouldersAndFeet(t *testing.T) {
	h, _, players := golemHub(t)
	h.world.SetBlock(10, 70, 10, ironBlockState)
	h.world.SetBlock(10, 71, 10, ironBlockState)
	h.world.SetBlock(9, 71, 10, ironBlockState)
	h.world.SetBlock(11, 71, 10, ironBlockState)
	h.world.SetBlock(11, 72, 10, worldgen.Stone) // a shoulder blocked
	h.world.SetBlock(10, 72, 10, carvedPumpkinBase)
	if h.checkGolemBuild(players, 0, 10, 72, 10, carvedPumpkinBase) {
		t.Fatal("a blocked shoulder must not build")
	}
	h.world.SetBlock(11, 72, 10, worldgen.Air)
	h.world.SetBlock(9, 70, 10, worldgen.Stone) // beside the foot
	if h.checkGolemBuild(players, 0, 10, 72, 10, carvedPumpkinBase) {
		t.Fatal("a block beside the foot must not build")
	}
	h.world.SetBlock(9, 70, 10, worldgen.Air)
	if !h.checkGolemBuild(players, 0, 10, 72, 10, carvedPumpkinBase) {
		t.Fatal("with the air cells clear the golem builds")
	}
}
