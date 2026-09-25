package server

import (
	"math"
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The state pack/unpack has to round-trip, or every rule below reads the
// wrong block.
func TestDripstoneStateRoundTrips(t *testing.T) {
	for th := 0; th < 5; th++ {
		for _, up := range []bool{true, false} {
			for _, wet := range []bool{true, false} {
				st := dripstoneState(th, up, wet)
				gth, gup, gwet, ok := dripstoneParts(st)
				if !ok || gth != th || gup != up || gwet != wet {
					t.Fatalf("state(%d,%v,%v) = %d → (%d,%v,%v,%v)", th, up, wet, st, gth, gup, gwet, ok)
				}
			}
		}
	}
	if !isStalactite(dripstoneState(dripTip, false, false)) {
		t.Error("a down-pointing tip is a stalactite")
	}
	if isStalactite(dripstoneState(dripTip, true, false)) {
		t.Error("an up-pointing tip is not a stalactite")
	}
	if !isStalagmiteTip(dripstoneState(dripTip, true, false)) {
		t.Error("an up-pointing tip is a stalagmite tip")
	}
	if isStalagmiteTip(dripstoneState(2, true, false)) {
		t.Error("a thick section is not a tip")
	}
}

// A stalactite hanging off dripstone stone lengthens, or raises a stalagmite
// from the floor under it.
func TestStalactiteGrows(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	x, y, z := 0, 190, 0
	w.SetBlock(x, y+1, z, dripstoneBlock)
	w.SetBlock(x, y, z, dripstoneState(dripTip, false, false))
	w.SetBlock(x, y-6, z, worldgen.BlockBase("stone")) // a floor within reach

	grewDown, grewUp := false, false
	for i := 0; i < 200 && !(grewDown && grewUp); i++ {
		w.SetBlock(x, y, z, dripstoneState(dripTip, false, false))
		for cy := y - 5; cy < y; cy++ {
			w.SetBlock(x, cy, z, worldgen.Air)
		}
		h.growStalactite(players, 0, x, y, z)
		if isStalactite(w.At(x, y-1, z)) {
			grewDown = true
		}
		if isStalagmiteTip(w.At(x, y-5, z)) {
			grewUp = true
		}
	}
	if !grewDown {
		t.Error("a stalactite never lengthened")
	}
	if !grewUp {
		t.Error("a stalactite never raised a stalagmite off the floor")
	}

	// With no dripstone block above it, nothing grows at all.
	w.SetBlock(x, y+1, z, worldgen.Air)
	w.SetBlock(x, y, z, dripstoneState(dripTip, false, false))
	w.SetBlock(x, y-1, z, worldgen.Air)
	for i := 0; i < 50; i++ {
		h.growStalactite(players, 0, x, y, z)
	}
	if w.At(x, y-1, z) != worldgen.Air {
		t.Error("dripstone grew with nothing to hang off")
	}
}

// Water above a stalactite drips down and fills a cauldron under the tip.
func TestDripstoneFillsACauldron(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	x, y, z := 0, 190, 0
	w.SetBlock(x, y+2, z, worldgen.WaterBase)
	w.SetBlock(x, y+1, z, dripstoneBlock)
	w.SetBlock(x, y, z, dripstoneState(dripTip, false, false))
	w.SetBlock(x, y-4, z, cauldronState)

	for i := 0; i < 500; i++ {
		h.dripThroughStalactite(players, 0, x, y, z)
		if _, _, ok := cauldronOf(w.At(x, y-4, z)); ok && w.At(x, y-4, z) != cauldronState {
			break
		}
	}
	kind, _, _ := cauldronOf(w.At(x, y-4, z))
	if kind != cauldronWater {
		t.Fatalf("the cauldron never filled with water (kind %d)", kind)
	}

	// Lava above gives a lava cauldron instead.
	w.SetBlock(x, y+2, z, worldgen.LavaBase)
	w.SetBlock(x, y-4, z, cauldronState)
	for i := 0; i < 1000; i++ {
		h.dripThroughStalactite(players, 0, x, y, z)
		if w.At(x, y-4, z) == lavaCauldronState {
			break
		}
	}
	if w.At(x, y-4, z) != lavaCauldronState {
		t.Error("lava above a stalactite never filled the cauldron")
	}
}

// Landing on a stalagmite hurts more than landing on the floor — and hurts at
// heights the three-block grace would otherwise forgive.
func TestStalagmiteImpales(t *testing.T) {
	tip := dripstoneState(dripTip, true, false)
	if _, ok := stalagmiteFallExtra(worldgen.BlockBase("stone"), 10); ok {
		t.Error("plain stone should not impale")
	}
	short, ok := stalagmiteFallExtra(tip, 2)
	if !ok || short <= 0 {
		t.Errorf("a two-block fall onto a stalagmite should still hurt, got %v", short)
	}
	long, _ := stalagmiteFallExtra(tip, 10)
	if long <= 10-3 {
		t.Errorf("a stalagmite should hurt more than the same fall onto ground: %v", long)
	}
}

// A stalactite whose grip is broken comes down whole and skewers what is
// under it. The tip carries the column's length, with a floor of six, and
// deals that per block of the drop up to forty.
func TestFallingStalactiteSkewers(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	golem := h.spawnMob(players, entityIronGolem, float64(x)+0.5, float64(y), float64(z)+0.5)
	golem.health = 100
	tip := dripstoneState(dripTip, false, false)
	w.SetBlock(x, y+6, z, worldgen.BlockBase("dripstone_block"))
	w.SetBlock(x, y+5, z, tip) // a one-long spike: it hurts as if six long
	for dy := 0; dy <= 4; dy++ {
		w.SetBlock(x, y+dy, z, worldgen.Air)
	}
	h.setBlockAt(players, 0, blockPos{x, y + 6, z}, worldgen.Air)
	runTicks(h, players, h.tick.Load()+1, h.tick.Load()+60)
	// It comes down five blocks: ceil(5 - 1) = 4 blocks count, 6 each.
	if lost := 100 - float64(golem.health); math.Abs(lost-24) > 0.01 {
		t.Fatalf("the golem lost %v, want 24", lost)
	}
}

// It falls, and where it lands it cannot stand — nothing holds a stalactite
// up from below — so it breaks into a pointed dripstone item
// (FallingBlockEntity: canSurvive false → callOnBrokenAfterFall + drop).
func TestUnsupportedStalactiteFallsAndBreaks(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	tip := dripstoneState(dripTip, false, false) // pointing down
	w.SetBlock(x, y+6, z, worldgen.BlockBase("dripstone_block"))
	w.SetBlock(x, y+5, z, tip)
	for dy := 0; dy <= 4; dy++ {
		w.SetBlock(x, y+dy, z, worldgen.Air)
	}
	// Take its anchor away.
	h.setBlockAt(players, 0, blockPos{x, y + 6, z}, worldgen.Air)
	h.dropUnsupported(players, 0, blockPos{x, y + 6, z})
	runTicks(h, players, h.tick.Load(), h.tick.Load()+40)

	if w.At(x, y+5, z) == tip {
		t.Fatal("the stalactite should have let go")
	}
	for dy := 0; dy <= 5; dy++ {
		if w.At(x, y+dy, z) == tip {
			t.Fatalf("the fallen stalactite stayed a block at y+%d", dy)
		}
	}
	dropped := false
	for _, it := range h.items {
		if it.item == int32(itemByName["pointed_dripstone"]) {
			dropped = true
		}
	}
	if !dropped {
		t.Fatal("it should have broken into a pointed dripstone item")
	}
}

// Two stalactites side by side, both let go in the same tick: each is still
// in place (it falls on a later tick), and each is the other's neighbour. The
// support sweep once re-queued them after each other forever and stalled the
// hub until the liveness probe killed the pod (2026-09-23).
func TestSideBySideStalactitesLetGoWithoutStallingTheSweep(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	tip := dripstoneState(dripTip, false, false)
	for dx := 0; dx <= 1; dx++ {
		w.SetBlock(x+dx, y+6, z, worldgen.BlockBase("dripstone_block"))
		w.SetBlock(x+dx, y+5, z, tip)
		for dy := 0; dy <= 4; dy++ {
			w.SetBlock(x+dx, y+dy, z, worldgen.Air)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.setBlockAt(players, 0, blockPos{x, y + 6, z}, worldgen.Air)
		h.setBlockAt(players, 0, blockPos{x + 1, y + 6, z}, worldgen.Air)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the support sweep never finished")
	}
	runTicks(h, players, h.tick.Load(), h.tick.Load()+40)
	for dx := 0; dx <= 1; dx++ {
		if w.At(x+dx, y+5, z) == tip {
			t.Errorf("stalactite %d should have let go", dx)
		}
	}
}

// Pointed dripstone placed under a ceiling while looking up hangs tip-down;
// a second piece under it becomes the tip and the first re-shapes to a
// frustum (SpeleothemBlock.getStateForPlacement / updateShape).
func TestPlacedDripstoneShapesItsColumn(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	w := h.world
	def := worldgen.BlockBase("pointed_dripstone")
	info, _ := worldgen.InfoForState(def)
	x, z := 5, 5
	w.SetBlock(x, 190, z, worldgen.Stone) // the ceiling
	first, ok := speleothemPlaced(w, blockPos{x, 189, z}, def, -30, false, false)
	if !ok || worldgen.GetProperty(info, first, "vertical_direction") != "down" || worldgen.GetProperty(info, first, "thickness") != "tip" {
		t.Fatalf("first piece: ok=%v dir=%s thickness=%s", ok, worldgen.GetProperty(info, first, "vertical_direction"), worldgen.GetProperty(info, first, "thickness"))
	}
	h.setBlockAt(players, 0, blockPos{x, 189, z}, first)
	second, ok := speleothemPlaced(w, blockPos{x, 188, z}, def, -30, false, false)
	if !ok || worldgen.GetProperty(info, second, "thickness") != "tip" {
		t.Fatalf("second piece: ok=%v thickness=%s", ok, worldgen.GetProperty(info, second, "thickness"))
	}
	h.setBlockAt(players, 0, blockPos{x, 188, z}, second)
	if th := worldgen.GetProperty(info, w.At(x, 189, z), "thickness"); th != "frustum" {
		t.Fatalf("the first piece above the new tip is %s, want frustum", th)
	}
	// Nothing to hang from or stand on: refused.
	if _, ok := speleothemPlaced(w, blockPos{20, 220, 20}, def, 30, false, false); ok {
		t.Error("dripstone placed in mid-air")
	}
}
