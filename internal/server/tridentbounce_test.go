package server

import (
	"testing"
)

// throwTridentAt has the rig's player (at the origin, facing +z) throw a
// plain trident and flies it until it lies stuck in the ground.
func throwTridentAt(t *testing.T, h *hub, players map[int32]*tracked, pl *tracked) *arrowEntity {
	t.Helper()
	pl.p.setHotbarSlot(0, itemTrident)
	pl.inv.slots[0] = invStack{item: itemTrident, count: 1}
	h.arrows = map[int32]*arrowEntity{}
	h.throwTrident(players, pl, pl.inv.slots[0])
	a := onlyProjectile(t, h)
	for i := 0; i < 80 && h.arrows[a.eid] != nil && !a.stuck; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if h.arrows[a.eid] == nil {
		t.Fatal("the trident was deleted by what it struck; vanilla's falls and stays")
	}
	if !a.stuck {
		t.Fatalf("the trident never came to rest (at %.1f %.1f %.1f)", a.x, a.y, a.z)
	}
	return a
}

// ThrownTrident.onHitEntity: a trident without Loyalty that strikes a mob
// bounces off and drops, and its thrower can pick it up again.
func TestTridentDropsAfterStrikingAMob(t *testing.T) {
	h, players, pl := breezeRig(t)
	z := h.spawnMob(players, entityZombie, 0.5, 180, 4.5)
	a := throwTridentAt(t, h, players, pl)
	if z.health >= z.maxHP() {
		t.Fatal("the trident never struck the zombie")
	}
	if a.z > z.z {
		t.Fatalf("the trident flew on through the zombie to z=%.1f", a.z)
	}
	pl.inv.slots[0] = invStack{}
	pl.x, pl.y, pl.z = a.x, a.y, a.z
	h.tick.Add(1)
	h.updateArrows(players)
	if h.arrows[a.eid] != nil {
		t.Error("the dropped trident could not be picked up")
	}
	got := false
	for _, s := range pl.inv.slots {
		got = got || s.item == itemTrident
	}
	if !got {
		t.Error("picking the trident up did not return it to the inventory")
	}
}

// The same off a vehicle: a minecart does not swallow the trident.
func TestTridentDropsAfterStrikingAVehicle(t *testing.T) {
	h, pl, players := vehRig(t)
	pl.yaw, pl.pitch = 0, 10
	rigVehicle(t, h, players, "minecart")
	throwTridentAt(t, h, players, pl)
}

// A trident its thrower can pick up never despawns (ThrownTrident.tickDespawn).
func TestStuckTridentDoesNotDespawn(t *testing.T) {
	h, players, pl := breezeRig(t)
	pl.pitch = 40
	a := throwTridentAt(t, h, players, pl)
	pl.x, pl.z = 30, 30 // walk away from it
	for i := 0; i < arrowLifeTicks+20; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if h.arrows[a.eid] == nil {
		t.Fatal("a thrown trident lying in the ground despawned")
	}
}
