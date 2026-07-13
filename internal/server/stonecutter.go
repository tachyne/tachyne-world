package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Stonecutter, on the vanilla model: one input slot, a recipe-button list
// the client filters from update_recipes (the gateways send the SAME
// generated table this menu reads, so button indices agree by construction),
// a server-owned result, and container_button_click selecting the recipe.
// The input consumes one per take; the selection survives repeat takes and
// resets when the input item changes (vanilla setupRecipeList).

const menuStonecutter = 24 // vanilla menu registration order

var (
	stonecutterMin = worldgen.BlockBase("stonecutter") // facing (4)
	stonecutterMax = worldgen.BlockBase("stonecutter") + 3
)

// stonecutIndex buckets the shared recipe table by input item, preserving
// the global order (= the order the client sees after filtering).
var stonecutIndex = func() map[int32][]protocol.StonecuttingRecipe {
	m := map[int32][]protocol.StonecuttingRecipe{}
	for _, r := range protocol.StonecuttingRecipes {
		m[r.In] = append(m[r.In], r)
	}
	return m
}()

type evOpenStonecut struct{ eid int32 }

func (evOpenStonecut) isHubEvent() {}

func (h *hub) openStonecutter(t *tracked) {
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
	t.winID, t.winKind = h.nextWin, winStonecut
	t.stoneSel = -1

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuStonecutter), Title: "Stonecutter"})
	h.sendStonecutWindow(t)
}

// stonecutResult resolves the current input + selection to a result stack.
func (h *hub) stonecutResult(t *tracked) invStack {
	in := t.anvil[0]
	if in.item == 0 || in.count <= 0 || t.stoneSel < 0 {
		return invStack{}
	}
	list := stonecutIndex[in.item]
	if t.stoneSel >= len(list) {
		return invStack{}
	}
	r := list[t.stoneSel]
	return invStack{item: r.Out, count: int(r.Count)}
}

// sendStonecutWindow pushes the whole window (input, result, inventory) and
// the selected-recipe property.
func (h *hub) sendStonecutWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 38) // 0 input, 1 result, 2-28 main, 29-37 hotbar
	slots = append(slots, stackEv(t.anvil[0]), stackEv(h.stonecutResult(t)))
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

// stonecutSelect applies a recipe-button click (container_button_click).
func (h *hub) stonecutSelect(t *tracked, button int32) {
	list := stonecutIndex[t.anvil[0].item]
	if button < 0 || int(button) >= len(list) || int(button) == t.stoneSel {
		return
	}
	t.stoneSel = int(button)
	h.sendWinSlot(t, 1, h.stonecutResult(t))
	t.p.trySendEv(attachproto.WindowData{ID: int32(t.winID), Prop: 0, Value: int32(t.stoneSel)})
}

// takeStonecutResult hands the result to the cursor and consumes one input
// (vanilla onTake: repeated takes keep cutting while input remains).
func (h *hub) takeStonecutResult(players map[int32]*tracked, t *tracked) {
	res := h.stonecutResult(t)
	if res.item == 0 {
		h.sendStonecutWindow(t)
		return
	}
	switch {
	case t.cursor.item == 0:
		t.cursor = res
	case t.cursor.item == res.item && t.cursor.count+res.count <= stackCap(res.item):
		t.cursor.count += res.count
	default:
		h.sendStonecutWindow(t)
		return
	}
	h.incStat(t, attachproto.StatCrafted, res.item, int32(res.count))
	if t.anvil[0].count--; t.anvil[0].count <= 0 {
		t.anvil[0] = invStack{}
		t.stoneSel = -1 // vanilla: empty input resets the recipe list
	}
	h.playSound(players, "minecraft:ui.stonecutter.take_result", sndBlock, t.x, t.y, t.z, 1, 1)
	h.sendCursor(t)
	h.sendStonecutWindow(t)
}
