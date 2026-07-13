package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

func TestLoomFlow(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		banner := int32(itemByName["white_banner"])
		redDye := int32(itemByName["red_dye"])

		h.openLoom(tr)
		if tr.winKind != winLoom {
			t.Errorf("kind %d", tr.winKind)
			return
		}
		tr.anvil[0] = invStack{item: banner, count: 2}
		tr.anvil[1] = invStack{item: redDye, count: 1}
		base, _ := protocol.LoomPatterns()
		if got := h.loomSelectable(tr); len(got) != len(base) || len(got) != 32 {
			t.Errorf("selectable %d, want 32", len(got))
		}
		// A pattern item narrows the list to its patterns.
		tr.extraSlot = invStack{item: int32(itemByName["creeper_banner_pattern"]), count: 1}
		if got := h.loomSelectable(tr); len(got) != 1 {
			t.Errorf("creeper item selectable %d, want 1", len(got))
		}
		tr.extraSlot = invStack{}

		h.loomSelect(tr, 0)
		res := h.loomResult(tr)
		if res.patCount() != 1 || res.pats[0].patPlus1 != int16(base[0]+1) || res.pats[0].color != 14 {
			t.Errorf("result: %+v", res.pats[0])
		}
		h.takeLoomResult(h.playersRef, tr)
		if tr.cursor.patCount() != 1 || tr.anvil[0].count != 1 || tr.anvil[1].item != 0 {
			t.Errorf("take: cursor=%d banner=%d dye=%+v", tr.cursor.patCount(), tr.anvil[0].count, tr.anvil[1])
		}
		// The pack round-trips patterns.
		if got := unpackStack(packStack(tr.cursor)); got != tr.cursor {
			t.Errorf("pack round trip: %+v vs %+v", got, tr.cursor)
		}
	})
}

func TestSmithingFlow(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		h.openSmithing(tr, 1, 64, 1)
		if tr.winKind != winSmith {
			t.Errorf("kind %d", tr.winKind)
			return
		}
		// Netherite transform carries enchantments + damage.
		sword := int32(itemByName["diamond_sword"])
		tr.extraSlot = invStack{item: protocol.SmithingUpgradeTemplate, count: 1}
		tr.anvil[0] = invStack{item: sword, count: 1, dmg: 7, ench: [2]enchApply{{id: 1, lvl: 3}}}
		tr.anvil[1] = invStack{item: int32(itemByName["netherite_ingot"]), count: 1}
		res := h.smithResult(tr)
		if res.item != int32(itemByName["netherite_sword"]) || res.dmg != 7 || res.enchLvl(1) != 3 {
			t.Errorf("transform: %+v", res)
		}
		h.takeSmithResult(h.playersRef, tr)
		if tr.cursor.item != res.item || tr.extraSlot.item != 0 || tr.anvil[0].item != 0 || tr.anvil[1].item != 0 {
			t.Errorf("take consumption: %+v %+v %+v", tr.extraSlot, tr.anvil[0], tr.anvil[1])
		}
		tr.cursor = invStack{}

		// Trim: sentry template + iron chestplate + gold ingot.
		tmpl := int32(itemByName["sentry_armor_trim_smithing_template"])
		chest := int32(itemByName["iron_chestplate"])
		gold := int32(itemByName["gold_ingot"])
		tr.extraSlot = invStack{item: tmpl, count: 1}
		tr.anvil[0] = invStack{item: chest, count: 1}
		tr.anvil[1] = invStack{item: gold, count: 1}
		res = h.smithResult(tr)
		wantPat := protocol.SmithingTrimTemplate[tmpl] + 1
		wantMat := protocol.SmithingTrimMaterial[gold] + 1
		if res.item != chest || int32(res.trimPat) != wantPat || int32(res.trimMat) != wantMat {
			t.Errorf("trim: %+v want mat %d pat %d", res, wantMat, wantPat)
		}
		// An identical re-trim produces nothing; a different material replaces.
		tr.anvil[0] = res
		if r2 := h.smithResult(tr); r2.item != 0 {
			t.Errorf("identical re-trim: %+v", r2)
		}
		tr.anvil[1] = invStack{item: int32(itemByName["diamond"]), count: 1}
		if r2 := h.smithResult(tr); r2.item == 0 || r2.trimMat == res.trimMat {
			t.Errorf("re-trim with diamond: %+v", r2)
		}
	})
}
