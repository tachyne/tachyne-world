package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A sponge dropped in water drinks it and turns wet; a dry one on land does
// nothing at all.
func TestSpongeAbsorbsWater(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}
	for dx := -2; dx <= 2; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				w.SetBlock(pos.x+dx, pos.y+dy, pos.z+dz, worldgen.WaterBase)
			}
		}
	}
	w.SetBlock(pos.x, pos.y, pos.z, spongeState)

	h.soakSponge(players, 0, pos)
	if w.At(pos.x, pos.y, pos.z) != wetSpongeState {
		t.Fatal("a sponge that drank should be wet")
	}
	if w.At(pos.x+1, pos.y, pos.z) != worldgen.Air {
		t.Error("the water next to the sponge is still there")
	}
	// It stops at 64 cells, as vanilla does — a 5x5x5 pool holds more water
	// than one sponge can drink, so the far corners are left wet.
	left := 0
	for dx := -2; dx <= 2; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				if worldgen.IsWater(w.At(pos.x+dx, pos.y+dy, pos.z+dz)) {
					left++
				}
			}
		}
	}
	if taken := 124 - left; taken != spongeMax {
		t.Errorf("a sponge took %d cells, want its %d limit", taken, spongeMax)
	}

	// On dry land there is nothing to take, so it stays dry.
	dry := blockPos{60, 180, 60}
	w.SetBlock(dry.x, dry.y, dry.z, spongeState)
	h.soakSponge(players, 0, dry)
	if w.At(dry.x, dry.y, dry.z) != spongeState {
		t.Error("a sponge in open air turned wet")
	}
}

// A sponge cannot reach through a wall it has no water path around.
func TestSpongeCannotReachThroughStone(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}
	stone := worldgen.BlockBase("stone")
	// A pane of stone one block east, water beyond it.
	w.SetBlock(pos.x, pos.y, pos.z, spongeState)
	for dy := -1; dy <= 1; dy++ {
		for dz := -1; dz <= 1; dz++ {
			w.SetBlock(pos.x+1, pos.y+dy, pos.z+dz, stone)
		}
	}
	w.SetBlock(pos.x+2, pos.y, pos.z, worldgen.WaterBase)

	h.soakSponge(players, 0, pos)
	if w.At(pos.x+2, pos.y, pos.z) != worldgen.WaterBase {
		t.Error("the sponge drank through a stone wall")
	}
}

// Blast resistance now attenuates the blast instead of switching it off:
// obsidian survives and shields what is behind it, dirt does not.
func TestExplosionAttenuatesAndShields(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	obsidian := worldgen.BlockBase("obsidian")
	dirt := worldgen.BlockBase("dirt")

	// A dirt wall two out, an obsidian wall two out the other way, each with
	// more dirt behind it.
	for dy := -2; dy <= 2; dy++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(3, 180+dy, dz, obsidian)
			w.SetBlock(4, 180+dy, dz, dirt)
			w.SetBlock(-3, 180+dy, dz, dirt)
			w.SetBlock(-4, 180+dy, dz, dirt)
		}
	}
	h.explodeIn(players, 0, 0.5, 180.5, 0.5, 4, 20)

	if w.At(3, 180, 0) != obsidian {
		t.Error("obsidian should survive a size-4 blast")
	}
	if w.At(4, 180, 0) != dirt {
		t.Error("obsidian should have shielded what stood behind it")
	}
	if w.At(-3, 180, 0) != worldgen.Air {
		t.Error("dirt should not survive a size-4 blast at three blocks")
	}
}

// An explosion in the nether must not blow a hole in the overworld.
func TestExplosionStaysInItsDimension(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	players := map[int32]*tracked{}
	dirt := worldgen.BlockBase("dirt")
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			h.world.SetBlock(dx, 180, dz, dirt)
			nw.SetBlock(dx, 180, dz, dirt)
		}
	}
	h.explodeIn(players, 1, 0.5, 180.5, 0.5, 4, 20)
	if nw.At(1, 180, 0) != worldgen.Air {
		t.Error("the nether explosion left its own blocks standing")
	}
	if h.world.At(1, 180, 0) != dirt {
		t.Error("a nether explosion destroyed overworld blocks")
	}
}
