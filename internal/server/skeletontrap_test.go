package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A trap skeleton horse springs when a player comes within ten blocks:
// four skeleton horses each with a helmeted, persistent skeleton on its
// back, the original tamed and disarmed.
func TestSkeletonTrapSprings(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	horse := h.spawnMob(players, entitySkeletonHorse, x, y, z)
	horse.trap = true
	pl := testTracked()
	pl.x, pl.y, pl.z = x+30, y, z
	players[pl.p.eid] = pl
	h.tickSkeletonTraps(players)
	if !horse.trap {
		t.Fatal("a player thirty blocks off must not spring the trap")
	}
	pl.x = x + 6
	h.tickSkeletonTraps(players)
	if horse.trap || !horse.tamed {
		t.Fatal("the trap should have sprung and tamed the horse")
	}
	horses, riders := 0, 0
	for _, m := range h.mobs {
		switch m.etype {
		case entitySkeletonHorse:
			horses++
			if m.mobRider == 0 {
				t.Errorf("horse %d carries no skeleton", m.eid)
			}
		case entitySkeleton:
			riders++
			if m.mount == 0 || !m.persistent || m.gear[0].item == 0 || m.held != itemBow {
				t.Errorf("skeleton %+v: mount=%d persistent=%v helmet=%d held=%d", m.eid, m.mount, m.persistent, m.gear[0].item, m.held)
			}
		}
	}
	if horses != 4 || riders != 4 {
		t.Errorf("%d horses and %d skeletons, want 4 and 4", horses, riders)
	}
	h.tickSkeletonTraps(players) // a sprung trap stays sprung
	n := 0
	for _, m := range h.mobs {
		if m.etype == entitySkeletonHorse {
			n++
		}
	}
	if n != 4 {
		t.Errorf("a second tick spawned more horses: %d", n)
	}
}

// Striders sometimes spawn with a rider; hard-mode spiders sometimes with
// an effect.
func TestStriderRidersAndSpiderEffects(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	riders, calves := 0, 0
	for i := 0; i < 600; i++ {
		s := h.spawnMobIn(players, entityStrider, 1, 0.5, 40, 0.5)
		h.configureNetherMob(players, s)
		if s.mobRider != 0 {
			r := h.mobs[s.mobRider]
			switch {
			case r.etype == entityZombifiedPiglin && s.saddled && r.held == int32(itemWarpedFungusStick):
				riders++
			case r.etype == entityStrider && r.baby:
				calves++
			default:
				t.Errorf("odd rider %+v", r)
			}
			delete(h.mobs, r.eid)
		}
		delete(h.mobs, s.eid)
	}
	if riders < 5 || riders > 50 || calves < 20 || calves > 110 {
		t.Errorf("of 600 striders: %d piglin riders (want ~20), %d calves (want ~58)", riders, calves)
	}
	h.rules.Difficulty = diffHard
	h.tick.Store(2_000_000)
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	withFx := 0
	for i := 0; i < 400; i++ {
		sp := h.spawnHostileY(players, entitySpider, x, y, z)
		if len(sp.effects) > 0 {
			withFx++
		}
		delete(h.mobs, sp.eid)
	}
	if withFx == 0 || withFx > 80 {
		t.Errorf("%d of 400 hard spiders spawned with an effect, want a few percent", withFx)
	}
}
