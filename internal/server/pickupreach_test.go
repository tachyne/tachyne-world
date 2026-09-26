package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// pickupScene lays a floor at y=180, stands a survival player at (px, 180,
// 0.5) and drops a redstone torch at rest at (ix, iy, 0.5), then runs the
// tick's item physics and pickup until the pickup delay has passed.
func pickupScene(t *testing.T, px, ix, iy float64, setup func(h *hub, pl *tracked)) (picked bool) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	flatFloor(h.world, 0, 180, 0, 3)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = px, 180, 0.5
	pl.p.onGround = true
	players := map[int32]*tracked{pl.p.eid: pl}
	if setup != nil {
		setup(h, pl)
	}
	torch := int32(itemByName["redstone_torch"])
	it := h.spawnItemAt(players, 0, torch, 1, ix, iy, 0.5, 0, 0, 0)
	for i := 0; i < pickupDelay+5 && h.items[it.eid] != nil; i++ {
		h.tick.Add(1)
		h.tickItems(players)
		h.pickupItems(players)
	}
	return h.items[it.eid] == nil
}

// Report #46: standing at the west edge of a block, a torch lying on the
// next block over is within Player.aiStep's pickup area — the player's box
// widened a block each way — though its centre is more than a block off.
func TestPickupReachesTheNextBlockFromTheEdge(t *testing.T) {
	// Player box x ∈ [0.01, 0.61], so the area reaches -0.99; the torch's
	// box at -0.8 spans [-0.925, -0.675]: 1.11 blocks centre to centre.
	if !pickupScene(t, 0.31, -0.8, 180, nil) {
		t.Fatal("an item on the next block, touching the widened box, must be picked up")
	}
	// A torch whose box ends before the area does stays where it is.
	if pickupScene(t, 0.31, -1.2, 180, nil) {
		t.Fatal("an item clear of the area (box edge at -1.075 vs -0.99) must stay")
	}
}

// The area reaches half a block below the feet and half above the head.
func TestPickupAreaHeight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	lo, hi := h.pickupArea(pl)
	if !near(lo[1], 179.5) || !near(hi[1], 182.3) || !near(lo[0], -0.8) || !near(hi[0], 1.8) {
		t.Fatalf("standing area %v..%v, want y 179.5..182.3 and x -0.8..1.8", lo, hi)
	}
	pl.p.sneaking = true
	if _, hi := h.pickupArea(pl); !near(hi[1], 182) {
		t.Fatalf("a crouched player's area tops out at %v, want 182", hi[1])
	}
}

// A rider's area stretches over its boat, then widens sideways only.
func TestPickupAreaCoversTheVehicle(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180.3, 0.5
	v := &vehicle{eid: 900, etype: entityID("oak_boat"), x: 0.5, y: 180, z: 0.5}
	h.vehicles[v.eid] = v
	pl.ridingEID = v.eid
	lo, hi := h.pickupArea(pl)
	if !near(lo[1], 180) || !near(hi[1], 182.1) {
		t.Fatalf("riding area y %v..%v, want the boat's floor to the head (180..182.1)", lo[1], hi[1])
	}
	if !near(lo[0], 0.5-1.375/2-1) {
		t.Fatalf("riding area west edge %v, want the boat's side a block out", lo[0])
	}
}
