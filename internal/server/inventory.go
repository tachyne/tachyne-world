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
	item   int32
	count  int
	dmg    int          // durability damage taken (tools/armor); 0 for everything else
	ench   [2]enchApply // up to two enchantments (zero slots = none); comparable
	name   string       // anvil rename ("" = none) — in-session only (not persisted yet)
	potion int8         // brewed potion type (potWater..): drives drink effects + label
}

// enchanted reports whether the stack carries any enchantment.
func (st invStack) enchanted() bool { return st.ench != [2]enchApply{} }

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
func packEnch(e [2]enchApply) int32 {
	return int32(uint8(e[0].id))<<24 | int32(uint8(e[0].lvl))<<16 |
		int32(uint8(e[1].id))<<8 | int32(uint8(e[1].lvl))
}

func unpackEnch(v int32) [2]enchApply {
	return [2]enchApply{
		{id: int8(v >> 24), lvl: int8(v >> 16)},
		{id: int8(v >> 8), lvl: int8(v)},
	}
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

// add inserts up to count of item, filling existing stacks then empty slots.
// Returns the changed slot indices and any leftover that didn't fit.
func (inv *inventory) add(item int32, count int) (changed []int, leftover int) {
	cap := stackCap(item)
	for i := range inv.slots {
		if count == 0 {
			break
		}
		if s := &inv.slots[i]; s.item == item && s.count < cap {
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
			s.item, s.count = item, n
			count -= n
			changed = append(changed, i)
		}
	}
	return changed, count
}

// addStack inserts a stack preserving its durability damage and enchantments:
// a damaged/enchanted item must not merge into a pristine stack (and tools
// don't stack anyway), so it takes its own empty slot; plain stacks go
// through the normal merge path.
func (inv *inventory) addStack(st invStack) (changed []int, leftover int) {
	if st.dmg == 0 && !st.enchanted() && st.name == "" {
		return inv.add(st.item, st.count)
	}
	for i := range inv.slots {
		if s := &inv.slots[i]; s.count == 0 {
			*s = st
			return []int{i}, 0
		}
	}
	return nil, st.count
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
			changed, leftover := t.inv.addStack(invStack{item: it.item, count: it.count, dmg: it.dmg, ench: it.ench})
			picked := it.count - leftover
			if picked == 0 {
				continue // inventory full — leave it on the ground
			}
			for _, slot := range changed {
				h.sendSlot(t, slot)
			}
			h.incStat(t, attachproto.StatPickedUp, it.item, int32(picked))
			h.toNearbyEv(players, it.dim, it.x, it.z, attachproto.Collect{Collected: eid, Collector: t.p.eid, Count: int32(picked)})
			h.playSound(players, "minecraft:entity.item.pickup", sndPlayer, it.x, it.y, it.z, 0.4, 1+h.rng.Float32())
			if leftover == 0 {
				delete(h.items, eid)
				h.toNearbyEv(players, it.dim, it.x, it.z, entGone(eid))
			} else {
				it.count = leftover
				h.toNearbyEv(players, it.dim, it.x, it.z, metaEv(itemMetadata(eid, it.item, it.count)))
			}
		}
	}
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
