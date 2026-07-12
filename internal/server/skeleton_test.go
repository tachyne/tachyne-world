package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestSkeletonShootsAndArrowHits(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5 // high in open air: no terrain in the flight path
	players := map[int32]*tracked{1: pl}

	m := h.spawnHostile(players, entitySkeleton, 8, 0)
	m.x, m.y, m.z = 8.5, 80, 0.5
	m.attackCD = 0

	h.skeletonShoot(players, m)
	if len(h.arrows) != 1 {
		t.Fatalf("skeleton should have fired exactly one arrow, got %d", len(h.arrows))
	}
	if m.attackCD != 19 { // 40-tick vanilla cadence on normal (incl. this update)
		t.Fatal("firing must start the shot cooldown")
	}

	// Shots carry vanilla's difficulty-scaled spread now (normal: inaccuracy
	// 6), so a single 8-block shot may legitimately MISS — fire a volley and
	// assert a hit lands within it.
	hp := pl.health
	for shot := 0; shot < 10 && pl.health >= hp; shot++ {
		for i := 0; i < 100 && len(h.arrows) > 0; i++ {
			h.tick.Add(1)
			h.updateArrows(players)
		}
		if pl.health < hp {
			break
		}
		m.attackCD = 0
		h.skeletonShoot(players, m)
	}
	if pl.health >= hp {
		t.Fatalf("no arrow of the volley hit: health still %v", pl.health)
	}
	if len(h.arrows) != 0 {
		t.Fatal("a landed arrow must be removed")
	}
}

func TestArrowSticksInTerrainAndExpires(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	surface := float64(h.world.SurfaceFeet(0, 0))
	pl.x, pl.y, pl.z = 0.5, surface, 0.5
	players := map[int32]*tracked{1: pl}

	// Fire straight down into the ground from above (no player in the path).
	eid := h.allocEID()
	a := &arrowEntity{eid: eid, x: 20.5, y: surface + 10, z: 20.5, vy: -arrowSpeed, born: h.tick.Load()}
	h.arrows[eid] = a

	for i := 0; i < 30 && !a.stuck; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if !a.stuck {
		t.Fatal("arrow fired into terrain must stick")
	}
	for i := 0; i < arrowLifeTicks; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if len(h.arrows) != 0 {
		t.Fatal("a stuck arrow must despawn after its lifetime")
	}
}

func TestSkeletonKites(t *testing.T) {
	h := newHub(world.New(1))
	m := &mob{etype: entitySkeleton, speed: speedFor(entitySkeleton), hasTarget: true, x: 0, z: 0}

	m.tx, m.tz = 2, 0 // target too close — back away (negative x)
	vx, _ := rangedBehavior{}.steer(h, m)
	if vx >= 0 {
		t.Fatalf("skeleton must retreat from a close target, vx=%v", vx)
	}
	m.tx = 14 // too far — advance
	vx, _ = rangedBehavior{}.steer(h, m)
	if vx <= 0 {
		t.Fatalf("skeleton must advance on a distant target, vx=%v", vx)
	}
	m.tx = 8 // sweet spot — STRAFE (vanilla circles the target while shooting)
	vx, vz := rangedBehavior{}.steer(h, m)
	if vx != 0 {
		t.Fatalf("strafing must be perpendicular to the firing line, got radial vx=%v", vx)
	}
	if vz == 0 {
		t.Fatal("in bow range the skeleton must circle its target, not freeze")
	}
	if math.Abs(vz) > m.speed*0.5+1e-9 {
		t.Fatalf("vanilla strafes at half speed, got %v", vz)
	}
}
