package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Hacked-client movement simulations: the hub must reject every physically
// impossible position claim (AUTHORITY) while never tripping on vanilla play.

// walkSetup places a survival player on the surface at (0.5, 0.5) with an
// empty movement budget and the clock at a known tick.
func walkSetup(h *hub) (*tracked, map[int32]*tracked) {
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	pl.y = float64(h.world.SurfaceFeet(0, 0))
	h.tick.Store(100)
	pl.lastMoveTick = 100
	pl.moveBudget = 0
	return pl, map[int32]*tracked{1: pl}
}

func TestLegitWalkIsNeverRejected(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	start := pl.x
	// Sprint-jump pace (~7.1 m/s = 0.36/tick) along +x for 4 seconds, feet
	// following the terrain like a real walker (fixed-altitude motion would
	// clip through hillsides — and correctly get rejected as noclip now).
	for i := 1; i <= 80; i++ {
		h.tick.Store(100 + uint64(i))
		nx := pl.x + 0.36
		ny := float64(h.world.SurfaceFeet(int(nx), 0))
		h.onMove(players, pl, evMove{eid: 1, x: nx, y: ny, z: pl.z, onGround: true})
	}
	if got := pl.x - start; got < 0.36*79 {
		t.Fatalf("legitimate sprint was rejected: advanced %.1f of %.1f blocks", got, 0.36*80)
	}
}

func TestDiagonalMotionCostsEuclideanNotAxisSum(t *testing.T) {
	// Regression (live bug): moving forward AND up at once was charged
	// hypot(dx,dz)+dy — an axis SUM — so uphill sprint-jumping drained the
	// budget ~40% faster than flat sprinting and rubber-banded legit climbing.
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	pl.moveBudget = 1.0
	pl.lastMoveTick = 101 // no replenish on this event: the budget is exactly 1.0
	h.tick.Store(101)
	// Euclidean cost √(0.6²+0.6²)=0.85 fits the budget; the old sum (1.2) didn't.
	h.onMove(players, pl, evMove{eid: 1, x: pl.x + 0.6, y: pl.y + 0.6, z: pl.z})
	if math.Abs(pl.x-(0.5+0.6)) > 0.01 { // relative-move fixed point quantizes slightly
		t.Fatalf("diagonal up+forward move within Euclidean budget was rejected (x=%v)", pl.x)
	}
}

func TestSustainedUphillSprintIsNeverRejected(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	start := pl.x
	// Sprint-jumping up a mountainside: ~7.1 m/s forward + constant step-up
	// gain. In real play terrain contact resets floatTicks; this test isolates
	// the speed budget, so reset it manually.
	for i := 1; i <= 200; i++ {
		h.tick.Store(100 + uint64(i))
		pl.floatTicks = 0
		h.onMove(players, pl, evMove{eid: 1, x: pl.x + 0.36, y: pl.y + 0.36, z: pl.z, onGround: true})
	}
	if got := pl.x - start; got < 0.36*199 {
		t.Fatalf("sustained uphill sprint was throttled: advanced %.1f of %.1f", got, 0.36*200)
	}
}

func TestSpeedHackOutrunsItsBudget(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	start := pl.x
	// 3 blocks per tick = 60 m/s, eight times sprint speed.
	for i := 1; i <= 40; i++ {
		h.tick.Store(100 + uint64(i))
		h.onMove(players, pl, evMove{eid: 1, x: pl.x + 3, y: pl.y, z: pl.z, onGround: true})
	}
	// A compliant server would have moved 120 blocks; the budget (0.5/tick +
	// burst bank) must cap the actual advance far below that.
	if got := pl.x - start; got > 40 {
		t.Fatalf("speed hack advanced %.1f blocks in 2s (budget should cap near ~30)", got)
	}
}

func TestTeleportHackRejectedOutright(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	x, y, z := pl.x, pl.y, pl.z
	h.tick.Store(101)
	h.onMove(players, pl, evMove{eid: 1, x: x + 50, y: y, z: z + 50})
	if pl.x != x || pl.y != y || pl.z != z {
		t.Fatalf("teleport hack was applied: now at (%.1f, %.1f, %.1f)", pl.x, pl.y, pl.z)
	}
	// The /tp path (server-initiated) still passes.
	h.onMove(players, pl, evMove{eid: 1, x: x + 50, y: y, z: z + 50, teleport: true})
	if pl.x == x && pl.z == z {
		t.Fatal("server-initiated teleport must be exempt from validation")
	}
}

func TestNaNPositionRejected(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	x := pl.x
	h.tick.Store(101)
	h.onMove(players, pl, evMove{eid: 1, x: math.NaN(), y: math.Inf(1), z: pl.z})
	if pl.x != x || math.IsNaN(pl.x) || math.IsInf(pl.y, 0) {
		t.Fatalf("non-finite position poisoned the tracked state: (%v, %v)", pl.x, pl.y)
	}
	h.onMove(players, pl, evMove{eid: 1, x: pl.x, y: pl.y, z: pl.z, yaw: float32(math.NaN())})
	if math.IsNaN(float64(pl.yaw)) {
		t.Fatal("NaN yaw must be rejected")
	}
}

func TestFlyHackIsGrounded(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	surface := pl.y
	pl.y = surface + 20 // hovering high in open air
	h.tick.Store(1000)
	pl.lastMoveTick = 1000
	// Hover in place (never descending) for well past floatLimit ticks.
	for i := 1; i <= 30; i++ {
		h.tick.Store(1000 + uint64(i*5))
		h.onMove(players, pl, evMove{eid: 1, x: 0.5, y: surface + 20, z: 0.5})
	}
	if pl.y > surface+2 {
		t.Fatalf("fly hack kept hovering at y=%.1f (surface %.1f)", pl.y, surface)
	}
}

func TestStandingStillNeverTripsFloatCheck(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	y := pl.y                  // feet on the ground: the block below is always within reach
	for i := 1; i <= 60; i++ { // 300 ticks ≫ floatLimit
		h.tick.Store(100 + uint64(i*5))
		h.onMove(players, pl, evMove{eid: 1, x: 0.5, y: y, z: 0.5, onGround: true})
	}
	if pl.y != y || pl.floatTicks != 0 {
		t.Fatalf("grounded player tripped the float check: y=%.1f floatTicks=%d", pl.y, pl.floatTicks)
	}
}

func TestCreativeMayFly(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	pl.gamemode = gmCreative
	surface := pl.y
	pl.y = surface + 30
	// Sustained diagonal ascending SPRINT-fly: ~1.09 blocks/tick horizontal
	// (double-tap sprint while flying, vanilla's fastest creative motion) plus
	// 0.38/tick up — legitimate, must never hitch. This is the case that
	// rubber-banded at flyPerTick=1.0 (it drained the bank over a few seconds).
	for i := 1; i <= 400; i++ {
		h.tick.Store(100 + uint64(i))
		h.onMove(players, pl, evMove{eid: 1, x: pl.x + 1.09, y: pl.y + 0.38, z: pl.z})
	}
	if pl.y < surface+30+0.38*399 {
		t.Fatalf("creative sprint-flight was throttled: y=%.1f (want >= %.1f)", pl.y, surface+30+0.38*399)
	}
}
