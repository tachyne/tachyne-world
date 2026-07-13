package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

func TestArmorStandFlow(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		bx, bz := int(tr.x)+2, int(tr.z)
		by := int(tr.y)
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemArmorStand, count: 1}
		h.onPlaceStand(h.playersRef, evPlaceStand{eid: p.eid, x: bx, y: by, z: bz, yaw: 93})
		var st *armorStand
		for _, s := range h.armorStands {
			st = s
		}
		if st == nil || tr.inv.slots[tr.p.heldSlot()].item != 0 {
			t.Errorf("place: %+v held=%+v", st, tr.inv.slots[tr.p.heldSlot()])
			return
		}
		if int(st.yaw)%45 != 0 {
			t.Errorf("yaw %f not snapped", st.yaw)
		}
		// Dress with an enchanted helmet; the swap preserves components.
		helm := invStack{item: int32(itemByName["iron_helmet"]), count: 1, ench: [2]enchApply{{id: 1, lvl: 2}}}
		tr.inv.slots[tr.p.heldSlot()] = helm
		h.interactStand(h.playersRef, tr, st)
		if st.equip[attachproto.EquipHead] != helm || tr.inv.slots[tr.p.heldSlot()].item != 0 {
			t.Errorf("dress: %+v", st.equip[attachproto.EquipHead])
		}
		// Empty hand undresses.
		h.interactStand(h.playersRef, tr, st)
		if st.equip[attachproto.EquipHead].item != 0 {
			t.Error("undress failed")
		}
		found := false
		for _, s := range tr.inv.slots {
			if s.item == helm.item && s.ench == helm.ench {
				found = true
			}
		}
		if !found {
			t.Error("helmet not returned")
		}
		// Double punch: first wobbles, second (same tick) breaks + drops.
		tr.inv.slots[tr.p.heldSlot()] = invStack{}
		h.interactStand(h.playersRef, tr, st) // no-op: nothing to take
		st.equip[attachproto.EquipChest] = invStack{item: int32(itemByName["iron_chestplate"]), count: 1}
		h.hitStand(h.playersRef, tr, st)
		if _, ok := h.armorStands[st.eid]; !ok {
			t.Error("first hit must not break")
		}
		h.hitStand(h.playersRef, tr, st)
		if _, ok := h.armorStands[st.eid]; ok {
			t.Error("second hit must break")
		}
		stands, chests := 0, 0
		for _, it := range h.items {
			switch it.item {
			case itemArmorStand:
				stands++
			case int32(itemByName["iron_chestplate"]):
				chests++
			}
		}
		if stands != 1 || chests != 1 {
			t.Errorf("drops: stands=%d chests=%d", stands, chests)
		}
		// Persistence round trip.
		st2 := &armorStand{eid: 1, dim: 0, x: 1.5, y: 64, z: 2.5, yaw: 45}
		st2.equip[attachproto.EquipFeet] = invStack{item: int32(itemByName["iron_boots"]), count: 1}
		cs := newContainerStore("")
		cs.recordStands(map[int32]*armorStand{1: st2})
		next := int32(100)
		loaded := cs.loadStands(func() int32 { next++; return next })
		if len(loaded) != 1 {
			t.Errorf("loaded %d stands", len(loaded))
			return
		}
		for _, l := range loaded {
			if l.x != 1.5 || l.yaw != 45 || l.equip[attachproto.EquipFeet].item != int32(itemByName["iron_boots"]) {
				t.Errorf("round trip: %+v", l)
			}
		}
	})
}
