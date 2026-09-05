package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The hub is the sole consumer of h.events. Hub-goroutine code that posts to
// it while the queue is full blocks forever — the tick loop stops, every
// gateway reader then parks behind it, and the process sits there passing its
// TCP liveness probe. Fire igniting TNT used to do exactly that.
//
// The test fills the queue to capacity (nothing is draining — no hub loop
// runs here, just as none can run while the hub itself is blocked) and then
// runs the fire step onto a TNT block. It must return.
func TestFireIgnitingTNTNeverBlocksOnTheEventQueue(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	h.playersRef = players

	for i := 0; i < cap(h.events); i++ {
		h.events <- evChat{text: "filler"}
	}
	if len(h.events) != cap(h.events) {
		t.Fatalf("queue not full: %d/%d", len(h.events), cap(h.events))
	}

	pos := blockPos{0, 70, 0}
	w.SetBlock(pos.x, pos.y, pos.z, tntStateMin)

	done := make(chan struct{})
	go func() {
		defer close(done)
		// resilience 1 → Intn(1) == 0 < burn: the block always "catches".
		h.checkBurnOut(players, pos, 1, 0)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("checkBurnOut blocked on a full event queue — hub self-post deadlock")
	}

	if got := w.Block(pos.x, pos.y, pos.z); got != worldgen.Air {
		t.Errorf("TNT block still present (state %d): the direct prime path did not run", got)
	}
	if len(h.tnt) != 1 {
		t.Errorf("%d primed charges, want 1", len(h.tnt))
	}
}

// postFromHub is the sanctioned escape hatch for hub-side deferral. It never
// blocks: with room it queues, without room it panics rather than deadlocks.
func TestPostFromHubNeverBlocks(t *testing.T) {
	h := newHub(world.New(1))
	h.postFromHub(evChat{text: "x"})
	if len(h.events) != 1 {
		t.Fatalf("queued %d events, want 1", len(h.events))
	}
	for len(h.events) < cap(h.events) {
		h.events <- evChat{text: "filler"}
	}
	defer func() {
		if recover() == nil {
			t.Error("postFromHub with a full queue returned instead of panicking")
		}
	}()
	h.postFromHub(evChat{text: "one too many"})
}
