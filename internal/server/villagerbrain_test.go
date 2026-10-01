package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// brainSetup is a floored, loaded patch at y=180 with a creative onlooker,
// at the given time of day. The patch sits over land with no water or tree
// under it (World.Walkable reads the ground below), so nothing but the
// fixture's own walls stops a walk; its corner (ox, oz) offsets every
// position in the test.
func brainSetup(t *testing.T, dayTime uint64) (*hub, map[int32]*tracked, int, int) {
	t.Helper()
	h := newTestHub(world.New(1))
	ox, oz, found := 0, 0, false
	for i := 0; i < 400 && !found; i++ {
		ox, oz = (i%20-10)*32, (i/20-10)*32
		found = true
		for x := ox - 6; x <= ox+12 && found; x++ {
			for z := oz - 6; z <= oz+6 && found; z++ {
				found = h.world.Walkable(x, z)
			}
		}
	}
	if !found {
		t.Fatal("no dry, treeless patch for the fixture")
	}
	h.world.ForceLoad(ox, oz, 2)
	poiFloor(h, ox, oz, 20)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.gamemode = float64(ox)+0.5, 180, float64(oz)-18.5, gmCreative
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.dayTime.Store(dayTime)
	h.tick.Store(1000)
	return h, players, ox, oz
}

// brainRun advances the hub: the mob update every step and the survival
// step every twenty ticks, until done says so or the steps run out.
func brainRun(h *hub, players map[int32]*tracked, steps int, done func() bool) bool {
	for i := 0; i < steps; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if h.tick.Load()%survivalTickN == 0 {
			h.updateBreeding(players)
		}
		if done() {
			return true
		}
	}
	return false
}

// brainPen walls in x0..x1 by z0..z1 (inside), two high, so an errand
// cannot stroll out of reach of the fixture.
func brainPen(h *hub, x0, z0, x1, z1 int) {
	for x := x0 - 1; x <= x1+1; x++ {
		for z := z0 - 1; z <= z1+1; z++ {
			if x >= x0 && x <= x1 && z >= z0 && z <= z1 {
				continue
			}
			h.world.SetBlock(x, 180, z, worldgen.Stone)
			h.world.SetBlock(x, 181, z, worldgen.Stone)
		}
	}
}

func librarian() int {
	for i, n := range professionNames {
		if n == "librarian" {
			return i
		}
	}
	return -1
}

// PoiCompetitorScan: two villagers holding one lectern — the one with more
// experience keeps it, and the other, never having traded, is unemployed.
// On a tie the other villager wins, so exactly one keeps it.
func TestPoiCompetitorScanKeepsTheExperienced(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 3000)
	fx, fz := float64(ox), float64(oz)
	lectern := blockPos{ox + 4, 180, oz}
	h.world.SetBlock(lectern.x, lectern.y, lectern.z, worldgen.BlockID("lectern"))
	a := h.spawnMob(players, entityVillager, fx+0.5, 180, fz+0.5)
	b := h.spawnMob(players, entityVillager, fx+0.5, 180, fz+3.5)
	for _, v := range []*mob{a, b} {
		h.initVillagerTrades(v, librarian())
		v.work = lectern
	}
	a.tradeXP = 10
	h.updateBreeding(players)
	if a.work != lectern || b.work != (blockPos{}) {
		t.Fatalf("the experienced one keeps the lectern: a %+v b %+v", a.work, b.work)
	}
	if b.profession != profUnemployed {
		t.Errorf("a loser that never traded is unemployed again, profession %d", b.profession)
	}
	if a.profession != librarian() {
		t.Errorf("the winner stays a librarian, profession %d", a.profession)
	}
	// A tie: the scanning villager gives way, and with a level behind it
	// keeps its trade.
	c := h.spawnMob(players, entityVillager, fx+2.5, 180, fz+2.5)
	h.initVillagerTrades(c, librarian())
	c.work, c.tradeXP, c.tradeLevel = lectern, 10, 2
	h.poiCompetitorScan(players, c)
	if a.work != lectern || c.work != (blockPos{}) || c.profession != librarian() {
		t.Errorf("on a tie the other keeps it: a %+v c %+v, c profession %d", a.work, c.work, c.profession)
	}
}

// JumpOnBed: a child with a bed nearby walks over and bounces on it.
func TestBabyVillagerJumpsOnBed(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 1000) // the baby timeline's IDLE
	bed := blockPos{ox + 4, 180, oz}
	h.world.SetBlock(bed.x, bed.y, bed.z, freeBed())
	foot := freeBed()
	info, _ := worldgen.InfoForState(foot)
	h.world.SetBlock(bed.x-1, bed.y, bed.z, worldgen.SetProperty(info, foot, "part", "foot"))
	brainPen(h, ox-3, oz-4, ox+6, oz+3)
	kid := h.spawnMob(players, entityVillager, float64(ox)+0.5, 180, float64(oz)-2.5)
	kid.baby, kid.growLeft = true, growUpTicks
	kid.bed = bed // its own: no AcquirePoi walk in the way
	jumped := brainRun(h, players, 1500, func() bool {
		return kid.leaping && h.onOrOverBed(kid)
	})
	if !kid.vBedSet || kid.vBed != bed {
		t.Fatalf("NEAREST_BED should be the bed, got %+v (%v)", kid.vBed, kid.vBedSet)
	}
	if !jumped {
		t.Fatalf("the child never jumped on the bed: at (%.1f, %.1f, %.1f) jump %+v", kid.x, kid.y, kid.z, kid.vJump)
	}
	// An adult never does.
	if h.startJumpOnBed(&mob{etype: entityVillager, vBed: bed, vBedSet: true, dim: kid.dim}, false) {
		t.Error("an adult started JumpOnBed")
	}
}

// InteractWith(CAT): an idle villager walks up to a cat in sight.
func TestVillagerWalksUpToACat(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 1000)
	brainPen(h, ox-2, oz-3, ox+8, oz+3)
	v := h.spawnMob(players, entityVillager, float64(ox)+0.5, 180, float64(oz)+0.5)
	cat := h.spawnMob(players, entityCat, float64(ox)+5.5, 180, float64(oz)+0.5)
	cat.statik = true
	// The cat is one look choice in a weighted RunOne, and the walk one of
	// the idle choices after it: give the dice room.
	if !brainRun(h, players, 2400, func() bool { return v.vWalk.set && v.vWalk.eid == cat.eid }) {
		t.Fatal("the villager never went to the cat")
	}
	// MoveToTargetSink: it walks up until within two blocks. (The walk may
	// also end early when the look target lapses, LookAtTargetSink's 45-90
	// ticks, and a later look can start it again; what matters is that it
	// gets there.)
	if !brainRun(h, players, 600, func() bool { return math.Hypot(cat.x-v.x, cat.z-v.z) <= 3.5 }) {
		t.Errorf("it should end beside the cat: %.1f away", math.Hypot(cat.x-v.x, cat.z-v.z))
	}
}

// WORK: at the job site the villager strolls about it (StrollAroundPoi) and
// back to it (StrollToPoi); a farmer also walks out to its farmland
// (StrollToPoiList over SECONDARY_JOB_SITE).
func TestVillagerStrollsAboutItsJobSite(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 3000)
	site := blockPos{ox + 3, 180, oz}
	h.world.SetBlock(site.x, site.y, site.z, composterBase)
	farmland := worldgen.BlockBase("farmland")
	for x := 0; x <= 2; x++ {
		h.world.SetBlock(ox+x, 179, oz+3, farmland)
	}
	v := h.spawnMob(players, entityVillager, float64(ox)+2.5, 180, float64(oz)+0.5)
	h.initVillagerTrades(v, profFarmer)
	v.work = site
	v.hoard = nil
	var toSite, around, toField bool
	brainRun(h, players, 1500, func() bool {
		w := v.vWalk
		switch {
		case !w.set:
		case w.block && floorInt(w.x) == site.x && floorInt(w.z) == site.z:
			toSite = true
		case w.block && floorInt(w.y) == 179:
			toField = true
		case !w.block && w.eid == 0 && w.speed == vSpeedPoiStroll:
			around = true
		}
		return toSite && around && toField
	})
	if !toSite || !around || !toField {
		t.Fatalf("strolls seen: to the site %v, about it %v, to the field %v", toSite, around, toField)
	}
	if v.work != site {
		t.Errorf("the job site was lost: %+v", v.work)
	}
}

// MEET: at the bell the villager strolls about it.
func TestVillagerStrollsAboutTheBell(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 10000)
	bell := blockPos{ox + 3, 180, oz}
	h.world.SetBlock(bell.x, bell.y, bell.z, bellDefault)
	v := h.spawnMob(players, entityVillager, float64(ox)+1.5, 180, float64(oz)+0.5)
	v.meet = bell
	if !brainRun(h, players, 600, func() bool { return v.vStrollNext[vStrollAroundBell] != 0 }) {
		t.Fatal("the villager never strolled about the bell")
	}
}

// Children play tag only in the baby timeline's PLAY hours.
func TestBabyVillagersPlayTagOnlyAtPlaytime(t *testing.T) {
	h, players, kids := playSetup(t, 3)
	h.dayTime.Store(1000) // IDLE
	kids[0].playMate = kids[1].eid
	if h.villagerPlayStep(players, kids[0]) || kids[0].playMate != 0 {
		t.Error("no tag outside play hours")
	}
	h.dayTime.Store(10500) // the afternoon PLAY
	if got := h.villagerActivity(kids[0]); got != vsPlay {
		t.Errorf("10500 should be PLAY for a child, got %d", got)
	}
	adult := &mob{etype: entityVillager}
	if got := h.villagerActivity(adult); got != vsRoam {
		t.Errorf("an adult with no bell idles at 10500, got %d", got)
	}
}
