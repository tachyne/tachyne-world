package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// patrolSetup puts a captain and one companion on real ground. The terrain
// decides which way a ten-block leg can actually go — MobFeet starts from the
// generator's own column height, and a generated tree makes a column
// unwalkable whatever is edited into it — so the tests pick the heading
// rather than assuming one.
func patrolSetup(t *testing.T) (*hub, map[int32]*tracked, *mob, *mob) {
	t.Helper()
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	h.playersRef = players
	lx, lz := h.findLand(0, 0)
	y := float64(h.world.MobFeet(lx, lz))
	capt := h.spawnMob(players, entityPillager, float64(lx)+0.5, y, float64(lz)+0.5)
	capt.patrolCaptain = true
	capt.patrolling = true
	mate := h.spawnMob(players, entityPillager, float64(lx)+1.5, y, float64(lz)+0.5)
	return h, players, capt, mate
}

// aimPatrol points the captain at a distant target on each compass heading in
// turn and returns the first that produced a leg (want=true) or refused one
// (want=false), with the heading it used.
func aimPatrol(h *hub, players map[int32]*tracked, capt *mob, want bool) (dx, dz float64, ok bool) {
	for i := 0; i < 16; i++ {
		ang := float64(i) * math.Pi / 8
		sx, sz := math.Cos(ang), math.Sin(ang)
		capt.patrolLeg = blockPos{}
		capt.patrolCooldown = 0
		capt.patrolTarget = blockPos{
			x: floorInt(capt.x + sx*400), y: floorInt(capt.y), z: floorInt(capt.z + sz*400),
		}
		if h.patrolStep(players, capt) == want {
			return sx, sz, true
		}
	}
	return 0, 0, false
}

// A patrol captain walks toward its distant target and hands each leg to the
// pillagers with it, which is what starts them patrolling. Patrols used to
// spawn and then amble on the wander goal, so they never crossed the country.
func TestPatrolCaptainLeadsItsCompanions(t *testing.T) {
	h, players, capt, mate := patrolSetup(t)
	if mate.patrolling {
		t.Fatal("a follower does not patrol until it is handed a waypoint")
	}
	if _, _, ok := aimPatrol(h, players, capt, true); !ok {
		t.Skip("no heading with ten walkable blocks from this spot")
	}
	if capt.vx == 0 && capt.vz == 0 {
		t.Error("the captain should be moving along its leg")
	}
	if capt.patrolLeg == (blockPos{}) {
		t.Fatal("it should have plotted a leg")
	}
	if got := math.Hypot(float64(capt.patrolLeg.x)-capt.x, float64(capt.patrolLeg.z)-capt.z); got > patrolLegLength+2 {
		t.Errorf("the leg should be about ten blocks out, got %.1f", got)
	}
	if !mate.patrolling || mate.patrolTarget != capt.patrolLeg {
		t.Errorf("the companion should have been handed the captain's leg, got %v patrolling=%v",
			mate.patrolTarget, mate.patrolling)
	}
}

// findPatrolTarget picks somewhere up to five hundred blocks off and starts
// the mob patrolling; a captain that has arrived picks a fresh one.
func TestPatrolRetargetsOnArrival(t *testing.T) {
	h, players, capt, _ := patrolSetup(t)
	capt.patrolling, capt.patrolTarget = false, blockPos{}
	h.findPatrolTarget(capt)
	if !capt.patrolling || capt.patrolTarget == (blockPos{}) {
		t.Fatal("finding a target should start the captain patrolling")
	}
	if d := math.Hypot(float64(capt.patrolTarget.x)-capt.x, float64(capt.patrolTarget.z)-capt.z); d > 800 {
		t.Errorf("the target should be within five hundred blocks each way, got %.0f", d)
	}
	capt.patrolTarget = blockPos{floorInt(capt.x) + 3, floorInt(capt.y), floorInt(capt.z)} // all but arrived
	h.patrolStep(players, capt)
	if d := math.Hypot(float64(capt.patrolTarget.x)-capt.x, float64(capt.patrolTarget.z)-capt.z); d < patrolArrived {
		t.Errorf("arriving should have picked a new, distant target, still %.1f away", d)
	}
}

// A patroller with no companions left stops being one, and a leg with nowhere
// to stand is vanilla's failed moveTo: rest two hundred ticks, stay a patrol.
func TestPatrolDisbandsAndRests(t *testing.T) {
	h, players, capt, mate := patrolSetup(t)
	_, _, hadGround := aimPatrol(h, players, capt, false)
	if hadGround {
		if capt.patrolCooldown != patrolFailCooldown {
			t.Errorf("a leg with no ground should rest for %d ticks, got %d",
				patrolFailCooldown, capt.patrolCooldown)
		}
		if !capt.patrolling {
			t.Error("resting is not disbanding: it is still a patrol")
		}
	}
	// Take the companion away and the patrol stops being one.
	delete(h.mobs, mate.eid)
	capt.patrolLeg, capt.patrolCooldown = blockPos{}, 0
	capt.patrolTarget = blockPos{floorInt(capt.x) + 400, floorInt(capt.y), floorInt(capt.z)}
	if h.patrolStep(players, capt) {
		t.Error("a patroller with nobody left should not keep walking")
	}
	if capt.patrolling {
		t.Error("…and should stop being a patrol at all")
	}
}
