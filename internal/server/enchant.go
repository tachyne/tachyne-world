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
	enchAquaAffinity         = 0 // head: mining underwater is not slowed
	enchBaneOfArthropods     = 1 // melee: +2.5/level vs spiders and their kin
	enchBindingCurse         = 2 // worn armour cannot be taken off
	enchBlastProtection      = 3 // armour: +2 protection points/level vs explosions
	enchDepthStrider         = 7 // boots: walk faster in water
	enchEfficiency           = 8
	enchFeatherFalling       = 9  // boots: +3 protection points/level vs falling
	enchFireAspect           = 10 // melee: sets the target alight for 4 s/level
	enchFireProtection       = 11 // armour: +2 protection points/level vs burning
	enchFrostWalker          = 14 // boots: freezes the water you walk over
	enchKnockback            = 17 // melee: +0.5 block/level extra knockback
	enchMultishot            = 23 // crossbow: fire three bolts in a spread
	enchPiercing             = 24 // crossbow: bolts pass through up to level+1 entities
	enchProjectileProtection = 26 // armour: +2 protection points/level vs arrows
	enchProtection           = 27
	enchQuickCharge          = 29 // crossbow: -0.25s charge time per level
	enchRespiration          = 30 // helmet: breath lasts longer underwater
	enchSharpness            = 32
	enchSmite                = 34 // melee: +2.5/level vs the undead
	enchSoulSpeed            = 35 // boots: run over soul sand and soul soil
	enchSweepingEdge         = 36 // sword sweep: +ratio·baseDamage to the sweep hit
	enchSwiftSneak           = 37 // leggings: crouch faster
	enchThorns               = 38 // armour: hurts whoever hits you
	enchUnbreaking           = 39
	enchVanishingCurse       = 40 // the item is destroyed rather than dropped on death

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
	t.winID, t.winKind, t.winPos = h.nextWin, winEnchant, simPos{dim: t.dim, blockPos: blockPos{x, y, z}}

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

// countBookshelves scans the vanilla 5×5 ring (two high) around the table.
func (h *hub) countBookshelves(pos simPos) int {
	w := h.worldFor(pos.dim)
	if w == nil {
		return 0
	}
	n := 0
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx > -2 && dx < 2 && dz > -2 && dz < 2 {
				continue // only the outer ring counts
			}
			for dy := 0; dy <= 1; dy++ {
				if w.At(pos.x+dx, pos.y+dy, pos.z+dz) == bookshelfState {
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
	t.enchLists = [3][]enchInstance{}
	item := t.enchSlots[0]
	// EnchantmentMenu.slotsChanged: an unenchanted item with an Enchantable
	// value gets three rows; each row's level requirement comes from
	// getEnchantmentCost (a row cheaper than its own index goes dark), and its
	// clue is one random member of the selection that row would apply.
	if enchantabilityOf(item.item) > 0 && item.count > 0 && !item.enchanted() {
		b := h.countBookshelves(t.winPos)
		for i := range t.enchOpts {
			cost := enchTableCost(h.rng, i, b, item.item)
			if cost < i+1 {
				continue
			}
			list := enchSelect(h.rng, item.item, cost, enchTableAllowed)
			if len(list) == 0 {
				continue
			}
			clue := list[h.rng.Intn(len(list))]
			t.enchOpts[i] = enchOption{cost: cost, id: clue.id, lvl: clue.lvl}
			t.enchLists[i] = list
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
	item.ench = enchApplyList(t.enchLists[button]) // every enchantment of the row's selection
	if item.item == itemBook {                     // a book takes the enchant as a STORED one
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
