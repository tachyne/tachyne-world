package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// Dying records the death cell in the player's store; the respawn frame
// and the attach Welcome's remote carry it; it survives a re-record of the
// loadout; a player who never died carries none.
func TestLastDeathLocation(t *testing.T) {
	h := newHub(world.New(1))
	h.invs = newInvStore("")
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	h.playersRef = players
	if h.deathOf(pl.p.name) != nil {
		t.Fatal("a fresh player has no death location")
	}
	pl.x, pl.y, pl.z, pl.dim = 10.7, 64.2, -3.4, 1
	h.hurtBy(players, pl, 100, dtMagic, deathCause{key: causeMagic})
	if !pl.dead {
		t.Fatal("the player should be dead")
	}
	want := attachproto.DeathPos{Dim: 1, X: 10, Y: 64, Z: -4}
	if d := h.deathOf(pl.p.name); d == nil || *d != want {
		t.Fatalf("death location %+v, want %+v", d, want)
	}
	h.invs.record(pl.p.name, pl) // a loadout flush keeps it
	if d := h.deathOf(pl.p.name); d == nil || *d != want {
		t.Fatalf("death location after record %+v", d)
	}
	h.respawn(pl)
	found := false
	for len(pl.p.out) > 0 {
		pkt := <-pl.p.out
		if e, ok := pkt.ev.(attachproto.Dimension); ok {
			found = true
			if e.Death == nil || *e.Death != want {
				t.Errorf("respawn frame death %+v", e.Death)
			}
		}
	}
	if !found {
		t.Error("no Dimension frame on respawn")
	}
	s := &Server{hub: h}
	r := &remotePlayer{s: s, p: pl.p, gm: -1}
	if d := r.Death(); d == nil || *d != want {
		t.Errorf("remote death %+v", d)
	}
}
