package server

import (
	"bytes"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
	"sort"
	"tachyne/internal/worldgen"
)

// Crafting: server-side container clicks + recipe matching, for the player's
// 2x2 grid (window 0) and the crafting table's 3x3 (an opened window). The
// server is authoritative: it tracks every slot move the client reports (the
// window_click packet carries the client's predicted slot changes), computes
// the recipe result for the active grid, and on a result-slot click performs
// the craft itself (consume one from each grid cell, hand over the result),
// resyncing the client's view of every slot it touched. Runs on the hub
// goroutine; connections only parse packets into events.

const (

	// The menu registry is STATIC (client built-in), so this is vanilla's
	// registration order — crafting = 12 (per ViaVersion mappings), NOT the
	// alphabetical position mcmeta's summary lists.
	menuCrafting = 12 // minecraft:crafting menu network id (1.21.5)
)

var (
	craftingTableState = worldgen.BlockBase("crafting_table") // crafting_table block state (single state)
)

// slotChange is one client-reported slot mutation inside a window_click.
type slotChange struct {
	slot int16
	st   invStack
}

// evClick is a parsed serverbound window_click; evCloseWin a close_window;
// evOpenCraft a right-click on a crafting table.
type evClick struct {
	eid      int32
	windowID int32
	slot     int16
	mode     int32
	changed  []slotChange
	cursor   invStack
}
type evCloseWin struct{ eid int32 }
type evOpenCraft struct{ eid int32 }
type evTossHeld struct { // Q / ctrl+Q outside a window: drop from the held slot
	eid  int32
	slot int // hotbar index 0-8
	all  bool
}
type evCreativeSlot struct { // creative set_creative_slot: write through to the inventory
	eid  int32
	slot int16 // player-window slot number (0-45)
	st   invStack
}

func (evClick) isHubEvent()        {}
func (evCloseWin) isHubEvent()     {}
func (evOpenCraft) isHubEvent()    {}
func (evTossHeld) isHubEvent()     {}
func (evCreativeSlot) isHubEvent() {}

// ---- recipe matching ----------------------------------------------------

// shapedIndex buckets shaped recipes by their WxH so a grid's bounding box
// selects a small candidate list; shapelessIndex buckets by ingredient count.
var (
	shapedIndex    = map[uint16][]int{}
	shapelessIndex = map[int][]int{}
)

func init() {
	for i, r := range shapedRecipes {
		k := uint16(r.W)<<8 | uint16(r.H)
		shapedIndex[k] = append(shapedIndex[k], i)
	}
	for i, r := range shapelessRecipes {
		shapelessIndex[len(r.Ingredients)] = append(shapelessIndex[len(r.Ingredients)], i)
	}
}

// matchRecipe finds the crafting result for a w×w grid (w = 2 or 3), or (0,0).
// Shaped patterns match on the grid's non-empty bounding box, in either
// orientation (vanilla allows horizontally mirrored placement); shapeless
// recipes match the multiset of non-empty cells.
func matchRecipe(grid []invStack, w int) (int32, int) {
	minR, minC, maxR, maxC, n := w, w, -1, -1, 0
	for r := 0; r < w; r++ {
		for c := 0; c < w; c++ {
			if s := grid[r*w+c]; s.item != 0 && s.count > 0 {
				n++
				if r < minR {
					minR = r
				}
				if r > maxR {
					maxR = r
				}
				if c < minC {
					minC = c
				}
				if c > maxC {
					maxC = c
				}
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	bw, bh := maxC-minC+1, maxR-minR+1

	cell := func(r, c int) int32 { return grid[(minR+r)*w+minC+c].item }
	for _, ri := range shapedIndex[uint16(bw)<<8|uint16(bh)] {
		rec := &shapedRecipes[ri]
		direct, mirror := true, true
		for r := 0; r < bh && (direct || mirror); r++ {
			for c := 0; c < bw; c++ {
				want := rec.Cells[r*bw+c]
				if cell(r, c) != want {
					direct = false
				}
				if cell(r, bw-1-c) != want {
					mirror = false
				}
			}
		}
		if direct || mirror {
			return rec.Result, int(rec.Count)
		}
	}

	ids := make([]int32, 0, n)
	for i := range grid {
		if s := grid[i]; s.item != 0 && s.count > 0 {
			ids = append(ids, s.item)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, ri := range shapelessIndex[n] {
		rec := &shapelessRecipes[ri]
		ok := true
		for i, id := range rec.Ingredients {
			if ids[i] != id {
				ok = false
				break
			}
		}
		if ok {
			return rec.Result, int(rec.Count)
		}
	}
	return 0, 0
}

// ---- window slot mapping -------------------------------------------------

// gridSize returns the active crafting grid's width (2 in the player inventory,
// 3 with a crafting table window open).
func gridSize(t *tracked) int {
	if t.winKind == winCraft {
		return 3
	}
	return 2
}

// winSlotPtr resolves a window slot number to the server-side stack it stores,
// plus the logical hotbar index (0-8) when the slot is a hotbar slot (so the
// connection's held-item mirror can be updated), else -1. Crafting result slots
// return nil — they are server-owned; furnace/chest slots are real storage.
// Layouts (1.21.5): player window 0 = result, 1-4 craft, 5-8 armor, 9-35 main,
// 36-44 hotbar, 45 offhand; crafting menu = 0 result, 1-9 craft, 10-36 main,
// 37-45 hotbar; furnace menu = 0-2 furnace, 3-29 main, 30-38 hotbar;
// chest (generic_9x3) = 0-26 chest, 27-53 main, 54-62 hotbar.
func (h *hub) winSlotPtr(t *tracked, slot int16) (*invStack, int) {
	switch t.winKind {
	case winFurnace:
		f := h.furnaces[t.winPos]
		switch {
		case f != nil && slot >= 0 && slot <= 2:
			return &f.slots[slot], -1
		case slot >= 3 && slot <= 29:
			return &t.inv.slots[slot+6], -1
		case slot >= 30 && slot <= 38:
			return &t.inv.slots[slot-30], int(slot - 30)
		}
		return nil, -1
	case winChest:
		c := h.chests[t.winPos]
		switch {
		case c != nil && slot >= 0 && slot <= 26:
			return &c.slots[slot], -1
		case slot >= 27 && slot <= 53:
			return &t.inv.slots[slot-18], -1
		case slot >= 54 && slot <= 62:
			return &t.inv.slots[slot-54], int(slot - 54)
		}
		return nil, -1
	case winCraft:
		switch {
		case slot >= 1 && slot <= 9:
			return &t.craft[slot-1], -1
		case slot >= 10 && slot <= 36:
			return &t.inv.slots[slot-1], -1
		case slot >= 37 && slot <= 45:
			return &t.inv.slots[slot-37], int(slot - 37)
		}
		return nil, -1
	case winBin: // dispenser/dropper 9 or hopper 5 slots, then main + hotbar
		c := h.bins[t.winPos]
		if c == nil {
			return nil, -1
		}
		n := int16(len(c.slots))
		switch {
		case slot >= 0 && slot < n:
			return &c.slots[slot], -1
		case slot >= n && slot < n+27:
			return &t.inv.slots[slot-n+9], -1
		case slot >= n+27 && slot < n+36:
			return &t.inv.slots[slot-n-27], int(slot - n - 27)
		}
		return nil, -1
	case winTrade: // same 3-slot shape as the anvil
		switch {
		case slot >= 0 && slot <= 1:
			return &t.trade[slot], -1
		case slot >= 3 && slot <= 29:
			return &t.inv.slots[slot+6], -1
		case slot >= 30 && slot <= 38:
			return &t.inv.slots[slot-30], int(slot - 30)
		}
		return nil, -1
	case winAnvil, winGrind: // 0,1 inputs, 2 result (server-owned), 3-29 main, 30-38 hotbar
		switch {
		case slot >= 0 && slot <= 1:
			return &t.anvil[slot], -1
		case slot >= 3 && slot <= 29:
			return &t.inv.slots[slot+6], -1
		case slot >= 30 && slot <= 38:
			return &t.inv.slots[slot-30], int(slot - 30)
		}
		return nil, -1
	case winEnchant: // enchantment menu = 0 item, 1 lapis, 2-28 main, 29-37 hotbar
		switch {
		case slot >= 0 && slot <= 1:
			return &t.enchSlots[slot], -1
		case slot >= 2 && slot <= 28:
			return &t.inv.slots[slot+7], -1
		case slot >= 29 && slot <= 37:
			return &t.inv.slots[slot-29], int(slot - 29)
		}
		return nil, -1
	}
	switch {
	case slot >= 1 && slot <= 4:
		return &t.craft[slot-1], -1
	case slot >= 5 && slot <= 8:
		return &t.armor[slot-5], -1
	case slot >= 9 && slot <= 35:
		return &t.inv.slots[slot], -1
	case slot >= 36 && slot <= 44:
		return &t.inv.slots[slot-36], int(slot - 36)
	case slot == 45:
		return &t.offhand, -1
	}
	return nil, -1
}

// ---- click handling --------------------------------------------------------

// handleClick applies one window_click. Normal slots are trust-applied from the
// client's declared changes (the client's container prediction mirrors vanilla);
// the result slot is handled authoritatively because only the server knows the
// recipe outcome and the ingredient consumption.
func (h *hub) handleClick(players map[int32]*tracked, e evClick) {
	t := players[e.eid]
	if t == nil || t.inv == nil {
		return
	}
	if e.windowID != t.winID {
		h.resyncWindow(t) // clicked a window we no longer consider open
		return
	}

	// Slot 0 is the crafting result only in windows that HAVE a result slot —
	// in a furnace/chest it's ordinary storage.
	if e.slot == 0 && (t.winKind == winPlayer || t.winKind == winCraft) {
		h.takeCraftResult(players, t, e.mode)
		return
	}
	// Anvil/grindstone slot 2 is their server-owned result.
	if e.slot == 2 && (t.winKind == winAnvil || t.winKind == winGrind) {
		h.takeTwoSlotResult(players, t)
		return
	}
	if e.slot == 2 && t.winKind == winTrade {
		h.takeTradeResult(players, t)
		return
	}

	// Conservation tally: items that net-disappear across this click's declared
	// changes were thrown out of the window (Q over a slot, or a click outside
	// the window at slot -999) — spawn them as drops or they vanish entirely.
	// dmgOf remembers durability wear on the stacks being moved: the client's
	// declared slot states carry no components, so without this a damaged tool
	// would come out of any inventory move fully repaired. (Keyed by item id —
	// tools don't stack, and moving two identical damaged tools in ONE click
	// isn't a thing a vanilla client does.)
	loss := map[int32]int{}
	dmgOf := map[int32]int{}
	enchOf := map[int32][2]enchApply{} // enchantments ride along like wear does
	nameOf := map[int32]string{}       // …and anvil names
	tally := func(st invStack, sign int) {
		if st.item != 0 && st.count > 0 {
			loss[st.item] += sign * st.count
			if st.dmg > 0 {
				dmgOf[st.item] = st.dmg
			}
			if st.enchanted() {
				enchOf[st.item] = st.ench
			}
			if st.name != "" {
				nameOf[st.item] = st.name
			}
		}
	}
	// AUTHORITY: pre-check the declared changes for item FABRICATION before
	// applying anything. Every legitimate non-creative click conserves items
	// (crafting-result takes never reach this path), so a net GAIN of any item
	// means a hacked client conjuring stacks — restore the server's view.
	if t.gamemode != gmCreative {
		gain := map[int32]int{}
		pre := func(st invStack, sign int) {
			if st.item != 0 && st.count > 0 {
				gain[st.item] += sign * st.count
			}
		}
		pre(t.cursor, -1)
		pre(e.cursor, +1)
		for _, ch := range e.changed {
			if ptr, _ := h.winSlotPtr(t, ch.slot); ptr != nil {
				pre(*ptr, -1)
				pre(ch.st, +1)
			}
		}
		for _, n := range gain {
			if n > 0 {
				h.resyncWindow(t)
				h.sendCursor(t)
				return
			}
		}
	}

	tally(t.cursor, +1)
	tally(e.cursor, -1)

	gridTouched, enchTouched := false, false
	for _, ch := range e.changed {
		ptr, hot := h.winSlotPtr(t, ch.slot)
		if ptr == nil {
			continue
		}
		tally(*ptr, +1)
		tally(ch.st, -1)
		// Taking smelted output pays the furnace's banked XP (vanilla: orbs
		// pop when you collect, not when the smelt finishes).
		if t.winKind == winFurnace && ch.slot == 2 && ch.st.count < ptr.count {
			if f := h.furnaces[t.winPos]; f != nil && f.xpBank >= 1 {
				h.spawnXPOrb(players, int(f.xpBank), t.x, t.y, t.z)
				f.xpBank -= float64(int(f.xpBank))
			}
		}
		*ptr = ch.st
		if d, ok := dmgOf[ch.st.item]; ok && ch.st.item != 0 {
			ptr.dmg = d // carry the wear along with the move
		}
		if e, ok := enchOf[ch.st.item]; ok && ch.st.item != 0 {
			ptr.ench = e // …and the enchantments (declared slots carry no components)
		}
		if n, ok := nameOf[ch.st.item]; ok && ch.st.item != 0 {
			ptr.name = n
		}
		if hot >= 0 {
			t.p.setHotbarSlot(hot, ch.st.item)
		}
		w := int16(gridSize(t))
		if (t.winKind == winPlayer || t.winKind == winCraft) && ch.slot >= 1 && ch.slot <= w*w {
			gridTouched = true
		}
		if t.winKind == winEnchant && ch.slot <= 1 {
			enchTouched = true // item or lapis changed — reroll the offers
		}
		if (t.winKind == winAnvil || t.winKind == winGrind) && ch.slot <= 1 {
			enchTouched = true // inputs changed — recompute the result
		}
		if t.winKind == winTrade && ch.slot <= 1 {
			enchTouched = true // trade inputs changed — recompute the result
		}
	}
	t.cursor = e.cursor
	if d, ok := dmgOf[t.cursor.item]; ok && t.cursor.item != 0 {
		t.cursor.dmg = d
	}
	if en, ok := enchOf[t.cursor.item]; ok && t.cursor.item != 0 {
		t.cursor.ench = en
	}
	if n, ok := nameOf[t.cursor.item]; ok && t.cursor.item != 0 {
		t.cursor.name = n
	}
	for item, n := range loss {
		if n > 0 {
			h.tossItem(players, t, item, n, dmgOf[item], enchOf[item])
		}
	}
	if gridTouched {
		h.sendCraftResult(t)
	}
	if enchTouched {
		switch t.winKind {
		case winEnchant:
			h.rollEnchOptions(t)
		case winAnvil, winGrind:
			h.sendTwoSlotWindow(t)
		case winTrade:
			h.sendTradeWindow(t)
		}
	}
	h.broadcastEquipment(players, t) // armor/held-item slots may have changed
}

// tossItem spawns a thrown item entity a step in front of the player, with an
// extended pickup delay so it isn't instantly re-collected by the thrower.
func (h *hub) tossItem(players map[int32]*tracked, t *tracked, item int32, count, dmg int, ench [2]enchApply) {
	yaw := float64(t.yaw) * math.Pi / 180
	tx := t.x - math.Sin(yaw)*1.5
	tz := t.z + math.Cos(yaw)*1.5
	if it := h.spawnItem(players, item, count, tx, t.y+1, tz); it != nil {
		it.noPickupUntil = it.born + 40 // ~2s before pickup (vanilla toss delay)
		it.dmg = dmg
		it.ench = ench
	}
}

// tossHeld handles Q / ctrl+Q with no window open: drop one (or all) of the
// held hotbar slot as an item entity.
func (h *hub) tossHeld(players map[int32]*tracked, t *tracked, slot int, all bool) {
	if t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	s := &t.inv.slots[slot]
	if s.item == 0 || s.count == 0 {
		return
	}
	n := 1
	if all {
		n = s.count
	}
	item, dmg, ench := s.item, s.dmg, s.ench
	if s.count -= n; s.count == 0 {
		*s = invStack{}
	}
	h.sendSlot(t, slot)
	h.tossItem(players, t, item, n, dmg, ench)
}

// takeCraftResult performs a click on the result slot: match the grid, hand the
// result over (cursor, or straight to inventory on shift-click), consume one
// item from every grid cell, and resync everything the craft touched.
func (h *hub) takeCraftResult(players map[int32]*tracked, t *tracked, mode int32) {
	w := gridSize(t)
	grid := t.craft[:w*w]
	item, count := matchRecipe(grid, w)
	if item == 0 {
		h.sendCraftResult(t) // clicked an empty/stale result — just resync it
		return
	}

	switch {
	case mode == 1: // shift-click: craft one straight into the inventory
		changed, leftover := t.inv.add(item, count)
		for _, slot := range changed {
			h.sendSlot(t, slot)
		}
		if leftover > 0 {
			h.spawnItem(players, item, leftover, t.x, t.y, t.z)
		}
	case t.cursor.item == 0:
		t.cursor = invStack{item: item, count: count}
	case t.cursor.item == item && t.cursor.count+count <= stackCap(item):
		t.cursor.count += count
	default: // cursor holds something else — vanilla refuses the take
		h.sendCraftResult(t)
		h.sendCursor(t)
		return
	}

	for i := range grid {
		if grid[i].item != 0 && grid[i].count > 0 {
			if grid[i].count--; grid[i].count == 0 {
				grid[i].item = 0
			}
		}
	}
	for i := range grid { // resync the consumed grid
		h.sendWinSlot(t, int16(i+1), grid[i])
	}
	h.sendCraftResult(t)
	h.sendCursor(t)
}

// ---- open / close ----------------------------------------------------------

// openCraftingTable opens the 3x3 crafting window for a player.
func (h *hub) openCraftingTable(t *tracked) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t) // switching windows: release any furnace/chest view
	h.reclaimCraft(nil, t)    // fold any 2x2 grid contents back first
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind = h.nextWin, winCraft

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuCrafting), Title: "Crafting"})
	h.sendCraftWindow(t)
}

// closeWindow folds the transient stacks (crafting grid + cursor) back into the
// inventory — vanilla behaviour on closing a screen — and resets to window 0.
// A furnace/chest keeps its contents (only the cursor is reclaimed) and loses
// its viewer, so a furnace smelts on unwatched.
func (h *hub) closeWindow(players map[int32]*tracked, t *tracked) {
	h.reclaimAnvil(players, t)
	h.reclaimTrade(players, t)
	if t.winKind == winChest {
		h.playSound(players, "minecraft:block.chest.close", sndBlock,
			float64(t.winPos.x)+0.5, float64(t.winPos.y), float64(t.winPos.z)+0.5, 0.5, 1)
	}
	h.releaseContainerView(t)
	h.reclaimCraft(players, t)
	h.reclaimEnchant(players, t) // table item + lapis come back too
	t.winID = 0
	h.sendInventory(t)
}

// releaseContainerView detaches the player from a viewed furnace/chest block
// and resets the window kind (the window id itself is the caller's business).
func (h *hub) releaseContainerView(t *tracked) {
	switch t.winKind {
	case winFurnace:
		if f := h.furnaces[t.winPos]; f != nil && f.viewer == t.p.eid {
			f.viewer = 0
		}
	}
	t.winKind, t.winPos = winPlayer, blockPos{}
}

// reclaimCraft returns grid + cursor contents to the inventory, dropping what
// doesn't fit at the player's feet (players may be nil to skip drops, e.g. when
// re-homing the 2x2 grid while opening a table — then leftovers stay in hand).
func (h *hub) reclaimCraft(players map[int32]*tracked, t *tracked) {
	give := func(st *invStack) {
		if st.item == 0 || st.count == 0 {
			return
		}
		changed, leftover := t.inv.addStack(*st)
		for _, slot := range changed {
			h.sendSlot(t, slot)
		}
		if leftover > 0 && players != nil {
			if it := h.spawnItem(players, st.item, leftover, t.x, t.y, t.z); it != nil {
				it.dmg = st.dmg
				it.ench = st.ench
			}
			leftover = 0
		}
		if st.count = leftover; leftover == 0 {
			*st = invStack{}
		}
	}
	for i := range t.craft {
		give(&t.craft[i])
	}
	give(&t.cursor)
	h.sendCursor(t)
}

// resyncWindow pushes the server's full view of the active window — the recovery
// path when a click references a window we don't consider open (stale id).
func (h *hub) resyncWindow(t *tracked) {
	switch t.winKind {
	case winFurnace:
		if f := h.furnaces[t.winPos]; f != nil {
			h.sendFurnaceWindow(t, f)
		}
	case winChest:
		if c := h.chests[t.winPos]; c != nil {
			h.sendChestWindow(t, c)
		}
	case winBin:
		if c := h.bins[t.winPos]; c != nil {
			h.sendBinWindow(t, c)
		}
	case winTrade:
		h.sendTradeWindow(t)
	case winCraft:
		h.sendCraftWindow(t)
	default:
		h.sendInventory(t)
		h.sendCursor(t)
	}
}

// (reclaimAll is gone: worn armor + offhand persist across relogs now, so
// leave only folds the crafting grid + cursor back via reclaimCraft.)

// ---- packet parsing (connection side) ---------------------------------------

// parseWindowClick decodes a serverbound window_click (0x10): windowId, stateId,
// slot, button, mode, changed slots (each an Option<HashedSlot> — item id, count,

func readI16(br *bytes.Reader) (int16, bool) {
	hi, err1 := br.ReadByte()
	lo, err2 := br.ReadByte()
	if err1 != nil || err2 != nil {
		return 0, false
	}
	return int16(uint16(hi)<<8 | uint16(lo)), true
}

// readHashedOption reads an Option<HashedSlot>: a presence bool, then item id +
// count + added components (type + i32 hash each) + removed components (type

// ---- client sync -----------------------------------------------------------

// sendWinSlot updates one slot of the player's active window on the client.
func (h *hub) sendWinSlot(t *tracked, slot int16, st invStack) {
	t.inv.stateId++
	t.p.trySendEv(attachproto.WindowSlot{ID: int32(t.winID), StateID: t.inv.stateId,
		Slot: int32(slot), Item: stackEv(st)})
}

// sendCraftResult recomputes the active grid's recipe result and syncs slot 0.
func (h *hub) sendCraftResult(t *tracked) {
	w := gridSize(t)
	item, count := matchRecipe(t.craft[:w*w], w)
	h.sendWinSlot(t, 0, invStack{item: item, count: count})
}

// sendCursor syncs the stack carried on the player's mouse cursor.
func (h *hub) sendCursor(t *tracked) {
	t.p.trySendEv(attachproto.CursorItem{Item: stackEv(t.cursor)})
}

// sendCraftWindow refreshes the whole crafting-table window (46 slots: result,
// 3x3 grid, main inventory, hotbar) plus the cursor.
func (h *hub) sendCraftWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 46)
	item, count := matchRecipe(t.craft[:9], 3)
	slots = append(slots, stackNumEv(item, count))
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.craft[i]))
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
