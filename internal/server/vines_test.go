package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// GrowingPlantHeadBlock, shared by kelp and the three vine families: the head
// advances along its growth direction while age < 25, leaving the body block
// behind.

func plantByHead(t *testing.T, name string) growingPlant {
	t.Helper()
	lo, _ := worldgen.BlockRange(name)
	for _, g := range growingPlants {
		if g.headLo == lo {
			return g
		}
	}
	t.Fatalf("%s is not a registered growing plant", name)
	return growingPlant{}
}

// Each family grows the right way and leaves its own body block behind.
func TestGrowingPlantsAdvanceAndLeaveBody(t *testing.T) {
	cases := []struct {
		head, body string
		wantDy     int
		medium     uint32
	}{
		{"kelp", "kelp_plant", +1, worldgen.WaterBase},
		{"twisting_vines", "twisting_vines_plant", +1, worldgen.Air},
		{"weeping_vines", "weeping_vines_plant", -1, worldgen.Air},
		{"cave_vines", "cave_vines_plant", -1, worldgen.Air},
	}
	for i, c := range cases {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		g := plantByHead(t, c.head)
		if g.dy != c.wantDy {
			t.Errorf("%s grows dy=%d, want %d", c.head, g.dy, c.wantDy)
		}
		x, y, z := 100+i*8, 100, 100

		// A column of the right medium for it to grow through.
		for k := -6; k <= 6; k++ {
			h.world.SetBlock(x, y+k, z, c.medium)
		}
		h.world.SetBlock(x, y, z, g.headLo) // age 0 head

		grew := false
		for n := 0; n < 4000 && !grew; n++ {
			h.tickGrowingPlant(players, 0, x, y, z, h.world.At(x, y, z))
			if h.world.At(x, y+g.dy, z) != c.medium {
				grew = true
			}
		}
		if !grew {
			t.Errorf("%s never grew", c.head)
			continue
		}
		// The old cell became the body block…
		wantBody := worldgen.BlockBase(c.body)
		if got := h.world.At(x, y, z); got != wantBody {
			t.Errorf("%s: origin is %d, want body %s (%d)", c.head, got, c.body, wantBody)
		}
		// …and the new tip is a head one age further on.
		tip := h.world.At(x, y+g.dy, z)
		if tip < g.headLo || tip > g.headHi {
			t.Errorf("%s: tip state %d is not a head", c.head, tip)
		} else if a := g.age(tip); a != 1 {
			t.Errorf("%s: tip age %d, want 1", c.head, a)
		}
	}
}

// Growth stops at age 25 rather than running forever.
func TestGrowingPlantStopsAtMaxAge(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	g := plantByHead(t, "twisting_vines")
	x, y, z := 40, 100, 40

	for k := 0; k <= 4; k++ {
		h.world.SetBlock(x, y+k, z, worldgen.Air)
	}
	maxHead := g.headAt(growingPlantMaxAge, false)
	h.world.SetBlock(x, y, z, maxHead)

	for i := 0; i < 3000; i++ {
		h.tickGrowingPlant(players, 0, x, y, z, maxHead)
	}
	if got := h.world.At(x, y+1, z); got != worldgen.Air {
		t.Errorf("a max-age head grew anyway: state %d above", got)
	}
}

// Kelp needs water; it must not grow up into air.
func TestKelpDoesNotGrowIntoAir(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	g := plantByHead(t, "kelp")
	x, y, z := 60, 100, 60

	h.world.SetBlock(x, y, z, g.headLo)
	h.world.SetBlock(x, y+1, z, worldgen.Air)

	for i := 0; i < 3000; i++ {
		h.tickGrowingPlant(players, 0, x, y, z, g.headLo)
	}
	if got := h.world.At(x, y+1, z); got != worldgen.Air {
		t.Errorf("kelp grew into air: state %d", got)
	}
}

// Cave vines sometimes carry glow berries on a new segment (11% in vanilla),
// so both forms must be reachable.
func TestCaveVinesSometimesGrowBerries(t *testing.T) {
	g := plantByHead(t, "cave_vines")
	if g.berryStride != 2 {
		t.Fatalf("cave vines stride %d, want 2 (age × berries)", g.berryStride)
	}
	// Checked arithmetically against the registry layout rather than through
	// InfoForState, which only covers ORIENTABLE blocks and knows nothing
	// about cave vines. States run age0/berries, age0/none, age1/berries, …
	withBerries, without := g.headAt(3, true), g.headAt(3, false)
	if wantBerries := g.headLo + 3*2; withBerries != wantBerries {
		t.Errorf("headAt(3, true) = %d, want %d", withBerries, wantBerries)
	}
	if without != withBerries+1 {
		t.Errorf("headAt(3, false) = %d, want %d (berries=true comes first)",
			without, withBerries+1)
	}
	if g.age(withBerries) != 3 || g.age(without) != 3 {
		t.Errorf("age round-trip broke: %d, %d", g.age(withBerries), g.age(without))
	}
	// Both forms must stay inside the block's own state range.
	if without > g.headHi {
		t.Errorf("state %d past the head range end %d", without, g.headHi)
	}
}
