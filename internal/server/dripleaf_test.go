package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func flatDripleaf(t *testing.T) uint32 {
	t.Helper()
	s := worldgen.BlockBase("big_dripleaf")
	info, ok := worldgen.InfoForState(s)
	if !ok {
		t.Fatal("no big_dripleaf info")
	}
	s = worldgen.SetProperty(info, s, "waterlogged", "false")
	return worldgen.SetProperty(info, s, "tilt", "none")
}

// TestDripleafTiltsUnderALoad: standing on a leaf tips it unstable → partial
// → full on vanilla's clock, and it springs back a hundred ticks later; a
// neighbour update between stages leaves the clock alone.
func TestDripleafTiltsUnderALoad(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	pos := blockPos{0, 179, 0}
	leaf := flatDripleaf(t)
	w.ForceLoad(0, 0, 2)
	w.SetBlock(0, 179, 0, leaf)
	pl.x, pl.y, pl.z = 0.5, 179.9375, 0.5
	pl.onGround = false
	h.insideBoth(players)
	if dripleafTilt(w.At(0, 179, 0)) != "none" {
		t.Fatal("a player in the air does not tip the leaf")
	}
	pl.onGround = true
	h.insideBoth(players)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "unstable" {
		t.Fatalf("stepped on: tilt %q, want unstable", got)
	}
	due, ok := h.blockTickDue(0, pos, leaf)
	if !ok || due != h.tick.Load()+10 {
		t.Fatalf("unstable lasts 10 ticks: due %d (pending %v) now %d", due, ok, h.tick.Load())
	}
	// A neighbour's update before the clock runs out changes nothing.
	h.processUpdate(players, 0, pos)
	stepTicks(h, players, 9)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "unstable" {
		t.Fatalf("early update advanced the tilt to %q", got)
	}
	stepTicks(h, players, 1)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "partial" {
		t.Fatalf("after 10 ticks: %q, want partial", got)
	}
	stepTicks(h, players, 10)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "full" {
		t.Fatalf("after 20 ticks: %q, want full", got)
	}
	if due, _ := h.blockTickDue(0, pos, leaf); due != h.tick.Load()+100 {
		t.Fatalf("full lasts 100 ticks: due %d now %d", due, h.tick.Load())
	}
	stepTicks(h, players, 100)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "none" {
		t.Fatalf("after 120 ticks it springs back: %q", got)
	}
	if _, ok := h.blockTickDue(0, pos, leaf); ok {
		t.Fatal("a flat leaf has no clock")
	}
}

// TestDripleafShotKeepsThePendingTick: setTiltAndScheduleTick asks for the
// new stage's tick, but one already pending is kept (Level.scheduleTick) —
// a leaf shot while unstable goes FULL at once and springs back on the
// unstable stage's clock, not a hundred ticks later.
func TestDripleafShotKeepsThePendingTick(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	w.ForceLoad(0, 0, 2)
	pos := blockPos{0, 179, 0}
	leaf := flatDripleaf(t)
	w.SetBlock(0, 179, 0, leaf)
	pl.x, pl.y, pl.z = 0.5, 179.9375, 0.5
	pl.onGround = true
	h.insideBoth(players)
	due, ok := h.blockTickDue(0, pos, leaf)
	if !ok {
		t.Fatal("stepping on the leaf booked no tick")
	}
	stepTicks(h, players, 5)
	h.projectileHitBlock(players, &arrowEntity{dim: 0}, pos, w.At(0, 179, 0))
	if got := dripleafTilt(w.At(0, 179, 0)); got != "full" {
		t.Fatalf("shot: %q, want full", got)
	}
	if again, _ := h.blockTickDue(0, pos, leaf); again != due {
		t.Fatalf("the shot replaced the pending tick (due %d, was %d)", again, due)
	}
	stepTicks(h, players, int(due-h.tick.Load()))
	if got := dripleafTilt(w.At(0, 179, 0)); got != "none" {
		t.Fatalf("on the unstable tick a full leaf springs back: %q", got)
	}
}

// TestDripleafPinnedByPowerAndShot: a signal beside the leaf keeps it flat,
// and a projectile tips it fully at once.
func TestDripleafPinnedByPowerAndShot(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	pos := blockPos{0, 179, 0}
	w.SetBlock(0, 179, 0, flatDripleaf(t))
	w.SetBlock(1, 179, 0, worldgen.BlockBase("redstone_block"))
	pl.x, pl.y, pl.z = 0.5, 179.9375, 0.5
	pl.onGround = true
	h.insideBoth(players)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "none" {
		t.Fatalf("a powered leaf stays flat, got %q", got)
	}
	w.SetBlock(1, 179, 0, worldgen.Air)
	a := &arrowEntity{dim: 0}
	h.projectileHitBlock(players, a, pos, w.At(0, 179, 0))
	if got := dripleafTilt(w.At(0, 179, 0)); got != "full" {
		t.Fatalf("shot: %q, want full", got)
	}
	// Power arriving resets a tilted leaf (neighborChanged).
	w.SetBlock(1, 179, 0, worldgen.BlockBase("redstone_block"))
	h.processUpdate(players, 0, pos)
	if got := dripleafTilt(w.At(0, 179, 0)); got != "none" {
		t.Fatalf("powered: %q, want none", got)
	}
}
