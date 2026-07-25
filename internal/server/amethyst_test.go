package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BuddingAmethystBlock.randomTick: one tick in five, a random face advances
// air → small → medium → large → cluster.

func TestAmethystGrowsThroughEveryStage(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 30, 60, 30

	// Budding amethyst with air all round, so every face is a candidate.
	h.world.SetBlock(x, y, z, buddingAmethyst)
	for _, d := range amethystDirs {
		h.world.SetBlock(x+d.dx, y+d.dy, z+d.dz, worldgen.Air)
	}

	seen := map[int]bool{}
	for i := 0; i < 40000 && len(seen) < len(amethystChain); i++ {
		h.tickAmethyst(players, 0, x, y, z, buddingAmethyst)
		for _, d := range amethystDirs {
			if st, _ := amethystStage(h.world.At(x+d.dx, y+d.dy, z+d.dz)); st >= 0 {
				seen[st] = true
			}
		}
	}
	for i, name := range amethystChain {
		if !seen[i] {
			t.Errorf("stage %d (%s) never appeared", i, name)
		}
	}
}

// A bud only advances on the face it already points at — growing "up" must not
// promote a bud that faces north.
func TestAmethystBudOnlyAdvancesOnItsOwnFacing(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 50, 60, 50

	h.world.SetBlock(x, y, z, buddingAmethyst)
	// Seal every face except UP so only that direction can ever be chosen.
	for _, d := range amethystDirs {
		h.world.SetBlock(x+d.dx, y+d.dy, z+d.dz, worldgen.Stone)
	}
	// Put a NORTH-facing small bud in the UP slot: wrong facing for this face.
	wrong, ok := amethystAt(0, "north", false)
	if !ok {
		t.Fatal("could not build a north-facing small bud")
	}
	h.world.SetBlock(x, y+1, z, wrong)

	for i := 0; i < 5000; i++ {
		h.tickAmethyst(players, 0, x, y, z, buddingAmethyst)
	}
	if got := h.world.At(x, y+1, z); got != wrong {
		t.Errorf("a north-facing bud advanced while growing up: %d -> %d", wrong, got)
	}
}

// Buds grow waterlogged when they replace water, so a submerged geode still
// grows and does not create an air pocket.
func TestAmethystGrowsWaterlogged(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 70, 60, 70

	h.world.SetBlock(x, y, z, buddingAmethyst)
	for _, d := range amethystDirs {
		h.world.SetBlock(x+d.dx, y+d.dy, z+d.dz, worldgen.Stone)
	}
	h.world.SetBlock(x, y+1, z, worldgen.WaterBase)

	for i := 0; i < 5000; i++ {
		h.tickAmethyst(players, 0, x, y, z, buddingAmethyst)
		if s := h.world.At(x, y+1, z); s != worldgen.WaterBase {
			info, ok := worldgen.InfoForState(s)
			if !ok {
				t.Fatalf("grown bud state %d has no block info", s)
			}
			if wl := worldgen.GetProperty(info, s, "waterlogged"); wl != "true" {
				t.Errorf("bud grown into water has waterlogged=%q, want true", wl)
			}
			return
		}
	}
	t.Error("no bud ever grew into the water above")
}

// Solid neighbours block growth entirely.
func TestAmethystDoesNotGrowIntoStone(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 90, 60, 90

	h.world.SetBlock(x, y, z, buddingAmethyst)
	for _, d := range amethystDirs {
		h.world.SetBlock(x+d.dx, y+d.dy, z+d.dz, worldgen.Stone)
	}
	for i := 0; i < 5000; i++ {
		h.tickAmethyst(players, 0, x, y, z, buddingAmethyst)
	}
	for _, d := range amethystDirs {
		if got := h.world.At(x+d.dx, y+d.dy, z+d.dz); got != worldgen.Stone {
			t.Errorf("amethyst replaced stone at %+v: state %d", d, got)
		}
	}
}
