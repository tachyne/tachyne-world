package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// newTestHub is newHub for a test: its one world stands in for every
// dimension in the table, so a fixture built there is found whichever
// dimension a player or mob is in, while only the overworld counts as run
// (dims.has) — the dragon fight, portal travel and the per-dimension passes
// stay off until a test registers a world of its own with h.dims.set.
func newTestHub(w *world.World) *hub {
	h := newHub(w)
	h.dims.standIn = w
	return h
}

// The table answers by id: the overworld is the hub's world, a registered
// dimension its own, and an id outside the table nothing at all — never the
// overworld, which is where an unknown dimension used to land.
func TestWorldForUnknownDimensionIsNil(t *testing.T) {
	ow, nw := world.New(1), world.New(2)
	h := newHub(ow)
	h.dims.set(dimNether, nw)
	if h.worldFor(dimOverworld) != ow || h.worldFor(dimNether) != nw {
		t.Fatal("worldFor does not answer the registered worlds")
	}
	if h.worldFor(dimEnd) != nil {
		t.Error("an unregistered dimension answered with a world")
	}
	for _, id := range []int{-1, len(world.Dimensions), 99} {
		if h.worldFor(id) != nil {
			t.Errorf("worldFor(%d) = a world, want nil", id)
		}
	}
	// A test hub's stand-in answers for a known id only.
	th := newTestHub(ow)
	if th.worldFor(dimEnd) != ow || th.dims.has(dimEnd) {
		t.Error("the stand-in should serve the End without registering it")
	}
	if th.worldFor(len(world.Dimensions)) != nil {
		t.Error("the stand-in answered for an id outside the table")
	}
}

// allDims walks every run dimension once, overworld first; a world
// registered under a second id is not walked twice.
func TestAllDimsVisitsEachWorldOnce(t *testing.T) {
	ow, ew := world.New(1), world.New(3)
	h := newHub(ow)
	h.dims.set(dimNether, ow) // aliased, as a test may
	h.dims.set(dimEnd, ew)
	var got []int
	for dim := range h.allDims() {
		got = append(got, dim)
	}
	if len(got) != 2 || got[0] != dimOverworld || got[1] != dimEnd {
		t.Errorf("allDims = %v, want [0 2]", got)
	}
}

// The table keeps the ids, keys and save files the engine has always used:
// saves and the attach protocol carry these.
func TestDimensionTableIsStable(t *testing.T) {
	want := []struct {
		id          int
		key, file   string
		scale       float64
		bed, anchor bool
	}{
		{dimOverworld, "minecraft:overworld", "world.gob", 1, true, false},
		{dimNether, "minecraft:the_nether", "nether.gob", 8, false, true},
		{dimEnd, "minecraft:the_end", "end.gob", 1, false, false},
	}
	for _, w := range want {
		d := world.Dimension(w.id)
		if d == nil || d.ID != w.id || d.Key != w.key || d.File != w.file || d.CoordinateScale != w.scale {
			t.Fatalf("dimension %d = %+v", w.id, d)
		}
		if id, ok := parseDimension(w.key); !ok || id != w.id {
			t.Errorf("parseDimension(%q) = %d, %v", w.key, id, ok)
		}
		if dimRegistryName(w.id) != w.key {
			t.Errorf("dimRegistryName(%d) = %q", w.id, dimRegistryName(w.id))
		}
	}
}
