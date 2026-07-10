package server

import (
	"encoding/binary"
	"tachyne/internal/worldgen"
	"testing"

	"tachyne/internal/world"
)

func TestBlockFaceOffset(t *testing.T) {
	cases := []struct {
		dir        int32
		dx, dy, dz int
	}{
		{0, 0, -1, 0}, {1, 0, 1, 0}, {2, 0, 0, -1},
		{3, 0, 0, 1}, {4, -1, 0, 0}, {5, 1, 0, 0},
	}
	for _, c := range cases {
		if dx, dy, dz := blockFaceOffset(c.dir); dx != c.dx || dy != c.dy || dz != c.dz {
			t.Errorf("dir %d: got (%d,%d,%d), want (%d,%d,%d)", c.dir, dx, dy, dz, c.dx, c.dy, c.dz)
		}
	}
}

func TestHeldItem(t *testing.T) {
	p := newPlayer(1, "x", [16]byte{})
	data := binary.BigEndian.AppendUint16(nil, 3) // select hotbar slot 3
	p.handleHeldItem(data)
	if p.held != 3 {
		t.Errorf("held = %d, want 3", p.held)
	}
}

func TestCreativeSlotTracking(t *testing.T) {
	p := newPlayer(1, "x", [16]byte{})
	srv := &Server{hub: newHub(world.New(1)), modes: newModeStore("", gmCreative)}

	// Put item id 5 in window slot 36 (= hotbar slot 0).
	srv.applyCreativeSlot(p, 36, 5, 1)
	if p.hotbar[0] != 5 {
		t.Errorf("hotbar[0] = %d, want 5", p.hotbar[0])
	}

	// An empty stack (count 0) in slot 44 (= hotbar slot 8) leaves it empty.
	srv.applyCreativeSlot(p, 44, 0, 0)
	if p.hotbar[8] != 0 {
		t.Errorf("hotbar[8] = %d, want 0 (empty)", p.hotbar[8])
	}

	// Non-hotbar slots (e.g. main inventory, 9) are ignored.
	srv.applyCreativeSlot(p, 9, 99, 1)
	for i, it := range p.hotbar {
		if i != 0 && it != 0 {
			t.Errorf("hotbar[%d] = %d, want unchanged", i, it)
		}
	}
}

func TestOrientState(t *testing.T) {
	// orientState/OrientInfo are keyed by the block's DEFAULT state, so inputs
	// use BlockID (default); expected states use BlockBase+offset.
	oakLog, logB := worldgen.BlockID("oak_log"), worldgen.BlockBase("oak_log")
	oakSlab, slabB := worldgen.BlockID("oak_slab"), worldgen.BlockBase("oak_slab")
	furnace, furnB := worldgen.BlockID("furnace"), worldgen.BlockBase("furnace")
	stone := worldgen.BlockBase("stone")
	cases := []struct {
		name         string
		state        uint32
		dir          int32
		cursorY, yaw float32
		want         uint32
	}{
		{"log on west face -> axis x", oakLog, 4, 0, 0, logB},      // axis x = base+0
		{"log on top face -> axis y", oakLog, 1, 0, 0, logB + 1},   // axis y
		{"log on south face -> axis z", oakLog, 3, 0, 0, logB + 2}, // axis z
		{"slab on top face -> bottom", oakSlab, 1, 0, 0, slabB + 3},
		{"slab on under face -> top", oakSlab, 0, 0, 0, slabB + 1},
		{"slab side, high cursor -> top", oakSlab, 2, 0.8, 0, slabB + 1},
		{"furnace faces player (yaw south)", furnace, 1, 0, 180, furnB + 3}, // opposite(north)=south
		{"non-orientable unchanged", stone, 4, 0, 0, stone},
	}
	for _, c := range cases {
		if got := orientState(c.state, c.dir, c.cursorY, c.yaw, 0); got != c.want {
			t.Errorf("%s: orientState = %d, want %d", c.name, got, c.want)
		}
	}
}
