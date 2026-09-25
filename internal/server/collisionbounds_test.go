package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The collision table gives each state its real height.
func TestCollisionBoundsHeights(t *testing.T) {
	for _, c := range []struct {
		state  uint32
		ok     bool
		lo, hi float64
	}{
		{worldgen.Stone, true, 0, 1},
		{worldgen.Air, false, 0, 0},
		{worldgen.BlockBase("torch"), false, 0, 0},
		{worldgen.BlockBase("oak_slab") + 3, true, 0, 0.5},
		{worldgen.BlockBase("oak_slab") + 1, true, 0.5, 1},
		{worldgen.BlockBase("oak_fence"), true, 0, 1.5},
		{worldgen.BlockBase("snow") + 2, true, 0, 0.25},
	} {
		lo, hi, ok := collisionBounds(c.state)
		if ok != c.ok || lo != c.lo || hi != c.hi {
			t.Errorf("state %d: %v [%v,%v], want %v [%v,%v]", c.state, ok, lo, hi, c.ok, c.lo, c.hi)
		}
	}
	// The table was generated from the canonical registry: its last state
	// is the registry's last.
	if _, ok := worldgen.StateName(collisionStateCount - 1); !ok {
		t.Error("the collision table runs past the registry")
	}
	if _, ok := worldgen.StateName(collisionStateCount); ok {
		t.Error("the registry has states the collision table has not seen")
	}
}
