package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A TNT blast mines: stone drops cobblestone and diamond ore its diamond
// through the loot tables with an empty tool (no correct-tool rule for an
// explosion), and with TNT's drop decay off (vanilla's default) everything
// the blast breaks drops.
func TestExplosionDropsThroughLootTables(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1400, 180, 1400
	for dx := -6; dx <= 6; dx++ {
		for dy := -6; dy <= 6; dy++ {
			for dz := -6; dz <= 6; dz++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	h.world.SetBlock(x+1, y, z, worldgen.Stone)
	h.world.SetBlock(x-1, y, z, worldgen.BlockID("diamond_ore"))
	h.explodeIn(players, 0, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 4, 4, blastTNT)
	if h.world.At(x+1, y, z) != worldgen.Air || h.world.At(x-1, y, z) != worldgen.Air {
		t.Fatal("the blast should have broken both blocks")
	}
	cobble, diamond := 0, 0
	for _, it := range h.items {
		switch it.item {
		case itemByName["cobblestone"]:
			cobble += it.count
		case itemByName["diamond"]:
			diamond += it.count
		}
	}
	if cobble != 1 || diamond != 1 {
		t.Fatalf("TNT-mined stone and diamond ore should drop cobblestone=1 diamond=1, got %d %d", cobble, diamond)
	}
}

// A burning arrow primes TNT and lights a campfire where it strikes.
func TestFlamingArrowPrimesTNTAndLightsCampfire(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1420, 180, 1420
	flatFloor(h.world, x, y, z, 3)
	tnt := worldgen.BlockID("tnt")
	h.world.SetBlock(x, y, z, tnt)
	campfire := worldgen.SetProperty(func() worldgen.BlockInfo { i, _ := worldgen.InfoForState(worldgen.BlockID("campfire")); return i }(), worldgen.BlockID("campfire"), "lit", "false")
	h.world.SetBlock(x+2, y, z, campfire)
	cold := &arrowEntity{dim: 0}
	h.projectileHitBlock(players, cold, blockPos{x, y, z}, tnt)
	if h.world.At(x, y, z) != tnt || len(h.tnt) != 0 {
		t.Fatal("an unlit arrow must not prime TNT")
	}
	hot := &arrowEntity{dim: 0, fire: true}
	h.projectileHitBlock(players, hot, blockPos{x, y, z}, tnt)
	if h.world.At(x, y, z) != worldgen.Air || len(h.tnt) != 1 {
		t.Fatalf("a flaming arrow primes TNT: block %d primed %d", h.world.At(x, y, z), len(h.tnt))
	}
	h.projectileHitBlock(players, hot, blockPos{x + 2, y, z}, campfire)
	if got := h.world.At(x+2, y, z); propOf(t, got, "lit") != "true" {
		t.Fatalf("a flaming arrow lights a campfire, got %d", got)
	}
}
