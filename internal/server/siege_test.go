package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// siegeVillage builds a loaded stone pad in open air with villagers holding
// beds across it, so every section of the pad counts as a village.
func siegeVillage(t *testing.T) (*hub, map[int32]*tracked, *tracked, int) {
	t.Helper()
	h := newHub(world.New(1))
	const cx, cz, fy = 5000, 5000, 200
	h.world.ForceLoad(cx, cz, 5)
	for x := cx - 60; x <= cx+60; x++ {
		for z := cz - 60; z <= cz+60; z++ {
			h.world.SetBlock(x, fy-1, z, worldgen.Stone)
		}
	}
	players := map[int32]*tracked{}
	for x := cx - 48; x <= cx+48; x += 16 {
		for z := cz - 48; z <= cz+48; z += 16 {
			m := h.spawnMob(players, entityVillager, float64(x)+0.5, fy, float64(z)+0.5)
			m.bed, m.home = blockPos{x, fy, z}, blockPos{x, fy, z}
		}
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = cx+0.5, fy, cz+0.5
	players[1] = pl
	h.dayTime.Store(siegeRollAt + 1) // the dark of the night
	return h, players, pl, fy
}

func zombiesIn(h *hub) (out []*mob) {
	for _, m := range h.mobs {
		if m.etype == entityZombie {
			out = append(out, m)
		}
	}
	return
}

// Light clears the night's state, as isBrightOutside does.
func TestVillageSiegeClearsInTheLight(t *testing.T) {
	h := newHub(world.New(1))
	h.siegeState, h.siegeSetUp, h.siegeLeft = siegeTonight, true, 5
	h.dayTime.Store(6000) // noon
	h.updateVillageSiege(nil)
	if h.siegeState != siegeDone || h.siegeSetUp {
		t.Fatalf("daylight left the siege at state=%d setUp=%v", h.siegeState, h.siegeSetUp)
	}
}

// The night is rolled at the siege marker, midnight, and nowhere else.
func TestVillageSiegeRollsAtMidnight(t *testing.T) {
	h := newHub(world.New(1))
	for _, d := range []uint64{13000, 14000, 17999, 18001, 22000} {
		h.dayTime.Store(d)
		for i := 0; i < 200; i++ {
			h.updateVillageSiege(nil)
		}
		if h.siegeState != siegeDone {
			t.Fatalf("a siege was rolled at %d, not at the marker", d)
		}
	}
	rolled := 0
	for i := 0; i < 400; i++ {
		h.siegeState = siegeDone
		h.dayTime.Store(siegeRollAt)
		h.updateVillageSiege(nil)
		if h.siegeState == siegeTonight {
			rolled++
		}
	}
	if rolled < 15 || rolled > 80 {
		t.Fatalf("%d sieges in 400 rolls, want about one in ten", rolled)
	}
}

// A siege night with a player standing in the village releases up to twenty
// zombies, one every three ticks, around a point 32 blocks from the player,
// each inside the village.
func TestVillageSiegeReleasesZombies(t *testing.T) {
	h, players, pl, fy := siegeVillage(t)
	h.siegeState = siegeTonight
	h.updateVillageSiege(players)
	if !h.siegeSetUp {
		t.Fatal("a player standing in the village did not set the siege up")
	}
	c := h.siegeCenter
	if d := math.Hypot(float64(c.x)-pl.x, float64(c.z)-pl.z); d < 30 || d > 34 {
		t.Fatalf("the siege point is %.1f blocks from the player, want 32", d)
	}
	for i := 0; i < 5; i++ { // five ticks: at most two zombies, not ten
		h.updateVillageSiege(players)
	}
	if n := len(zombiesIn(h)); n > 2 {
		t.Fatalf("%d zombies after six ticks, want one every three", n)
	}
	for i := 0; i < 80; i++ {
		h.updateVillageSiege(players)
	}
	zs := zombiesIn(h)
	if len(zs) == 0 || len(zs) > siegeZombies {
		t.Fatalf("%d zombies, want some and at most %d", len(zs), siegeZombies)
	}
	for _, z := range zs {
		if math.Abs(z.x-float64(c.x)) > siegeSpread+1 || math.Abs(z.z-float64(c.z)) > siegeSpread+1 || floorInt(z.y) != fy {
			t.Fatalf("zombie at %.1f,%.1f,%.1f is off the siege point %v", z.x, z.y, z.z, c)
		}
	}
	if h.siegeState != siegeDone {
		t.Fatal("the siege should be done once all twenty have had their turn")
	}
}

// With nobody standing in a village the siege waits, and nothing spawns.
func TestVillageSiegeWaitsForAPlayerInAVillage(t *testing.T) {
	h, players, pl, _ := siegeVillage(t)
	pl.x, pl.z = pl.x+400, pl.z+400 // well outside it
	h.siegeState = siegeTonight
	for i := 0; i < 100; i++ {
		h.updateVillageSiege(players)
	}
	if h.siegeSetUp || len(zombiesIn(h)) != 0 {
		t.Fatalf("a siege set up with nobody in a village (setUp=%v, zombies=%d)", h.siegeSetUp, len(zombiesIn(h)))
	}
}
