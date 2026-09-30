package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// SetWalkTargetFromLookTarget: an idle villager whose look behaviours pick a
// player nearby sometimes walks over to them.
func TestVillagerWalksToItsLookTarget(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 1000)
	brainPen(h, ox-2, oz-3, ox+8, oz+3)
	var pl *tracked
	for _, p := range players {
		pl = p
	}
	pl.x, pl.y, pl.z = float64(ox)+6.5, 180, float64(oz)+0.5
	v := h.spawnMob(players, entityVillager, float64(ox)+0.5, 180, float64(oz)+0.5)
	sawLook := false
	if !brainRun(h, players, 3000, func() bool {
		if v.vLook.set && v.vLook.player && v.vLook.eid == pl.p.eid {
			sawLook = true
		}
		return v.vWalk.set && v.vWalk.player && v.vWalk.eid == pl.p.eid
	}) {
		t.Fatalf("the villager never walked to the player it looked at (look seen %v)", sawLook)
	}
	if v.vWalk.close != vLookWalkClose || v.vWalk.speed != vSpeedBase {
		t.Errorf("the walk is SetWalkTargetFromLookTarget(0.5, 2): %+v", v.vWalk)
	}
}

// LookAtTargetSink: a LOOK_TARGET lasts 45-90 ticks, and one out of sight
// is dropped at once.
func TestVillagerLookTargetLapses(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 1000)
	v := h.spawnMob(players, entityVillager, float64(ox)+0.5, 180, float64(oz)+0.5)
	v.setLookBlock(blockPos{ox + 3, 180, oz})
	v.vLookHold = h.tick.Load() + 10000 // no new pick in the way
	start := h.tick.Load()
	h.villagerLookTick(players, v)
	if v.vLookSink < start+lookSinkMin || v.vLookSink > start+lookSinkMax {
		t.Fatalf("the sink should run 45-90 ticks: ends %d from %d", v.vLookSink, start)
	}
	h.tick.Store(v.vLookSink)
	h.villagerLookTick(players, v)
	if v.vLook.set {
		t.Error("LOOK_TARGET outlived LookAtTargetSink")
	}
	// A mob out of sight: gone on the next check.
	o := h.spawnMob(players, entityVillager, float64(ox)+80.5, 180, float64(oz)+0.5)
	v.setLookMob(o)
	h.villagerLookTick(players, v)
	if v.vLook.set {
		t.Error("a target beyond the follow range is not visible and is dropped")
	}
}

// SocializeAtBell only starts without an INTERACTION_TARGET, and sets one
// (a villager: kept for TradeWithVillager's 60 ticks).
func TestSocializeAtBellNeedsNoInteractionTarget(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 10000)
	bell := blockPos{ox + 3, 180, oz}
	h.world.SetBlock(bell.x, bell.y, bell.z, bellDefault)
	v := h.spawnMob(players, entityVillager, float64(ox)+2.5, 180, float64(oz)+1.5)
	o := h.spawnMob(players, entityVillager, float64(ox)+4.5, 180, float64(oz)+1.5)
	v.meet, o.meet = bell, bell
	o.statik = true
	socialized := func() bool { return v.vWalk.set && v.vWalk.eid == o.eid && v.vWalk.speed == vSpeedSocialize }
	busy := h.spawnMob(players, entityVillager, float64(ox)+2.5, 180, float64(oz)-1.5)
	v.vInteract, v.vInteractUntil = busy.eid, h.tick.Load()+1_000_000
	for i := 0; i < 3000; i++ {
		v.vWalk = villagerWalk{}
		h.villagerMeetStrolls(v, h.tick.Load())
		if socialized() {
			t.Fatal("SocializeAtBell started with an INTERACTION_TARGET present")
		}
	}
	v.vInteract, v.vInteractUntil = 0, 0
	ok := false
	for i := 0; i < 3000 && !ok; i++ {
		v.vWalk = villagerWalk{}
		h.villagerMeetStrolls(v, h.tick.Load())
		ok = socialized()
	}
	if !ok {
		t.Fatal("SocializeAtBell never started with no INTERACTION_TARGET")
	}
	if v.vInteract != o.eid || v.vInteractUntil != h.tick.Load()+vTradeWithTicks || !v.vLook.set || v.vLook.eid != o.eid {
		t.Errorf("socializing sets INTERACTION_TARGET (60 ticks) and LOOK_TARGET: interact %d until %d look %+v",
			v.vInteract, v.vInteractUntil, v.vLook)
	}
	// Through the mob update: the MEET gate erases it once TradeWithVillager's
	// 60 ticks are up.
	brainRun(h, players, vTradeWithTicks/mobMoveInterval+2, func() bool { return false })
	if v.vInteract == o.eid && h.tick.Load() >= v.vInteractUntil {
		t.Errorf("INTERACTION_TARGET outlived TradeWithVillager: until %d now %d", v.vInteractUntil, h.tick.Load())
	}
}

// ResetProfession runs whenever the villager has no job site: a level-one
// villager with no experience loses its trade; one that traded, or a
// nitwit, keeps what it is.
func TestResetProfessionWithoutJobSite(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 3000)
	fx, fz := float64(ox), float64(oz)
	fresh := h.spawnMob(players, entityVillager, fx+0.5, 180, fz+0.5)
	h.initVillagerTrades(fresh, librarian())
	traded := h.spawnMob(players, entityVillager, fx+2.5, 180, fz+0.5)
	h.initVillagerTrades(traded, librarian())
	traded.tradeXP = 3
	nit := h.spawnMob(players, entityVillager, fx+4.5, 180, fz+0.5)
	h.initVillagerTrades(nit, profNitwit)
	h.updateBreeding(players)
	if fresh.profession != profUnemployed || fresh.offers != nil {
		t.Errorf("a fresh librarian with no lectern is unemployed again: profession %d, %d offers", fresh.profession, len(fresh.offers))
	}
	if traded.profession != librarian() {
		t.Errorf("a villager that traded keeps its profession: %d", traded.profession)
	}
	if nit.profession != profNitwit {
		t.Errorf("a nitwit stays a nitwit: %d", nit.profession)
	}
}

// WorkAtPoi: once started at the job site it holds the WORK RunOne for 60
// ticks — no stroll starts — and looks at the workstation.
func TestWorkAtPoiHoldsTheWorkRunOne(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 3000)
	site := blockPos{ox + 3, 180, oz}
	h.world.SetBlock(site.x, site.y, site.z, worldgen.BlockID("lectern"))
	v := h.spawnMob(players, entityVillager, float64(ox)+3.5, 180, float64(oz)+1.5)
	h.initVillagerTrades(v, librarian())
	v.work = site
	v.statik = true
	started := false
	for i := 0; i < 400 && !started; i++ {
		h.tick.Add(mobMoveInterval)
		h.villagerWorkTick(players, v)
		started = v.vWorkUntil > h.tick.Load()
	}
	if !started {
		t.Fatal("WorkAtPoi never started beside the lectern")
	}
	if !v.vLook.set || v.vLook.eid != 0 || floorInt(v.vLook.x) != site.x || floorInt(v.vLook.z) != site.z {
		t.Errorf("WorkAtPoi looks at the job site: %+v", v.vLook)
	}
	if v.vWorkUntil != h.tick.Load()+vWorkAtTicks {
		t.Errorf("WorkAtPoi runs 60 ticks: until %d at %d", v.vWorkUntil, h.tick.Load())
	}
	v.vStrollNext = [4]uint64{}
	v.vWalk = villagerWalk{}
	for h.tick.Load()+mobMoveInterval < v.vWorkUntil {
		h.tick.Add(mobMoveInterval)
		h.villagerBrainStep(players, v)
		if v.vWalk.set {
			t.Fatalf("a stroll started while WorkAtPoi ran: %+v", v.vWalk)
		}
	}
}

// NearestBedSensor: a bed it tried stays in the sensor's cache — skipped —
// until a failed scan clears the lapsed entries, 40 ticks past its scan.
func TestNearestBedSensorCachesTriedBeds(t *testing.T) {
	h, players, ox, oz := brainSetup(t, 1000)
	bed := blockPos{ox + 6, 180, oz}
	h.world.SetBlock(bed.x, bed.y, bed.z, freeBed())
	foot := freeBed()
	info, _ := worldgen.InfoForState(foot)
	h.world.SetBlock(bed.x-1, bed.y, bed.z, worldgen.SetProperty(info, foot, "part", "foot"))
	// Box the bed in so no path reaches it.
	var box []blockPos
	for x := bed.x - 2; x <= bed.x+1; x++ {
		for z := bed.z - 1; z <= bed.z+1; z++ {
			for y := 180; y <= 182; y++ {
				if y < 182 && z == bed.z && x >= bed.x-1 && x <= bed.x {
					continue
				}
				if y == 181 && z == bed.z && (x == bed.x-1 || x == bed.x) {
					continue
				}
				box = append(box, blockPos{x, y, z})
				h.world.SetBlock(x, y, z, worldgen.Stone)
			}
		}
	}
	kid := h.spawnMob(players, entityVillager, float64(ox)+0.5, 180, float64(oz)+0.5)
	kid.baby, kid.growLeft = true, growUpTicks
	kid.statik = true
	t0 := uint64(10000)
	h.tick.Store(t0)
	h.senseNearestBed(kid)
	if kid.vBedSet {
		t.Fatal("a boxed-in bed is not reachable")
	}
	if _, ok := kid.vBedCache[bed]; !ok {
		t.Fatalf("the tried bed should be cached: %v", kid.vBedCache)
	}
	for _, p := range box {
		h.world.SetBlock(p.x, p.y, p.z, 0)
	}
	h.tick.Store(t0 + 20)
	h.senseNearestBed(kid)
	if kid.vBedSet {
		t.Fatal("a cached bed is skipped, even once reachable")
	}
	h.tick.Store(t0 + 100)
	h.senseNearestBed(kid) // skipped again, but this failed scan clears the lapsed entry
	if _, ok := kid.vBedCache[bed]; ok {
		t.Fatal("a failed scan clears entries past their 40 ticks")
	}
	h.tick.Store(t0 + 120)
	h.senseNearestBed(kid)
	if !kid.vBedSet || kid.vBed != bed {
		t.Fatalf("with the cache cleared the bed is found: %+v %v", kid.vBed, kid.vBedSet)
	}
}
