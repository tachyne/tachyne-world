package server

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/tachyne/tachyne-common/handover"
)

// pollUntil polls cond up to d, returning false on timeout.
func pollUntil(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

func TestPeerMeshExchange(t *testing.T) {
	lnA, _ := net.Listen("tcp", "127.0.0.1:0")
	lnB, _ := net.Listen("tcp", "127.0.0.1:0")
	defer lnA.Close()
	defer lnB.Close()
	addrOf := func(sid int32) string {
		if sid == 0 {
			return lnA.Addr().String()
		}
		return lnB.Addr().String()
	}

	got := make(chan handover.MigrateEntity, 1)
	a := newPeerMesh(0, "topo", "tok", addrOf, nil)
	b := newPeerMesh(1, "topo", "tok", addrOf, func(from int32, typ byte, payload []byte) {
		if from == 0 && typ == handover.MsgMigrate {
			var me handover.MigrateEntity
			if err := json.Unmarshal(payload, &me); err == nil {
				got <- me
			}
		}
	})
	defer a.close()
	defer b.close()
	go a.serve(lnA)
	go b.serve(lnB)
	a.dial([]int32{1}) // 0 < 1, so A dials B
	b.dial([]int32{0}) // 1 > 0, so B does NOT dial (waits for A) — must be a no-op

	if !pollUntil(2*time.Second, func() bool { return a.connected(1) && b.connected(0) }) {
		t.Fatal("peers never connected")
	}

	// A migrates a player-bearing frame to B over the warm link.
	pl := handover.PlayerState{EID: 191, Name: "wesley", Dim: 0, X: -8, Y: 71, Health: 18}
	if err := a.send(1, handover.MsgMigrate, handover.MigrateEntity{Kind: handover.KindPlayer, MigID: "x1", Player: &pl}); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case me := <-got:
		if me.MigID != "x1" || me.Player == nil || me.Player.Name != "wesley" {
			t.Fatalf("bad frame received: %+v", me)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("frame never arrived at neighbour")
	}
}

func TestPeerMeshRejectsMismatch(t *testing.T) {
	lnA, _ := net.Listen("tcp", "127.0.0.1:0")
	lnB, _ := net.Listen("tcp", "127.0.0.1:0")
	defer lnA.Close()
	defer lnB.Close()
	addrOf := func(sid int32) string {
		if sid == 0 {
			return lnA.Addr().String()
		}
		return lnB.Addr().String()
	}

	// B has a DIFFERENT topology hash — the handshake must reject the link.
	a := newPeerMesh(0, "topoA", "tok", addrOf, nil)
	b := newPeerMesh(1, "topoB", "tok", addrOf, nil)
	defer a.close()
	defer b.close()
	go a.serve(lnA)
	go b.serve(lnB)
	a.dial([]int32{1})

	// Give it time to (fail to) connect; it must never register.
	if pollUntil(500*time.Millisecond, func() bool { return a.connected(1) }) {
		t.Fatal("mismatched-topology peer must not connect")
	}

	// A wrong token is likewise rejected.
	c := newPeerMesh(0, "topoA", "wrong", addrOf, nil)
	defer c.close()
	if pollUntil(500*time.Millisecond, func() bool { return c.connected(1) }) {
		t.Fatal("wrong-token peer must not connect")
	}
}
