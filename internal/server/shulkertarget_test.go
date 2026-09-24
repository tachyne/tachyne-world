package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// ShulkerNearestAttackGoal: a floor shulker picks targets within sixteen
// blocks sideways but only four up or down, and keeps one it has while it
// stays within sixteen.
func TestShulkerTargetSlab(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	m := &mob{etype: entityShulker, x: 0.5, y: 100, z: 0.5}
	pl.x, pl.y, pl.z = 10.5, 110, 0.5 // ten up: outside the slab
	if h.shulkerQuarry(players, m) != nil {
		t.Error("a shulker picked a target ten blocks above it")
	}
	pl.y = 102
	if h.shulkerQuarry(players, m) != pl {
		t.Fatal("a shulker ignored a target in its slab")
	}
	pl.y = 108 // still within sixteen of it: kept
	if h.shulkerQuarry(players, m) != pl {
		t.Error("a shulker dropped a target it had, within its follow range")
	}
	pl.x = 20.5
	if h.shulkerQuarry(players, m) != nil {
		t.Error("a shulker kept a target past its follow range")
	}
}
