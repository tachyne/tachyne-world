package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestCatRelaxesOnSleepingOwner: a tamed cat near its sleeping owner walks
// to the foot of the bed, watches (relax pose), then lies down; the owner
// waking gets it up. A second cat finds the spot taken.
func TestCatRelaxesOnSleepingOwner(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	bed := worldgen.BlockBase("red_bed")
	info, _ := worldgen.InfoForState(bed)
	head := worldgen.SetProperty(info, worldgen.SetProperty(info, bed, "part", "head"), "facing", "north")
	foot := worldgen.SetProperty(info, head, "part", "foot")
	w.SetBlock(2, 180, 0, head)
	w.SetBlock(2, 180, 1, foot)
	pl.x, pl.y, pl.z = 2.5, 180, 0.5
	pl.sleeping, pl.sleepPos = true, blockPos{2, 180, 0}
	c := h.spawnMob(players, entityCat, 0.5, 180, 3.5)
	c.tamed, c.owner = true, pl.p.eid
	_, goal, ok := h.catRelaxGoal(players, c)
	if !ok || goal != (blockPos{2, 180, 1}) {
		t.Fatalf("the goal is the foot of the bed: %v %+v", ok, goal)
	}
	for i := 0; i < 200 && !c.lying; i++ {
		if !h.catRelaxStep(players, c) {
			t.Fatalf("the goal should hold the cat (step %d)", i)
		}
		c.x += c.vx
		c.z += c.vz
	}
	if !c.lying || c.relaxOne {
		t.Fatalf("lying by now: lying %v relax %v at %.1f,%.1f", c.lying, c.relaxOne, c.x, c.z)
	}
	other := h.spawnMob(players, entityCat, 0.5, 180, 3.5)
	other.tamed, other.owner = true, pl.p.eid
	h.gridDirty()
	if _, _, ok := h.catRelaxGoal(players, other); ok {
		t.Fatal("one cat per bed")
	}
	pl.sleeping = false
	if h.catRelaxStep(players, c) || c.lying || c.relaxTicks != 0 {
		t.Fatal("the owner waking gets the cat up")
	}
	// CatLieOnBedGoal: the bed alone is enough, later — but vanilla's
	// vertical walk from verticalSearchStart −2 visits offsets −3, +2, −4,
	// +3 … (never the cat's own level), so this bed is out of reach and a
	// bed three below is found.
	c.lieNext = 0
	if h.catLieStep(players, c) || c.lieBlock != (blockPos{}) {
		t.Fatalf("a bed on the cat's own level is not in the goal's search: %+v", c.lieBlock)
	}
	w.SetBlock(-3, 177, 0, head)
	w.SetBlock(-3, 177, 1, foot)
	c.lieNext = 0
	if !h.catLieStep(players, c) || c.lieBlock.y != 177 {
		t.Fatalf("a bed three below is: %+v", c.lieBlock)
	}
}
