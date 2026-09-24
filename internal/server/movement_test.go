package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Hacked-client movement simulations: the hub must reject every physically
// impossible position claim (AUTHORITY) while never tripping on vanilla play.

// walkSetup places a survival player on the surface at (0.5, 0.5) with an
// the clock at a known tick.
func walkSetup(h *hub) (*tracked, map[int32]*tracked) {
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	pl.y = float64(h.world.SurfaceFeet(0, 0))
	h.tick.Store(100)
	pl.lastMoveTick = 100
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

// Vanilla's "moved too quickly": a packet may carry the player up to 10
// blocks (squared 100) from the last good position, per move packet seen in
// the tick; sustained fast motion within that is never refused.
func TestMovedTooQuicklyIsVanillas(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	pl.gamemode = gmCreative
	pl.y += 30
	x := pl.x
	h.tick.Store(101)
	h.onMove(players, pl, evMove{eid: 1, x: x + 9.9, y: pl.y, z: pl.z})
	if pl.x != x+9.9 {
		t.Fatalf("a 9.9-block packet was refused (x=%v)", pl.x)
	}
	h.tick.Store(102)
	h.onMove(players, pl, evMove{eid: 1, x: pl.x + 10.1, y: pl.y, z: pl.z})
	if pl.x != x+9.9 {
		t.Fatalf("a 10.1-block packet was applied (x=%v)", pl.x)
	}
	// Fast creative flight, 3 blocks a tick for ten seconds: never refused.
	for i := 1; i <= 200; i++ {
		h.tick.Store(200 + uint64(i))
		h.onMove(players, pl, evMove{eid: 1, x: pl.x + 3, y: pl.y, z: pl.z})
	}
	if pl.x < x+9.9+3*199 {
		t.Fatalf("fast flight was throttled: x=%.1f", pl.x)
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
	// 0.38/tick up — legitimate, must never hitch.
	for i := 1; i <= 400; i++ {
		h.tick.Store(100 + uint64(i))
		h.onMove(players, pl, evMove{eid: 1, x: pl.x + 1.09, y: pl.y + 0.38, z: pl.z})
	}
	if pl.y < surface+30+0.38*399 {
		t.Fatalf("creative sprint-flight was throttled: y=%.1f (want >= %.1f)", pl.y, surface+30+0.38*399)
	}
}
