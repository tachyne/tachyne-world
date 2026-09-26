package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The boot clean-up of report #48's remnants: a door edit generation has no
// door for, standing alone within reach of a village, is reverted, both
// halves; a door in a wall, a door far from any village and a door
// generation made are kept; and it runs once.
func TestLoneVillageDoorsCleared(t *testing.T) {
	dir := t.TempDir()
	w, err := world.NewWithStore(1, world.NewFileStore(filepath.Join(dir, "world.gob")))
	if err != nil {
		t.Fatal(err)
	}
	v := w.Gen().VillageIn(-249, -586) // a plains village
	if !v.Exists {
		t.Fatal("setup: no village")
	}
	lower := worldgen.StateWith("oak_door", map[string]string{"half": "lower", "facing": "north"})
	upper := worldgen.StateWith("oak_door", map[string]string{"half": "upper", "facing": "north"})
	putDoor := func(x, y, z int) {
		w.SetBlock(x, y, z, lower)
		w.SetBlock(x, y+1, z, upper)
	}
	high := v.Y + 40 // in the open air above the village

	lone := blockPos{v.X + 30, high, v.Z} // a remnant: nothing round it
	putDoor(lone.x, lone.y, lone.z)
	walled := blockPos{v.X - 30, high, v.Z} // a door set in a wall, by its upper half
	putDoor(walled.x, walled.y, walled.z)
	w.SetBlock(walled.x+1, walled.y+1, walled.z, worldgen.StateWith("stone", nil))
	far := blockPos{33, 200, -95} // spawn's village-free zone
	putDoor(far.x, far.y, far.z)

	// A door generation made, swung open (an edit) and left with nothing
	// beside it: generation has a door there, so it stays.
	w.ForceLoad(v.X, v.Z, 4)
	var gen blockPos
	found := false
	for x := v.X - 60; x <= v.X+60 && !found; x++ {
		for z := v.Z - 60; z <= v.Z+60 && !found; z++ {
			for y := v.Y - 6; y <= v.Y+10; y++ {
				if s := w.GeneratedAt(x, y, z); worldgen.IsDoor(s) && doorHalf(s) == "lower" {
					gen, found = blockPos{x, y, z}, true
					break
				}
			}
		}
	}
	if !found {
		t.Fatal("setup: the village has no generated door")
	}
	for _, y := range []int{gen.y, gen.y + 1} {
		s := w.At(gen.x, y, gen.z)
		info, _ := worldgen.InfoForState(s)
		w.SetBlock(gen.x, y, gen.z, worldgen.SetProperty(info, s, "open", "true"))
		for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			w.SetBlock(gen.x+d[0], y, gen.z+d[1], worldgen.Air)
		}
	}

	s := &Server{WorldFile: filepath.Join(dir, "world.gob"), world: w}
	s.clearLoneDoors()

	for _, y := range []int{lone.y, lone.y + 1} {
		if st, ok := w.EditAt(lone.x, y, lone.z); ok {
			t.Errorf("the lone door near the village is still an edit at y=%d (%d)", y, st)
		}
	}
	for name, p := range map[string]blockPos{"walled": walled, "far": far, "generated": gen} {
		for _, y := range []int{p.y, p.y + 1} {
			if st, ok := w.EditAt(p.x, y, p.z); !ok || !worldgen.IsDoor(st) {
				t.Errorf("the %s door at y=%d should be kept", name, y)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(dir, loneDoorsMarker)); err != nil {
		t.Fatalf("no marker written: %v", err)
	}
	// The save holds the result.
	re, err := world.NewWithStore(1, world.NewFileStore(filepath.Join(dir, "world.gob")))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := re.EditAt(lone.x, lone.y, lone.z); ok {
		t.Error("the reverted door came back from the save")
	}

	// Once: a remnant made after the marker is left for the player to see.
	again := blockPos{v.X + 20, high, v.Z + 20}
	putDoor(again.x, again.y, again.z)
	s.clearLoneDoors()
	if _, ok := w.EditAt(again.x, again.y, again.z); !ok {
		t.Error("the clean-up ran a second time")
	}
}

func doorHalf(s uint32) string {
	info, _ := worldgen.InfoForState(s)
	return worldgen.GetProperty(info, s, "half")
}
