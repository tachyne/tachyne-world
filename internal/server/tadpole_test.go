package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestTadpoleGrowsAndVexCharges: a tadpole becomes a frog at twenty-four
// thousand ticks, a slime ball hurries it; a vex's charging flag follows
// its target.
func TestTadpoleGrowsAndVexCharges(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	tp := h.spawnMob(players, entityTadpole, 0.5, 180, 0.5)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemSlimeball, count: 1}
	if !h.feedTadpole(players, pl, tp) || tp.tadpoleAge != 2400 || pl.inv.slots[pl.p.heldSlot()].count != 0 {
		t.Fatalf("a slime ball takes a tenth off: age %d count %d", tp.tadpoleAge, pl.inv.slots[pl.p.heldSlot()].count)
	}
	for i := 0; i < tadpoleTicksToFrog/mobMoveInterval+1 && h.mobs[tp.eid] != nil; i++ {
		h.tadpoleTick(players, tp)
	}
	frogs := 0
	for _, m := range h.mobs {
		if m.etype == entityFrog {
			frogs++
		}
	}
	if h.mobs[tp.eid] != nil || frogs != 1 {
		t.Fatalf("grown into a frog: tadpole %v frogs %d", h.mobs[tp.eid] != nil, frogs)
	}
	v := h.spawnMob(players, entityVex, 0.5, 180, 0.5)
	v.hasTarget = true
	h.vexChargeTick(players, v)
	if !v.vexCharging {
		t.Fatal("charging with a target")
	}
	v.hasTarget = false
	h.vexChargeTick(players, v)
	if v.vexCharging {
		t.Fatal("and not without")
	}
}
