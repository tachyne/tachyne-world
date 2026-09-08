package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A fox stalks a chicken, bites it beside it, picks up a dropped cake and
// eats it after half a minute, and sleeps through a quiet sheltered day.
func TestFoxHuntsEatsAndSleeps(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.z = 100, 100 // well away: nothing to alert the fox
	h.tick.Store(20)
	h.dayTime.Store(15000) // night: no sleeping while it hunts and eats
	fox := h.spawnSpecies(players, entityFox, 0, 0.5, 70, 0.5)
	if fox == nil {
		t.Fatal("no fox")
	}
	w := h.worldFor(0)
	y := float64(w.SurfaceFeet(0, 0)) - 3 // under the surface: sheltered from the sky
	fox.x, fox.y, fox.z = 0.5, y, 0.5
	hen := h.spawnSpecies(players, entityChicken, 0, 8.5, y, 0.5)
	hen.x, hen.y, hen.z = 8.5, y, 0.5
	if !h.foxStep(players, fox) || fox.vx <= 0 || fox.foxFlags&foxFlagCrouching == 0 {
		t.Fatalf("the fox should creep toward the hen: vx=%.3f flags=%#x", fox.vx, fox.foxFlags)
	}
	hen.x = 1.5
	hp := hen.health
	if !h.foxStep(players, fox) || hen.health >= hp {
		t.Fatalf("beside the hen it bites: health %d→%d", hp, hen.health)
	}
	h.despawnMob(players, hen)
	// A loaf on the ground beside it goes into its mouth, and later down.
	cake := int32(itemByName["bread"])
	it := h.spawnItem(players, cake, 1, 1.0, y, 0.5)
	it.y, it.noPickupUntil = y, 0
	if !h.foxStep(players, fox) || fox.held != cake || h.items[it.eid] != nil {
		t.Fatalf("the fox should pick the cake up: held=%d", fox.held)
	}
	for i := 0; i < foxEatAfter/mobMoveInterval+2; i++ {
		h.foxStep(players, fox)
	}
	if fox.held != 0 {
		t.Fatal("after 600 ticks the cake is eaten")
	}
	// A sheltered quiet day: after the wait it sleeps; a player nearby wakes it.
	h.dayTime.Store(1000)
	fox.foxSleepIn = 0
	if !h.foxStep(players, fox) || fox.foxFlags&foxFlagSleeping == 0 {
		t.Fatalf("with shelter and quiet it sleeps: flags=%#x", fox.foxFlags)
	}
	pl.x, pl.z = 3, 3
	h.foxStep(players, fox)
	if fox.foxFlags&foxFlagSleeping != 0 {
		t.Fatal("a player walking up wakes it")
	}
}
