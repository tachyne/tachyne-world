package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bins: the shared container behind dispensers, droppers (9 slots,
// generic_3x3) and hoppers (5 slots). Dispensers/droppers eject on a rising
// redstone edge (the `triggered` property tracks the edge); hoppers move one
// item every 8 ticks — pull from the container above (or suck item entities
// above/inside), push into the container they face — and pause while powered.

const (
	menuGeneric3x3 = 6 // static menu registry, vanilla order
	menuHopper     = 16

	hopperCadence = 8 // game ticks per moved item (vanilla)
)

var (
	hopperMin = worldgen.BlockBase("hopper") // enabled(2) × facing(down,north,south,west,east)
	hopperMax = worldgen.BlockBase("hopper") + 9
)

var (
	itemBucket     = itemByName["bucket"]
	itemBucketH2O  = itemByName["water_bucket"]
	itemBucketLav  = itemByName["lava_bucket"]
	itemFireCharge = int32(itemByName["fire_charge"])
)

func isHopper(s uint32) bool { return s >= hopperMin && s <= hopperMax }

func hopperEnabled(s uint32) bool { return (s-hopperMin)/5 == 0 } // enabled=true first
func hopperWith(s uint32, enabled bool) uint32 {
	f := (s - hopperMin) % 5
	if enabled {
		return hopperMin + f
	}
	return hopperMin + 5 + f
}

// hopperDelta is the push direction (down or a cardinal).
func hopperDelta(s uint32) (int, int, int) {
	switch (s - hopperMin) % 5 {
	case 0:
		return 0, -1, 0
	case 1:
		return 0, 0, -1 // north
	case 2:
		return 0, 0, 1 // south
	case 3:
		return -1, 0, 0 // west
	}
	return 1, 0, 0 // east
}

// bin is the storage for one dispenser/dropper/hopper block.
type bin struct {
	slots []invStack
}

func binSizeFor(state uint32) int {
	if isHopper(state) || isBrewStand(state) {
		return 5
	}
	return 9
}

type evOpenBin struct {
	eid     int32
	x, y, z int
}

func (evOpenBin) isHubEvent() {}

// openBin opens the dispenser/dropper/hopper window at a block position.
func (h *hub) openBin(t *tracked, x, y, z int) {
	if t.inv == nil {
		return
	}
	state := h.world.At(x, y, z)
	menu, title := int32(menuGeneric3x3), "Dispenser"
	switch {
	case isDropper(state):
		title = "Dropper"
	case isHopper(state):
		menu, title = menuHopper, "Item Hopper"
	case isBrewStand(state):
		menu, title = menuBrewing, "Brewing Stand"
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	pos := blockPos{x, y, z}
	c := h.bins[pos]
	if c == nil {
		c = &bin{slots: make([]invStack, binSizeFor(state))}
		h.bins[pos] = c
	}
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind = h.nextWin, pos, winBin

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menu), Title: title})
	h.sendBinWindow(t, c)
}

// sendBinWindow refreshes the whole bin window: container + main + hotbar.
func (h *hub) sendBinWindow(t *tracked, c *bin) {
	t.inv.stateId++
	n := len(c.slots)
	slots := make([]attachproto.ItemStack, 0, n+36)
	for i := 0; i < n; i++ {
		slots = append(slots, stackEv(c.slots[i]))
	}
	for i := 9; i <= 35; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// binInsert moves count items of a stack into a slot list (merge, then empty
// slots). Returns how many did NOT fit.
func binInsert(slots []invStack, st invStack) int {
	left := st.count
	for i := range slots {
		s := &slots[i]
		if left == 0 {
			break
		}
		if s.item == st.item && s.count > 0 && s.dmg == st.dmg && s.ench == st.ench && s.name == st.name {
			room := stackMax - s.count
			if room <= 0 {
				continue
			}
			take := left
			if take > room {
				take = room
			}
			s.count += take
			left -= take
		}
	}
	for i := range slots {
		if left == 0 {
			break
		}
		if slots[i].item == 0 || slots[i].count == 0 {
			slots[i] = st
			slots[i].count = left
			left = 0
		}
	}
	return left
}

// updateBinTrigger is the dispenser/dropper redstone step: eject one item on
// the rising edge, tracked by the `triggered` block property.
func (h *hub) updateBinTrigger(players map[int32]*tracked, pos blockPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	triggered := boolProp(state, "triggered")
	if powered == triggered {
		return
	}
	h.setBlock(players, pos, setBoolProp(state, "triggered", powered))
	if powered {
		h.ejectFromBin(players, pos, state)
	}
}

// ejectFromBin fires/drops the first non-empty slot's item out of the face.
func (h *hub) ejectFromBin(players map[int32]*tracked, pos blockPos, state uint32) {
	c := h.bins[pos]
	if c == nil {
		return
	}
	var st *invStack
	for i := range c.slots {
		if c.slots[i].item != 0 && c.slots[i].count > 0 {
			st = &c.slots[i]
			break
		}
	}
	if st == nil {
		h.playSound(players, "minecraft:block.dispenser.fail", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 1.2)
		return
	}
	dx, dy, dz := pistonDelta(state) // same 6-way facing math
	fx, fy, fz := float64(pos.x)+0.5+float64(dx)*0.7, float64(pos.y)+0.5+float64(dy)*0.7, float64(pos.z)+0.5+float64(dz)*0.7
	vx, vy, vz := float64(dx)*1.1, float64(dy)*1.1+0.05, float64(dz)*1.1 // dispenser projectile power 1.1
	front := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	item := st.item
	dispense := isDispenser(state)
	took := true
	switch {
	case dispense && item == itemArrowAmmo:
		a := h.launchArrow(players, fx, fy, fz, vx, vy, vz)
		a.dmg, a.playerShot = arrowDamage, true // hits mobs; retrievable when stuck
	case dispense && item == itemSnowball:
		h.launchProjectileIn(players, entitySnowball, 0, fx, fy, fz, vx, vy, vz).breaks = true
	case dispense && item == itemEgg:
		h.launchProjectileIn(players, entityEggProj, 0, fx, fy, fz, vx, vy, vz).breaks = true
	case dispense && item == itemFireCharge:
		h.launchProjectileIn(players, entitySmallFireball, 0, fx, fy, fz, vx, vy, vz)
	case dispense && item == itemTNTBlock:
		h.primeTNT(players, front.x, front.y, front.z, tntFuseTicks)
	case dispense && item == itemFlintSteel:
		if h.world.At(front.x, front.y, front.z) == worldgen.Air {
			h.igniteFire(players, front, 0) // light a fire in the cell ahead
		}
		took = false // vanilla damages the tool instead of consuming it
		st.dmg++
		if max := itemMaxDurability[item]; max > 0 && st.dmg >= max {
			*st = invStack{} // worn out
		}
	case dispense && item == itemBoneMeal:
		if !h.applyBoneMeal(players, 0, front.x, front.y, front.z, h.world.At(front.x, front.y, front.z)) {
			// nothing growable ahead → fall back to tossing the meal out
			if it := h.spawnItem(players, item, 1, fx, fy, fz); it != nil {
				it.dmg, it.ench = st.dmg, st.ench
			}
		}
	case dispense && (item == itemBucketH2O || item == itemBucketLav):
		bs, bok := protocol.BlockForItem(item)
		if ts := h.world.At(front.x, front.y, front.z); bok && (ts == worldgen.Air || worldgen.IsReplaceable(ts)) {
			h.setBlock(players, front, bs)
			st.item = itemBucket // the bucket empties in place
			st.count, took = 1, false
		} else {
			took = false
		}
	default: // dropper (or a dispenser with a plain item): toss it out
		if it := h.spawnItem(players, item, 1, fx, fy, fz); it != nil {
			it.dmg, it.ench = st.dmg, st.ench
		}
	}
	if took {
		st.count--
		if st.count <= 0 {
			*st = invStack{}
		}
	}
	h.playSound(players, "minecraft:block.dispenser.dispense", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.5, 1)
	h.refreshBinViewers(players, pos)
}

// updateHopper: sync enabled with (inverse) power; move one item on the
// 8-tick cadence; self-reschedule while the hopper exists.
func (h *hub) updateHopper(players map[int32]*tracked, pos blockPos, state uint32) {
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if hopperEnabled(state) == powered { // enabled must be the inverse of powered
		state = hopperWith(state, !powered)
		h.setBlock(players, pos, state)
	}
	if hopperEnabled(state) {
		c := h.bins[pos]
		if c == nil {
			c = &bin{slots: make([]invStack, 5)}
			h.bins[pos] = c
		}
		moved := h.hopperPull(players, pos, c)
		if h.hopperPush(players, pos, state, c) {
			moved = true
		}
		if moved {
			h.refreshBinViewers(players, pos)
		}
	}
	h.schedule(pos, hopperCadence)
}

// hopperPull takes one item from the container above, or sucks up item
// entities sitting above or inside the hopper cell.
func (h *hub) hopperPull(players map[int32]*tracked, pos blockPos, c *bin) bool {
	above := blockPos{pos.x, pos.y + 1, pos.z}
	if src := h.containerSlots(above); src != nil {
		for i := range src {
			s := &src[i]
			if s.item == 0 || s.count == 0 {
				continue
			}
			one := *s
			one.count = 1
			if binInsert(c.slots, one) == 0 {
				s.count--
				if s.count <= 0 {
					*s = invStack{}
				}
				h.refreshBinViewers(players, above)
				return true
			}
		}
		return false
	}
	// No container above: vacuum item entities in this cell and the one above.
	for eid, it := range h.items {
		ix, iy, iz := floorInt(it.x), floorInt(it.y), floorInt(it.z)
		if ix != pos.x || iz != pos.z || (iy != pos.y && iy != pos.y+1) {
			continue
		}
		st := invStack{item: it.item, count: it.count, dmg: it.dmg, ench: it.ench,
			mapID: it.mapID, pats: it.pats, trimMat: it.trimMat, trimPat: it.trimPat, bookID: it.bookID}
		if left := binInsert(c.slots, st); left < st.count {
			if left == 0 {
				delete(h.items, eid)
				h.toNearbyEv(players, it.dim, it.x, it.z, entGone(eid))
			} else {
				it.count = left
			}
			h.playSound(players, "minecraft:entity.item.pickup", sndBlock,
				it.x, it.y, it.z, 0.2, 1.4)
			return true
		}
	}
	return false
}

// hopperPush moves one item into the container the hopper faces. Furnaces
// take smelt input from above and fuel from the side (vanilla).
func (h *hub) hopperPush(players map[int32]*tracked, pos blockPos, state uint32, c *bin) bool {
	dx, dy, dz := hopperDelta(state)
	target := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	dst := h.containerSlots(target)
	if dst == nil {
		return false
	}
	if f := h.furnaces[target]; f != nil {
		if dy < 0 {
			dst = f.slots[0:1] // top: smelt input
		} else {
			dst = f.slots[1:2] // side: fuel
		}
	}
	for i := range c.slots {
		s := &c.slots[i]
		if s.item == 0 || s.count == 0 {
			continue
		}
		one := *s
		one.count = 1
		if binInsert(dst, one) == 0 {
			s.count--
			if s.count <= 0 {
				*s = invStack{}
			}
			h.refreshBinViewers(players, target)
			return true
		}
	}
	return false
}

// containerSlots exposes any container's raw slots at a position (nil if the
// position holds no known container).
func (h *hub) containerSlots(pos blockPos) []invStack {
	if c := h.chests[pos]; c != nil {
		return c.slots[:]
	}
	if f := h.furnaces[pos]; f != nil {
		return f.slots[:]
	}
	if b := h.bins[pos]; b != nil {
		return b.slots
	}
	return nil
}

// containerSignal is the comparator's read of a container: 0 when empty, else
// 1 + floor(14 × average slot fullness). Returns -1 for non-containers.
func (h *hub) containerSignal(pos blockPos) int {
	slots := h.containerSlots(pos)
	if slots == nil {
		return -1
	}
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
	return 1 + int(full/float64(len(slots))*14)
}

// refreshBinViewers resyncs any player looking at a container we just mutated.
func (h *hub) refreshBinViewers(players map[int32]*tracked, pos blockPos) {
	for _, t := range players {
		if t.winID == 0 {
			continue
		}
		if t.winKind == winDoubleChest {
			if t.winPos == pos || t.winPos2 == pos {
				h.sendDoubleChestWindow(t)
			}
			continue
		}
		if t.winPos != pos {
			continue
		}
		switch t.winKind {
		case winBin:
			if c := h.bins[pos]; c != nil {
				h.sendBinWindow(t, c)
			}
		case winChest:
			if c := h.chests[pos]; c != nil {
				h.sendChestWindow(t, c)
			}
		case winFurnace:
			if f := h.furnaces[pos]; f != nil {
				h.sendFurnaceWindow(t, f)
			}
		}
	}
}
