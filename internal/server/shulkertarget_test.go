package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ShulkerNearestAttackGoal: a floor shulker picks targets within sixteen
// blocks sideways but only four up or down, and keeps one it has while it
// stays within sixteen.
func TestShulkerTargetSlab(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.world.ForceLoad(0, 0, 2)
	m := &mob{etype: entityShulker, x: 0.5, y: 180, z: 0.5}
	pl.x, pl.y, pl.z = 10.5, 190, 0.5 // ten up: outside the slab
	if h.shulkerQuarry(players, m) != nil {
		t.Error("a shulker picked a target ten blocks above it")
	}
	pl.y = 182
	if h.shulkerQuarry(players, m) != pl {
		t.Fatal("a shulker ignored a target in its slab")
	}
	pl.y = 188 // still within sixteen of it: kept
	if h.shulkerQuarry(players, m) != pl {
		t.Error("a shulker dropped a target it had, within its follow range")
	}
	pl.x = 20.5
	if h.shulkerQuarry(players, m) != nil {
		t.Error("a shulker kept a target past its follow range")
	}
}

// The shulker's player goal is mustSee: it does not lock on to a player on
// the far side of a wall, and lets one go that has been out of sight past
// the sixty-tick memory.
func TestShulkerNeedsToSeeItsTarget(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.world.ForceLoad(0, 0, 2)
	m := &mob{etype: entityShulker, x: 0.5, y: 180, z: 0.5}
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	for y := 178; y <= 184; y++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(3, y, z, worldgen.Stone)
		}
	}
	if h.shulkerQuarry(players, m) != nil {
		t.Fatal("a shulker picked a target through a wall")
	}
	for y := 178; y <= 184; y++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(3, y, z, worldgen.Air)
		}
	}
	if h.shulkerQuarry(players, m) != pl {
		t.Fatal("a shulker ignored a player in plain sight")
	}
	for y := 178; y <= 184; y++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(3, y, z, worldgen.Stone)
		}
	}
	kept := 0
	for i := 0; i < 60 && h.shulkerQuarry(players, m) == pl; i++ {
		kept++
	}
	if want := targetUnseenMemory / mobMoveInterval; kept != want {
		t.Errorf("kept an unseen target %d updates, want %d (sixty ticks)", kept, want)
	}
}
