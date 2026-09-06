package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func specialCart(t *testing.T, h *hub, players map[int32]*tracked, etype, x int) *vehicle {
	t.Helper()
	if !h.spawnVehicleAt(players, 0, etype, x, cartY, 10) {
		t.Fatal("the cart should place on the rail")
	}
	for _, v := range h.vehicles {
		if v.etype == etype {
			return v
		}
	}
	return nil
}

// A hopper cart sucks up an item lying on its rail and hands it over to a
// hopper block below; a hopper block above a chest cart fills the cart.
func TestHopperCartSucksItems(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, railMin, false)
	v := specialCart(t, h, players, entityHopperMinecart, 5)
	if v.bin == nil || len(v.bin.slots) != 5 {
		t.Fatal("a hopper cart carries five slots")
	}
	stone := itemByName["cobblestone"]
	h.spawnItemIn(players, 0, stone, 3, v.x+0.3, v.y, v.z)
	h.tickMinecart(players, v)
	if v.bin.slots[0].item != stone || v.bin.slots[0].count != 3 || len(h.items) != 0 {
		t.Fatalf("the cart should have sucked the stack up: %+v items=%d", v.bin.slots[0], len(h.items))
	}
	// A live activator rail switches it off.
	h.world.SetBlock(5, cartY, 10, railWith(activatorMin, shapeEW, true))
	h.spawnItemIn(players, 0, stone, 1, v.x, v.y, v.z)
	h.tickMinecart(players, v)
	if !v.disabled || len(h.items) != 1 {
		t.Errorf("a powered activator rail disables the hopper cart: disabled=%v items=%d", v.disabled, len(h.items))
	}
	// A hopper block sees the cart parked in the cell above it.
	below := simPos{dim: 0, blockPos: blockPos{floorInt(v.x), floorInt(v.y) - 1, floorInt(v.z)}}
	if got := h.containerSlots(below.at(blockPos{below.x, below.y + 1, below.z})); len(got) != 5 {
		t.Errorf("containerSlots at the cart's cell should expose its slots, got %d", len(got))
	}
}

// The special carts cannot be ridden; a chest cart opens instead and spills
// its cargo when broken.
func TestChestCartOpensAndSpills(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, railMin, false)
	v := specialCart(t, h, players, entityChestMinecart, 5)
	r := testTracked()
	players[r.p.eid] = r
	r.x, r.y, r.z = v.x, v.y, v.z
	h.mountVehicle(players, r, v)
	if v.rider != 0 {
		t.Fatal("a chest cart is not rideable")
	}
	v.chest.slots[3] = invStack{item: itemByName["diamond"], count: 4}
	if slotsSignal(v.cartSlots()) == 0 {
		t.Error("a cart with cargo reads on a comparator")
	}
	h.breakVehicle(players, v)
	found := false
	for _, it := range h.items {
		if it.item == itemByName["diamond"] && it.count == 4 {
			found = true
		}
	}
	if !found || len(h.vehicles) != 0 {
		t.Errorf("breaking the chest cart should spill the diamonds: items=%d vehicles=%d", len(h.items), len(h.vehicles))
	}
}

// A live activator rail lights a TNT cart; eighty ticks later it blows up
// and the rails survive the blast.
func TestTNTCartFuseAndBlast(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, activatorMin, true)
	v := specialCart(t, h, players, entityTntMinecart, 5)
	if v.fuse != -1 {
		t.Fatal("a fresh TNT cart is not primed")
	}
	h.tickMinecart(players, v)
	if v.fuse != cartFuse-1 { // primed on the rail, then this tick's own count-down
		t.Fatalf("the activator rail should prime it: fuse=%d", v.fuse)
	}
	for i := 0; i < cartFuse+1 && len(h.vehicles) > 0; i++ {
		h.tickMinecart(players, v)
	}
	if len(h.vehicles) != 0 {
		t.Fatal("the cart should have exploded")
	}
	for x := 3; x <= 7; x++ {
		if !isAnyRail(h.world.At(x, cartY, 10)) || h.world.At(x, cartY-1, 10) != worldgen.BlockBase("stone") {
			t.Errorf("the blast must spare the rails and their bed at x=%d", x)
		}
	}
	if h.blastSpareRails {
		t.Error("the rail-sparing flag must not leak past the blast")
	}
}

// A moving TNT cart that runs into a wall explodes; a broken moving one
// lights rather than drops.
func TestTNTCartCollisionAndBreak(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 10, railMin, false)
	h.world.SetBlock(11, cartY, 10, worldgen.BlockBase("stone"))
	h.world.SetBlock(11, cartY+1, 10, worldgen.BlockBase("stone"))
	v := specialCart(t, h, players, entityTntMinecart, 5)
	v.vx = 0.4
	for i := 0; i < 40 && len(h.vehicles) > 0; i++ {
		h.tickMinecart(players, v)
	}
	if len(h.vehicles) != 0 {
		t.Errorf("a fast TNT cart hitting a wall explodes, still at x=%.2f vx=%.3f", v.x, v.vx)
	}
	v2 := specialCart(t, h, players, entityTntMinecart, 5)
	v2.vx = 0.3
	h.breakVehicle(players, v2)
	if v2.fuse < 0 || len(h.vehicles) != 1 {
		t.Errorf("breaking a moving TNT cart primes it: fuse=%d vehicles=%d", v2.fuse, len(h.vehicles))
	}
}

// Coal in hand pushes a furnace cart away from the player at half the
// ordinary cap, lights it, and it coasts out when the fuel is spent.
func TestFurnaceCartPush(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 120, railMin, false)
	v := specialCart(t, h, players, entityFurnaceMinecart, 5)
	r := testTracked()
	players[r.p.eid] = r
	r.x, r.y, r.z = v.x-1, v.y, v.z // standing west of it
	r.inv.slots[r.p.heldSlot()] = invStack{item: itemByName["coal"], count: 2}
	h.interactCart(players, r, v)
	if v.fuel != cartFuelPerItem || r.inv.slots[r.p.heldSlot()].count != 1 {
		t.Fatalf("one coal should be taken for %d ticks of push: fuel=%d left=%d", cartFuelPerItem, v.fuel, r.inv.slots[r.p.heldSlot()].count)
	}
	if v.pushX <= 0 {
		t.Fatalf("the push points away from the player (east): pushX=%.2f", v.pushX)
	}
	for i := 0; i < 60; i++ {
		h.tickMinecart(players, v)
	}
	if !v.lit || v.x < 8 {
		t.Errorf("a fuelled cart rolls east and burns: lit=%v x=%.2f vx=%.3f", v.lit, v.x, v.vx)
	}
	before := v.x
	h.tickMinecart(players, v)
	if step := v.x - before; step > cartTopSpeed*0.5+1e-9 {
		t.Errorf("a furnace cart is capped at half speed: step %.4f", step)
	}
	v.fuel = 1
	for i := 0; i < 200; i++ {
		h.tickMinecart(players, v)
	}
	if v.lit || v.pushX != 0 || math.Hypot(v.vx, v.vz) > 1e-3 {
		t.Errorf("out of fuel the cart goes dark and stops: lit=%v push=%.2f speed=%.5f", v.lit, v.pushX, math.Hypot(v.vx, v.vz))
	}
}

// The special carts' state survives a save/restore.
func TestSpecialCartsPersist(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, railMin, false)
	f := specialCart(t, h, players, entityFurnaceMinecart, 5)
	f.fuel, f.pushX, f.lit = 500, 0.5, true
	tnt := specialCart(t, h, players, entityTntMinecart, 8)
	tnt.fuse = 12
	hop := specialCart(t, h, players, entityHopperMinecart, 11)
	hop.bin.slots[2] = invStack{item: itemByName["coal"], count: 7}
	hop.disabled = true
	saved := h.snapshotVehicles()
	h2, _ := cartHub()
	h2.restoreVehicles(saved)
	if len(h2.vehicles) != 3 {
		t.Fatalf("restored %d vehicles, want 3", len(h2.vehicles))
	}
	for _, v := range h2.vehicles {
		switch v.etype {
		case entityFurnaceMinecart:
			if v.fuel != 500 || v.pushX != 0.5 {
				t.Errorf("furnace cart lost its fuel/push: %+v", v)
			}
		case entityTntMinecart:
			if v.fuse != 12 {
				t.Errorf("TNT cart fuse: got %d want 12", v.fuse)
			}
		case entityHopperMinecart:
			if v.bin == nil || v.bin.slots[2].count != 7 || !v.disabled {
				t.Errorf("hopper cart lost its slots/disabled flag: %+v", v)
			}
		}
	}
	// An unprimed TNT cart restores unprimed.
	h3, p3 := cartHub()
	cartTrack(h3, 0, 20, railMin, false)
	specialCart(t, h3, p3, entityTntMinecart, 5)
	h4, _ := cartHub()
	h4.restoreVehicles(h3.snapshotVehicles())
	for _, v := range h4.vehicles {
		if v.fuse != -1 {
			t.Errorf("an unprimed TNT cart must restore unprimed, fuse=%d", v.fuse)
		}
	}
}
