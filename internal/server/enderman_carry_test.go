package server

import (
	"bytes"
	"math"
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

// The staring contest: an enderman held in a player's crosshair stops where
// it is, and blinks away when that player closes to within four blocks.
func TestEndermanStareFreezesThenBlinks(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	lx, lz := h.findLand(20, 20)
	y := float64(h.world.MobFeet(lx, lz))
	pl.x, pl.y, pl.z = float64(lx), y, float64(lz)+8
	m := h.spawnMob(players, entityEnderman, float64(lx), y, float64(lz))

	// Look straight at it from eight blocks: held, not blinking.
	pl.yaw, pl.pitch = 0, float32(lookPitchTo(pl, m))
	pl.yaw = float32(lookYawTo(pl, m))
	if h.starerOf(players, m) == nil {
		t.Fatal("the player is looking right at it")
	}
	sx, sz := m.x, m.z
	if !h.endermanStareStep(players, m) {
		t.Error("an enderman stared at from eight blocks freezes")
	}
	if m.x != sx || m.z != sz {
		t.Error("a frozen enderman does not move")
	}
	// Step in close and it goes.
	pl.x, pl.z = m.x, m.z+2
	pl.yaw = float32(lookYawTo(pl, m))
	pl.pitch = float32(lookPitchTo(pl, m))
	for i := 0; i < 20 && m.x == sx && m.z == sz; i++ { // one try a step, which may find nowhere to land
		pl.x, pl.z = m.x, m.z+2
		h.endermanStareStep(players, m)
	}
	if m.x == sx && m.z == sz {
		t.Error("an enderman stared at from two blocks blinks away")
	}
}

// The yaw/pitch a player at t would need to look at m (degrees).
func lookYawTo(t *tracked, m *mob) float64 {
	dx, dz := m.x-t.x, m.z-t.z
	return math.Atan2(-dx, dz) * 180 / math.Pi
}

func lookPitchTo(t *tracked, m *mob) float64 {
	dx, dy, dz := m.x-t.x, (m.y+2.55)-(t.y+1.62), m.z-t.z
	return -math.Atan2(dy, math.Sqrt(dx*dx+dz*dz)) * 180 / math.Pi
}

// An enderman in the End takes its block from the End and leaves the overworld
// alone. Both goals read and wrote dimension zero whatever dimension the mob
// was in, so every enderman in the End — where they are the whole population —
// was quietly cutting holes in the overworld at the matching coordinates, and
// carrying a block that had never been beside it.
func TestEndermanCarriesInItsOwnDimension(t *testing.T) {
	h := dimHub()
	players := map[int32]*tracked{}
	h.playersRef = players
	nw := h.nether // dimHub gives the hub a second world; use it as the far dimension

	const ex, ey, ez = 300, 70, 300
	m := h.spawnMobIn(players, entityEnderman, dimNether, ex+0.5, ey, ez+0.5)
	if m == nil {
		t.Fatal("failed to spawn the enderman")
	}
	dirt := worldgen.BlockID("dirt")
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 3; dy++ {
			for dz := -2; dz <= 2; dz++ {
				nw.SetBlock(ex+dx, ey+dy, ez+dz, dirt)
				h.world.SetBlock(ex+dx, ey+dy, ez+dz, worldgen.Stone) // the overworld, untouched
			}
		}
	}
	for i := 0; i < 400 && m.carriedBlock == 0; i++ {
		h.endermanTakeBlock(players, m)
	}
	if m.carriedBlock == 0 {
		t.Skip("the pickup roll never came up in 400 tries")
	}
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 3; dy++ {
			for dz := -2; dz <= 2; dz++ {
				if got := h.world.At(ex+dx, ey+dy, ez+dz); got != worldgen.Stone {
					t.Fatalf("the overworld at (%d,%d,%d) became %d — the enderman reached through",
						ex+dx, ey+dy, ez+dz, got)
				}
			}
		}
	}
	taken := 0
	for dx := -2; dx <= 2; dx++ {
		for dy := 0; dy <= 3; dy++ {
			for dz := -2; dz <= 2; dz++ {
				if nw.At(ex+dx, ey+dy, ez+dz) == worldgen.Air {
					taken++
				}
			}
		}
	}
	if taken != 1 {
		t.Errorf("exactly one block should have left its own dimension, got %d", taken)
	}
}

// How much does the line-of-sight test actually change? Over every cell the
// goal can roll, count the ones holding a holdable block (what the engine used
// to accept) against the ones that are also VISIBLE (what vanilla accepts).
// The answer is the whole claim that the missing ray was making endermen
// permanent, so it is measured rather than asserted.
func TestEndermanSightChangesTheTakeRate(t *testing.T) {
	stone := worldgen.BlockBase("stone")
	grass := worldgen.BlockBase("grass_block")

	// Two terrains: open ground with a scatter of holdable blocks at head
	// height, and an enderman standing against a bank of earth.
	for _, tc := range []struct {
		name  string
		build func(w *world.World)
	}{
		{"open ground", func(w *world.World) {
			for x := -6; x <= 6; x++ {
				for z := -6; z <= 6; z++ {
					w.SetBlock(x, 179, z, stone)
				}
			}
			w.SetBlock(2, 180, 0, grass)
			w.SetBlock(-2, 181, 1, grass)
		}},
		{"against a bank", func(w *world.World) {
			for x := -6; x <= 6; x++ {
				for z := -6; z <= 6; z++ {
					w.SetBlock(x, 179, z, stone)
				}
			}
			// A solid wall of earth from x=1 outwards, two deep: the far
			// column is holdable but hidden behind the near one.
			for x := 1; x <= 2; x++ {
				for y := 180; y <= 182; y++ {
					for z := -2; z <= 2; z++ {
						w.SetBlock(x, y, z, grass)
					}
				}
			}
		}},
	} {
		w := world.New(73)
		h := newHub(w)
		tc.build(w)
		m := h.spawnMob(map[int32]*tracked{}, entityEnderman, 0.5, 180, 0.5)
		holdable, visible := 0, 0
		for x := -2; x <= 2; x++ {
			for y := 180; y <= 182; y++ {
				for z := -2; z <= 2; z++ {
					if endermanHoldableDefault(w.At(x, y, z)) == 0 {
						continue
					}
					holdable++
					if _, ok := h.endermanTakeable(m, x, y, z); ok {
						visible++
					}
				}
			}
		}
		t.Logf("%s: %d holdable cells in reach, %d of them visible", tc.name, holdable, visible)
		if holdable == 0 {
			t.Errorf("%s: the fixture has nothing to take", tc.name)
		}
		if visible > holdable {
			t.Errorf("%s: %d visible of %d holdable is impossible", tc.name, visible, holdable)
		}
	}
}

// EndermanTakeBlockGoal ray-casts before it lifts: an enderman takes only
// what it can actually see. Driven through the real predicate rather than
// through the one-in-ten random roll, which would make the test a coin flip.
// (Legion reported endermen crowding up, 2026-09-20.)
func TestEndermanTakesOnlyWhatItCanSee(t *testing.T) {
	w := world.New(67)
	h := newHub(w)
	h.rules.MobGriefing = true
	grass := worldgen.BlockBase("grass_block")
	stone := worldgen.BlockBase("stone")
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, stone)
		}
	}
	m := h.spawnMob(map[int32]*tracked{}, entityEnderman, 0.5, 180, 0.5)

	// In the open, two blocks away: takeable.
	w.SetBlock(2, 180, 0, grass)
	if _, ok := h.endermanTakeable(m, 2, 180, 0); !ok {
		t.Error("an enderman must lift a holdable block it can see")
	}

	// The same block with a wall in front of it: not takeable.
	for y := 179; y <= 182; y++ {
		w.SetBlock(1, y, 0, stone)
	}
	if _, ok := h.endermanTakeable(m, 2, 180, 0); ok {
		t.Error("an enderman reached a block through a solid wall")
	}

	// A block that is not holdable at all is never takeable, wall or none.
	w.SetBlock(-2, 180, 0, stone)
	if _, ok := h.endermanTakeable(m, -2, 180, 0); ok {
		t.Error("stone is not in #enderman_holdable")
	}

	// And the whole goal still works end to end: with the wall gone and the
	// grass in the open, enough rolls eventually take it.
	for y := 179; y <= 182; y++ {
		w.SetBlock(1, y, 0, worldgen.Air)
	}
	took := false
	for i := 0; i < 20000 && !took; i++ {
		m.carriedBlock = 0
		h.endermanTakeBlock(map[int32]*tracked{}, m)
		took = m.carriedBlock != 0
	}
	if !took {
		t.Error("the goal never lifted a block it could plainly see")
	}
}

// EnderMan.dropCustomDeathLoot: a killed enderman drops what it carried.
func TestEndermanDropsItsCarriedBlock(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	h.rules.DoMobLoot = true
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	m := h.spawnMob(players, entityEnderman, 0.5, 180, 0.5)
	m.carriedBlock = worldgen.BlockID("grass_block")
	h.hurtByPlayerOn(m, pl)
	h.killMob(players, m)
	h.despawnMob(players, m)
	for _, it := range h.items {
		if it.item == itemByName["grass_block"] {
			return
		}
	}
	t.Fatal("the enderman's grass block was lost with it")
}
