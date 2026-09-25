package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// dropFrom places a falling block `up` cells above the pad through the
// engine's own write path (the onPlace that schedules FallingBlock.tick),
// over open air down to the pad's stone floor.
func dropFrom(h *hub, players map[int32]*tracked, x, y, z, up int, state uint32) {
	for dy := 0; dy <= up+1; dy++ {
		h.world.SetBlock(x, y+dy, z, worldgen.Air)
	}
	h.setBlockAt(players, 0, blockPos{x, y + up, z}, state)
}

// fallTicks is vanilla's fall, worked through by hand: gravity 0.04 a
// tick, then drag 0.98, until the block has come down `cells`. It returns
// the number of entity ticks the fall takes.
func fallTicks(cells float64) int {
	y, vy := 0.0, 0.0
	for n := 1; n < 1000; n++ {
		vy -= 0.04
		y += vy
		if y <= -cells {
			return n
		}
		vy *= 0.98
	}
	return -1
}

// A block with air under it waits two ticks (getDelayAfterPlace), then
// leaves the world as a falling_block entity that speeds up as it falls,
// and sets itself back down where it lands.
func TestSandFallsAsAnAcceleratingEntity(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	dropFrom(h, players, x, y, z, 10, worldgen.Sand)
	stepTicks(h, players, 1)
	if w.At(x, y+10, z) != worldgen.Sand || len(h.fallingBlocks) != 0 {
		t.Fatal("the sand let go before its two-tick delay")
	}
	stepTicks(h, players, 1)
	if w.At(x, y+10, z) != worldgen.Air || len(h.fallingBlocks) != 1 {
		t.Fatalf("after two ticks the sand should be an entity: cell %d, %d entities", w.At(x, y+10, z), len(h.fallingBlocks))
	}
	fb := h.fallingBlocks[0]
	if fb.state != worldgen.Sand || fb.x != float64(x)+0.5 || fb.z != float64(z)+0.5 {
		t.Fatalf("the entity carries %d at %v,%v", fb.state, fb.x, fb.z)
	}
	stepTicks(h, players, 4)
	if dropped := float64(y+10) - fb.y; dropped >= 1 || dropped < 0.5 {
		t.Fatalf("five ticks in it should have come down about 0.59, came %v", dropped)
	}
	// The whole ten-block fall, to the tick.
	want := fallTicks(10)
	stepTicks(h, players, want-6)
	if len(h.fallingBlocks) != 1 {
		t.Fatalf("landed %d ticks early", want-5)
	}
	stepTicks(h, players, 1)
	if len(h.fallingBlocks) != 0 || w.At(x, y, z) != worldgen.Sand {
		t.Fatalf("after %d ticks the sand should rest on the floor: %d entities, cell %d", want, len(h.fallingBlocks), w.At(x, y, z))
	}
}

// A falling block that comes to rest in a cell it may not take — a torch's,
// a flower's, a bottom slab's (it sits half a block up, inside the slab's
// cell), three layers of snow — breaks into its item and leaves the cell as
// it was. One layer of snow is replaceable, and the block takes its place.
func TestFallingBlockBreaksWhereItCannotLand(t *testing.T) {
	cases := []struct {
		name     string
		obstacle uint32
		placed   bool
	}{
		{"torch", worldgen.BlockBase("torch"), false},
		{"poppy", worldgen.BlockBase("poppy"), false},
		{"bottom slab", worldgen.BlockBase("oak_slab") + 3, false},
		{"three layers of snow", worldgen.BlockBase("snow") + 2, false},
		{"one layer of snow", worldgen.BlockBase("snow"), true},
		{"short grass", worldgen.BlockBase("short_grass"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h, w, players, x, y, z := redSetup(t)
			dropFrom(h, players, x, y, z, 4, worldgen.Sand)
			w.SetBlock(x, y, z, c.obstacle)
			stepTicks(h, players, 40)
			if len(h.fallingBlocks) != 0 {
				t.Fatal("still falling")
			}
			items := 0
			for _, it := range h.items {
				if it.item == int32(itemByName["sand"]) {
					items++
				}
			}
			if c.placed {
				if w.At(x, y, z) != worldgen.Sand || items != 0 {
					t.Fatalf("the sand should take the cell: %d, %d items", w.At(x, y, z), items)
				}
				return
			}
			if w.At(x, y, z) != c.obstacle || w.At(x, y+1, z) == worldgen.Sand {
				t.Fatalf("the %s should be left alone: %d, above %d", c.name, w.At(x, y, z), w.At(x, y+1, z))
			}
			if items != 1 {
				t.Fatalf("the sand should drop as one item, got %d", items)
			}
		})
	}
}

// An anvil hurts what it lands on: two per block of the fall past the
// first (FallingBlockEntity.causeFallDamage, ceil(fallDistance - 1)).
func TestFallingAnvilHurtsWhatItLandsOn(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	cow := h.spawnMob(players, entityCow, float64(x)+0.5, float64(y), float64(z)+0.5)
	hp := cow.health
	dropFrom(h, players, x, y, z, 4, worldgen.BlockBase("anvil"))
	stepTicks(h, players, 40)
	if lost := hp - cow.health; lost != 6 {
		t.Fatalf("a four-block fall should cost the cow 6, it lost %d", lost)
	}
	if !worldgen.IsAnvil(w.At(x, y, z)) {
		t.Fatalf("the anvil should rest on the floor, holds %d", w.At(x, y, z))
	}
}

// A long fall is sure to wear an anvil (5% + 5% per block): an intact one
// lands chipped, and a damaged one breaks to nothing — no block, no item.
func TestLongFallWearsAndBreaksAnvils(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	anvil := worldgen.BlockBase("anvil") + 2
	dropFrom(h, players, x, y, z, 25, anvil)
	dropFrom(h, players, x+2, y, z, 25, worldgen.BlockBase("damaged_anvil"))
	stepTicks(h, players, 80)
	if len(h.fallingBlocks) != 0 {
		t.Fatal("still falling")
	}
	if got := w.At(x, y, z); got != worldgen.BlockBase("chipped_anvil")+2 {
		t.Fatalf("the anvil should land chipped, facing kept: %d", got)
	}
	if got := w.At(x+2, y, z); got != worldgen.Air {
		t.Fatalf("the damaged anvil should break, the cell holds %d", got)
	}
	for _, it := range h.items {
		t.Fatalf("a broken anvil drops nothing, found item %d", it.item)
	}
}

// Suspicious sand never survives a fall (BrushableBlock disables its drop,
// so it cannot be placed again): it breaks where it lands, leaving nothing.
func TestSuspiciousSandBreaksWhenItFalls(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	dropFrom(h, players, x, y, z, 3, worldgen.SuspiciousSand)
	stepTicks(h, players, 40)
	for dy := 0; dy <= 4; dy++ {
		if w.At(x, y+dy, z) == worldgen.SuspiciousSand {
			t.Fatalf("the suspicious sand came to rest at y+%d", dy)
		}
	}
	if len(h.items) != 0 || len(h.fallingBlocks) != 0 {
		t.Fatalf("it should leave nothing: %d items, %d entities", len(h.items), len(h.fallingBlocks))
	}
}

// Concrete powder stops in the first water it enters and sets there
// (FallingBlockEntity: isStuckInWater; ConcretePowderBlock.onLand).
func TestConcretePowderSetsInTheWaterItFallsInto(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	powder := worldgen.BlockBase("white_concrete_powder")
	dropFrom(h, players, x, y, z, 6, powder)
	w.SetBlock(x, y, z, worldgen.WaterBase)
	w.SetBlock(x, y+1, z, worldgen.WaterBase)
	stepTicks(h, players, 40)
	if got := w.At(x, y+1, z); got != worldgen.ConcreteFor(powder) {
		t.Fatalf("the powder should set in the top water cell, holds %d", got)
	}
	if got := w.At(x, y, z); got != worldgen.WaterBase {
		t.Fatalf("the water under it stays, holds %d", got)
	}
}

// Falling works in the Nether as it does at home, and never writes into the
// overworld at the same coordinates.
func TestSandFallsInTheNether(t *testing.T) {
	h := dimHub()
	players := map[int32]*tracked{}
	x, y, z := 400, 100, 400
	h.nether.ForceLoad(x, z, 1)
	h.world.ForceLoad(x, z, 1)
	h.nether.SetBlock(x, y-1, z, worldgen.BlockBase("netherrack"))
	for dy := 0; dy <= 6; dy++ {
		h.nether.SetBlock(x, y+dy, z, worldgen.Air)
	}
	before := h.world.At(x, y, z)
	h.setBlockAt(players, dimNether, blockPos{x, y + 5, z}, worldgen.Gravel)
	stepTicks(h, players, 40)
	if h.nether.At(x, y, z) != worldgen.Gravel || h.nether.At(x, y+5, z) != worldgen.Air {
		t.Fatalf("the Nether gravel should have fallen: floor %d, start %d", h.nether.At(x, y, z), h.nether.At(x, y+5, z))
	}
	if h.world.At(x, y, z) != before {
		t.Fatal("a Nether fall wrote into the overworld")
	}
}

// A falling block saved mid-air is back in the air after a restart, and
// lands.
func TestFallingBlockSurvivesARestart(t *testing.T) {
	h, _, players, x, y, z := redSetup(t)
	dropFrom(h, players, x, y, z, 10, worldgen.Sand)
	stepTicks(h, players, 8)
	if len(h.fallingBlocks) != 1 {
		t.Fatal("the sand should be in the air")
	}
	saved := h.snapshotFalling()

	h2 := newHub(h.world)
	h2.tick.Store(h.tick.Load())
	h2.restoreFalling(saved)
	stepTicks(h2, players, 60)
	if len(h2.fallingBlocks) != 0 || h2.world.At(x, y, z) != worldgen.Sand {
		t.Fatalf("the restored sand should have landed: %d entities, floor %d", len(h2.fallingBlocks), h2.world.At(x, y, z))
	}
}

// ScaffoldingBlock.tick: a scaffold set at distance 7 with nothing holding
// it comes loose as a falling block the next tick and settles on the floor
// as scaffolding again; one that loses its support from a lower distance
// breaks and drops instead.
func TestUnsupportedScaffoldingFalls(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	lo, _ := worldgen.BlockRange("scaffolding")
	si, _ := worldgen.InfoForState(lo)
	scaf := worldgen.SetProperty(si, worldgen.SetProperty(si, lo, "waterlogged", "false"), "distance", "7")
	dropFrom(h, players, x, y, z, 6, scaf)
	stepTicks(h, players, 2)
	if w.At(x, y+6, z) != worldgen.Air || len(h.fallingBlocks) != 1 {
		t.Fatalf("the scaffold should be falling: cell %d, %d falling", w.At(x, y+6, z), len(h.fallingBlocks))
	}
	stepTicks(h, players, 60)
	if !isScaffolding(w.At(x, y, z)) {
		t.Fatalf("the scaffold should land on the floor, got %d", w.At(x, y, z))
	}
	if d := scaffoldDist(w.At(x, y, z)); d != 0 {
		t.Fatalf("the landed scaffold stands on stone: distance %d", d)
	}
}

// WebBlock.entityInside → makeStuckInBlock: an anvil that falls through a
// cobweb loses its fall there, so the cow under it is not hurt.
func TestFallingAnvilCaughtByCobweb(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	cow := h.spawnMob(players, entityCow, float64(x)+0.5, float64(y), float64(z)+0.5)
	hp := cow.health
	dropFrom(h, players, x, y, z, 8, worldgen.BlockBase("anvil"))
	w.SetBlock(x, y+2, z, cobwebState)
	stepTicks(h, players, 400)
	if lost := hp - cow.health; lost != 0 {
		t.Fatalf("an anvil whose fall a cobweb reset cost the cow %d", lost)
	}
}
