package server

import (
	"math"
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func findStronghold(w *world.World) (worldgen.Stronghold, bool) {
	for x := -8000; x <= 8000; x += 1536 {
		for z := -8000; z <= 8000; z += 1536 {
			if st := w.Gen().StrongholdIn(x, z); st.Exists {
				return st, true
			}
		}
	}
	return worldgen.Stronghold{}, false
}

func TestStrongholdGeneratesWithFrames(t *testing.T) {
	w := world.New(7)
	st, ok := findStronghold(w)
	if !ok {
		t.Fatal("no stronghold in range")
	}
	if math.Hypot(float64(st.X), float64(st.Z)) < 500 {
		t.Fatalf("stronghold too close to spawn: (%d,%d)", st.X, st.Z)
	}
	frames := 0
	for _, f := range st.FramePositions(w.Seed()) {
		if isEndFrame(w.At(f.X, f.Y, f.Z)) {
			frames++
		}
	}
	if frames != 12 {
		t.Fatalf("want 12 generated frames, got %d", frames)
	}
	// The interior must NOT already be portal.
	if w.At(st.X, st.Y, st.Z) == worldgen.EndPortalBlock {
		t.Fatal("portal must not pre-open")
	}
}

func TestTwelveEyesOpenThePortal(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	st, ok := findStronghold(w)
	if !ok {
		t.Skip("no stronghold")
	}
	pl := testTracked()
	pl.gamemode = gmCreative
	pl.x, pl.y, pl.z = float64(st.X), float64(st.Y), float64(st.Z)
	players := map[int32]*tracked{1: pl}
	for _, f := range st.FramePositions(w.Seed()) {
		if s := w.At(f.X, f.Y, f.Z); !frameHasEye(s) {
			h.insertEye(players, pl, blockPos{f.X, f.Y, f.Z}, s)
		}
	}
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if w.At(st.X+dx, st.Y, st.Z+dz) != worldgen.EndPortalBlock {
				t.Fatalf("portal interior not filled at (%d,%d)", dx, dz)
			}
		}
	}
}

func TestEndPortalContactFlagsTravel(t *testing.T) {
	ow := world.New(7)
	ew, _ := world.NewEnd(7, nil)
	h := newHub(ow)
	h.end = ew
	pl := testTracked()
	pl.x, pl.y, pl.z = 20.5, 30, 20.5
	ow.SetBlock(20, 30, 20, worldgen.EndPortalBlock)
	players := map[int32]*tracked{1: pl}
	h.updateEndPortalContact(players)
	if pl.p.pendingDim.Load() != 2 {
		t.Fatalf("end portal should flag dim 2, got %d", pl.p.pendingDim.Load())
	}
	// The End's exit portal goes home.
	pl.p.pendingDim.Store(-1)
	pl.dim = 2
	ew.SetBlock(20, 30, 20, worldgen.EndPortalBlock)
	h.updateEndPortalContact(players)
	if pl.p.pendingDim.Load() != 0 {
		t.Fatalf("End exit portal should flag dim 0, got %d", pl.p.pendingDim.Load())
	}
}

func TestPlayerBuiltEndPortalOpens(t *testing.T) {
	w := world.New(7)
	h := newHub(w)
	pl := testTracked()
	pl.gamemode = gmCreative
	players := map[int32]*tracked{1: pl}
	y := 80
	// Build the 12-frame ring by hand somewhere no stronghold lives.
	bx, bz := 500, 500
	for _, d := range endRingOffsets {
		h.world.SetBlock(bx+d[0], y, bz+d[1], worldgen.EndPortalFrame+4) // eye=false
	}
	// Insert an eye into each frame; the twelfth must open the portal.
	for _, d := range endRingOffsets {
		pos := blockPos{bx + d[0], y, bz + d[1]}
		h.insertEye(players, pl, pos, h.world.At(pos.x, pos.y, pos.z))
	}
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if got := h.world.At(bx+dx, y, bz+dz); got != worldgen.EndPortalBlock {
				t.Fatalf("interior (%d,%d) = %d, want end portal", dx, dz, got)
			}
		}
	}
}
