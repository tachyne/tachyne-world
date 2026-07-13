package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Smithing table, on the vanilla model: template + base + addition. The
// netherite-upgrade template transforms diamond gear (components carried:
// damage, enchantments, name, trim); a trim template applies an armor trim —
// replacing any different one, refusing an identical re-trim. All three
// inputs shrink by one on take, with the vanilla level event.

const (
	menuSmithing            = 21   // vanilla menu registration order
	worldEventSmithingTable = 1044 // level event: smithing table used
)

var smithingTableState = worldgen.BlockBase("smithing_table") // single state

type evOpenSmith struct {
	eid     int32
	x, y, z int
}

func (evOpenSmith) isHubEvent() {}

func (h *hub) openSmithing(t *tracked, x, y, z int) {
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
	t.winID, t.winKind, t.winPos = h.nextWin, winSmith, blockPos{x, y, z}

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuSmithing), Title: "Smithing Table"})
	h.sendSmithWindow(t)
}

// smithResult computes the current result: transform first, then trim.
func (h *hub) smithResult(t *tracked) invStack {
	tmpl, base, add := t.extraSlot, t.anvil[0], t.anvil[1]
	if tmpl.item == 0 || tmpl.count <= 0 || base.item == 0 || base.count <= 0 ||
		add.item == 0 || add.count <= 0 {
		return invStack{}
	}
	if tmpl.item == protocol.SmithingUpgradeTemplate {
		out, ok := protocol.SmithingTransform[base.item]
		if !ok || add.item != int32(itemByName["netherite_ingot"]) {
			return invStack{}
		}
		res := base // components carried: dmg, ench, name, trim (vanilla)
		res.item = out
		res.count = 1
		return res
	}
	pat, isTrim := protocol.SmithingTrimTemplate[tmpl.item]
	mat, isMat := protocol.SmithingTrimMaterial[add.item]
	if !isTrim || !isMat || !smithTrimmable[base.item] {
		return invStack{}
	}
	res := base
	res.count = 1
	res.trimMat, res.trimPat = int8(mat+1), int8(pat+1)
	if res.trimMat == base.trimMat && res.trimPat == base.trimPat {
		return invStack{} // vanilla: an identical re-trim produces nothing
	}
	return res
}

var smithTrimmable = func() map[int32]bool {
	m := map[int32]bool{}
	for _, id := range protocol.SmithingTrimmable {
		m[id] = true
	}
	return m
}()

// sendSmithWindow pushes the smithing window: template, base, addition,
// result, inventory — plus the recipe-error property (prop 0).
func (h *hub) sendSmithWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 40) // 0 template, 1 base, 2 addition, 3 result, 4-30 main, 31-39 hotbar
	res := h.smithResult(t)
	slots = append(slots, stackEv(t.extraSlot), stackEv(t.anvil[0]), stackEv(t.anvil[1]), stackEv(res))
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
	errFlag := int32(0) // vanilla: all inputs present but no recipe → the red hint
	if res.item == 0 && t.extraSlot.item != 0 && t.anvil[0].item != 0 && t.anvil[1].item != 0 {
		errFlag = 1
	}
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: errFlag})
}

// takeSmithResult shrinks all three inputs and hands over the result.
func (h *hub) takeSmithResult(players map[int32]*tracked, t *tracked) {
	res := h.smithResult(t)
	if res.item == 0 || t.cursor.item != 0 {
		h.sendSmithWindow(t)
		return
	}
	for _, s := range []*invStack{&t.extraSlot, &t.anvil[0], &t.anvil[1]} {
		if s.count--; s.count <= 0 {
			*s = invStack{}
		}
	}
	t.cursor = res
	h.toNearbyEv(players, t.dim, float64(t.winPos.x), float64(t.winPos.z), attachproto.WorldFX{
		Event: worldEventSmithingTable, X: t.winPos.x, Y: t.winPos.y, Z: t.winPos.z})
	h.sendCursor(t)
	h.sendSmithWindow(t)
}
