package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func TestBreakDoorRemovesBothHalves(t *testing.T) {
	s := &Server{world: world.New(1)}
	s.hub = newHub(s.world)
	p := newPlayer(1, "tester", [16]byte{})

	oakDoor := worldgen.BlockID("oak_door")
	info, _ := worldgen.OrientInfo(oakDoor)
	lower := worldgen.SetProperty(info, oakDoor, "half", "lower")
	upper := worldgen.SetProperty(info, lower, "half", "upper")
	s.world.SetBlock(5, 70, 5, lower)
	s.world.SetBlock(5, 71, 5, upper)

	s.breakPairedHalf(p, 5, 70, 5, lower) // break the bottom
	if got := s.world.Block(5, 71, 5); got != worldgen.Air {
		t.Errorf("breaking the lower door half should remove the upper, got %d", got)
	}
}

func TestSanitizeNBT(t *testing.T) {
	// An emoji (supplementary plane) must not survive raw — it breaks NBT decode.
	got := sanitizeNBT("hi 😀 there\x00!")
	for _, r := range got {
		if r == 0 || r > 0xFFFF {
			t.Fatalf("sanitizeNBT left an unsafe rune %U in %q", r, got)
		}
	}
	if got == "" {
		t.Fatal("sanitizeNBT dropped everything")
	}
}

func TestHeldItemMirror(t *testing.T) {
	// Survival held item comes from the hub mirroring the inventory into the hotbar.
	p := newPlayer(1, "tester", [16]byte{})
	if p.heldItem() != 0 {
		t.Fatal("fresh hotbar should be empty")
	}
	p.setHotbarSlot(0, itemByName["wheat_seeds"]) // wheat seeds, as the hub would after a pickup
	p.held = 0
	if p.heldItem() != itemByName["wheat_seeds"] {
		t.Errorf("held item should mirror the hotbar slot, got %d", p.heldItem())
	}
}

func TestDoorBedDetection(t *testing.T) {
	oakDoorDefault, redBedDefault, stoneState := worldgen.BlockID("oak_door"), worldgen.BlockID("red_bed"), worldgen.BlockID("stone")
	if info, ok := worldgen.OrientInfo(oakDoorDefault); !ok || !isTwoTall(info) {
		t.Errorf("oak_door should be detected as two-tall (ok=%v)", ok)
	}
	if info, ok := worldgen.OrientInfo(redBedDefault); !ok || !isBed(info) {
		t.Errorf("red_bed should be detected as a bed (ok=%v)", ok)
	}
	if info, ok := worldgen.InfoForState(stoneState); ok && (isTwoTall(info) || isBed(info)) {
		t.Error("stone is neither a door nor a bed")
	}
}

func TestDoorOpenToggle(t *testing.T) {
	oakDoorDefault := worldgen.BlockID("oak_door")
	info, ok := worldgen.OrientInfo(oakDoorDefault)
	if !ok {
		t.Fatal("no oak_door info")
	}
	opened := worldgen.SetProperty(info, oakDoorDefault, "open", "true")
	if worldgen.GetProperty(info, opened, "open") != "true" {
		t.Errorf("open should round-trip to true, got %q", worldgen.GetProperty(info, opened, "open"))
	}
	// Toggling preserves the half (it's still the same door block).
	if worldgen.GetProperty(info, opened, "half") != worldgen.GetProperty(info, oakDoorDefault, "half") {
		t.Error("toggling open should not change the door half")
	}
}
