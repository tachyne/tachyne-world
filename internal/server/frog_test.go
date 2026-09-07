package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A warm frog eats a small magma cube for a pearlescent froglight and a
// small slime for nothing; a big slime is not food.
func TestFrogEatsForFroglight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.rules.DoMobLoot = true
	frog := h.spawnSpecies(players, entityFrog, 0, 0.5, 70, 0.5)
	if frog == nil {
		t.Fatal("no frog")
	}
	frog.variant, frog.variantSet = frogWarm, true
	cube := h.spawnSpecies(players, entityMagmaCube, 0, 1.5, 70, 0.5)
	if cube == nil {
		t.Fatal("no magma cube")
	}
	cube.size, cube.y = 1, 70
	frog.y = 70
	if !h.frogStep(players, frog) || cube.dying == 0 {
		t.Fatalf("the frog should eat the cube beside it: dying=%d", cube.dying)
	}
	cube.dying = 1
	items := len(h.items)
	h.despawnMob(players, cube)
	var got int32
	for _, it := range h.items {
		got = it.item
	}
	if len(h.items) != items+1 || got != froglightFor(frogWarm) {
		t.Fatalf("a warm frog's magma cube should leave a pearlescent froglight: items %d→%d item %d", items, len(h.items), got)
	}
	// A big slime is left alone.
	big := h.spawnSpecies(players, entitySlime, 0, 1.5, 70, 0.5)
	big.size, big.y = 2, 70
	if h.frogStep(players, frog) {
		t.Fatal("a big slime is not frog food")
	}
	// The sneeze wind-up lands twenty ticks later.
	cub := h.spawnSpecies(players, entityPanda, 0, 5, 70, 5)
	cub.baby = true
	cub.sneezeAt = h.tick.Load()
	h.pandaSneezeTick(players, cub)
	if cub.sneezeAt != 0 {
		t.Fatal("a due sneeze should land")
	}
}
