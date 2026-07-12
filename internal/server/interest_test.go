package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// drain counts how many packets are queued on a player's out channel.
func drain(p *player) int {
	n := 0
	for {
		select {
		case <-p.out:
			n++
		default:
			return n
		}
	}
}

// TestMoveRelayIsInterestManaged: a move is relayed to a nearby player but NOT
// to one far outside the view radius — the O(n²) fan-out fix.
func TestMoveRelayIsInterestManaged(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(100) // a live clock: at tick 0 the movement budget has no bank yet
	players := map[int32]*tracked{}

	mover := &tracked{p: newPlayer(1, "mover", [16]byte{}), x: 0, z: 0}
	near := &tracked{p: newPlayer(2, "near", [16]byte{}), x: 8, z: 8}                   // same chunk-ish
	far := &tracked{p: newPlayer(3, "far", [16]byte{}), x: (viewRadius + 5) * 16, z: 0} // well out of range
	players[1], players[2], players[3] = mover, near, far
	drain(near.p)
	drain(far.p)

	h.onMove(players, mover, evMove{eid: 1, x: 1, y: 0, z: 0, onGround: true})

	if got := drain(near.p); got == 0 {
		t.Fatal("a nearby player must receive the move relay")
	}
	if got := drain(far.p); got != 0 {
		t.Fatalf("an out-of-range player must NOT receive the move relay, got %d packets", got)
	}
}
