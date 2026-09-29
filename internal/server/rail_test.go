package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BaseRailBlock.getStateForPlacement lays a rail along the placer's
// facing; onPlace (RailState.place) then connects it, and each rail it
// connects to turns to meet it.
func TestRailShapesOnPlacement(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	plain := uint32(railMin + 1)
	place := func(px, py, pz int, yaw float32) uint32 {
		h.setBlockAt(players, 0, blockPos{px, py, pz}, h.placeRailShape(w, px, py, pz, plain, yaw))
		return w.At(px, py, pz)
	}
	// A lone rail follows the look axis.
	if got := place(x, y, z, 90); railShape(got) != shapeEW { // yaw 90 → west
		t.Fatalf("lone rail should lie along the look axis, shape %d", railShape(got))
	}
	h.setBlockAt(players, 0, blockPos{x, y, z}, worldgen.Air)
	// A north-south rail east of a new one turns east-west to meet it, and
	// the new one lies east-west too.
	w.SetBlock(x+1, y, z, railWith(plain, shapeNS, false))
	if got := place(x, y, z, 180); railShape(got) != shapeEW {
		t.Fatalf("a rail east of it makes it east-west, got %d", railShape(got))
	}
	if railShape(w.At(x+1, y, z)) != shapeEW {
		t.Fatalf("the rail east of it turns to meet it: shape %d", railShape(w.At(x+1, y, z)))
	}
	// Rails east and south of a new plain rail: a south-east corner.
	h.setBlockAt(players, 0, blockPos{x, y, z}, worldgen.Air)
	w.SetBlock(x+1, y, z, railWith(plain, shapeEW, false))
	w.SetBlock(x, y, z+1, railWith(plain, shapeNS, false))
	if got := place(x, y, z, 0); railShape(got) != shapeSE {
		t.Fatalf("east+south neighbours should corner SE, got %d", railShape(got))
	}
	// A rail one block up to the east: the new one climbs east.
	for _, p := range []blockPos{{x, y, z}, {x + 1, y, z}, {x, y, z + 1}} {
		w.SetBlock(p.x, p.y, p.z, worldgen.Air)
	}
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x+1, y+1, z, railWith(plain, shapeEW, false))
	if got := place(x, y, z, 0); railShape(got) != shapeAscE {
		t.Fatalf("raised east neighbour should ascend east, got %d", railShape(got))
	}
	// BaseRailBlock.shouldBeRemoved: the slope loses the block it climbs
	// onto, and the rail breaks.
	h.setBlockAt(players, 0, blockPos{x + 1, y + 1, z}, worldgen.Air)
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Air)
	h.notifyAround(players, 0, blockPos{x + 1, y, z})
	if isAnyRail(w.At(x, y, z)) {
		t.Fatal("an ascending rail whose high side lost its block breaks")
	}
}

// PoweredRailBlock.findPoweredRailSignal: power runs along a line of powered
// rails, eight beyond the one a signal reaches.
func TestPoweredRailPassesPowerEightAlong(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	for i := 0; i < 11; i++ {
		w.SetBlock(x+i, y-1, z, worldgen.Stone)
		w.SetBlock(x+i, y, z, railWith(poweredRailMin, shapeEW, false))
	}
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.notifyAround(players, 0, blockPos{x, y, z - 1})
	stepTicks(h, players, 3)
	for i := 0; i <= 8; i++ {
		if !railPowered(w.At(x+i, y, z)) {
			t.Fatalf("rail %d along the line should be powered", i)
		}
	}
	if railPowered(w.At(x+9, y, z)) {
		t.Fatal("the ninth rail past the source is out of reach")
	}
}

func TestPoweredRailSyncsWithRedstone(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, railWith(poweredRailMin, shapeEW, false))
	lever := withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "floor", "powered": "false"})
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
