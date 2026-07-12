package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestNoclipIntoSolidRejected(t *testing.T) {
	h := newHub(world.New(1))
	pl, players := walkSetup(h)
	h.tick.Store(101)
	// Claim a position with the head inside the hillside below the surface.
	deep := pl.y - 5
	h.onMove(players, pl, evMove{eid: 1, x: pl.x, y: deep, z: pl.z})
	if pl.y == deep {
		t.Fatal("phasing into solid ground must be rejected")
	}
	// But escaping FROM inside a block is always allowed: bury them first.
	w := h.world
	fx, fz := 0, 0
	w.SetBlock(fx, int(pl.y), fz, worldgen.Stone)
	w.SetBlock(fx, int(pl.y)+1, fz, worldgen.Stone)
	h.tick.Store(140)
	h.onMove(players, pl, evMove{eid: 1, x: pl.x + 1, y: pl.y, z: pl.z})
	if pl.x != 1.5 {
		t.Fatalf("escaping a burial must be allowed, x=%v", pl.x)
	}
}

func TestBuriedHeadSuffocates(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	pl.food = 10
	players := map[int32]*tracked{1: pl}
	h.world.SetBlock(0, 71, 0, worldgen.Stone) // head block
	before := pl.health
	h.survivalTick(players)
	if pl.health != before-suffocateDamagePerSec {
		t.Fatalf("buried head should take %d/s, health %v→%v", suffocateDamagePerSec, before, pl.health)
	}
}

func TestFastBreakCheatReverted(t *testing.T) {
	// Stone (hardness 1.5) by hand needs ~22 ticks even at 50% tolerance; a
	// Finish 1 tick after Start is a cheat.
	if minDigTicks(worldgen.Stone, 0) < 5 {
		t.Fatalf("stone-by-hand minimum should be many ticks, got %d", minDigTicks(worldgen.Stone, 0))
	}
	// A diamond pick is legitimately fast — the floor scales with the tool.
	if minDigTicks(worldgen.Stone, itemByName["diamond_pickaxe"]) >= minDigTicks(worldgen.Stone, 0) {
		t.Fatal("tools must lower the floor")
	}
	if minDigTicks(worldgen.GrassBlock, 0) <= 0 {
		t.Fatal("even dirt has a nonzero floor by hand")
	}
}

func TestStreamGateFollowsHubPosition(t *testing.T) {
	p := newPlayer(1, "gate", [16]byte{})
	p.x, p.z = 500, 500 // connection-side claim, far from anything validated
	p.setHubPos(0.5, 0.5)
	if p.streamAllowed() {
		t.Fatal("chunks must not stream around an unvalidated position")
	}
	p.setHubPos(495, 505) // hub caught up — normal play
	if !p.streamAllowed() {
		t.Fatal("streaming should resume once the hub agrees")
	}
}
