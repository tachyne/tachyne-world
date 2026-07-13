package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Cartography table: the third two-slot menu (anvil/grindstone family).
// Slot 0 takes a filled map, slot 1 the modifier — paper zooms out (a fresh
// BLANK map one scale coarser), a glass pane locks (a frozen pixel-copy
// under a new id), an empty map clones (two copies sharing the id, so both
// keep updating). Like vanilla's map_post_processing component, the preview
// result carries the SOURCE map id; the derived map is minted at take time.

const menuCartography = 23 // vanilla menu registration order (crafting 12 anchor)

var (
	cartographyTableState = worldgen.BlockBase("cartography_table") // single state
	itemGlassPane         = int32(itemByName["glass_pane"])
)

type evOpenCarto struct{ eid int32 }

func (evOpenCarto) isHubEvent() {}

func (h *hub) openCartography(t *tracked) {
	h.openTwoSlot(t, winCarto, menuCartography, "Cartography Table")
}

// cartoResult computes the table's current result. The client enforces the
// slot predicates in its own menu, but a hacked client can put anything in
// the inputs — everything is re-validated here.
func (h *hub) cartoResult(a, b invStack) invStack {
	if h.maps == nil || a.item != itemFilledMap || a.count <= 0 || a.mapID == 0 ||
		b.item == 0 || b.count <= 0 {
		return invStack{}
	}
	src := h.maps.get(a.mapID)
	if src == nil {
		return invStack{}
	}
	res := a
	res.count = 1
	switch b.item {
	case itemPaper: // zoom out — refused at the scale cap or on locked maps
		if src.Locked || src.Scale >= mapMaxScale {
			return invStack{}
		}
	case itemGlassPane: // lock — already-locked maps have nothing to lock
		if src.Locked {
			return invStack{}
		}
	case itemEmptyMap: // clone — works on locked maps too (copies stay locked)
		res.count = 2
	default:
		return invStack{}
	}
	return res
}

// takeCartoResult finalizes a result take: mint the derived map (zoom/lock),
// consume one of each input, hand the result to the cursor.
func (h *hub) takeCartoResult(players map[int32]*tracked, t *tracked, res invStack) {
	if src := h.maps.get(res.mapID); src != nil {
		switch t.anvil[1].item {
		case itemPaper:
			res.mapID = h.maps.derive(src, src.Scale+1, false).ID
		case itemGlassPane:
			res.mapID = h.maps.derive(src, src.Scale, true).ID
		}
	}
	for i := range t.anvil {
		if t.anvil[i].count--; t.anvil[i].count <= 0 {
			t.anvil[i] = invStack{}
		}
	}
	h.playSound(players, "minecraft:ui.cartography_table.take_result", sndBlock, t.x, t.y, t.z, 1, 1)
	t.cursor = res
	h.sendCursor(t)
	h.sendTwoSlotWindow(t)
}
