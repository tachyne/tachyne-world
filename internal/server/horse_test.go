package server

import "testing"

// Mount inventory: chest-equip sizes the grid, the menu opens on sneak, the
// slots live on the mob, equipment slots validate, death drops everything.
func TestHorseInventory(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		m := h.spawnSpecies(h.playersRef, entityDonkey, 0, tr.x+2, tr.y, tr.z)
		if m == nil {
			t.Error("no donkey")
			return
		}
		// A held chest equips (5 columns for donkeys).
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: int32(itemByName["chest"]), count: 1}
		if !h.tryHorseScreen(h.playersRef, tr, m, false) {
			t.Error("chest equip not consumed")
		}
		if !m.chested || len(m.chest) != 15 || horseColumns(m) != 5 {
			t.Errorf("chested=%v len=%d cols=%d", m.chested, len(m.chest), horseColumns(m))
		}
		// Sneak-click opens the screen.
		if !h.tryHorseScreen(h.playersRef, tr, m, true) || tr.winKind != winHorse || tr.horseEID != m.eid {
			t.Errorf("open: kind=%d eid=%d", tr.winKind, tr.horseEID)
		}
		// Slot mapping: 0 saddle, 1 body, 2..16 chest, 17.. player inv.
		if ptr, _ := h.horseSlotPtr(tr, 0); ptr != &m.saddleSt {
			t.Error("slot 0 is not the saddle")
		}
		if ptr, _ := h.horseSlotPtr(tr, 5); ptr != &m.chest[3] {
			t.Error("slot 5 is not chest[3]")
		}
		if ptr, _ := h.horseSlotPtr(tr, 17); ptr != &tr.inv.slots[9] {
			t.Error("slot 17 is not main inv 9")
		}
		// Equipment validation: junk in the saddle slot is cleared.
		m.saddleSt = invStack{item: int32(itemByName["stone"]), count: 1}
		h.horseEquipSync(h.playersRef, m)
		if m.saddleSt.item != 0 || m.saddled {
			t.Errorf("junk saddle kept: %+v saddled=%v", m.saddleSt, m.saddled)
		}
		m.saddleSt = invStack{item: int32(itemSaddle), count: 1}
		h.horseEquipSync(h.playersRef, m)
		if !m.saddled {
			t.Error("real saddle not recognized")
		}
		// Death drops saddle + chest + contents.
		m.chest[0] = invStack{item: int32(itemByName["stone"]), count: 3}
		h.despawnMob(h.playersRef, m)
		saddles, chests, stones := 0, 0, 0
		for _, it := range h.items {
			switch it.item {
			case int32(itemSaddle):
				saddles += it.count
			case int32(itemByName["chest"]):
				chests += it.count
			case int32(itemByName["stone"]):
				stones += it.count
			}
		}
		if saddles != 1 || chests != 1 || stones != 3 {
			t.Errorf("drops: saddles=%d chests=%d stones=%d", saddles, chests, stones)
		}
	})
}
