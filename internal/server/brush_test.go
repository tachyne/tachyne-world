package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Archaeology only exists if something in the world actually buries a
// suspicious block. Before the desert well there was a brush item, two block
// ids and six loot tables, and nothing to point them at.

// findWell walks the seeds until one produces a desert well, and returns it.
// Wells need desert, so most seeds in a small search have none.
func findWell(t *testing.T) (*hub, worldgen.DesertWell) {
	t.Helper()
	for seed := int64(1); seed < 40; seed++ {
		h := newHub(world.New(seed))
		g := h.world.Gen()
		for cx := -3; cx <= 3; cx++ {
			for cz := -3; cz <= 3; cz++ {
				if w := g.DesertWellIn(cx*512, cz*512); w.Exists {
					return h, w
				}
			}
		}
	}
	t.Skip("no desert within the searched seeds")
	return nil, worldgen.DesertWell{}
}

// The two caches are inside the well, one and two blocks under a water cell,
// and they are where the generator says they are — a brush finds nothing if
// the block and the loot query disagree about position.
func TestDesertWellBuriesSuspiciousSand(t *testing.T) {
	h, w := findWell(t)
	for i, s := range w.Sus {
		got := h.world.At(s[0], s[1], s[2])
		if _, ok := suspiciousTurnsInto(got); !ok {
			t.Errorf("cache %d at (%d,%d,%d): state %d is not a suspicious block",
				i, s[0], s[1], s[2], got)
		}
		if name, ok := h.brushLootTable(blockPos{s[0], s[1], s[2]}); !ok || name != "archaeology/desert_well" {
			t.Errorf("cache %d: loot table %q ok=%v, want archaeology/desert_well", i, name, ok)
		}
	}
	// …and an ordinary cell nearby is not a cache, or every block would pay out.
	if _, ok := h.brushLootTable(blockPos{w.X + 7, w.Y, w.Z + 7}); ok {
		t.Error("a cell outside the well should hold no archaeology loot")
	}
}

// Ten strokes on the cooldown, then it gives up its contents and leaves plain
// sand. Fewer than ten leaves it standing.
func TestBrushingOpensTheCacheAfterTenStrokes(t *testing.T) {
	h, w := findWell(t)
	pos := blockPos{w.Sus[0][0], w.Sus[0][1], w.Sus[0][2]}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(pos.x), float64(pos.y), float64(pos.z)
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}
	stroke := evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1}

	for i := 0; i < 9; i++ {
		h.tick.Store(uint64(i) * brushCooldown)
		h.brush(players, pl, stroke)
	}
	if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); !ok {
		t.Fatal("nine strokes should not have opened it")
	}
	before := len(h.items)

	h.tick.Store(uint64(9) * brushCooldown)
	h.brush(players, pl, stroke)
	if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); ok {
		t.Error("the tenth stroke should have opened it")
	}
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Sand {
		t.Errorf("an opened cache should leave sand, got %d", h.world.At(pos.x, pos.y, pos.z))
	}
	if len(h.items) <= before {
		t.Error("opening a desert-well cache should have dropped something")
	}
}

// Strokes inside the cooldown do not count, which is what makes brushing take
// a couple of seconds rather than one frantic click-storm.
func TestBrushCooldownRateLimitsStrokes(t *testing.T) {
	h, w := findWell(t)
	pos := blockPos{w.Sus[0][0], w.Sus[0][1], w.Sus[0][2]}
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}
	stroke := evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1}

	h.tick.Store(100)
	for i := 0; i < 30; i++ { // same tick, thirty clicks
		h.brush(players, pl, stroke)
	}
	if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); !ok {
		t.Fatal("thirty clicks in one tick should not open a cache")
	}
	if got := h.brushes[pos].count; got != 1 {
		t.Errorf("only one stroke should have counted, got %d", got)
	}
}

// Left alone, the dust settles back — and faster than it was cleared.
func TestBrushingDecaysWhenLeftAlone(t *testing.T) {
	h, w := findWell(t)
	pos := blockPos{w.Sus[0][0], w.Sus[0][1], w.Sus[0][2]}
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}
	stroke := evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1}

	for i := 0; i < 5; i++ {
		h.tick.Store(uint64(i) * brushCooldown)
		h.brush(players, pl, stroke)
	}
	if h.brushes[pos].count != 5 {
		t.Fatalf("expected 5 strokes, got %d", h.brushes[pos].count)
	}
	// Walk away: each retraction takes two strokes back.
	for i := 0; i < 4; i++ {
		h.tick.Store(h.tick.Load() + brushResetAfte + brushRetract)
		h.tickBrushes(players)
	}
	if b, still := h.brushes[pos]; still {
		t.Errorf("the dust should have settled completely, count=%d", b.count)
	}
	if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); !ok {
		t.Error("decay must not open the block")
	}
}

// A suspicious block nothing buried holds nothing — brushing one a player
// placed themselves is just a slow way to make sand.
func TestUnseededSuspiciousBlockDropsNothing(t *testing.T) {
	h := newHub(world.New(7))
	pos := blockPos{2000, 100, 2000}
	h.world.SetBlock(pos.x, pos.y, pos.z, suspiciousSandBase)
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}
	stroke := evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1}

	before := len(h.items)
	for i := 0; i < 10; i++ {
		h.tick.Store(uint64(i) * brushCooldown)
		h.brush(players, pl, stroke)
	}
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Sand {
		t.Error("it should still open into sand")
	}
	if len(h.items) != before {
		t.Error("an unseeded cache should drop nothing")
	}
}

// The uneven dusted stages are vanilla's, not a linear ramp.
func TestDustedStagesMatchVanilla(t *testing.T) {
	for _, c := range []struct{ count, want int }{
		{0, 0}, {1, 1}, {2, 1}, {3, 2}, {4, 2}, {5, 2}, {6, 3}, {9, 3},
	} {
		if got := dustedStage(c.count); got != c.want {
			t.Errorf("dustedStage(%d) = %d, want %d", c.count, got, c.want)
		}
	}
}
