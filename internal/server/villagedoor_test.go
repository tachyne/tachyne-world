package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Report #48: "random doors around villages?" — a lone oak door on a beach,
// no house round it. The world's edits held it: generation has no door there
// any more. A villager walking through a house door opened it and shut it
// again, and each swing was saved as an edit of both halves; when a later
// GenVersion laid the village out afresh, the house moved and the door the
// edits remembered stayed where it had been. A door swung shut is the door
// generation made, and now leaves no edit.
func TestAVillagersDoorLeavesNoEdit(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	v := w.Gen().VillageIn(-249, -586) // a plains village
	if !v.Exists {
		t.Fatal("setup: no village")
	}
	w.ForceLoad(v.X, v.Z, 4)
	// A generated house door: the lower half of a closed wooden door.
	var door blockPos
	found := false
	for x := v.X - 60; x <= v.X+60 && !found; x++ {
		for z := v.Z - 60; z <= v.Z+60 && !found; z++ {
			for y := v.Y - 6; y <= v.Y+10; y++ {
				s := w.At(x, y, z)
				if worldgen.IsWoodenDoor(s) && worldgen.IsClosedDoor(s) && halfOf(s) == "lower" {
					door, found = blockPos{x, y, z}, true
					break
				}
			}
		}
	}
	if !found {
		t.Fatal("setup: the village has no generated door")
	}
	m := h.spawnMob(players, entityVillager, float64(door.x)+0.5, float64(door.y), float64(door.z)+1.5)
	m.usesDoors = true
	m.behavior = villagerBehavior{}

	h.villagerDoors(players, m)
	if !boolProp(w.At(door.x, door.y, door.z), "open") {
		t.Fatal("setup: the villager should have opened the door")
	}
	m.x, m.z = float64(door.x)+200, float64(door.z)+200 // walked on
	h.tick.Store(doorCloseGrace + 1)
	h.updateOpenDoors(players)
	for _, y := range []int{door.y, door.y + 1} {
		if boolProp(w.At(door.x, y, door.z), "open") {
			t.Fatalf("the door at y=%d should be shut again", y)
		}
		if s, ok := w.EditAt(door.x, y, door.z); ok {
			t.Errorf("the shut door at y=%d is still an edit (%d): a new layout would leave it standing alone", y, s)
		}
	}
}

func halfOf(s uint32) string {
	info, _ := worldgen.InfoForState(s)
	return worldgen.GetProperty(info, s, "half")
}
