package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The enchanting table: place an item + lapis, pick one of three rolled
// options, pay levels. Server-authoritative throughout — the client only ever
// sees the options we rolled and the enchant_item button it pressed; costs,
// lapis, level checks and the applied enchantment all live here. Bookshelves
// around the table raise the available costs exactly like vanilla.
//
// Enchantment ids are OUR declared registry order (registries_gen.go), which
// is identical for every client version because the server declares the same
// list — see configuration.go.

const (
	menuEnchantment = 13 // minecraft:enchantment menu network id (crafting 12 / furnace 14)

	// Enchantment network ids (our declared order — see registries_gen.go).
	enchEfficiency = 8
	enchProtection = 27
	enchSharpness  = 32
	enchUnbreaking = 39

	maxBookshelves = 15 // vanilla: power caps at 15 shelves
)

var (
	enchTableState = worldgen.BlockBase("enchanting_table") // enchanting_table block state (single state)
	bookshelfState = worldgen.BlockBase("bookshelf")        // bookshelf block state
)

var (
	itemLapisLazuli = itemByName["lapis_lazuli"]
)

// enchOption is one of the three rolled offers.
type enchOption struct {
	cost int // level requirement (and the property the client displays)
	id   int8
	lvl  int8
}

type evOpenEnchant struct {
	eid     int32
	x, y, z int
}
type evEnchant struct {
	eid    int32
	button int32 // 0-2, from serverbound enchant_item
}

func (evOpenEnchant) isHubEvent() {}
func (evEnchant) isHubEvent()     {}

// openEnchantTable opens the enchanting window for a player.
func (h *hub) openEnchantTable(t *tracked, x, y, z int) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind, t.winPos = h.nextWin, winEnchant, blockPos{x, y, z}

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuEnchantment), Title: "Enchant"})
	h.sendEnchantWindow(t)
	h.rollEnchOptions(t)
}

// sendEnchantWindow pushes the full enchantment window contents (2 table
// slots + the player inventory).
func (h *hub) sendEnchantWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 38) // 0 item, 1 lapis, 2-28 main, 29-37 hotbar
	slots = append(slots, stackEv(t.enchSlots[0]), stackEv(t.enchSlots[1]))
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// enchCategory rolls which enchantment an offer carries for an item: swords
// favor sharpness with a looting chance, tools favor efficiency with fortune
// and the rare silk touch, armor gets protection, and plain books can roll
// anything (they become enchanted books). Zero = not table-enchantable.
func (h *hub) enchCategory(item int32) int8 {
	r := h.rng.Intn(100)
	if _, isSword := meleeDamage[item]; isSword {
		if swordPeriod[item] { // swords proper
			if r < 75 {
				return enchSharpness
			}
			return enchLooting
		}
		// axes/picks/shovels reached via meleeDamage too
		switch {
		case r < 60:
			return enchEfficiency
		case r < 85:
			return enchFortune
		}
		return enchSilkTouch
	}
	if _, ok := armorInfo[item]; ok {
		return enchProtection
	}
	if item == itemBook { // books take anything — the anvil applies it later
		pool := []int8{enchSharpness, enchEfficiency, enchProtection, enchUnbreaking, enchFortune, enchLooting, enchSilkTouch}
		return pool[h.rng.Intn(len(pool))]
	}
	if _, ok := itemMaxDurability[item]; ok {
		return enchEfficiency // other durable tools (hoes, shears, …)
	}
	return 0
}

// enchMaxLvl is the vanilla cap per enchantment.
func enchMaxLvl(id int8) int8 {
	switch id {
	case enchProtection:
		return 4
	case enchUnbreaking, enchFortune, enchLooting:
		return 3
	case enchSilkTouch:
		return 1
	}
	return 5 // sharpness, efficiency
}

// countBookshelves scans the vanilla 5×5 ring (two high) around the table.
func (h *hub) countBookshelves(pos blockPos) int {
	n := 0
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx > -2 && dx < 2 && dz > -2 && dz < 2 {
				continue // only the outer ring counts
			}
			for dy := 0; dy <= 1; dy++ {
				if h.world.At(pos.x+dx, pos.y+dy, pos.z+dz) == bookshelfState {
					n++
				}
			}
		}
	}
	return min(n, maxBookshelves)
}

// rollEnchOptions rolls the three offers for the current table item and sends
// the window properties (costs, seed, and the hover hints).
func (h *hub) rollEnchOptions(t *tracked) {
	t.enchOpts = [3]enchOption{}
	item := t.enchSlots[0]
	if cat := h.enchCategory(item.item); cat != 0 && item.count > 0 && !item.enchanted() {
		b := h.countBookshelves(t.winPos)
		base := h.rng.Intn(8) + 1 + b/2 + h.rng.Intn(b+1)
		costs := [3]int{max(base/3, 1), base*2/3 + 1, max(base, b*2)}
		for i, cost := range costs {
			lvl := int8(1 + cost*int(enchMaxLvl(cat)-1)/30) // scale toward the cap at cost 30
			t.enchOpts[i] = enchOption{cost: cost, id: cat, lvl: lvl}
		}
		// The top-tier roll on a strong table also lands unbreaking (a second
		// enchantment), mirroring vanilla's multi-enchant rolls.
		if costs[2] >= 15 && cat != enchUnbreaking {
			t.enchOpts[2].lvl = enchMaxLvl(cat)
		}
	}
	prop := func(p, v int) {
		t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: int32(p), Value: int32(v)})
	}
	for i, o := range t.enchOpts {
		prop(i, o.cost) // 0-2: level requirements (0 = row disabled)
		hintID, hintLvl := -1, -1
		if o.cost > 0 {
			hintID, hintLvl = int(o.id), int(o.lvl)
		}
		prop(4+i, hintID)  // 4-6: enchantment hover hint
		prop(7+i, hintLvl) // 7-9: hint level
	}
	prop(3, h.rng.Intn(1<<15)) // seed: drives the galactic glyph animation
}

// handleEnchant applies a clicked option: validate, pay, enchant.
func (h *hub) handleEnchant(players map[int32]*tracked, t *tracked, button int32) {
	if t.winKind != winEnchant || button < 0 || button > 2 {
		return
	}
	opt := t.enchOpts[button]
	item := &t.enchSlots[0]
	if opt.cost == 0 || item.count == 0 || item.enchanted() {
		return
	}
	lapis := &t.enchSlots[1]
	need := int(button) + 1
	if t.gamemode != gmCreative { // creative enchants free (vanilla)
		if t.xpLevel < opt.cost || lapis.item != itemLapisLazuli || lapis.count < need {
			return // AUTHORITY: the client can press any button; we hold the books
		}
		if lapis.count -= need; lapis.count == 0 {
			*lapis = invStack{}
		}
		t.xpLevel -= need // vanilla: pay 1-3 LEVELS (the cost is the gate)
		h.sendExperience(t)
	}
	item.ench[0] = enchApply{id: opt.id, lvl: opt.lvl}
	if button == 2 && opt.cost >= 15 && opt.id != enchUnbreaking {
		item.ench[1] = enchApply{id: enchUnbreaking, lvl: 2}
	}
	if item.item == itemBook { // a book takes the enchant as a STORED one
		item.item = itemEnchantedBook
	}
	h.sendEnchantWindow(t)
	h.rollEnchOptions(t) // now enchanted → all rows switch off
	h.playSound(players, "minecraft:block.enchantment_table.use", sndBlock, t.x, t.y, t.z, 1, 1)
	h.advance(players, t, "enchanted_item", advMatch{})
	h.incCustom(t, "enchant_item", 1)
	h.bus.publish("enchant", map[string]any{"name": t.p.name, "item": item.item, "ench": int(opt.id), "lvl": int(opt.lvl)})
}

// reclaimEnchant folds the table slots back into the inventory when the
// window closes (dropping what no longer fits), like the crafting grid.
func (h *hub) reclaimEnchant(players map[int32]*tracked, t *tracked) {
	for i := range t.enchSlots {
		st := t.enchSlots[i]
		t.enchSlots[i] = invStack{}
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
				it.dmg = st.dmg
				it.ench = st.ench
			}
		}
	}
	t.enchOpts = [3]enchOption{}
}
