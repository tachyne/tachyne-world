package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDragonEggLandsOnSomething: DragonEggBlock.teleport picks an air cell
// with a non-air block below it, so the egg never blinks into mid-air.
func TestDragonEggLandsOnSomething(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	from := blockPos{0, 200, 0}
	// Open sky all around except scattered stone pillars: a free cell with air
	// under it is common, one with stone under it rare.
	for x := -16; x <= 16; x++ {
		for z := -16; z <= 16; z++ {
			for y := 190; y <= 210; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
			if (x+z)%7 == 0 {
				w.SetBlock(x, 199, z, worldgen.Stone)
			}
		}
	}
	for i := 0; i < 200; i++ {
		to, ok := h.dragonEggTarget(0, from)
		if !ok {
			continue
		}
		if s := w.At(to.x, to.y-1, to.z); isAnyAir(s) {
			t.Fatalf("the egg blinked to %v with air (%d) under it", to, s)
		}
	}
}
