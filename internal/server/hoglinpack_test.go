package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestHoglinPackAndCamelLeash: a hoglin's bite brings the adult hoglins
// within sixteen onto the same player; a sat camel led six blocks off
// stands up.
func TestHoglinPackAndCamelLeash(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 2.5, 180, 0.5
	a := h.spawnMob(players, entityHoglin, 0.5, 180, 0.5)
	b := h.spawnMob(players, entityHoglin, 10.5, 180, 0.5)
	far := h.spawnMob(players, entityHoglin, 30.5, 180, 0.5)
	a.baby, b.baby, far.baby = false, false, false
	b.hasTarget, far.hasTarget = false, false
	h.gridDirty()
	h.hoglinBroadcastTarget(players, a, pl)
	if !b.hasTarget || far.hasTarget {
		t.Fatalf("the pack within sixteen joins in, the far one not: near %v far %v", b.hasTarget, far.hasTarget)
	}
	h.tick.Store(1000)
	c := h.spawnMob(players, entityCamel, 0.5, 180, 20.5)
	h.camelSitDown(players, c)
	h.tick.Store(1100)
	c.leash = pl.p.eid
	pl.x, pl.z = 0.5, 28.5 // eight blocks off along the lead
	h.updateLeashes(players)
	if c.camelSitting() {
		t.Fatal("led past six blocks, a sat camel stands")
	}
}
