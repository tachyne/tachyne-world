package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestRavagerStunAndRoar: a stunned ravager stands still for forty ticks,
// then roars, hurting a player within four blocks; a bite pauses it ten.
func TestRavagerStunAndRoar(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	r := h.spawnMob(players, entityRavager, 0.5, 180, 0.5)
	pl.x, pl.y, pl.z = 3.5, 180, 0.5
	r.ravStunTick = ravagerStunTicks
	hp := pl.health
	held := 0
	for i := 0; i < 40; i++ { // 80 ticks: the stun, then the roar
		if h.ravagerStep(players, r) {
			held++
		}
	}
	if held < 25 {
		t.Fatalf("stunned and roaring, the ravager is held for sixty ticks: %d updates", held)
	}
	if pl.health >= hp {
		t.Fatalf("the roar should hurt the player within four: %.1f vs %.1f", pl.health, hp)
	}
	if r.ravagerImmobile() {
		t.Fatal("free again after the roar")
	}
	h.ravagerBite(players, r)
	if !h.ravagerStep(players, r) || r.ravAttackTick != ravagerAttackTicks-2 {
		t.Fatalf("a bite pauses it: tick %d", r.ravAttackTick)
	}
}
