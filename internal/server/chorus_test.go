package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ChorusFlowerBlock: grow up, branch sideways, or die.

// chorusEndHub gives a hub with an End world and a slab of end stone under the
// target column.
func chorusEndHub(x, y, z int) (*hub, map[int32]*tracked) {
	h := newHub(world.New(1))
	h.end = world.New(3)
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			h.end.SetBlock(x+dx, y-1, z+dz, endStoneBlock)
			for dy := 0; dy <= 12; dy++ {
				h.end.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	return h, map[int32]*tracked{}
}

// A flower on end stone climbs, leaving chorus plant behind it.
func TestChorusGrowsUpwardLeavingPlant(t *testing.T) {
	x, y, z := 20, 80, 20
	h, players := chorusEndHub(x, y, z)
	h.end.SetBlock(x, y, z, chorusFlowerBase)

	grew := false
	for i := 0; i < 500 && !grew; i++ {
		for k := 0; k <= 10; k++ {
			if s := h.end.At(x, y+k, z); isChorusFlower(s) {
				h.tickChorus(players, 2, x, y+k, z, s)
			}
		}
		if isChorusPlant(h.end.At(x, y, z)) {
			grew = true
		}
	}
	if !grew {
		t.Fatal("chorus never grew off its starting block")
	}
	// The base became a plant, and something chorus-y sits above it.
	if !isChorusPlant(h.end.At(x, y, z)) {
		t.Errorf("base is state %d, want a chorus plant", h.end.At(x, y, z))
	}
	above := h.end.At(x, y+1, z)
	if !isChorusFlower(above) && !isChorusPlant(above) {
		t.Errorf("nothing chorus above the base: state %d", above)
	}
}

// The plant's connection flags follow its neighbours: a stem with end stone
// below and a flower above must report down=true and up=true.
func TestChorusPlantConnects(t *testing.T) {
	x, y, z := 40, 80, 40
	h, _ := chorusEndHub(x, y, z)
	h.end.SetBlock(x, y+1, z, chorusFlowerBase)

	s := h.chorusPlantAt(2, x, y, z)
	info, ok := worldgen.InfoForState(s)
	if !ok {
		t.Fatalf("chorus plant state %d has no block info", s)
	}
	if v := worldgen.GetProperty(info, s, "down"); v != "true" {
		t.Errorf("down=%q over end stone, want true", v)
	}
	if v := worldgen.GetProperty(info, s, "up"); v != "true" {
		t.Errorf("up=%q under a flower, want true", v)
	}
	if v := worldgen.GetProperty(info, s, "north"); v != "false" {
		t.Errorf("north=%q with nothing there, want false", v)
	}
}

// A flower with no room dies rather than hanging around forever.
func TestBlockedChorusDies(t *testing.T) {
	x, y, z := 60, 80, 60
	h, players := chorusEndHub(x, y, z)
	h.end.SetBlock(x, y, z, chorusFlowerBase+4) // age 4: too old to branch
	// Wall it in horizontally so it cannot climb or branch.
	for _, d := range horizNeighbors {
		h.end.SetBlock(x+d.x, y+1, z+d.z, endStoneBlock)
	}

	for i := 0; i < 200; i++ {
		if s := h.end.At(x, y, z); isChorusFlower(s) {
			h.tickChorus(players, 2, x, y, z, s)
		}
	}
	if got := h.end.At(x, y, z); got != chorusFlowerBase+chorusDeadAge {
		t.Errorf("blocked flower is state %d, want dead (%d)", got, chorusFlowerBase+chorusDeadAge)
	}
}

// A dead flower is inert.
func TestDeadChorusStaysDead(t *testing.T) {
	x, y, z := 80, 80, 80
	h, players := chorusEndHub(x, y, z)
	dead := chorusFlowerBase + chorusDeadAge
	h.end.SetBlock(x, y, z, dead)

	for i := 0; i < 200; i++ {
		h.tickChorus(players, 2, x, y, z, dead)
	}
	if got := h.end.At(x, y, z); got != dead {
		t.Errorf("dead flower changed to %d", got)
	}
	if got := h.end.At(x, y+1, z); got != worldgen.Air {
		t.Errorf("dead flower grew: state %d above", got)
	}
}
