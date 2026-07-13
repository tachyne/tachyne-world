package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestCocoaGrowth(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	info, ok := worldgen.InfoForState(cocoaBase)
	if !ok {
		t.Fatal("no block info for cocoa")
	}
	onHub(t, h, func() {
		start := cocoaBase + 3 // a non-default facing, age 0
		if a := worldgen.GetProperty(info, start, "age"); a != "0" {
			t.Fatalf("cocoa start age %q, want 0", a)
		}
		wantFacing := worldgen.GetProperty(info, start, "facing")
		w.SetBlock(5, 70, 0, start)
		for i := 0; i < 500 && worldgen.GetProperty(info, w.At(5, 70, 0), "age") != "2"; i++ {
			h.tickCocoa(h.playersRef, 5, 70, 0, w.At(5, 70, 0))
		}
		final := w.At(5, 70, 0)
		if a := worldgen.GetProperty(info, final, "age"); a != "2" {
			t.Errorf("cocoa never ripened (age %q)", a)
		}
		if f := worldgen.GetProperty(info, final, "facing"); f != wantFacing {
			t.Errorf("cocoa facing changed %q→%q", wantFacing, f)
		}

		// Bone meal advances a pod one stage.
		w.SetBlock(6, 70, 0, cocoaBase) // age 0
		if !h.applyBoneMeal(h.playersRef, 0, 6, 70, 0, cocoaBase) {
			t.Error("bone meal did nothing to cocoa")
		}
		if a := worldgen.GetProperty(info, w.At(6, 70, 0), "age"); a != "1" {
			t.Errorf("cocoa bone-meal age %q, want 1", a)
		}
	})
}

func TestSweetBerryGrowth(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		// High, open column so the bush is sky-lit.
		for ay := 121; ay <= 130; ay++ {
			w.SetBlock(5, ay, 0, worldgen.Air)
		}
		w.SetBlock(5, 120, 0, berryBase) // age 0
		for i := 0; i < 500 && w.At(5, 120, 0) != berryBase+3; i++ {
			h.tickBerry(h.playersRef, 5, 120, 0, w.At(5, 120, 0))
		}
		if got := w.At(5, 120, 0); got != berryBase+3 {
			t.Errorf("berry age %d, want 3 (never fully ripened)", got-berryBase)
		}

		// Bone meal advances a bush one stage.
		w.SetBlock(6, 120, 0, berryBase+1)
		if !h.applyBoneMeal(h.playersRef, 0, 6, 120, 0, berryBase+1) {
			t.Error("bone meal did nothing to berry bush")
		}
		if got := w.At(6, 120, 0); got != berryBase+2 {
			t.Errorf("berry bone-meal age %d, want 2", got-berryBase)
		}
	})
}
