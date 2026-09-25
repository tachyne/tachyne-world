package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// VillagerGoalPackages RAID: during a wave a villager hides at a bed; after
// a victory it cheers under the sky and sends up fireworks.
func TestVillagersHideAndCelebrate(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	poiFloor(h, 0, 0, 24)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.gamemode = 0.5, 180, -20.5, gmCreative
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	bed := blockPos{12, 180, 4}
	h.world.SetBlock(bed.x, bed.y, bed.z, freeBed())
	center := blockPos{0, 180, 0}
	r := &raid{center: center, uuid: raidUUID(center),
		alive: map[int32]bool{}, shown: map[int32]bool{}, numGroups: 3, wave: 1}
	h.raids[center] = r
	v := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	h.configureVillageMob(players, v)
	hidden := false
	for i := 0; i < 400 && !hidden; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		hidden = v.vRaidHideSet && v.vRaidHide == bed && dist3(v.x, 180, v.z, 12.5, 180, 4.5) <= 1.5
	}
	if !hidden {
		t.Fatalf("during a wave the villager hides at the bed: at (%.1f, %.1f) hide %v", v.x, v.z, v.vRaidHide)
	}
	r.wonLeft = 30
	for i := 0; i < 300 && len(h.rockets) == 0; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		h.updateRockets(players)
	}
	if len(h.rockets) == 0 && v.vCelebrate == 0 {
		t.Fatal("after a victory the villager celebrates under the sky")
	}
}
