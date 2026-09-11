package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A baby zombie can mount a nearby chicken (or one spawned for it), the
// chicken then follows under the zombie, lays no eggs, pays ten experience
// and is not a persistent creature; a spider can spawn with a skeleton on
// its back that stays glued to it.
func TestJockeys(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))

	// Force the ride: a chicken beside a baby zombie, rolled by hand.
	chicken := h.spawnSpecies(players, entityChicken, 0, x+1, y, z)
	zombie := h.spawnHostileY(players, entityZombie, x, y, z)
	zombie.baby = true
	zombie.mount, zombie.mountDrives, chicken.mobRider, chicken.jockey = 0, false, 0, false
	chicken.jockey = true
	h.mountMobOn(players, zombie, chicken, true)
	if zombie.mount != chicken.eid || chicken.mobRider != zombie.eid || !zombie.mountDrives {
		t.Fatalf("mount: zombie.mount=%d chicken.rider=%d drives=%v", zombie.mount, chicken.mobRider, zombie.mountDrives)
	}
	zombie.x, zombie.z = x+4, z+4
	h.updateMobs(players)
	if chicken.x != zombie.x || chicken.z != zombie.z {
		t.Errorf("the chicken should be carried under its rider: chicken %.1f,%.1f zombie %.1f,%.1f", chicken.x, chicken.z, zombie.x, zombie.z)
	}
	if xp := xpForMob(chicken, h.rng.Intn); xp != 10 {
		t.Errorf("jockey chicken pays %d, want 10", xp)
	}
	chicken.eggIn = 1
	h.updateBreeding(players)
	for _, it := range h.items {
		if it.item == itemEgg {
			t.Error("a jockey chicken lays no eggs")
		}
	}
	if !chicken.jockey || mobSpawnCategory(chicken) != catCreature {
		t.Error("the chicken is still a creature by category; the sweep treats jockeys as monsters")
	}
	// The rider dies: the chicken sheds it.
	delete(h.mobs, zombie.eid)
	h.updateMobs(players)
	if chicken.mobRider != 0 {
		t.Error("a chicken whose rider is gone carries nobody")
	}

	// Over many baby zombies with chickens about, some become jockeys.
	rides := 0
	for i := 0; i < 400; i++ {
		c := h.spawnSpecies(players, entityChicken, 0, x, y, z)
		zb := h.spawnHostileY(players, entityZombie, x, y, z)
		zb.baby = true
		h.rollChickenJockey(players, zb)
		if zb.mount != 0 {
			rides++
		}
		delete(h.mobs, c.eid)
		delete(h.mobs, zb.eid)
	}
	if rides < 8 || rides > 60 {
		t.Errorf("%d of 400 baby zombies mounted a chicken, want about 5%%", rides)
	}

	// A spider jockey: the skeleton is glued to the spider.
	spider := h.spawnHostileY(players, entitySpider, x, y, z)
	sk := h.spawnHostileY(players, entitySkeleton, x, y, z)
	h.mountMobOn(players, sk, spider, false)
	spider.x, spider.z = x+3, z-2
	h.updateMobs(players)
	if sk.x != spider.x || sk.z != spider.z || sk.mountDrives {
		t.Errorf("the skeleton should ride the spider: sk %.1f,%.1f spider %.1f,%.1f", sk.x, sk.z, spider.x, spider.z)
	}
}

// A saved jockey pair comes back mounted, and a reloaded baby zombie does
// not roll a fresh chicken.
func TestMountsSurviveReload(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	chicken := h.spawnSpecies(players, entityChicken, 0, x, y, z)
	zombie := h.spawnHostileY(players, entityZombie, x, y, z)
	zombie.baby, chicken.jockey = true, true
	h.mountMobOn(players, zombie, chicken, true)
	sz, sc := toSavedMob(zombie), toSavedMob(chicken)
	if sz.Mount != chicken.eid || !sz.MountDrives || sz.EID != zombie.eid || !sc.Jockey {
		t.Fatalf("saved rider %+v", sz)
	}
	h2 := newHub(world.New(1))
	h2.reloading = true
	byOld := map[int32]*mob{}
	var riders []*mob
	for _, sm := range []savedMob{sz, sc} {
		sm := sm
		m := h2.reloadMob(players, &sm)
		byOld[sm.EID] = m
		if m.savedMount != 0 {
			riders = append(riders, m)
		}
	}
	h2.relinkMounts(players, riders, byOld)
	h2.reloading = false
	z2, c2 := byOld[zombie.eid], byOld[chicken.eid]
	if z2.mount != c2.eid || c2.mobRider != z2.eid || !z2.mountDrives || !c2.jockey {
		t.Errorf("relink: zombie.mount=%d chicken.rider=%d drives=%v jockey=%v", z2.mount, c2.mobRider, z2.mountDrives, c2.jockey)
	}
	// Reloading many baby zombies must not spawn chickens.
	h3 := newHub(world.New(1))
	h3.reloading = true
	for i := 0; i < 200; i++ {
		sm := toSavedMob(zombie)
		sm.Mount, sm.EID = 0, 0
		h3.reloadMob(players, &sm)
	}
	h3.reloading = false
	for _, m := range h3.mobs {
		if m.etype == entityChicken {
			t.Fatal("a reload rolled a jockey chicken")
		}
	}
}
