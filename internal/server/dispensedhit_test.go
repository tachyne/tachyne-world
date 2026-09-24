package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// dispenseAt fires one item from a dispenser at (0,181,2) facing north
// (toward −z) and flies what it launched out.
func dispenseAt(t *testing.T, h *hub, players map[int32]*tracked, item int32) {
	t.Helper()
	info, _ := worldgen.InfoForState(dispenserMin)
	state := worldgen.SetProperty(info, dispenserMin, "facing", "north")
	pos := blockPos{0, 181, 2}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	b := &bin{slots: make([]invStack, 9)}
	b.slots[0] = invStack{item: item, count: 1}
	h.bins[simPos{blockPos: pos}] = b
	h.ejectFromBin(players, simPos{blockPos: pos}, state)
	if len(h.arrows) != 1 {
		t.Fatalf("the dispenser fired %d projectiles", len(h.arrows))
	}
	flyUntilGone(h, players, 100)
}

// A dispensed projectile has no owner, and an ownerless projectile hits
// any entity in its way (Projectile.canHitEntity): a snowball stings a
// blaze, a fire charge sets a zombie alight.
func TestDispensedProjectilesHitMobs(t *testing.T) {
	h, pl, players := killRig(t)
	pl.x, pl.z = 6, -6 // out of the line of fire
	h.arrows = map[int32]*arrowEntity{}
	blaze := h.spawnMob(players, entityBlaze, 0.5, 180.5, -1.5)
	before := blaze.health
	dispenseAt(t, h, players, itemSnowball)
	if blaze.health >= before {
		t.Errorf("a dispensed snowball passed through the blaze (health %v)", blaze.health)
	}

	h, pl, players = killRig(t)
	pl.x, pl.z = 6, -6
	h.arrows = map[int32]*arrowEntity{}
	z := h.spawnMob(players, entityZombie, 0.5, 180, -1.5)
	dispenseAt(t, h, players, itemFireCharge)
	if !z.burning && z.fireSecs == 0 {
		t.Errorf("a dispensed fire charge passed through the zombie (health %v)", z.health)
	}
}
