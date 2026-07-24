package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// endermanHoldableDefault recognises tag members (at any state) and returns the
// block's default state, and rejects everything else.
func TestEndermanHoldableLookup(t *testing.T) {
	// A plain single-state holdable.
	if got := endermanHoldableDefault(worldgen.BlockID("sand")); got != worldgen.BlockID("sand") {
		t.Errorf("sand should be holdable, got %d", got)
	}
	// A multi-state holdable at a non-default state (cactus age > 0) still maps
	// to the block's default state.
	lo, hi := worldgen.BlockRange("cactus")
	if hi > lo {
		if got := endermanHoldableDefault(lo + 1); got != worldgen.BlockID("cactus") {
			t.Errorf("aged cactus should map to default cactus, got %d", got)
		}
	}
	// Non-holdable blocks return 0.
	if got := endermanHoldableDefault(worldgen.Stone); got != 0 {
		t.Errorf("stone must not be holdable, got %d", got)
	}
	if got := endermanHoldableDefault(worldgen.Air); got != 0 {
		t.Errorf("air must not be holdable, got %d", got)
	}
}

// enderCarryMeta emits the DATA_CARRY_STATE entry: index 16, OPTIONAL_BLOCK_STATE
// type 15, a single VarInt state, terminator.
func TestEnderCarryMetaBytes(t *testing.T) {
	state := worldgen.BlockID("dirt")
	body := enderCarryMeta(42, state)
	r := bytes.NewReader(body)
	if eid, _ := protocol.ReadVarInt(r); eid != 42 {
		t.Fatalf("eid = %d, want 42", eid)
	}
	if idx, _ := r.ReadByte(); idx != endermanCarryIndex {
		t.Fatalf("index = %d, want %d", idx, endermanCarryIndex)
	}
	if typ, _ := protocol.ReadVarInt(r); typ != metaTypeOptState {
		t.Fatalf("type = %d, want %d", typ, metaTypeOptState)
	}
	if v, _ := protocol.ReadVarInt(r); uint32(v) != state {
		t.Fatalf("state = %d, want %d", v, state)
	}
	if b, _ := r.ReadByte(); b != itemMetaEnd {
		t.Fatal("terminator missing")
	}
}

// An enderman lifts a holdable block out of the world (leaving air) and latches
// it as its carried state.
func TestEndermanTakesBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	ex, ez := h.findLand(0, 0)
	ey := h.world.SurfaceFeet(ex, ez) + 10 // well clear of terrain
	m := h.spawnMob(players, entityEnderman, float64(ex)+0.5, float64(ey), float64(ez)+0.5)
	if m == nil {
		t.Fatal("failed to spawn enderman")
	}
	// Fill the whole pickup sample box (x±2, y..y+2, z±2) with dirt so any cell
	// the goal samples is holdable.
	dirt := worldgen.BlockID("dirt")
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				h.world.SetBlock(ex+dx, ey+dy, ez+dz, dirt)
			}
		}
	}
	for i := 0; i < 5000 && m.carriedBlock == 0; i++ {
		h.endermanTakeBlock(players, m)
	}
	if m.carriedBlock != dirt {
		t.Fatalf("enderman should carry dirt, carried %d", m.carriedBlock)
	}
	air := 0 // exactly one cell in the box was cleared to air
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				if h.world.At(ex+dx, ey+dy, ez+dz) == worldgen.Air {
					air++
				}
			}
		}
	}
	if air != 1 {
		t.Fatalf("exactly one block should have been lifted, %d cells are air", air)
	}
}

// A carrying enderman sets its block back down on a solid full block and clears
// its carried state.
func TestEndermanPlacesBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	ex, ez := h.findLand(30, 30)
	ey := h.world.SurfaceFeet(ex, ez) + 10
	m := h.spawnMob(players, entityEnderman, float64(ex)+0.5, float64(ey), float64(ez)+0.5)
	if m == nil {
		t.Fatal("failed to spawn enderman")
	}
	dirt := worldgen.BlockID("dirt")
	m.carriedBlock = dirt
	// A stone floor one below the enderman across the place box (x±1, z±1); the
	// target cells at ey and ey+1 stay air.
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(ex+dx, ey-1, ez+dz, worldgen.Stone)
			h.world.SetBlock(ex+dx, ey, ez+dz, worldgen.Air)
			h.world.SetBlock(ex+dx, ey+1, ez+dz, worldgen.Air)
		}
	}
	for i := 0; i < 500000 && m.carriedBlock != 0; i++ {
		h.endermanPlaceBlock(players, m)
	}
	if m.carriedBlock != 0 {
		t.Fatal("enderman should have placed its block")
	}
	placed := 0 // the dirt landed on the floor level (below is stone)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if h.world.At(ex+dx, ey, ez+dz) == dirt {
				placed++
			}
		}
	}
	if placed != 1 {
		t.Fatalf("exactly one dirt block should have been placed, found %d", placed)
	}
}
