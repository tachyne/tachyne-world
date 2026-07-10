package server

import (
	"testing"

	"tachyne/internal/worldgen"
)

func TestRailShapesOnPlacement(t *testing.T) {
	h, w, _, x, y, z := redSetup(t)
	// Lone rail follows the player's look axis (east-west).
	got := h.placeRailShape(x, y, z, uint32(railMin+1), 90) // yaw 90 → west
	if railShape(got) != shapeEW {
		t.Fatalf("lone rail should lie along the look axis, shape %d", railShape(got))
	}
	// A rail to the east bends a north-look placement into east-west.
	w.SetBlock(x+1, y, z, railMin+1)
	got = h.placeRailShape(x, y, z, uint32(railMin+1), 180) // looking north
	if railShape(got) != shapeEW {
		t.Fatalf("neighbour east should force EW, got %d", railShape(got))
	}
	// Rails east + south → south_east corner.
	w.SetBlock(x, y, z+1, railMin+1)
	got = h.placeRailShape(x, y, z, uint32(railMin+1), 0)
	if railShape(got) != shapeSE {
		t.Fatalf("east+south neighbours should corner SE, got %d", railShape(got))
	}
	// Rail one block up to the east → ascending east.
	w.SetBlock(x, y, z+1, worldgen.Air)
	w.SetBlock(x+1, y, z, worldgen.Air)
	w.SetBlock(x+1, y+1, z, railMin+1)
	got = h.placeRailShape(x, y, z, uint32(railMin+1), 0)
	if railShape(got) != shapeAscE {
		t.Fatalf("raised east neighbour should ascend east, got %d", railShape(got))
	}
}

func TestPoweredRailSyncsWithRedstone(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, railWith(poweredRailMin, shapeEW, false))
	lever := setBoolProp(uint32((worldgen.BlockBase("lever") + 9)), "powered", false)
	w.SetBlock(x, y, z-1, lever)
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 4)
	if !railPowered(w.At(x, y, z)) {
		t.Fatalf("powered rail should light from the lever: %d", w.At(x, y, z))
	}
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 6)
	if railPowered(w.At(x, y, z)) {
		t.Fatal("powered rail should drop with the lever")
	}
	if railShape(w.At(x, y, z)) != shapeEW {
		t.Fatal("power syncs must preserve the shape")
	}
}

func TestCornerDegradesOnSpecialRail(t *testing.T) {
	if got := railWith(poweredRailMin, shapeSE, true); railShape(got) != shapeEW || !railPowered(got) {
		t.Fatalf("special rails cannot corner: %d (shape %d)", got, railShape(got))
	}
}
