package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// The special minecarts, on top of the cart physics in minecart.go: the
// chest cart (a 27-slot container), the hopper cart (five slots that suck
// items in), the TNT cart (a fuse, a blast that grows with speed) and the
// furnace cart (a coal-fed push). Vanilla MinecartChest, MinecartHopper,
// MinecartTNT, MinecartFurnace and AbstractMinecartContainer.

const (
	cartFuelPerItem = 3600  // ticks of push per coal
	cartFuelMax     = 32000 // a furnace cart refuses fuel past this
	cartFuse        = 80
	cartMetaFuel    = 13 // MinecartFurnace's lit flag: Entity 8 + VehicleEntity 3 + AbstractMinecart 2
	cartPrimeEvent  = 70 // entity event: the TNT cart starts its fuse animation

	particleSmoke      = 59 // canonical 770 ids (remapped per client)
	particleLargeSmoke = 52
)

var (
	entityChestMinecart   = entityID("chest_minecart")
	entityHopperMinecart  = entityID("hopper_minecart")
	entityTntMinecart     = entityID("tnt_minecart")
	entityFurnaceMinecart = entityID("furnace_minecart")

	// cartTypes: every entity the cart physics rolls.
	cartTypes = map[int]bool{entityMinecart: true, entityChestMinecart: true,
		entityHopperMinecart: true, entityTntMinecart: true, entityFurnaceMinecart: true}

	// cartFuelItems is the #furnace_minecart_fuel tag.
	cartFuelItems = func() map[int32]bool {
		m := map[int32]bool{}
		for _, n := range []string{"coal", "charcoal"} {
			if id, ok := itemByName[n]; ok {
				m[id] = true
			}
		}
		return m
	}()
)

// initCartKind gives a freshly placed or restored cart its kind's state.
func initCartKind(v *vehicle) {
	switch v.etype {
	case entityChestMinecart:
		v.chest = &chest{}
	case entityHopperMinecart:
		v.bin = &bin{slots: make([]invStack, 5)}
	case entityTntMinecart:
		v.fuse = -1
	}
}

// cartSlots exposes a container cart's slots (nil for the rest).
func (v *vehicle) cartSlots() []invStack {
	switch {
	case v.chest != nil:
		return v.chest.slots[:]
	case v.bin != nil:
		return v.bin.slots
	}
	return nil
}

// vehicleContainerAt finds a container cart sitting in a cell: a hopper
// block above feeds it, a hopper block below draws from it, a comparator on
// a detector rail reads it.
func (h *hub) vehicleContainerAt(pos simPos) []invStack {
	for _, v := range h.vehicles {
		if v.dim == pos.dim && floorInt(v.x) == pos.x && floorInt(v.y) == pos.y && floorInt(v.z) == pos.z {
			if s := v.cartSlots(); s != nil {
				return s
			}
		}
	}
	return nil
}

// slotsSignal is the comparator's read of a slot list (the container cart's
// own fullness, which also sets how freely it rolls).
func slotsSignal(slots []invStack) int {
	full, any := 0.0, false
	for _, s := range slots {
		if s.item != 0 && s.count > 0 {
			any = true
			full += float64(s.count) / float64(stackMax)
		}
	}
	if !any {
		return 0
	}
	return 1 + int(math.Floor(14*full/float64(len(slots))))
}

// cartMaxSpeed is getMaxSpeed per kind: a furnace cart is a slow hauler.
func cartMaxSpeed(v *vehicle, inWater bool) float64 {
	s := cartTopSpeed
	if inWater {
		s = cartTopSpeedWater
	}
	if v.etype == entityFurnaceMinecart {
		if inWater {
			return s * 0.75
		}
		return s * 0.5
	}
	return s
}

// cartNaturalSlowdown is applyNaturalSlowdown per kind, applied to the
// horizontal motion (the vertical is always dropped on the rails).
func cartNaturalSlowdown(v *vehicle, inWater bool) {
	switch v.etype {
	case entityChestMinecart, entityHopperMinecart:
		// A container cart rolls more freely the emptier it is.
		keep := 0.98 + float64(15-slotsSignal(v.cartSlots()))*0.001
		if inWater {
			keep *= 0.95
		}
		v.vx, v.vy, v.vz = v.vx*keep, 0, v.vz*keep
		return
	case entityFurnaceMinecart:
		if v.pushX*v.pushX+v.pushZ*v.pushZ > 1e-7 {
			// The push follows the track: re-aim it along the motion, its
			// strength kept (calculateNewPushAlong).
			if v.pushX*v.pushX+v.pushZ*v.pushZ > 1e-4 && v.vx*v.vx+v.vz*v.vz > 0.001 {
				dot := v.pushX*v.vx + v.pushZ*v.vz
				m2 := v.vx*v.vx + v.vz*v.vz
				px, pz := v.vx*dot/m2, v.vz*dot/m2
				if l := math.Hypot(px, pz); l > 0 {
					k := math.Hypot(v.pushX, v.pushZ) / l
					v.pushX, v.pushZ = px*k, pz*k
				}
			}
			v.vx, v.vz = v.vx*0.8+v.pushX, v.vz*0.8+v.pushZ
			if inWater {
				v.vx, v.vz = v.vx*0.1, v.vz*0.1
			}
		} else {
			v.vx, v.vz = v.vx*0.98, v.vz*0.98
		}
	}
	// The base slowdown: an empty cart bleeds 4% a tick, a ridden one 0.3%.
	f := 0.96
	if v.rider != 0 {
		f = 0.997
	}
	v.vx, v.vy, v.vz = v.vx*f, 0, v.vz*f
	if inWater {
		v.vx, v.vz = v.vx*0.95, v.vz*0.95
	}
}

// cartActivate is activateMinecart per kind, for a cart on an activator rail.
func (h *hub) cartActivate(players map[int32]*tracked, v *vehicle, powered bool) {
	switch v.etype {
	case entityMinecart:
		if powered && v.rider != 0 { // a live activator rail throws the rider off
			if t := players[v.rider]; t != nil {
				h.dismount(players, t)
			}
		}
	case entityHopperMinecart:
		v.disabled = powered
	case entityTntMinecart:
		if powered && v.fuse < 0 {
			h.primeCart(players, v, cartFuse)
		}
	}
}

// primeCart lights a TNT cart's fuse (primeFuse): the client plays the
// flashing from the entity event.
func (h *hub) primeCart(players map[int32]*tracked, v *vehicle, fuse int) {
	if v.etype != entityTntMinecart || v.fuse >= 0 {
		return
	}
	v.fuse = fuse
	h.toNearbyEv(players, v.dim, v.x, v.z, attachproto.EntityStatus{EID: v.eid, Status: cartPrimeEvent})
	h.playSoundDim(players, v.dim, "minecraft:entity.tnt.primed", sndBlock, v.x, v.y, v.z, 1, 1)
}

// explodeCart is MinecartTNT.explode: power 4 plus up to 1.5 per block/tick
// of speed (capped at five), and the blast leaves the rails alone.
func (h *hub) explodeCart(players map[int32]*tracked, v *vehicle, speedSqr float64) {
	if v.rider != 0 {
		if t := players[v.rider]; t != nil {
			h.dismount(players, t)
		}
	}
	h.releaseCartMob(players, v)
	delete(h.vehicles, v.eid)
	h.toNearbyEv(players, v.dim, v.x, v.z, entGone(v.eid))
	speed := math.Min(math.Sqrt(speedSqr), 5)
	power := 4 + h.rng.Float64()*1.5*speed
	h.blastSpareRails = true
	h.explodeIn(players, v.dim, v.x, v.y, v.z, int(math.Round(power)), int(power*10))
	h.blastSpareRails = false
}

// tickSpecialCart is the per-kind part of the tick, after the cart moved.
// Returns false when the cart is gone.
func (h *hub) tickSpecialCart(players map[int32]*tracked, v *vehicle) bool {
	switch v.etype {
	case entityHopperMinecart:
		if !v.disabled {
			h.cartSuckItems(players, v)
		}
	case entityTntMinecart:
		if v.fuse > 0 {
			v.fuse--
			h.spawnParticles(players, particleSmoke, v.x, v.y+0.5, v.z, 0, 0, 1)
		} else if v.fuse == 0 {
			h.explodeCart(players, v, v.vx*v.vx+v.vz*v.vz)
			return false
		}
		if v.hitWall && v.hitSpeedSqr >= 0.01 {
			h.explodeCart(players, v, v.hitSpeedSqr)
			return false
		}
		if v.landedFall >= 3 {
			p := v.landedFall / 10
			h.explodeCart(players, v, p*p)
			return false
		}
	case entityFurnaceMinecart:
		if v.fuel > 0 {
			v.fuel--
		}
		if v.fuel <= 0 {
			v.pushX, v.pushZ = 0, 0
		}
		if lit := v.fuel > 0; lit != v.lit {
			v.lit = lit
			h.toNearbyEv(players, v.dim, v.x, v.z, metaEv(cartFuelMeta(v.eid, lit)))
		}
		if v.lit && h.rng.Intn(4) == 0 {
			h.spawnParticles(players, particleLargeSmoke, v.x, v.y+0.8, v.z, 0, 0, 1)
		}
	}
	return true
}

// cartFuelMeta is the furnace cart's synced lit flag.
func cartFuelMeta(eid int32, lit bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, cartMetaFuel)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, lit)
	return protocol.AppendU8(b, itemMetaEnd)
}

// cartSuckItems is MinecartHopper.suckInItems: one item a tick from the
// container in the cell above, else an item entity overlapping the cart.
func (h *hub) cartSuckItems(players map[int32]*tracked, v *vehicle) bool {
	above := simPos{dim: v.dim, blockPos: blockPos{floorInt(v.x), floorInt(v.y) + 1, floorInt(v.z)}}
	src := h.containerSlots(above)
	if f := h.furnaces[above]; f != nil {
		src = f.slots[2:3] // only a furnace's output
	}
	if src != nil {
		for i := range src {
			s := &src[i]
			if s.item == 0 || s.count == 0 {
				continue
			}
			one := *s
			one.count = 1
			if binInsert(v.bin.slots, one) == 0 {
				s.count--
				if s.count <= 0 {
					*s = invStack{}
				}
				h.refreshBinViewers(players, above)
				h.refreshCartViewers(players, v)
				return true
			}
		}
		return false
	}
	for eid, it := range h.items {
		if it.dim != v.dim || math.Abs(it.x-v.x) > 0.74 || math.Abs(it.z-v.z) > 0.74 ||
			it.y < v.y-0.25 || it.y > v.y+0.95 {
			continue // the cart's box, widened by a quarter block
		}
		st := it.stack()
		if left := binInsert(v.bin.slots, st); left < st.count {
			if left == 0 {
				delete(h.items, eid)
				h.toNearbyEv(players, it.dim, it.x, it.z, entGone(eid))
			} else {
				it.count = left
			}
			h.playSoundDim(players, v.dim, "minecraft:entity.item.pickup", sndBlock, it.x, it.y, it.z, 0.2, 1.4)
			h.refreshCartViewers(players, v)
			return true
		}
	}
	return false
}

// refreshCartViewers re-sends a container cart's window to whoever has it open.
func (h *hub) refreshCartViewers(players map[int32]*tracked, v *vehicle) {
	for _, t := range players {
		if t.winID == 0 {
			continue
		}
		switch {
		case v.chest != nil && t.viewChest == v.chest:
			h.sendChestWindow(t, v.chest)
		case v.bin != nil && t.viewBin == v.bin:
			h.sendBinWindow(t, v.bin)
		}
	}
}

// openVehicleBin opens a hopper cart's five slots in the hopper menu.
func (h *hub) openVehicleBin(players map[int32]*tracked, t *tracked, v *vehicle) {
	if t.inv == nil || v.bin == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind, t.viewBin = h.nextWin, simPos{}, winBin, v.bin
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuHopper), Title: "Minecart with Hopper"})
	h.sendBinWindow(t, v.bin)
}

// fuelCart is MinecartFurnace.interact: a coal in hand feeds the furnace
// and sets the push away from the player.
func (h *hub) fuelCart(players map[int32]*tracked, t *tracked, v *vehicle) {
	if t.inv == nil {
		return
	}
	slot := t.p.heldSlot()
	st := &t.inv.slots[slot]
	if st.count == 0 || !cartFuelItems[st.item] || v.fuel+cartFuelPerItem > cartFuelMax {
		return
	}
	v.fuel += cartFuelPerItem
	v.pushX, v.pushZ = v.x-t.x, v.z-t.z
	if t.gamemode == gmSurvival {
		st.count--
		if st.count == 0 {
			*st = invStack{}
		}
		h.sendSlot(t, slot)
	}
}

// interactCart is the click on a cart: containers open, the furnace takes
// fuel, the plain cart is boarded, the TNT cart ignores you.
func (h *hub) interactCart(players map[int32]*tracked, t *tracked, v *vehicle) {
	switch v.etype {
	case entityChestMinecart:
		h.openVehicleChest(players, t, v)
	case entityHopperMinecart:
		h.openVehicleBin(players, t, v)
	case entityFurnaceMinecart:
		h.fuelCart(players, t, v)
	case entityMinecart:
		h.mountVehicle(players, t, v)
	}
}
