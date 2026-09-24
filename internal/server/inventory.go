package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Survival inventory + item pickup. A survival player has a 36-slot inventory
// (9 hotbar + 27 main); walking over a dropped item collects it into the first
// available stack/slot and updates the client. Creative players manage their own
// inventory client-side, so pickup is survival-only. Runs on the hub goroutine.

const (
	invSize        = 36 // 0-8 hotbar, 9-35 main
	stackMax       = 64
	pickupDelay    = 10 // ticks before a fresh drop can be collected (~0.5s)
	playerInvSlots = 46 // full player window (0-45) for a full refresh
)

// enchApply is one enchantment on a stack: the network id (our declared
// enchantment-registry order, identical on every client version) + level.
type enchApply struct{ id, lvl int8 }

type invStack struct {
	item       int32
	count      int
	dmg        int      // durability damage taken (tools/armor); 0 for everything else
	ench       enchList // up to four enchantments (zero slots = none); comparable
	name       string   // anvil rename ("" = none); persisted by id via the nameStore (names.go)
	repairCost int      // anvil prior-work penalty (grows 2·max+1 per use)
	potion     int8     // brewed potion type (potWater..): drives drink effects + label
	mapID      int32    // filled_map: which map this stack shows (0 = none)
	color      int32    // dyed_color rgb for leather armour (0 = undyed; dye.go)
	stew       int8     // suspicious_stew_effects: 1 + the stewEffects index (0 = none; stew.go)
	shieldBase int8     // base_color: 1 + the dye of a decorated shield's banner (0 = plain; craftspecial.go)

	// Banner pattern layers (loom): patPlus1 is the banner_pattern registry
	// id + 1 (0 = empty layer, layers fill from index 0); color is the dye
	// enum. Fixed-size so invStack stays comparable.
	pats [6]bannerLayer
	// Armor trim (smithing): registry id + 1 each, 0 = untrimmed.
	trimMat, trimPat int8
	// Book identity (0 = none): pages/title live in the hub's bookStore,
	// composed into the content component at send time (the map model).
	bookID int32
	// Goat horn: which of the eight instruments this horn sounds (0 = ponder,
	// which is also what an unset horn plays).
	instrument int8
	// Firework rocket flight duration, 1-3 (0 = an unset rocket, which flies
	// as 1 does). The gunpowder in its recipe is what sets it.
	flight int8
	// A decorated pot's four faces as item ids (back, left, right, front);
	// all zero is the plain brick pot. Fixed-size so invStack stays comparable.
	sherds potSherds
	// Firework bursts (0 = none): a star's one burst or a rocket's up to
	// seven live in the hub's star store. Same indirection as bundles, for
	// the same reason — the list is variable-length and invStack is
	// comparable.
	starID int32
	// Lodestone compass target (lodestone.go); zero = a plain compass.
	lode lodeTracker
	// Shulker-box identity (0 = none): the 27 slots live in the hub's box
	// store, so a broken box carries its contents as an item. Same indirection
	// as maps and books, for the same reason — invStack stays comparable.
	boxID int32
	// Carried-hive identity (0 = none): a Silk-Touched hive's bees + honey
	// live in the hub's hiveItems store. Same indirection as boxID.
	hiveID int32
	// Bundle identity (0 = empty pouch): the contents live in the hub's bundle
	// store. Same indirection again — and the reason a bundle can be dropped,
	// chested or put inside another bundle without losing what it holds.
	bundleID int32
}

// bannerLayer is one loom-applied pattern layer (wire encoding: id+1, dye).
type bannerLayer struct {
	patPlus1 int16
	color    int8
}

// patCount is the number of applied banner layers.
func (st invStack) patCount() int {
	for i, l := range st.pats {
		if l.patPlus1 == 0 {
			return i
		}
	}
	return len(st.pats)
}

// sameItemComponents is ItemStack.isSameItemSameComponents: the same item
// carrying the same data in every field, whatever the counts. It is the one
// test for whether two stacks may merge. Each merge site used to list the
// fields it remembered, and a field added later (a potion, a shulker box's
// contents, a bundle, a dye colour) was dropped by every site that did not
// list it: picking a potion up turned it into a plain one.
func sameItemComponents(a, b invStack) bool {
	a.count, b.count = 0, 0
	return a == b
}

// enchanted reports whether the stack carries any enchantment.
func (st invStack) enchanted() bool { return st.ench != enchList{} }

// enchLvl is the stack's level of one enchantment id (0 = not present).
func (st invStack) enchLvl(id int8) int {
	for _, e := range st.ench {
		if e.id == id && e.lvl > 0 {
			return int(e.lvl)
		}
	}
	return 0
}

// packEnch/unpackEnch squeeze the two (id, lvl) pairs into one int32 for the
// JSON stores (row shape [item, count, dmg, ench]); old 3-column rows load
// with a zero here — unenchanted — the same trick as the armor migration.
// enchList is a stack's enchantments: up to four (vanilla loot and table
// rolls reach three or four; the anvil can stack more but four covers what
// play produces). Fixed-size so invStack stays comparable. Persisted as two
// int32 columns of two (id, level) byte pairs each — packEnch (slots 0-1,
// the original column) and packEnchHi (slots 2-3, added 2026-09-06; older
// rows unpack with the high column zero).
// enchList holds a stack's enchantments. Vanilla's ItemEnchantments has no
// cap; eight covers every combination a survival tool, sword or armour
// piece can legitimately carry (a sword takes at most seven: sharpness,
// unbreaking, mending, looting, fire aspect, knockback, sweeping edge).
// Fixed-size so stacks stay comparable with ==.
type enchList = [8]enchApply

// packEnchAt squeezes the two (id, lvl) pairs at slots i and i+1 into one
// int32 — the persisted columns are two pairs each.
func packEnchAt(e enchList, i int) int32 {
	return int32(uint8(e[i].id))<<24 | int32(uint8(e[i].lvl))<<16 |
		int32(uint8(e[i+1].id))<<8 | int32(uint8(e[i+1].lvl))
}

func packEnch(e enchList) int32   { return packEnchAt(e, 0) }
func packEnchHi(e enchList) int32 { return packEnchAt(e, 2) }
func packEnch3(e enchList) int32  { return packEnchAt(e, 4) }
func packEnch4(e enchList) int32  { return packEnchAt(e, 6) }

// unpackEnchAt decodes one persisted column into slots i and i+1.
func unpackEnchAt(e *enchList, i int, v int32) {
	e[i] = enchApply{id: int8(v >> 24), lvl: int8(v >> 16)}
	e[i+1] = enchApply{id: int8(v >> 8), lvl: int8(v)}
}

func unpackEnch(v int32) enchList {
	var e enchList
	unpackEnchAt(&e, 0, v)
	return e
}

// unpackEnch2 decodes the first two persisted columns (slots 0-3).
func unpackEnch2(lo, hi int32) enchList {
	e := unpackEnch(lo)
	unpackEnchAt(&e, 2, hi)
	return e
}

// unpackEnch4 decodes all four persisted columns (slots 0-7).
func unpackEnch4(c0, c1, c2, c3 int32) enchList {
	e := unpackEnch2(c0, c1)
	unpackEnchAt(&e, 4, c2)
	unpackEnchAt(&e, 6, c3)
	return e
}

type inventory struct {
	slots   [invSize]invStack
	stateId int32
}

// stackCap returns an item's max stack size. Non-block items (tools, buckets,
// pearls, …) come from the generated items.json table; block items use
// blocks.json's per-block stackSize (64/16/1).
func stackCap(item int32) int {
	if item > 0 {
		if sz, ok := itemStackSize[item]; ok {
			return sz
		}
		if state, ok := protocol.BlockForItem(item); ok {
			return worldgen.StackSizeState(state)
		}
	}
	return stackMax
}

// add inserts up to count of a plain item (no stored data), filling
// matching stacks then empty slots. Returns the changed slot indices and any
// leftover that didn't fit.
func (inv *inventory) add(item int32, count int) (changed []int, leftover int) {
	return inv.addStack(invStack{item: item, count: count})
}

// addStack inserts a whole stack (Inventory.add → addResource): it tops up
// stacks that are the same item with the same data, then fills empty slots
// with copies of it, so whatever the stack carries goes in with it.
func (inv *inventory) addStack(st invStack) (changed []int, leftover int) {
	count := st.count
	cap := stackCap(st.item)
	for i := range inv.slots {
		if count == 0 {
			break
		}
		if s := &inv.slots[i]; s.count > 0 && s.count < cap && sameItemComponents(*s, st) {
			n := min(cap-s.count, count)
			s.count += n
			count -= n
			changed = append(changed, i)
		}
	}
	for i := range inv.slots {
		if count == 0 {
			break
		}
		if s := &inv.slots[i]; s.count == 0 {
			n := min(cap, count)
			*s = st
			s.count = n
			count -= n
			changed = append(changed, i)
		}
	}
	return changed, count
}

// hasRoomFor reports whether at least part of a stack would fit. Vanilla's
// shift-click loop stops when the quick-move can no longer place anything, so
// a repeat-craft asks this before going round again.
func (inv *inventory) hasRoomFor(st invStack) bool {
	if st.count == 0 {
		return true
	}
	cap := stackCap(st.item)
	for i := range inv.slots {
		s := &inv.slots[i]
		if s.count == 0 {
			return true
		}
		if s.count < cap && sameItemComponents(*s, st) {
			return true
		}
	}
	return false
}

// windowSlot maps a logical inventory index to its player-window slot: hotbar
// (0-8) lives in window slots 36-44, the main inventory (9-35) maps directly.
func windowSlot(logical int) int16 {
	if logical < 9 {
		return int16(36 + logical)
	}
	return int16(logical)
}

// pickupItems collects nearby dropped items into survival players' inventories.
func (h *hub) pickupItems(players map[int32]*tracked) {
	now := h.tick.Load()
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead || t.inv == nil {
			continue
		}
		for eid, it := range h.items {
			if now < it.noPickupUntil || it.dim != t.dim {
				continue
			}
			if math.Abs(it.x-t.x) > 1 || math.Abs(it.z-t.z) > 1 || math.Abs(it.y-t.y) > 1.5 {
				continue
			}
			changed, leftover := t.inv.addStack(it.stack())
			picked := it.count - leftover
			if picked == 0 {
				continue // inventory full — leave it on the ground
			}
			for _, slot := range changed {
				h.sendSlot(t, slot)
			}
			h.incStat(t, attachproto.StatPickedUp, it.item, int32(picked))
			// ServerPlayer.onItemPickup: the PICKER's trigger, naming whoever
			// threw it (a player, or an allay delivering).
			if name := h.throwerName(players, it.thrower); name != "" {
				h.advance(players, t, "thrown_item_picked_up_by_player", advMatch{item: it.item, entity: name})
			}
			h.toTracking(players, eid, it.dim, it.x, it.z, attachproto.Collect{Collected: eid, Collector: t.p.eid, Count: int32(picked)})
			h.playSoundDim(players, it.dim, "minecraft:entity.item.pickup", sndPlayer, it.x, it.y, it.z, 0.4, 1+h.rng.Float32())
			if leftover == 0 {
				delete(h.items, eid)
				h.entityGone(players, it.dim, eid)
			} else {
				it.count = leftover
				h.toNearbyEv(players, it.dim, it.x, it.z, metaEv(itemMetadata(eid, it.stack())))
			}
		}
	}
}

// throwerName is the advancement entity type of an item's thrower ("" when
// nobody threw it, or the thrower is gone).
func (h *hub) throwerName(players map[int32]*tracked, eid int32) string {
	if eid == 0 {
		return ""
	}
	if players[eid] != nil {
		return "player"
	}
	if m := h.mobs[eid]; m != nil {
		return advEntityName[m.etype]
	}
	return ""
}

// sendSlot updates one inventory slot on the client.
func (h *hub) sendSlot(t *tracked, logical int) {
	t.inv.stateId++
	s := t.inv.slots[logical]
	t.p.trySendEv(attachproto.WindowSlot{ID: 0, StateID: t.inv.stateId,
		Slot: int32(windowSlot(logical)), Item: stackEv(s)})
	if logical < 9 { // mirror the hotbar so the connection knows the held item
		t.p.setHotbarSlot(logical, s.item)
	}
}

// sendHandSlot resyncs a hand's stack: a hotbar slot, or the offhand
// (offhandSlot is not a main-inventory index).
func (h *hub) sendHandSlot(t *tracked, slot int) {
	if slot == offhandSlot {
		h.sendOffhand(t)
		return
	}
	h.sendSlot(t, slot)
}

// syncHotbar mirrors all hotbar slots into the player (so survival placement can
// read the held item connection-side).
func (h *hub) syncHotbar(t *tracked) {
	if t.inv == nil {
		return
	}
	for i := 0; i < 9; i++ {
		t.p.setHotbarSlot(i, t.inv.slots[i].item)
	}
}

// sendInventory refreshes the player's whole inventory window (used on join and
// respawn to sync — e.g. clear it after death).
func (h *hub) sendInventory(t *tracked) {
	if t.inv == nil {
		return
	}
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, playerInvSlots)
	for w := 0; w < playerInvSlots; w++ {
		switch {
		case w == 0: // 2x2 crafting result (server-computed)
			item, count := matchRecipe(t.craft[:4], 2)
			slots = append(slots, stackNumEv(item, count))
		case w >= 1 && w <= 4: // 2x2 crafting grid
			slots = append(slots, stackEv(t.craft[w-1]))
		case w >= 5 && w <= 8: // armor
			slots = append(slots, stackEv(t.armor[w-5]))
		case w >= 9 && w <= 35: // main
			slots = append(slots, stackEv(t.inv.slots[w]))
		case w >= 36 && w <= 44: // hotbar
			slots = append(slots, stackEv(t.inv.slots[w-36]))
		default: // offhand (45)
			slots = append(slots, stackEv(t.offhand))
		}
	}
	t.p.trySendEv(attachproto.WindowItems{ID: 0, StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
	h.syncHotbar(t) // keep the connection's held-item view current
}
