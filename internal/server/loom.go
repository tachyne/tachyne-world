package server

import (
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Loom, on the vanilla model: banner + dye (+ optional pattern item) and a
// pattern-button list. The selectable list comes from protocol.LoomPatterns —
// the SAME tag data the clients receive — so button indices agree by
// construction. The result appends one layer (6 max); taking it consumes the
// banner and dye, never the pattern item.

const menuLoom = 18 // vanilla menu registration order

var (
	loomStateMin = worldgen.BlockBase("loom") // facing (4)
	loomStateMax = worldgen.BlockBase("loom") + 3
)

// dyeColorOf maps dye items to the DyeColor enum (white=0 … black=15).
var dyeColorOf = func() map[int32]int8 {
	names := []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}
	m := map[int32]int8{}
	for i, n := range names {
		if id := int32(itemByName[n+"_dye"]); id != 0 {
			m[id] = int8(i)
		}
	}
	return m
}()

// bannerItems is the 16 banner items (any color; the base color is the item's own).
var bannerItems = func() map[int32]bool {
	m := map[int32]bool{}
	for name, id := range itemByName {
		if strings.HasSuffix(name, "_banner") && !strings.Contains(name, "pattern") {
			m[int32(id)] = true
		}
	}
	return m
}()

// loomPatternItems maps a *_banner_pattern ITEM id to its pattern ids, from
// the shared tag data (tag suffix = item name minus "_banner_pattern").
var loomPatternItems = func() map[int32][]int32 {
	_, byTag := protocol.LoomPatterns()
	m := map[int32][]int32{}
	for suffix, ids := range byTag {
		if id, ok := itemByName[suffix+"_banner_pattern"]; ok {
			m[int32(id)] = ids
		}
	}
	return m
}()

type evOpenLoom struct{ eid int32 }

func (evOpenLoom) isHubEvent() {}

func (h *hub) openLoom(t *tracked) {
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
	t.winID, t.winKind = h.nextWin, winLoom
	t.stoneSel = -1 // shared selection field (one menu open at a time)

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuLoom), Title: "Loom"})
	h.sendLoomWindow(t)
}

// loomSelectable is the pattern list for the current inputs (client order).
func (h *hub) loomSelectable(t *tracked) []int32 {
	if !bannerItems[t.anvil[0].item] || t.anvil[0].count <= 0 {
		return nil
	}
	if _, isDye := dyeColorOf[t.anvil[1].item]; !isDye || t.anvil[1].count <= 0 {
		return nil
	}
	if t.anvil[0].patCount() >= 6 {
		return nil // vanilla: at the layer cap nothing is selectable
	}
	if t.extraSlot.item != 0 && t.extraSlot.count > 0 {
		return loomPatternItems[t.extraSlot.item]
	}
	base, _ := protocol.LoomPatterns()
	return base
}

// loomResult applies the selected pattern layer to a copy of the banner.
func (h *hub) loomResult(t *tracked) invStack {
	list := h.loomSelectable(t)
	if t.stoneSel < 0 || t.stoneSel >= len(list) {
		return invStack{}
	}
	res := t.anvil[0]
	res.count = 1
	n := res.patCount()
	if n >= len(res.pats) {
		return invStack{}
	}
	res.pats[n] = bannerLayer{patPlus1: int16(list[t.stoneSel] + 1), color: dyeColorOf[t.anvil[1].item]}
	return res
}

// sendLoomWindow pushes the loom window: banner, dye, pattern item, result,
// inventory — and the selected-pattern property.
func (h *hub) sendLoomWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 40) // 0 banner, 1 dye, 2 pattern, 3 result, 4-30 main, 31-39 hotbar
	slots = append(slots, stackEv(t.anvil[0]), stackEv(t.anvil[1]), stackEv(t.extraSlot), stackEv(h.loomResult(t)))
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(t.stoneSel)})
}

// loomSelect applies a pattern-button click.
func (h *hub) loomSelect(t *tracked, button int32) {
	list := h.loomSelectable(t)
	if button < 0 || int(button) >= len(list) || int(button) == t.stoneSel {
		return
	}
	t.stoneSel = int(button)
	h.sendWinSlot(t, 3, h.loomResult(t))
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(t.stoneSel)})
}

// takeLoomResult consumes one banner + one dye (the pattern item stays).
func (h *hub) takeLoomResult(players map[int32]*tracked, t *tracked) {
	res := h.loomResult(t)
	if res.item == 0 || t.cursor.item != 0 {
		h.sendLoomWindow(t)
		return
	}
	for i := 0; i <= 1; i++ {
		if t.anvil[i].count--; t.anvil[i].count <= 0 {
			t.anvil[i] = invStack{}
		}
	}
	if t.anvil[0].item == 0 || t.anvil[1].item == 0 {
		t.stoneSel = -1
	}
	t.cursor = res
	h.playSound(players, "minecraft:ui.loom.take_result", sndBlock, t.x, t.y, t.z, 1, 1)
	h.sendCursor(t)
	h.sendLoomWindow(t)
}
