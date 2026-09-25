package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A raiding vindicator stopped by a closed wooden door opens it
// (RaiderOpenDoorGoal) or beats it down (VindicatorBreakDoorGoal); outside a
// raid it does neither.
func TestRaidingVindicatorGetsThroughDoors(t *testing.T) {
	run := func(inRaid bool) bool {
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 3)
		w := h.world
		for x := -4; x <= 40; x++ {
			for z := -4; z <= 4; z++ {
				w.SetBlock(x, 179, z, worldgen.Stone)
				for y := 180; y <= 183; y++ {
					w.SetBlock(x, y, z, worldgen.Air)
				}
			}
		}
		// A wall across x=4 with an oak door in it.
		for z := -4; z <= 4; z++ {
			w.SetBlock(4, 180, z, worldgen.Stone)
			w.SetBlock(4, 181, z, worldgen.Stone)
		}
		door := worldgen.BlockBase("oak_door") // the first state: upper half…
		lower := setBoolProp(door, "open", false)
		info, _ := worldgen.InfoForState(lower)
		lower = worldgen.SetProperty(info, lower, "half", "lower")
		upper := worldgen.SetProperty(info, lower, "half", "upper")
		w.SetBlock(4, 180, 0, lower)
		w.SetBlock(4, 181, 0, upper)
		pl := survPlayer(h)
		pl.x, pl.y, pl.z = -3.5, 180, -3.5
		pl.gamemode = gmCreative
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		h.rules.Difficulty = diffNormal
		center := blockPos{38, 180, 0}
		h.raids[center] = &raid{center: center, uuid: raidUUID(center),
			alive: map[int32]bool{}, shown: map[int32]bool{}, numGroups: 3}
		v := h.spawnHostileYIn(players, entityVindicator, 0, 1.5, 180, 0.5)
		if inRaid {
			v.raidCenter = center
		}
		for i := 0; i < 600; i++ {
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
			if s := w.At(4, 180, 0); !worldgen.IsClosedDoor(s) {
				return true
			}
		}
		return false
	}
	if !run(true) {
		t.Fatal("a raiding vindicator gets through a closed door")
	}
	if run(false) {
		t.Fatal("a vindicator outside a raid leaves the door shut")
	}
}
