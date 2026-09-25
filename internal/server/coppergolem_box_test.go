package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TransportItemsBetweenContainers looks 32 blocks sideways and 8 up or
// down from the golem's block, not 65 and 17.
func TestCopperGolemSearchBox(t *testing.T) {
	h := newHub(world.New(1))
	m := &mob{x: 0.5, y: 64, z: 0.5}
	for _, tc := range []struct {
		p  blockPos
		in bool
	}{{blockPos{32, 64, 0}, true}, {blockPos{33, 64, 0}, false}, {blockPos{0, 72, -32}, true}, {blockPos{0, 73, 0}, false}, {blockPos{50, 64, 0}, false}} {
		if got := h.inSortRange(m, tc.p); got != tc.in {
			t.Errorf("%v in the search box: %v, want %v", tc.p, got, tc.in)
		}
	}
}
