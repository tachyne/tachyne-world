package server

import (
	"strings"
	"unicode"
	"unicode/utf16"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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

)

var (
	anvilWearChance    = 0.12                        // AnvilMenu.onTake: random.nextFloat() < 0.12F
	anvilStateMin      = worldgen.BlockBase("anvil") // anvil/chipped/damaged × facing
	anvilStateMax      = worldgen.BlockBase("damaged_anvil") + 3
	grindstoneStateMin = worldgen.BlockBase("grindstone")
	grindstoneStateMax = worldgen.BlockBase("grindstone") + 11
)

var (
	itemBook          = itemByName["book"]
	itemEnchantedBook = itemByName["enchanted_book"]
)

type evOpenAnvil struct {
	eid     int32
	x, y, z int // the anvil block, for the wear roll
}
type evOpenGrind struct{ eid, x, y, z int32 }
type evRename struct {
	eid  int32
	name string
}

func (evOpenAnvil) isHubEvent() {}
func (evOpenGrind) isHubEvent() {}
func (evRename) isHubEvent()    {}

// openAnvil / openGrindstone open the two-input + result windows.
func (h *hub) openAnvil(t *tracked, pos blockPos) {
	h.openTwoSlot(t, winAnvil, menuAnvil, "Repair & Name")
	if t.winKind == winAnvil {
		t.winPos = simPos{dim: t.dim, blockPos: pos}
	}
}
func (h *hub) openGrindstone(t *tracked, pos blockPos) {
	h.openTwoSlot(t, winGrind, menuGrindstone, "Repair & Disenchant")
	if t.winKind == winGrind {
		t.winPos = simPos{dim: t.dim, blockPos: pos} // the menu's access: where the orbs and the sound come from
	}
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
	case winCarto:
		return h.cartoResult(t.anvil[0], t.anvil[1]), 0
	}
	return invStack{}, 0
}

// anvilMaxCost is vanilla's "Too Expensive!" threshold: a survival player cannot
// take a result whose level cost is >= 40 (AnvilMenu).
const anvilMaxCost = 40

// anvilResult applies the vanilla-shaped rules: rename, merge enchantments
// from an identical item or an enchanted book (equal levels bump one, capped),
// and repair from an identical sacrifice (combined remaining + 12% of max).
func anvilResult(a, b invStack, rename string) (invStack, int) {
	if a.item == 0 || a.count == 0 {
		return invStack{}, 0
	}
	res, cost, renameCost := a, 0, 0
	switch {
	case !nameBlank(rename):
		if rename != a.name {
			res.name = rename
			cost, renameCost = cost+1, 1
		}
	case a.name != "":
		// A cleared box takes a custom name off, for a level like a rename.
		res.name = ""
		cost, renameCost = cost+1, 1
	}
	material := false
	if b.item != 0 && b.count > 0 {
		// The item's own repair material comes first (AnvilMenu.createResult):
		// a quarter of the durability back per ingot, gem or plank, one level
		// each. Enchantments ride along untouched.
		mended, _, repairCost, ok := anvilMaterialRepair(res, b)
		material = ok
		if ok {
			res = mended
			cost += repairCost
		}
		sameItem := b.item == a.item
		book := b.item == itemEnchantedBook
		if material {
			sameItem, book = false, false // the sacrifice was spent on durability
		} else if !sameItem && !book {
			return invStack{}, 0 // incompatible sacrifice
		}
		// AnvilMenu.createResult: each sacrifice enchantment must support the
		// target item (a book of Sharpness will not go on boots) and be
		// compatible with what the target already carries (a Smite book will
		// not join Sharpness); an incompatible one costs a level and is
		// dropped. Levels combine upward when equal, capped at the max; the
		// price is the enchantment's anvil cost per level, halved for a book.
		applied, refused := false, false
		for _, e := range b.ench {
			if material {
				break // a diamond carries no enchantments to merge
			}
			if e.lvl <= 0 {
				continue
			}
			cur := int8(res.enchLvl(e.id))
			// An enchanted BOOK takes anything (that is how a book library is
			// built); anything else takes only what its item supports.
			ok := a.item == itemEnchantedBook || enchIsSupported(e.id, a.item)
			for _, x := range res.ench { // …and only what it is compatible with
				if x.lvl > 0 && x.id != e.id && !enchCompatible(e.id, x.id) {
					ok = false
					cost++ // each clash costs a level even though nothing lands
				}
			}
			if !ok {
				refused = true
				continue
			}
			lvl := e.lvl
			if cur == e.lvl {
				lvl = e.lvl + 1
			} else if cur > e.lvl {
				lvl = cur
			}
			if lvl > enchMaxLvl(e.id) {
				lvl = enchMaxLvl(e.id)
			}
			per := enchDefs[e.id].anvilCost
			if book {
				per = max(1, per/2)
			}
			res = withEnch(res, e.id, lvl)
			cost += per * int(lvl)
			applied = true
			if a.count > 1 {
				cost = anvilMaxCost // a stack is never worth enchanting
			}
		}
		if refused && !applied {
			return invStack{}, 0 // nothing on the sacrifice applies to this item
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
	// Vanilla prior-work penalty (AnvilMenu): the accumulated repair cost of both
	// inputs is added to the level cost, and the result's own repair cost grows
	// to 2·max(inputs)+1 — so each successive anvil use costs more.
	prior := a.repairCost
	rc := a.repairCost
	if b.item != 0 && b.count > 0 {
		prior += b.repairCost
		if b.repairCost > rc {
			rc = b.repairCost
		}
	}
	onlyRenaming := renameCost > 0 && renameCost == cost
	cost += prior
	if onlyRenaming {
		// A pure rename is the one anvil job that stays affordable forever:
		// vanilla caps it at 39 and leaves the item's repair cost alone.
		if cost >= anvilMaxCost {
			cost = anvilMaxCost - 1
		}
	} else {
		res.repairCost = rc*2 + 1
	}
	if cost < 1 {
		cost = 1
	}
	return res, cost
}

// withEnch sets an enchantment level on a stack (a free slot; overflow dropped).
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

// grindResult is GrindstoneMenu.computeResult: one item comes out stripped
// of everything but its curses, and two of the same item are merged into
// one whose durability is what both had left plus five per cent of the
// maximum. The second return is the XP the grindstone gives back.
func grindResult(a, b invStack) (invStack, int) {
	if a.count > 1 || b.count > 1 {
		return invStack{}, 0 // a stack is never ground
	}
	refund := grindXP(a) + grindXP(b)
	switch {
	case a.item != 0 && b.item != 0:
		res, ok := grindMerge(a, b)
		if !ok {
			return invStack{}, 0
		}
		return res, refund
	case a.item == 0 && b.item == 0:
		return invStack{}, 0
	}
	src := a
	if src.item == 0 {
		src = b
	}
	if !src.enchanted() {
		return invStack{}, 0 // nothing to take off
	}
	return grindStripped(src), refund
}

// grindStripped is removeNonCursesFrom: the curses stay, which is the
// whole reason a cursed helmet cannot be cleaned up on a grindstone.
func grindStripped(src invStack) invStack {
	res := src
	res.ench = enchList{}
	kept := 0
	for _, e := range src.ench {
		if e.lvl > 0 && enchDefs[e.id].flags&enchCurse != 0 {
			res.ench[kept] = e
			kept++
		}
	}
	if res.item == itemEnchantedBook && kept == 0 {
		res.item = itemBook
	}
	// The prior-work cost is rebuilt from what is left rather than cleared:
	// one calculateIncreasedRepairCost step per surviving curse, so a clean
	// item comes off at 0 and a cursed one keeps a small anvil penalty.
	res.repairCost = 0
	for i := 0; i < kept; i++ {
		res.repairCost = res.repairCost*2 + 1
	}
	return res
}

// grindMerge is GrindstoneMenu.mergeItems: two of the same item become one
// with both their remaining durability and a five-per-cent bonus.
func grindMerge(a, b invStack) (invStack, bool) {
	if a.item != b.item {
		return invStack{}, false
	}
	max, damageable := itemMaxDurability[a.item]
	if !damageable {
		// Two undamageable copies merge only when they are identical down to
		// the components and actually stack — then the pair comes out as one
		// stack of two, stripped.
		if stackCap(a.item) < 2 || a != b {
			return invStack{}, false
		}
		res := grindMergeEnch(a, b)
		res.count = 2
		return res, true
	}
	left := (max - a.dmg) + (max - b.dmg) + max*5/100
	res := grindMergeEnch(a, b)
	res.dmg = max - left
	if res.dmg < 0 {
		res.dmg = 0
	}
	res.count = 1
	return res, true
}

// grindMergeEnch is mergeEnchantsFrom followed by removeNonCursesFrom: the
// second item's curses come across too (a cursed sacrifice infects the
// result), everything else is ground off.
func grindMergeEnch(a, b invStack) invStack {
	res := a
	for _, e := range b.ench {
		if e.lvl <= 0 || enchDefs[e.id].flags&enchCurse == 0 {
			continue
		}
		if cur := int8(res.enchLvl(e.id)); cur != 0 {
			continue // upgrade skips a curse the target already carries
		}
		res = withEnch(res, e.id, e.lvl)
	}
	return grindStripped(res)
}

// grindXP is getExperienceFromItem: the sum of each non-curse
// enchantment's minimum cost at the level it sits on, which is what the
// grindstone hands back (halved and jittered by the caller's roll).
func grindXP(st invStack) int {
	n := 0
	for _, e := range st.ench {
		if e.lvl <= 0 || enchDefs[e.id].flags&enchCurse != 0 {
			continue
		}
		n += enchDefs[e.id].minCost(int(e.lvl))
	}
	return n
}

// takeTwoSlotResult handles a click on the result slot: enforce the level
// cost (anvil), consume the inputs, hand the result to the cursor, pay/refund.
func (h *hub) takeTwoSlotResult(players map[int32]*tracked, t *tracked, mode int32) {
	res, cost := h.twoSlotResult(t)
	if res.item == 0 || !h.canTakeResult(t, res, mode) {
		h.sendTwoSlotWindow(t)
		return
	}
	if t.winKind == winCarto { // cartography: mint the derived map, consume one of each
		h.takeCartoResult(players, t, res)
		return
	}
	if t.winKind == winAnvil {
		if t.gamemode != gmCreative && cost >= anvilMaxCost {
			h.sendTwoSlotWindow(t) // "Too Expensive!" — vanilla blocks >=40 in survival
			return
		}
		if t.gamemode != gmCreative && t.xpLevel < cost {
			h.sendTwoSlotWindow(t) // AUTHORITY: can't afford — resync, don't apply
			return
		}
		if t.gamemode != gmCreative {
			t.xpLevel -= cost
			h.sendExperience(t)
		}
		// AnvilMenu.onTake: a material repair eats only repairItemCountCost of
		// the stack (three diamonds out of a stack of sixty-four), everything
		// else eats the whole sacrifice.
		_, used, _, material := anvilMaterialRepair(t.anvil[0], t.anvil[1])
		consumed := t.anvil[1].item == itemEnchantedBook || t.anvil[1].item == t.anvil[0].item
		t.anvil[0] = invStack{}
		switch {
		case material:
			if t.anvil[1].count -= used; t.anvil[1].count <= 0 {
				t.anvil[1] = invStack{}
			}
		case consumed:
			t.anvil[1] = invStack{}
		}
		h.wearAnvil(players, t) // AnvilMenu.onTake: one use in eight chips it; the sound rides the event
	} else { // grindstone: both inputs go in, and the XP comes back
		both := t.anvil[0].item != 0 && t.anvil[1].item != 0
		t.anvil[0], t.anvil[1] = invStack{}, invStack{}
		_ = both
		// getExperienceAmount: half the enchantments' worth, plus a roll of
		// the same again — so the same gear never pays out quite the same.
		// GrindstoneMenu's result onTake: the orbs at the grindstone's centre
		// (Vec3.atCenterOf) and level event 1042, its use sound, there.
		pos := t.winPos.blockPos
		if cost > 0 {
			half := (cost + 1) / 2 // ceil(n/2)
			h.spawnXPOrbIn(players, t.dim, half+h.rng.Intn(half), float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5)
		}
		h.levelEvent(players, t.dim, worldEventGrindstoneUse, pos.x, pos.y, pos.z, 0)
	}
	h.resultTake(t, res, mode) // onto the cursor, or into the inventory on a shift-click
	h.sendCursor(t)
	h.sendTwoSlotWindow(t)
}

// reclaimAnvil folds the input slots (and the loom/smithing third slot)
// back on close/leave.
func (h *hub) reclaimAnvil(players map[int32]*tracked, t *tracked) {
	slots := []*invStack{&t.anvil[0], &t.anvil[1], &t.extraSlot}
	for i := range slots {
		st := *slots[i]
		*slots[i] = invStack{}
		if st.item == 0 || st.count == 0 {
			continue
		}
		changed, leftover := t.inv.addStack(st)
		for _, slot := range changed {
			h.sendSlot(t, slot)
		}
		if leftover > 0 && players != nil {
			st.count = leftover
			if it := h.spawnItemIn(players, t.dim, st.item, st.count, t.x, t.y, t.z); it != nil {
				it.setFrom(st)
				h.refreshItemMeta(players, it) // the spawn broadcast went out bare; show the real stack
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

// wearAnvil is AnvilMenu.onTake's damage roll: outside creative, one use in
// eight (0.12) chips the anvil a stage — anvil, chipped, damaged, gone — and
// the sound is the level event the client plays (SOUND_ANVIL_USED, or
// SOUND_ANVIL_BROKEN when it goes).
func (h *hub) wearAnvil(players map[int32]*tracked, t *tracked) {
	pos := t.winPos.blockPos
	w := h.worldFor(t.dim)
	st := w.At(pos.x, pos.y, pos.z)
	if st < anvilStateMin || st > anvilStateMax {
		h.levelEvent(players, t.dim, worldEventAnvilUsed, floorInt(t.x), floorInt(t.y), floorInt(t.z), 0)
		return
	}
	if t.gamemode != gmCreative && h.rng.Float64() < anvilWearChance {
		if stage := (st - anvilStateMin) / 4; stage >= 2 {
			h.setBlockAt(players, t.dim, pos, worldgen.Air)
			h.levelEvent(players, t.dim, worldEventAnvilBroken, pos.x, pos.y, pos.z, 0)
			h.closeWindowServer(players, t) // AnvilMenu.stillValid fails: the menu closes
			return
		}
		h.setBlockAt(players, t.dim, pos, st+4)
	}
	h.levelEvent(players, t.dim, worldEventAnvilUsed, pos.x, pos.y, pos.z, 0)
}

// anvilName is AnvilMenu.validateName: the name box's text with the
// characters chat refuses dropped (the section sign, control characters,
// DEL), or ok false when what is left runs past 50 — a name that long is
// refused, not cut, and the box keeps its last name.
func anvilName(s string) (string, bool) {
	var b strings.Builder
	for _, r := range s {
		if r != 0xa7 && r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	out := b.String()
	return out, len(utf16.Encode([]rune(out))) <= anvilMaxName
}

// nameBlank is StringUtil.isBlank: empty, or nothing but whitespace.
func nameBlank(s string) bool {
	return strings.TrimFunc(s, unicode.IsSpace) == ""
}
