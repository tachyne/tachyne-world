package server

import (
	"testing"

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
