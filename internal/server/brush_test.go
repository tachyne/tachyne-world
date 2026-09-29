package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
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
		h := newTestHub(world.New(seed))
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

// up is the top face, the one a stroke from above strikes.
var up = blockPos{0, 1, 0}

// Ten strokes on the cooldown, then it gives up its contents and leaves plain
// sand. Fewer than ten leaves it standing.
func TestBrushingOpensTheCacheAfterTenStrokes(t *testing.T) {
	h, w := findWell(t)
	pos := blockPos{w.Sus[0][0], w.Sus[0][1], w.Sus[0][2]}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(pos.x), float64(pos.y), float64(pos.z)
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}

	for i := 0; i < 9; i++ {
		h.tick.Store(uint64(i) * brushCooldown)
		h.brushStroke(players, pl, pos, up)
	}
	if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); !ok {
		t.Fatal("nine strokes should not have opened it")
	}
	before := len(h.items)

	h.tick.Store(uint64(9) * brushCooldown)
	h.brushStroke(players, pl, pos, up)
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

// Strokes inside the block's cooldown do not count.
func TestBrushCooldownRateLimitsStrokes(t *testing.T) {
	h, w := findWell(t)
	pos := blockPos{w.Sus[0][0], w.Sus[0][1], w.Sus[0][2]}
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}

	h.tick.Store(100)
	for i := 0; i < 30; i++ { // same tick, thirty strokes
		h.brushStroke(players, pl, pos, up)
	}
	if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); !ok {
		t.Fatal("thirty strokes in one tick should not open a cache")
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

	for i := 0; i < 5; i++ {
		h.tick.Store(uint64(i) * brushCooldown)
		h.brushStroke(players, pl, pos, up)
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
	h := newTestHub(world.New(7))
	pos := blockPos{2000, 100, 2000}
	h.world.SetBlock(pos.x, pos.y, pos.z, suspiciousSandBase)
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{pl.p.eid: pl}

	before := len(h.items)
	for i := 0; i < 10; i++ {
		h.tick.Store(uint64(i) * brushCooldown)
		h.brushStroke(players, pl, pos, up)
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

// BrushableBlock.getBrushSound: suspicious gravel sounds like gravel, not
// sand, and the brusher's own client plays it (Level.playSound(player, …)),
// so only the others are sent it.
func TestBrushingGravelSoundsLikeGravel(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pos := blockPos{2, 180, 2}
	h.world.SetBlock(pos.x, pos.y, pos.z, suspiciousGravelBase)
	brusher, onlooker := testTracked(), testTracked()
	onlooker.p = newPlayer(2, "onlooker", [16]byte{2})
	for _, p := range []*tracked{brusher, onlooker} {
		p.x, p.y, p.z = 2.5, 181, 3.5
	}
	brusher.p.setHotbarSlot(0, itemBrush)
	players := map[int32]*tracked{brusher.p.eid: brusher, onlooker.p.eid: onlooker}
	drainEvents(brusher)
	drainEvents(onlooker)
	h.brushStroke(players, brusher, pos, up)
	heard := func(pl *tracked) string {
		for len(pl.p.out) > 0 {
			if s, ok := (<-pl.p.out).ev.(attachproto.Sound); ok && strings.Contains(s.Name, "brush") {
				return s.Name
			}
		}
		return ""
	}
	if got := heard(onlooker); got != "minecraft:item.brush.brushing.gravel" {
		t.Errorf("an onlooker hears %q, want the gravel brushing", got)
	}
	if got := heard(brusher); got != "" {
		t.Errorf("the brusher's client plays its own sound; the server sent %q", got)
	}
}

// brushFixture is a brusher standing on the block below them and looking
// straight down at it, with an onlooker beside them.
func brushFixture(t *testing.T, state uint32) (*hub, map[int32]*tracked, *tracked, *tracked, blockPos) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pos := blockPos{2, 180, 2}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	brusher, onlooker := testTracked(), testTracked()
	onlooker.p = newPlayer(2, "onlooker", [16]byte{2})
	brusher.x, brusher.y, brusher.z = 2.5, 181, 2.5
	brusher.pitch = 90
	onlooker.x, onlooker.y, onlooker.z = 4.5, 181, 4.5
	brusher.p.setHotbarSlot(0, itemBrush)
	brusher.inv.slots[0] = invStack{item: itemBrush, count: 1}
	players := map[int32]*tracked{brusher.p.eid: brusher, onlooker.p.eid: onlooker}
	h.playersRef = players
	h.tick.Store(1000)
	return h, players, brusher, onlooker, pos
}

// BrushItem is a held use: one click, then a stroke on the tick before each
// backswing (the fifth of every ten), and the tenth stroke — on the
// ninety-fifth tick — opens the block. Clicking again does not stroke.
func TestBrushHeldUseStrokesEveryTenTicks(t *testing.T) {
	h, players, pl, _, pos := brushFixture(t, suspiciousSandBase)
	h.useOnEvent(players, evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1})
	if pl.brushFrom == 0 {
		t.Fatal("a click with the brush should start the held use")
	}
	if b := h.brushes[pos]; b != nil && b.count != 0 {
		t.Fatalf("the click itself stroked: count %d", b.count)
	}
	var strokes []int
	for tick := 1; tick <= 95; tick++ {
		h.tick.Add(1)
		was := 0
		if b := h.brushes[pos]; b != nil {
			was = b.count
		}
		h.tickBrushing(players)
		if b := h.brushes[pos]; (b != nil && b.count > was) || (b == nil && was > 0) {
			strokes = append(strokes, tick)
		}
		if tick < 95 {
			if _, ok := suspiciousTurnsInto(h.world.At(pos.x, pos.y, pos.z)); !ok {
				t.Fatalf("the block opened early, on tick %d", tick)
			}
		}
	}
	want := []int{5, 15, 25, 35, 45, 55, 65, 75, 85, 95}
	if len(strokes) != len(want) {
		t.Fatalf("strokes on ticks %v, want %v", strokes, want)
	}
	for i := range want {
		if strokes[i] != want[i] {
			t.Fatalf("strokes on ticks %v, want %v", strokes, want)
		}
	}
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Sand {
		t.Fatalf("the tenth stroke should leave sand, got %d", h.world.At(pos.x, pos.y, pos.z))
	}
}

// Letting go of the button (release_use_item) ends the use: no more strokes.
func TestBrushReleaseStopsStrokes(t *testing.T) {
	h, players, pl, _, pos := brushFixture(t, suspiciousSandBase)
	h.useOnEvent(players, evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1})
	for i := 0; i < 6; i++ {
		h.tick.Add(1)
		h.tickBrushing(players)
	}
	if b := h.brushes[pos]; b == nil || b.count != 1 {
		t.Fatalf("one stroke by the sixth tick, have %+v", b)
	}
	stopBrushing(pl) // what evStopEat does on release
	for i := 0; i < 30; i++ {
		h.tick.Add(1)
		h.tickBrushing(players)
	}
	if b := h.brushes[pos]; b == nil || b.count != 1 {
		t.Fatalf("a released brush kept stroking: %+v", b)
	}
}

// Looking away from every block ends the use (releaseUsingItem).
func TestBrushLookingAwayEndsTheUse(t *testing.T) {
	h, players, pl, _, pos := brushFixture(t, suspiciousSandBase)
	h.useOnEvent(players, evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1})
	pl.pitch = -90 // up at the open sky
	h.tick.Add(1)
	h.tickBrushing(players)
	if pl.brushFrom != 0 {
		t.Fatal("a brush looking at nothing should stop")
	}
}

// Brushing a block that is not suspicious still strokes: the others hear the
// generic brushing sound, and nothing about the block changes.
func TestBrushingOrdinaryBlockSoundsGeneric(t *testing.T) {
	h, players, pl, onlooker, pos := brushFixture(t, worldgen.BlockBase("stone"))
	h.useOnEvent(players, evBrush{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, dy: 1})
	drainEvents(onlooker)
	heard := ""
	for i := 0; i < 5; i++ {
		h.tick.Add(1)
		h.tickBrushing(players)
	}
	for len(onlooker.p.out) > 0 {
		if s, ok := (<-onlooker.p.out).ev.(attachproto.Sound); ok && strings.Contains(s.Name, "brush") {
			heard = s.Name
		}
	}
	if heard != "minecraft:item.brush.brushing.generic" {
		t.Fatalf("an onlooker hears %q, want the generic brushing", heard)
	}
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.BlockBase("stone") {
		t.Fatal("brushing stone changed it")
	}
}
