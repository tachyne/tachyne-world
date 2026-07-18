package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// TestReliableLifecycleOverflow is the regression guard for the "ghost mob" bug:
// a despawn (EntityRemove) dropped under back-pressure left the client rendering
// a frozen entity forever. Lifecycle frames must divert to the reliable overflow
// instead of dropping, while ordinary position frames still drop.
func TestReliableLifecycleOverflow(t *testing.T) {
	p := newPlayer(1, "x", [16]byte{})

	// Saturate the outbound channel so every non-blocking send would drop.
	for i := 0; i < cap(p.out); i++ {
		p.out <- outPkt{ev: attachproto.EntityMove{}}
	}

	// A droppable frame is dropped (out full) — it must NOT enter the overflow.
	p.trySendEv(attachproto.EntityMove{})
	if len(p.crit) != 0 {
		t.Fatalf("droppable frame leaked into the reliable overflow: crit=%d", len(p.crit))
	}

	// A lifecycle removal must survive: parked in crit, never dropped.
	p.trySendEv(attachproto.EntityRemove{EIDs: []int32{7}})
	if len(p.crit) != 1 {
		t.Fatalf("lifecycle removal dropped instead of parked: crit=%d", len(p.crit))
	}

	// Once the overflow is active, later lifecycle frames stay in crit for FIFO,
	// even though there's now room appearing in out.
	<-p.out // free a slot
	p.trySendEv(attachproto.EntityAdd{})
	if len(p.crit) != 2 {
		t.Fatalf("lifecycle frame jumped the overflow queue: crit=%d", len(p.crit))
	}

	// Draining yields both in order.
	batch := p.takeCrit()
	if len(batch) != 2 {
		t.Fatalf("takeCrit len=%d, want 2", len(batch))
	}
	if _, ok := batch[0].ev.(attachproto.EntityRemove); !ok {
		t.Fatalf("crit[0] = %T, want EntityRemove", batch[0].ev)
	}
	if _, ok := batch[1].ev.(attachproto.EntityAdd); !ok {
		t.Fatalf("crit[1] = %T, want EntityAdd", batch[1].ev)
	}
	if len(p.crit) != 0 {
		t.Fatalf("takeCrit did not clear the overflow: crit=%d", len(p.crit))
	}
}

// TestReliableFastPath confirms that with an empty overflow, a lifecycle frame
// rides the normal queue (staying FIFO with the metadata frames that follow a
// spawn) rather than always detouring through crit.
func TestReliableFastPath(t *testing.T) {
	p := newPlayer(2, "y", [16]byte{})
	p.trySendEv(attachproto.EntityRemove{EIDs: []int32{1}})
	if len(p.crit) != 0 {
		t.Fatalf("lifecycle frame used overflow while out had room: crit=%d", len(p.crit))
	}
	if len(p.out) != 1 {
		t.Fatalf("lifecycle frame not queued on out: out=%d", len(p.out))
	}
}

// TestChatLinesReliable guards the "backpressured player loses all chat" bug:
// a chat LINE must divert to the reliable overflow under back-pressure (a lost
// message never self-heals), while the HUD action-bar overlay may still drop.
func TestChatLinesReliable(t *testing.T) {
	p := newPlayer(3, "z", [16]byte{})
	for i := 0; i < cap(p.out); i++ { // saturate out
		p.out <- outPkt{ev: attachproto.EntityMove{}}
	}
	// The HUD action bar (ActionBar) may drop.
	p.trySendEv(attachproto.Chat{Text: "hud", ActionBar: true})
	if len(p.crit) != 0 {
		t.Fatalf("action-bar chat leaked into the reliable overflow: crit=%d", len(p.crit))
	}
	// A chat line must survive — parked in crit, never dropped.
	p.trySendEv(attachproto.Chat{Text: "<a> hello"})
	if len(p.crit) != 1 {
		t.Fatalf("chat line dropped instead of parked: crit=%d", len(p.crit))
	}
}
