package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An unemployed villager finds a lectern, walks to it, becomes a librarian
// with trades, and is unemployed again when the lectern goes; a second
// villager cannot claim a lectern the first holds.
func TestVillagerTakesAndLosesAJob(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -6; x <= 12; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 40.5, 180, 0.5
	h.dayTime.Store(3000)
	h.tick.Store(1000)
	v := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	v.profession, v.work, v.jobSearchAt = profUnemployed, blockPos{}, 0
	lectern := worldgen.BlockID("lectern")
	w.SetBlock(5, 180, 0, lectern)
	h.villagerJobTick(players, v)
	if v.jobPos != (blockPos{5, 180, 0}) {
		t.Fatalf("no potential job site: %+v", v.jobPos)
	}
	if !h.villagerJobWalk(players, v) || v.vx <= 0 {
		t.Error("the villager is not walking to the lectern")
	}
	v.x = 4.5
	h.villagerJobWalk(players, v)
	if v.work != (blockPos{5, 180, 0}) || professionNames[v.profession] != "librarian" || len(v.offers) == 0 {
		t.Fatalf("after the claim: work %+v profession %d offers %d", v.work, v.profession, len(v.offers))
	}
	// A second villager finds the lectern taken.
	u := h.spawnMob(players, entityVillager, 0.5, 180, 2.5)
	u.profession, u.work, u.jobSearchAt = profUnemployed, blockPos{}, 0
	h.villagerJobTick(players, u)
	if u.jobPos != (blockPos{}) {
		t.Error("a second villager claimed a held lectern")
	}
	// The lectern goes: the librarian who never traded is unemployed again.
	w.SetBlock(5, 180, 0, worldgen.Air)
	h.villagerJobTick(players, v)
	if v.work != (blockPos{}) || v.profession != profUnemployed {
		t.Errorf("after losing the lectern: work %+v profession %d", v.work, v.profession)
	}
	// One with experience keeps the trade.
	w.SetBlock(5, 180, 0, lectern)
	v.jobSearchAt = 0
	h.villagerJobTick(players, v)
	v.x = 4.5
	h.villagerJobWalk(players, v)
	v.tradeXP = 10
	w.SetBlock(5, 180, 0, worldgen.Air)
	h.villagerJobTick(players, v)
	if v.profession < 0 {
		t.Error("an experienced librarian was fired")
	}
}

// YieldJobSite: an unemployed villager walking to a composter gives it up
// to a farmer nearby who lost its own, and the farmer walks there instead.
func TestVillagerYieldsJobSite(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -6; x <= 12; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 40.5, 180, 0.5
	h.dayTime.Store(3000)
	h.tick.Store(1000)
	composter := worldgen.BlockID("composter")
	w.SetBlock(5, 180, 0, composter)
	u := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	u.profession, u.work, u.jobSearchAt = profUnemployed, blockPos{}, 0
	h.villagerJobTick(players, u)
	if u.jobPos != (blockPos{5, 180, 0}) {
		t.Fatalf("no potential job site: %+v", u.jobPos)
	}
	farmer := h.spawnMob(players, entityVillager, 0.5, 180, 3.5)
	farmer.work, farmer.jobPos = blockPos{}, blockPos{}
	for i, n := range professionNames {
		if n == "farmer" {
			farmer.profession = i
		}
	}
	h.villagerJobTick(players, u)
	if u.jobPos != (blockPos{}) || farmer.jobPos != (blockPos{5, 180, 0}) {
		t.Fatalf("the composter should pass to the farmer: unemployed %+v farmer %+v", u.jobPos, farmer.jobPos)
	}
	// A farmer that already has a workstation wants nothing.
	u.jobPos, farmer.jobPos, farmer.work = blockPos{5, 180, 0}, blockPos{}, blockPos{7, 180, 0}
	h.villagerJobTick(players, u)
	if u.jobPos != (blockPos{5, 180, 0}) {
		t.Error("a farmer at work took a second composter")
	}
}
