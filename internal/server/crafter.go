package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"

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

// crafterDelay is CrafterBlock.neighborChanged's scheduleTick(pos, this, 4):
// a crafter crafts 4 ticks after its rising edge.
const crafterDelay = 4

// updateCrafter is CrafterBlock.neighborChanged: a rising edge latches
// TRIGGERED and schedules the craft; a falling edge clears the latch.
func (h *hub) updateCrafter(players map[int32]*tracked, pos simPos, state uint32) {
	// A crafting=true state is the animation flag — settle it back to false.
	if (state-crafterMin)/24 == 0 {
		state = crafterWithCrafting(state, false)
		h.setBlockAt(players, pos.dim, pos.blockPos, state)
	}
	powered := h.inputPower(pos.x, pos.y, pos.z, false) > 0
	if powered == crafterTriggered(state) {
		return
	}
	if powered {
		h.inDim(pos.dim, func() { h.scheduleTick(pos.blockPos, crafterDelay, tickNormal) })
	}
	h.setBlockAt(players, pos.dim, pos.blockPos, crafterWithTriggered(state, powered))
}

// crafterTick is CrafterBlock.tick → dispenseFrom: the craft itself, which
// does not look at the power again (a pulse shorter than 4 still crafts).
func (h *hub) crafterTick(players map[int32]*tracked, pos simPos, state uint32) {
	if !h.crafterCraft(players, pos, state) {
		return // a failed craft only clicks: no animation
	}
	// setCraftingTicksRemaining(6) + CRAFTING: the arm animates for six
	// ticks, and the block entity's countdown settles it back.
	if cur := h.worldFor(pos.dim).At(pos.x, pos.y, pos.z); isCrafter(cur) {
		h.setBlockAt(players, pos.dim, pos.blockPos, crafterWithCrafting(cur, true))
	}
	h.scheduleIn(pos.dim, pos.blockPos, crafterAnimTicks)
}

// crafterAnimTicks is CrafterBlock.MAX_CRAFTING_TICKS.
const crafterAnimTicks = 6

// crafterCraft matches the 3×3 grid and, on a hit, consumes one of each
// ingredient and ejects the result.
func (h *hub) crafterCraft(players map[int32]*tracked, pos simPos, state uint32) bool {
	c := h.bins[pos]
	if c == nil || len(c.slots) < 9 {
		h.craftFail(players, pos)
		return false
	}
	res, kind := h.crafterMatch(c) // the same match the preview shows: a suspicious stew keeps its flower
	if res.item == 0 || res.count == 0 {
		h.craftFail(players, pos)
		return false
	}
	switch kind { // assemble: the zoomed map and the book copy are new
	case mapCraftZoom:
		src := h.maps.get(res.mapID)
		res.mapID = h.maps.derive(src, src.Scale+1, false).ID
	case craftBookClone:
		if b, ok := h.books.get(res.bookID); ok {
			b.Gen++
			res.bookID = h.books.create(b)
		}
	}
	item := res.item
	remains := crafterRemainders(c.slots[:9], kind)
	for i := 0; i < 9; i++ {
		if c.slots[i].item != 0 && c.slots[i].count > 0 {
			if c.slots[i].count--; c.slots[i].count <= 0 {
				c.slots[i] = invStack{}
			}
		}
	}
	// CrafterRecipeCraftedTrigger: vanilla credits every player inside a
	// 17-block cube around the crafter (CrafterBlock.craft).
	for _, t := range players {
		if t.dim == pos.dim && math.Abs(t.x-float64(pos.x)-0.5) <= 8.5 && math.Abs(t.y-float64(pos.y)-0.5) <= 8.5 && math.Abs(t.z-float64(pos.z)-0.5) <= 8.5 {
			h.advance(players, t, "crafter_recipe_crafted", advMatch{recipe: itemNameOf[item]})
		}
	}
	h.ejectCrafted(players, pos, state, res)
	for _, st := range remains { // dispenseFrom ejects the remainders after the result
		h.ejectCrafted(players, pos, state, st)
	}
	h.refreshBinViewers(players, pos)
	h.playSoundDim(players, pos.dim, "minecraft:block.crafter.craft", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	return true
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
	pos := simPos{dim: t.dim, blockPos: blockPos{x, y, z}}
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
//
// It runs the SAME resolver a crafting table does — vanilla's crafter goes
// through the recipe manager, and the "special" recipes are ordinary
// recipes there — so an auto-crafter makes tipped arrows, dyed armour,
// suspicious stew, firework rockets, the box/bundle transmutes, map
// cloning and extending and book cloning. A zoomed map and a copied book
// are minted when the crafter fires (crafterCraft), as a table mints them
// at take time; the preview shows the recipe's match.
func (h *hub) crafterResult(c *bin) invStack {
	res, _ := h.crafterMatch(c)
	return res
}

// crafterMatch is crafterResult with the recipe kind the craft needs.
func (h *hub) crafterMatch(c *bin) (invStack, int) {
	res, kind := h.craftResult(c.slots[:9], 3)
	if res.item == 0 || res.count == 0 {
		return invStack{}, mapCraftNone
	}
	return res, kind
}

// crafterRemainders is Recipe.getRemainingItems for the grid: the
// patterned banner a banner copy keeps, the written book a book copy
// keeps, and each ingredient's craft remainder (a milk bucket's empty
// bucket, a honey bottle's glass bottle).
func crafterRemainders(grid []invStack, kind int) []invStack {
	var out []invStack
	for _, st := range grid {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		switch {
		case kind == craftKeepPattern && st.pats[0].patPlus1 != 0, kind == craftBookClone && st.bookID != 0:
			keep := st
			keep.count = 1
			out = append(out, keep)
		case craftRemainder[st.item] != 0:
			out = append(out, invStack{item: craftRemainder[st.item], count: 1})
		}
	}
	return out
}

// crafterResultSlot is the menu index of the result preview: vanilla's
// CrafterMenu adds the 3×3 grid (0-8), the standard inventory (9-35) and
// hotbar (36-44), then the NonInteractiveResultSlot last.
const crafterResultSlot = 45

// sendCrafterWindow refreshes the whole crafter window: 9 grid slots, main
// inventory + hotbar, then the result preview (slot 45).
func (h *hub) sendCrafterWindow(t *tracked, c *bin) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 46)
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(c.slots[i]))
	}
	for i := 9; i <= 35; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i <= 8; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	slots = append(slots, stackEv(h.crafterResult(c))) // slot 45: result preview
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
func (h *hub) refreshCrafterResult(players map[int32]*tracked, pos simPos) {
	c := h.bins[pos]
	if c == nil {
		return
	}
	res := h.crafterResult(c)
	for _, t := range players {
		if t.winKind == winCrafter && t.winPos == pos {
			h.sendWinSlot(t, crafterResultSlot, res)
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
func (h *hub) crafterBinAt(pos simPos) *bin {
	if w := h.worldFor(pos.dim); w != nil && isCrafter(w.At(pos.x, pos.y, pos.z)) {
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

func (h *hub) craftFail(players map[int32]*tracked, pos simPos) {
	h.playSoundDim(players, pos.dim, "minecraft:block.crafter.fail", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

// ejectCrafted pushes the result into the container the crafter faces, or drops
// it into the world if none accepts it (CrafterBlockEntity output behaviour).
func (h *hub) ejectCrafted(players map[int32]*tracked, pos simPos, state uint32, st invStack) {
	dx, dy, dz := crafterFront(state)
	target := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	if dst := h.containerSlots(pos.at(target)); dst != nil {
		var left int
		if cb := h.crafterBinAt(pos.at(target)); cb != nil {
			left = crafterInsert(cb, st) // crafter → crafter
		} else {
			left = binInsert(dst, st)
		}
		if left == 0 {
			h.refreshBinViewers(players, pos.at(target))
			return
		} else if left < st.count {
			h.refreshBinViewers(players, pos.at(target))
			st.count = left
		}
	}
	fx := float64(pos.x) + 0.5 + float64(dx)*0.7
	fy := float64(pos.y) + 0.5 + float64(dy)*0.7
	fz := float64(pos.z) + 0.5 + float64(dz)*0.7
	if it := h.spawnItemIn(players, pos.dim, st.item, st.count, fx, fy, fz); it != nil {
		it.setFrom(st) // the whole stack: a stew's flower, a map's id, a rocket's stars
		h.refreshItemMeta(players, it)
	}
}
