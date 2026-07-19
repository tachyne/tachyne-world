package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Crafter (1.21 auto-crafter): a 3×3 block that crafts its grid's recipe on a
// rising redstone edge and ejects the result out its front. Reimplemented from
// CrafterBlock. Storage reuses the 9-slot `bin` machinery, so hopper-fill and
// persistence come for free; the block adds the redstone-triggered craft +
// eject, its own menu (crafter_3x3) with a live result preview + per-slot
// disable toggles, the disabled-aware hopper insert, and the filled+disabled
// comparator signal.

const menuCrafter = 7 // minecraft:crafter_3x3 menu network id

var crafterMin, crafterMax = worldgen.BlockRange("crafter") // crafting(2)×orientation(12)×triggered(2)

func isCrafter(s uint32) bool { return s >= crafterMin && s <= crafterMax }

// State layout (alphabetical props, booleans list true first):
// off = crafting_idx*24 + orientation_idx*2 + triggered_idx.
func crafterTriggered(s uint32) bool { return (s-crafterMin)%2 == 0 }
func crafterWithTriggered(s uint32, t bool) uint32 {
	b := s - (s-crafterMin)%2
	if t {
		return b
	}
	return b + 1
}
func crafterWithCrafting(s uint32, c bool) uint32 {
	off := s - crafterMin
	rest := off % 24
	if c {
		return crafterMin + rest // crafting index 0 = true
	}
	return crafterMin + 24 + rest
}

// crafterFront maps the orientation to the ejection direction (the first token
// of the orientation name — down_east → down, north_up → north).
func crafterFront(s uint32) (int, int, int) {
	switch (s - crafterMin) / 2 % 12 {
	case 0, 1, 2, 3: // down_*
		return 0, -1, 0
	case 4, 5, 6, 7: // up_*
		return 0, 1, 0
	case 8: // west_up
		return -1, 0, 0
	case 9: // east_up
		return 1, 0, 0
	case 10: // north_up
		return 0, 0, -1
	}
	return 0, 0, 1 // south_up
}

// updateCrafter tracks the redstone edge; a rising edge crafts once.
func (h *hub) updateCrafter(players map[int32]*tracked, pos blockPos, state uint32) {
	// A crafting=true state is the animation flag — settle it back to false.
	if (state-crafterMin)/24 == 0 {
		state = crafterWithCrafting(state, false)
		h.setBlock(players, pos, state)
	}
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if powered == crafterTriggered(state) {
		return
	}
	state = crafterWithTriggered(state, powered)
	if powered {
		state = crafterWithCrafting(state, true) // brief craft animation
		h.setBlock(players, pos, state)
		h.crafterCraft(players, pos, state)
		h.schedule(pos, 4) // clear the animation shortly after
		return
	}
	h.setBlock(players, pos, state)
}

// crafterCraft matches the 3×3 grid and, on a hit, consumes one of each
// ingredient and ejects the result.
func (h *hub) crafterCraft(players map[int32]*tracked, pos blockPos, state uint32) {
	c := h.bins[pos]
	if c == nil || len(c.slots) < 9 {
		h.craftFail(players, pos)
		return
	}
	grid := make([]invStack, 9)
	copy(grid, c.slots[:9])
	item, count := matchRecipe(grid, 3)
	if item == 0 || count == 0 {
		h.craftFail(players, pos)
		return
	}
	for i := 0; i < 9; i++ {
		if c.slots[i].item != 0 && c.slots[i].count > 0 {
			if c.slots[i].count--; c.slots[i].count <= 0 {
				c.slots[i] = invStack{}
			}
		}
	}
	h.ejectCrafted(players, pos, state, item, count)
	h.refreshBinViewers(players, pos)
	h.playSound(players, "minecraft:block.crafter.craft", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

// ---- crafter menu (crafter_3x3): result preview + disabled slots -----------

type evSlotState struct {
	eid    int32
	slot   int32
	enable bool // vanilla newState: true = enable the slot, false = disable it
}

func (evSlotState) isHubEvent() {}

// openCrafter opens the crafter's own menu — the 9-slot grid, a non-interactive
// result-preview slot, and the per-slot disabled overlay carried as container
// data. Storage is the shared bin at this position.
func (h *hub) openCrafter(t *tracked, x, y, z int) {
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	pos := blockPos{x, y, z}
	c := h.bins[pos]
	if c == nil {
		c = &bin{slots: make([]invStack, 9)}
		h.bins[pos] = c
	}
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind = h.nextWin, pos, winCrafter
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuCrafter), Title: "Crafter"})
	h.sendCrafterWindow(t, c)
	h.sendCrafterData(t, c)
}

// crafterResult is the current recipe output for the grid (empty = no match).
// Disabled slots are always empty, so the grid is the recipe as-is.
func crafterResult(c *bin) invStack {
	item, count := matchRecipe(c.slots[:9], 3)
	if item == 0 || count == 0 {
		return invStack{}
	}
	return invStack{item: item, count: count}
}

// sendCrafterWindow refreshes the whole crafter window: 9 grid slots, the
// result preview (slot 9), then main inventory + hotbar.
func (h *hub) sendCrafterWindow(t *tracked, c *bin) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 46)
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(c.slots[i]))
	}
	slots = append(slots, stackEv(crafterResult(c))) // slot 9: result preview
	for i := 9; i <= 35; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// sendCrafterData syncs the container properties the client renders the disabled
// overlay from: props 0-8 = per-slot state (1 = disabled), prop 9 = triggered.
func (h *hub) sendCrafterData(t *tracked, c *bin) {
	for i := 0; i < 9; i++ {
		v := int32(0)
		if c.disabled[i] {
			v = 1
		}
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: int32(i), Value: v})
	}
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 9, Value: 0}) // triggered
}

// refreshCrafterResult resends the result-preview slot (window slot 9) to every
// player viewing the crafter at pos, after its grid changed.
func (h *hub) refreshCrafterResult(players map[int32]*tracked, pos blockPos) {
	c := h.bins[pos]
	if c == nil {
		return
	}
	res := crafterResult(c)
	for _, t := range players {
		if t.winKind == winCrafter && t.winPos == pos {
			h.sendWinSlot(t, 9, res)
		}
	}
}

// onSlotState toggles a crafter grid slot's disabled flag (vanilla
// CrafterBlockEntity.setSlotState). Only an EMPTY slot may be toggled — the
// vanilla screen never disables a filled slot — and only crafter viewers of the
// clicked block are affected.
func (h *hub) onSlotState(players map[int32]*tracked, e evSlotState) {
	t := players[e.eid]
	if t == nil || t.winKind != winCrafter || e.slot < 0 || e.slot > 8 {
		return
	}
	c := h.bins[t.winPos]
	if c == nil || c.slots[e.slot].item != 0 { // never disable a filled slot
		return
	}
	c.disabled[e.slot] = !e.enable
	// (persistence is a periodic full snapshot; no per-bin dirty flag)
	// Re-broadcast the property to everyone viewing this crafter.
	val := int32(0)
	if c.disabled[e.slot] {
		val = 1
	}
	for _, v := range players {
		if v.winKind == winCrafter && v.winPos == t.winPos {
			v.p.trySendEv(attachproto.WindowData{ID: int32(v.winID), Prop: e.slot, Value: val})
		}
	}
	h.refreshCrafterResult(players, t.winPos) // a new hole can change the match
}

// ---- crafter-aware hopper fill + comparator --------------------------------

// crafterBinAt returns the bin backing a crafter block at pos (nil if the block
// there is not a crafter) — the disabled mask lives on the bin.
func (h *hub) crafterBinAt(pos blockPos) *bin {
	if isCrafter(h.world.At(pos.x, pos.y, pos.z)) {
		return h.bins[pos]
	}
	return nil
}

// crafterInsertTarget is vanilla CrafterBlockEntity.canPlaceItem +
// smallerStackExist: the enabled grid slot a hopper should fill for `item` —
// the one holding the FEWEST matching items (empties count as zero, filled
// first), ties broken toward the lowest index. -1 if none can take it.
func crafterInsertTarget(c *bin, item int32) int {
	best, bestCount := -1, 1<<30
	for i := 0; i < 9; i++ {
		if c.disabled[i] {
			continue
		}
		s := c.slots[i]
		if s.item != 0 && (s.item != item || s.count >= stackMax) {
			continue // occupied by a different item, or already full
		}
		count := 0
		if s.item != 0 {
			count = s.count
		}
		if count < bestCount { // strictly-fewer wins; equal keeps the lower index
			best, bestCount = i, count
		}
	}
	return best
}

// crafterInsert fills a crafter grid the vanilla way — skip disabled slots and
// route each item to the emptiest enabled slot — one item at a time. Returns
// the count that did not fit.
func crafterInsert(c *bin, st invStack) int {
	left := st.count
	for left > 0 {
		slot := crafterInsertTarget(c, st.item)
		if slot < 0 {
			break
		}
		if c.slots[slot].item == 0 {
			c.slots[slot] = invStack{item: st.item, count: 1}
		} else {
			c.slots[slot].count++
		}
		left--
	}
	return left
}

// crafterComparator is vanilla CrafterBlockEntity.getRedstoneSignal: the number
// of grid slots that are filled OR disabled (0-9), not the fullness average.
func crafterComparator(c *bin) int {
	n := 0
	for i := 0; i < 9; i++ {
		if c.disabled[i] || (c.slots[i].item != 0 && c.slots[i].count > 0) {
			n++
		}
	}
	return n
}

func (h *hub) craftFail(players map[int32]*tracked, pos blockPos) {
	h.playSound(players, "minecraft:block.crafter.fail", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

// ejectCrafted pushes the result into the container the crafter faces, or drops
// it into the world if none accepts it (CrafterBlockEntity output behaviour).
func (h *hub) ejectCrafted(players map[int32]*tracked, pos blockPos, state uint32, item int32, count int) {
	dx, dy, dz := crafterFront(state)
	target := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	if dst := h.containerSlots(target); dst != nil {
		var left int
		if cb := h.crafterBinAt(target); cb != nil {
			left = crafterInsert(cb, invStack{item: item, count: count}) // crafter → crafter
		} else {
			left = binInsert(dst, invStack{item: item, count: count})
		}
		if left == 0 {
			h.refreshBinViewers(players, target)
			return
		} else if left < count {
			h.refreshBinViewers(players, target)
			count = left
		}
	}
	fx := float64(pos.x) + 0.5 + float64(dx)*0.7
	fy := float64(pos.y) + 0.5 + float64(dy)*0.7
	fz := float64(pos.z) + 0.5 + float64(dz)*0.7
	h.spawnItem(players, item, count, fx, fy, fz)
}
