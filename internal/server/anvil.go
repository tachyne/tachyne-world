package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"tachyne/internal/worldgen"
)

// Anvil + grindstone. The anvil combines: sacrifice an identical item or an
// enchanted book onto the target to merge enchantments (equal levels bump one,
// capped), repair durability (combined remaining +12%), and rename — all for
// a level cost the server enforces. The grindstone strips enchantments and
// refunds a slug of XP. Both are server-owned result slots like crafting.

const (
	menuAnvil      = 8 // vanilla menu registration order (crafting 12 anchor)
	menuGrindstone = 15

	playServerNameItem = 0x2e // serverbound name_item: the anvil's rename box

	enchFortune   = 13 // our declared enchantment-registry order
	enchLooting   = 18
	enchSilkTouch = 33

	anvilMaxName = 50 // vanilla rename length cap

	grindXPPerLevel = 6 // refund per stripped enchantment level (vanilla-ish mid-roll)
)

var (
	anvilStateMin      = worldgen.BlockBase("anvil") // anvil/chipped/damaged × facing
	anvilStateMax      = worldgen.BlockBase("damaged_anvil") + 3
	grindstoneStateMin = worldgen.BlockBase("grindstone")
	grindstoneStateMax = worldgen.BlockBase("grindstone") + 11
)

var (
	itemBook          = itemByName["book"]
	itemEnchantedBook = itemByName["enchanted_book"]
)

type evOpenAnvil struct{ eid int32 }
type evOpenGrind struct{ eid int32 }
type evRename struct {
	eid  int32
	name string
}

func (evOpenAnvil) isHubEvent() {}
func (evOpenGrind) isHubEvent() {}
func (evRename) isHubEvent()    {}

// openAnvil / openGrindstone open the two-input + result windows.
func (h *hub) openAnvil(t *tracked) { h.openTwoSlot(t, winAnvil, menuAnvil, "Repair & Name") }
func (h *hub) openGrindstone(t *tracked) {
	h.openTwoSlot(t, winGrind, menuGrindstone, "Repair & Disenchant")
}

func (h *hub) openTwoSlot(t *tracked, kind winKind, menu int32, title string) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.reclaimEnchant(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind = h.nextWin, kind
	t.renameTo = ""

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menu), Title: title})
	h.sendTwoSlotWindow(t)
}

// sendTwoSlotWindow pushes the full anvil/grindstone contents (3 container
// slots + player inventory) and the current result.
func (h *hub) sendTwoSlotWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 39) // 0,1 inputs, 2 result, 3-29 main, 30-38 hotbar
	slots = append(slots, stackEv(t.anvil[0]), stackEv(t.anvil[1]))
	res, _ := h.twoSlotResult(t)
	slots = append(slots, stackEv(res))
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
	if t.winKind == winAnvil {
		_, cost := h.twoSlotResult(t)
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(cost)}) // property 0: repair cost (levels)
	}
}

// twoSlotResult computes the current window's result + level cost.
func (h *hub) twoSlotResult(t *tracked) (invStack, int) {
	switch t.winKind {
	case winAnvil:
		return anvilResult(t.anvil[0], t.anvil[1], t.renameTo)
	case winGrind:
		return grindResult(t.anvil[0], t.anvil[1])
	}
	return invStack{}, 0
}

// anvilResult applies the vanilla-shaped rules: rename, merge enchantments
// from an identical item or an enchanted book (equal levels bump one, capped),
// and repair from an identical sacrifice (combined remaining + 12% of max).
func anvilResult(a, b invStack, rename string) (invStack, int) {
	if a.item == 0 || a.count == 0 {
		return invStack{}, 0
	}
	res, cost := a, 0
	if rename != "" && rename != a.name {
		res.name = rename
		cost++
	}
	if b.item != 0 && b.count > 0 {
		sameItem := b.item == a.item
		book := b.item == itemEnchantedBook
		if !sameItem && !book {
			return invStack{}, 0 // incompatible sacrifice
		}
		for _, e := range b.ench {
			if e.lvl <= 0 {
				continue
			}
			lvl := e.lvl
			if cur := res.enchLvl(e.id); cur > 0 {
				if int8(cur) == e.lvl && lvl < enchMaxLvl(e.id) {
					lvl = e.lvl + 1 // equal levels combine upward (vanilla)
				} else if int8(cur) > e.lvl {
					lvl = int8(cur)
				}
			}
			res = withEnch(res, e.id, lvl)
			cost += int(lvl)
		}
		if sameItem && a.dmg > 0 {
			if max, ok := itemMaxDurability[a.item]; ok {
				remaining := (max - a.dmg) + (max - b.dmg) + max*12/100
				res.dmg = max - remaining
				if res.dmg < 0 {
					res.dmg = 0
				}
				cost += 2
			}
		}
	}
	if res == a {
		return invStack{}, 0 // nothing would change
	}
	if cost < 1 {
		cost = 1
	}
	return res, cost
}

// withEnch sets an enchantment level on a stack (two slots; overflow dropped).
func withEnch(st invStack, id, lvl int8) invStack {
	for i := range st.ench {
		if st.ench[i].id == id && st.ench[i].lvl > 0 {
			st.ench[i].lvl = lvl
			return st
		}
	}
	for i := range st.ench {
		if st.ench[i].lvl == 0 {
			st.ench[i] = enchApply{id: id, lvl: lvl}
			return st
		}
	}
	return st
}

// grindResult strips enchantments from whichever input holds an item; the
// second return is the XP refund banked for the take.
func grindResult(a, b invStack) (invStack, int) {
	src := a
	if src.item == 0 {
		src = b
	}
	if src.item == 0 || !src.enchanted() {
		return invStack{}, 0
	}
	refund := 0
	for _, e := range src.ench {
		refund += int(e.lvl) * grindXPPerLevel
	}
	res := src
	res.ench = [2]enchApply{}
	if res.item == itemEnchantedBook {
		res.item = itemBook
	}
	return res, refund
}

// takeTwoSlotResult handles a click on the result slot: enforce the level
// cost (anvil), consume the inputs, hand the result to the cursor, pay/refund.
func (h *hub) takeTwoSlotResult(players map[int32]*tracked, t *tracked) {
	res, cost := h.twoSlotResult(t)
	if res.item == 0 || t.cursor.item != 0 {
		h.sendTwoSlotWindow(t)
		return
	}
	if t.winKind == winAnvil {
		if t.gamemode != gmCreative && t.xpLevel < cost {
			h.sendTwoSlotWindow(t) // AUTHORITY: can't afford — resync, don't apply
			return
		}
		if t.gamemode != gmCreative {
			t.xpLevel -= cost
			h.sendExperience(t)
		}
		consumed := t.anvil[1].item == itemEnchantedBook || t.anvil[1].item == t.anvil[0].item
		t.anvil[0] = invStack{}
		if consumed {
			t.anvil[1] = invStack{}
		}
		h.playSound(players, "minecraft:block.anvil.use", sndBlock, t.x, t.y, t.z, 1, 1)
	} else { // grindstone: refund XP for the stripped enchantments
		if t.anvil[0].item != 0 {
			t.anvil[0] = invStack{}
		} else {
			t.anvil[1] = invStack{}
		}
		h.spawnXPOrb(players, cost, t.x, t.y, t.z)
		h.playSound(players, "minecraft:block.grindstone.use", sndBlock, t.x, t.y, t.z, 1, 1)
	}
	t.cursor = res
	h.sendCursor(t)
	h.sendTwoSlotWindow(t)
}

// reclaimAnvil folds the input slots back on close/leave.
func (h *hub) reclaimAnvil(players map[int32]*tracked, t *tracked) {
	for i := range t.anvil {
		st := t.anvil[i]
		t.anvil[i] = invStack{}
		if st.item == 0 || st.count == 0 {
			continue
		}
		changed, leftover := t.inv.addStack(st)
		for _, slot := range changed {
			h.sendSlot(t, slot)
		}
		if leftover > 0 && players != nil {
			st.count = leftover
			if it := h.spawnItem(players, st.item, st.count, t.x, t.y, t.z); it != nil {
				it.dmg, it.ench = st.dmg, st.ench
			}
		}
	}
	t.renameTo = ""
}

// silkTouchDrop is what a block yields under Silk Touch (the block itself) —
// the curated set where it differs from the normal drop table.
var silkTouchDrop = map[uint32]int32{
	worldgen.Stone: 1, worldgen.GrassBlock: 27,
	worldgen.CoalOre: 64, worldgen.DeepslateCoalOre: 65,
	worldgen.IronOre: 66, worldgen.DeepslateIronOre: 67,
	worldgen.CopperOre: 68, worldgen.DeepslateCopperOre: 69,
	worldgen.GoldOre: 70, worldgen.DeepslateGoldOre: 71,
	worldgen.DiamondOre: 78, worldgen.DeepslateDiamondOre: 79,
}

// isOreState reports whether a state is one of the generated ore blocks
// (Fortune's multiplier applies to their drops).
func isOreState(s uint32) bool {
	_, ok := silkTouchDrop[s]
	return ok && s != worldgen.Stone && s != worldgen.GrassBlock
}
