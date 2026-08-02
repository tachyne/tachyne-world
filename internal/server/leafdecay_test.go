package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Leaf decay by DISTANCE — LeavesBlock's rule, replacing a radius-4 box scan
// for any log. The box was wrong in both directions, and each direction gets
// its test here.

// leafAt writes an oak leaf at a given distance state.
func leafAt(w *world.World, x, y, z, d int) {
	base, _ := worldgen.BlockRange("oak_leaves")
	w.SetBlock(x, y, z, base+uint32((d-1)*4)+3) // persistent=false, waterlogged=false
}

// A chain of six leaves from a log survives entirely: the far end is distance
// 6 in vanilla but sat OUTSIDE the old radius-4 box, so it used to rot.
func TestLeafChainWithinSixSurvives(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 500, 200, 500
	h.world.SetBlock(x, y, z, worldgen.OakLog)
	for i := 1; i <= 6; i++ {
		leafAt(h.world, x+i, y, z, 7) // stale 7s, as an old world would carry
	}
	for i := 1; i <= 6; i++ {
		s := h.world.At(x+i, y, z)
		h.tickLeaf(players, 0, x+i, y, z, s)
		if !isAnyLeaf(h.world.At(x+i, y, z)) {
			t.Fatalf("leaf %d of a 6-chain rotted — the box scan is back", i)
		}
	}
}

// The seventh leaf out is past vanilla's reach and rots — even though the old
// box would have needed only a log within 4 of IT, not a connected path.
func TestSeventhLeafDecays(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 540, 200, 540
	h.world.SetBlock(x, y, z, worldgen.OakLog)
	for i := 1; i <= 7; i++ {
		leafAt(h.world, x+i, y, z, 7)
	}
	s := h.world.At(x+7, y, z)
	h.tickLeaf(players, 0, x+7, y, z, s)
	if isAnyLeaf(h.world.At(x+7, y, z)) {
		t.Fatal("the seventh leaf from a trunk should decay")
	}
}

// The other direction the box got wrong: a leaf NEAR a log but with no leaf
// path to it decays in vanilla — proximity through air is not attachment.
func TestLeafNearButNotConnectedDecays(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 580, 200, 580
	h.world.SetBlock(x, y, z, worldgen.OakLog)
	leafAt(h.world, x+3, y, z, 7) // 3 away through AIR — inside the old box
	s := h.world.At(x+3, y, z)
	h.tickLeaf(players, 0, x+3, y, z, s)
	if isAnyLeaf(h.world.At(x+3, y, z)) {
		t.Fatal("a leaf with no leaf path to any trunk should decay, however near the log")
	}
}

// Stripped logs and wood hold a canopy up — the LOGS tag, not just plain logs.
// A player who strips their tree's trunk must not watch the canopy rot.
func TestStrippedLogHoldsLeaves(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 620, 200, 620
	lo, _ := worldgen.BlockRange("stripped_oak_log")
	h.world.SetBlock(x, y, z, lo+1)
	leafAt(h.world, x+1, y, z, 7)
	s := h.world.At(x+1, y, z)
	h.tickLeaf(players, 0, x+1, y, z, s)
	if !isAnyLeaf(h.world.At(x+1, y, z)) {
		t.Fatal("a leaf on a stripped log rotted — the trunk set must be the LOGS tag")
	}
}

// Felling: remove the trunk and the recompute wave carries the distances up to
// 7, after which random ticks rot the canopy. This is the "degrade the same"
// behaviour — the canopy dies because its distances RISE, not because a box
// stopped finding a log.
func TestFellingSendsTheDecayWave(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 660, 200, 660
	h.world.SetBlock(x, y, z, worldgen.OakLog)
	for i := 1; i <= 3; i++ {
		leafAt(h.world, x+i, y, z, i) // correctly seeded 1,2,3
	}
	// Fell the trunk, then run the recompute the sim would run, sweeping the
	// chain until it settles (each write schedules its neighbours in game).
	h.world.SetBlock(x, y, z, worldgen.Air)
	for pass := 0; pass < 8; pass++ {
		for i := 1; i <= 3; i++ {
			h.updateLeafDistance(players, 0, x+i, y, z, h.world.At(x+i, y, z))
		}
	}
	for i := 1; i <= 3; i++ {
		_, d, _, ok := leafInfo(h.world.At(x+i, y, z))
		if !ok || d != 7 {
			t.Fatalf("leaf %d should have recomputed to 7 after felling, got distance %d", i, d)
		}
		h.tickLeaf(players, 0, x+i, y, z, h.world.At(x+i, y, z))
		if isAnyLeaf(h.world.At(x+i, y, z)) {
			t.Fatalf("leaf %d survived its random tick at distance 7", i)
		}
	}
}

// The stale-world guard: a distance-7 leaf that is genuinely within reach is
// HEALED to its true distance rather than rotted. Every canopy stamped before
// this port is in that state, so this is what keeps the live world's forests
// standing through the deploy.
func TestStaleSevenLeafHealsInsteadOfRotting(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 700, 200, 700
	h.world.SetBlock(x, y, z, worldgen.OakLog)
	leafAt(h.world, x+1, y+1, z, 7) // diagonal-adjacent? no — orthogonal path via x+1,y
	leafAt(h.world, x+1, y, z, 7)
	h.tickLeaf(players, 0, x+1, y+1, z, h.world.At(x+1, y+1, z))
	got := h.world.At(x+1, y+1, z)
	if !isAnyLeaf(got) {
		t.Fatal("a stale-7 leaf two steps from a trunk rotted — the live world's canopies would too")
	}
	if _, d, _, _ := leafInfo(got); d != 2 {
		t.Fatalf("healing should write the true distance 2, got %d", d)
	}
}
