package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// SpreadingSnowyBlock: grass AND mycelium creep over nearby dirt and revert to
// dirt when smothered. Mycelium shares the vanilla class with grass but was
// never dispatched here, so it did neither.

// spreadHub lays a dirt field at y=70 with clear sky, and one spreading block.
func spreadHub(t *testing.T, spread uint32, x, y, z int) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			h.world.SetBlock(x+dx, y, z+dz, worldgen.Dirt)
			for dy := 1; dy <= 4; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	h.world.SetBlock(x, y, z, spread)
	return h, map[int32]*tracked{}
}

func TestMyceliumSpreadsToDirt(t *testing.T) {
	myc := worldgen.BlockID("mycelium")
	x, y, z := 200, 70, 200
	h, players := spreadHub(t, myc, x, y, z)

	spread := false
	for i := 0; i < 3000 && !spread; i++ {
		h.tickSpread(players, 0, x, y, z, h.world.At(x, y, z))
		for dx := -2; dx <= 2 && !spread; dx++ {
			for dz := -2; dz <= 2 && !spread; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				if s := h.world.At(x+dx, y, z+dz); s != worldgen.Dirt {
					if _, isSpreader := spreaders[s]; isSpreader {
						spread = true
					}
				}
			}
		}
	}
	if !spread {
		t.Error("mycelium never spread to neighbouring dirt")
	}
}

func TestMyceliumSmothersToDirt(t *testing.T) {
	myc := worldgen.BlockID("mycelium")
	x, y, z := 220, 70, 220
	h, players := spreadHub(t, myc, x, y, z)
	h.world.SetBlock(x, y+1, z, worldgen.Stone) // covered

	h.tickSpread(players, 0, x, y, z, h.world.At(x, y, z))
	if got := h.world.At(x, y, z); got != worldgen.Dirt {
		t.Errorf("smothered mycelium is state %d, want dirt %d", got, worldgen.Dirt)
	}
}

// Grass keeps working through the same shared path.
func TestGrassStillSpreadsAndSmothers(t *testing.T) {
	x, y, z := 240, 70, 240
	h, players := spreadHub(t, worldgen.GrassBlock, x, y, z)

	spread := false
	for i := 0; i < 3000 && !spread; i++ {
		h.tickSpread(players, 0, x, y, z, h.world.At(x, y, z))
		for dx := -2; dx <= 2 && !spread; dx++ {
			for dz := -2; dz <= 2 && !spread; dz++ {
				if (dx != 0 || dz != 0) && h.world.At(x+dx, y, z+dz) == worldgen.GrassBlock {
					spread = true
				}
			}
		}
	}
	if !spread {
		t.Error("grass never spread to neighbouring dirt")
	}

	h2, p2 := spreadHub(t, worldgen.GrassBlock, x, y, z)
	h2.world.SetBlock(x, y+1, z, worldgen.Stone)
	h2.tickSpread(p2, 0, x, y, z, h2.world.At(x, y, z))
	if got := h2.world.At(x, y, z); got != worldgen.Dirt {
		t.Errorf("smothered grass is state %d, want dirt", got)
	}
}

// Spreading is gated on brightness >= 9 at the block above — the grass-only
// version had no light gate at all.
func TestSpreadNeedsLight(t *testing.T) {
	x, y, z := 60, 40, 60 // underground: lighting is height-capped, so go deep
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	for dx := -3; dx <= 3; dx++ {
		for dy := -1; dy <= 3; dy++ {
			for dz := -3; dz <= 3; dz++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	// A sealed pocket: grass on dirt, air above, no light source.
	h.world.SetBlock(x, y, z, worldgen.GrassBlock)
	h.world.SetBlock(x+1, y, z, worldgen.Dirt)
	h.world.SetBlock(x, y+1, z, worldgen.Air)
	h.world.SetBlock(x+1, y+1, z, worldgen.Air)

	if b := h.plantBrightness(0, x, y+1, z, -1); b >= 9 {
		t.Fatalf("test setup: brightness %d above the grass, want < 9", b)
	}
	for i := 0; i < 2000; i++ {
		h.tickSpread(players, 0, x, y, z, h.world.At(x, y, z))
	}
	if h.world.At(x+1, y, z) != worldgen.Dirt {
		t.Error("grass spread in the dark")
	}
}
