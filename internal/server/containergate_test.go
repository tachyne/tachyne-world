package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A chest with a solid block on top will not open — no menu, no lid, no sound
// — and a barrel in the same spot opens fine.
func TestChestBlockedFromAbove(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	x, z := 4, 4
	y := h.world.SurfaceFeet(x, z)
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x, y, z}, worldgen.BlockBase("chest"))
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x, y + 1, z}, worldgen.BlockBase("stone"))
	h.openChest(pl, x, y, z)
	if pl.winKind == winChest {
		t.Fatal("a chest under a solid block must not open")
	}
	// Clear the headroom and it opens.
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x, y + 1, z}, worldgen.Air)
	h.openChest(pl, x, y, z)
	if pl.winKind != winChest {
		t.Fatalf("an unblocked chest should open, got winKind %d", pl.winKind)
	}
	// A barrel has no such rule.
	h.releaseContainerView(pl)
	pl.winKind = 0
	bx := x + 3
	by := h.world.SurfaceFeet(bx, z)
	h.setBlockAt(h.playersRef, pl.dim, blockPos{bx, by, z}, worldgen.BlockBase("barrel"))
	h.setBlockAt(h.playersRef, pl.dim, blockPos{bx, by + 1, z}, worldgen.BlockBase("stone"))
	h.openChest(pl, bx, by, z)
	if pl.winKind != winChest {
		t.Fatal("a barrel opens with a block on top — that is what it is for")
	}
}

// A shulker box needs the half block its lid slides into: a solid block on
// the side it faces holds it shut.
func TestShulkerBoxBlockedByItsLidSide(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	x, z := 14, 14
	y := h.world.SurfaceFeet(x, z)
	box := worldgen.BlockBase("shulker_box") // base state faces up
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x, y, z}, box)
	d, ok := propDir(box, "facing")
	if !ok {
		t.Fatal("a shulker box has a facing")
	}
	dx, dy, dz := d.delta()
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x + dx, y + dy, z + dz}, worldgen.BlockBase("stone"))
	h.openChest(pl, x, y, z)
	if pl.winKind == winChest {
		t.Fatal("a shulker box with no room for its lid must not open")
	}
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x + dx, y + dy, z + dz}, worldgen.Air)
	h.openChest(pl, x, y, z)
	if pl.winKind != winChest {
		t.Fatalf("a clear shulker box should open, got winKind %d", pl.winKind)
	}
}

// A sitting cat on the lid holds a chest shut; a cat that is only standing
// there does not.
func TestChestBlockedBySittingCat(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	x, z := 9, 9
	y := h.world.SurfaceFeet(x, z)
	h.setBlockAt(h.playersRef, pl.dim, blockPos{x, y, z}, worldgen.BlockBase("chest"))
	cat := h.spawnMob(h.playersRef, entityCat, float64(x)+0.5, float64(y+1), float64(z)+0.5)
	if cat == nil {
		t.Fatal("the cat should have spawned")
	}
	cat.dim = pl.dim
	if h.chestBlockedAt(pl.dim, blockPos{x, y, z}) {
		t.Fatal("a standing cat does not block a chest")
	}
	cat.sitting = true
	if !h.chestBlockedAt(pl.dim, blockPos{x, y, z}) {
		t.Fatal("a sitting cat blocks the chest it is sitting on")
	}
}
