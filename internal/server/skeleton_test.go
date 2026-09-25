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
	for i := 0; i < arrowGroundLifeTicks-1; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if len(h.arrows) != 1 {
		t.Fatal("a stuck arrow lasts a minute in the ground (AbstractArrow.tickDespawn)")
	}
	h.tick.Add(1)
	h.updateArrows(players)
	if len(h.arrows) != 0 {
		t.Fatal("a stuck arrow must despawn after 1200 ticks in the ground")
	}
}

func TestSkeletonKites(t *testing.T) {
	h := newHub(world.New(1))
	m := &mob{etype: entitySkeleton, hasTarget: true, x: 0, z: 0}
	m.setMoveSpeed(speedFor(entitySkeleton))

	// Outside the bow radius (15) the goal simply closes, at full speed and
	// straight at the target.
	m.tx, m.tz = 20, 0
	vx, vz := rangedBehavior{}.steer(h, m)
	if vx <= 0 || math.Abs(vz) > 1e-9 {
		t.Fatalf("a distant target is closed on directly: vx=%v vz=%v", vx, vz)
	}
	if math.Abs(vx-m.moveSpeed()) > 1e-9 {
		t.Fatalf("closing runs at full speed, got %v", vx)
	}
	// Inside a quarter of the radius squared (7.5 blocks) it drifts backwards
	// while it circles — a sideways component and a negative radial one.
	m.tx = 4
	vx, vz = rangedBehavior{}.steer(h, m)
	if vx >= 0 {
		t.Fatalf("a close target is backed away from: vx=%v", vx)
	}
	if vz == 0 {
		t.Fatal("it circles while it backs off, not straight out")
	}
	// The two half-speed components never exceed the mob's speed.
	if got := math.Hypot(vx, vz); got > m.moveSpeed()+1e-9 {
		t.Fatalf("strafing at %v exceeds the walking speed %v", got, m.moveSpeed())
	}
	// Past three quarters of the radius squared (about 13) it turns back in.
	m.tx = 14
	vx, _ = rangedBehavior{}.steer(h, m)
	if vx <= 0 {
		t.Fatalf("inside the radius but far out, it closes again: vx=%v", vx)
	}
}

// AbstractSkeleton.reassessWeaponGoal: what it holds decides the goal it
// runs — a bow keeps its distance, anything else walks in and swings.
func TestSkeletonReassessesItsWeapon(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnHostileY(players, entitySkeleton, 0.5, 70, 0.5)
	m.held = itemBow
	h.reassessWeapon(m)
	if _, ok := m.behavior.(rangedBehavior); !ok {
		t.Fatalf("a bow skeleton shoots, got %s", m.behavior.name())
	}
	m.held = itemIronSword
	h.reassessWeapon(m)
	if _, ok := m.behavior.(hostileBehavior); !ok {
		t.Fatalf("a sworded skeleton melees, got %s", m.behavior.name())
	}
	m.held = itemBow
	h.reassessWeapon(m)
	if _, ok := m.behavior.(rangedBehavior); !ok {
		t.Fatalf("picking a bow back up goes back to shooting, got %s", m.behavior.name())
	}
	// Wither skeletons are not in the family that reassesses (they have no
	// bow goal at all), so their stance is left alone.
	ws := h.spawnHostileY(players, entityWitherSkeleton, 3.5, 70, 0.5)
	before := ws.behavior.name()
	ws.held = itemBow
	h.reassessWeapon(ws)
	if ws.behavior.name() != before {
		t.Errorf("a wither skeleton's goal should not change, %s → %s", before, ws.behavior.name())
	}
}
