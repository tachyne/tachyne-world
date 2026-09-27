package server

import (
	"encoding/json"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// handleInteract runs interactOn with the packet's hand: a name tag held in
// the offhand names a pig when the main hand (a pickaxe) passed, and is
// spent from the offhand, not the main hand.
func TestOffhandNameTagNamesMob(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	r := &remotePlayer{s: &Server{hub: h}, p: p, gm: -1}
	pick := invStack{item: itemByName["diamond_pickaxe"], count: 1}
	var pig *mob
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		tr.inv.slots[tr.p.heldSlot()] = pick
		tr.offhand = invStack{item: int32(itemByName["name_tag"]), count: 1, name: "Bacon"}
		tr.p.setOffhand(tr.offhand.item)
		pig = h.spawnMob(h.playersRef, entityPig, tr.x+1, tr.y, tr.z)
	})
	r.Action(attachproto.UseEntity{Target: pig.eid, Hand: 1})
	onHub(t, h, func() {})
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		if pig.customName != "Bacon" {
			t.Errorf("an offhand name tag did not name the pig: %q", pig.customName)
		}
		if tr.offhand.count != 0 {
			t.Errorf("the offhand name tag was not spent: %+v", tr.offhand)
		}
		if tr.inv.slots[tr.p.heldSlot()] != pick {
			t.Errorf("the main hand changed: %+v", tr.inv.slots[tr.p.heldSlot()])
		}
	})
}

// FishingRodItem.use in the offhand casts with the offhand's rod and wears
// it when reeled; FishingHook.shouldStopFishing keeps the line while either
// hand holds a rod.
func TestOffhandFishingRod(t *testing.T) {
	h, players, pl, r := offhandUseRig(t)
	pl.pitch = 90
	setOffhand(pl, invStack{item: itemFishingRod, count: 1})
	run := func(hand int32) {
		r.Action(attachproto.UseItem{Hand: hand})
		for len(h.events) > 0 {
			if e, ok := (<-h.events).(evFishUse); ok {
				prev := pl.useOffhand
				pl.useOffhand = e.off
				h.useRod(players, pl)
				pl.useOffhand = prev
			}
		}
	}
	run(handOffhand)
	if h.bobbers[pl.p.eid] == nil {
		t.Fatal("an offhand rod did not cast")
	}
	h.updateBobbers(players)
	if h.bobbers[pl.p.eid] == nil {
		t.Fatal("the line snapped though the offhand holds the rod")
	}
	h.bobbers[pl.p.eid].grounded = true // reeling a grounded hook wears 2
	run(handOffhand)
	if pl.offhand.dmg != 2 {
		t.Fatalf("reeling wore the offhand rod %d, want 2", pl.offhand.dmg)
	}
	if pl.inv.slots[0].dmg != 0 {
		t.Fatalf("reeling wore the main-hand pickaxe: %+v", pl.inv.slots[0])
	}
}

// WrittenBookItem.use opens the book in the hand it was used from.
func TestOffhandBookOpensOffhand(t *testing.T) {
	_, _, pl, r := offhandUseRig(t)
	var hand int32 = -1
	r.emit = func(typ byte, payload []byte) {
		if typ == attachproto.MsgOpenBook {
			var ob attachproto.OpenBook
			if json.Unmarshal(payload, &ob) == nil {
				hand = ob.Hand
			}
		}
	}
	setOffhand(pl, invStack{item: itemWrittenBook, count: 1})
	r.Action(attachproto.UseItem{Hand: handOffhand})
	if hand != handOffhand {
		t.Fatalf("an offhand book opened hand %d, want the offhand", hand)
	}
}

// Equippable.swapWithEquipmentSlot from the offhand wears the offhand's
// piece, not whatever the main hand holds.
func TestOffhandArmourEquips(t *testing.T) {
	h, players, pl, _ := offhandUseRig(t)
	helm := invStack{item: int32(itemByName["iron_helmet"]), count: 1}
	setOffhand(pl, helm)
	h.onEquipHeld(players, evEquipHeld{eid: pl.p.eid, off: true})
	if pl.armor[0] != helm || pl.offhand.item != 0 {
		t.Fatalf("the offhand helmet was not worn: armour %+v, offhand %+v", pl.armor[0], pl.offhand)
	}
	if pl.inv.slots[0].item != itemByName["diamond_pickaxe"] {
		t.Fatalf("the main hand changed: %+v", pl.inv.slots[0])
	}
}

// A main-hand shear followed by the client's offhand interact shears once:
// the offhand pass reads the (empty) offhand.
func TestOffhandPassDoesNotRepeatMainHand(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	r := &remotePlayer{s: &Server{hub: h}, p: p, gm: -1}
	var sheep *mob
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: int32(itemShears), count: 1}
		sheep = h.spawnMob(h.playersRef, entitySheep, tr.x+1, tr.y, tr.z)
	})
	r.Action(attachproto.UseEntity{Target: sheep.eid})
	r.Action(attachproto.UseEntity{Target: sheep.eid, Hand: 1})
	onHub(t, h, func() {})
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		if !sheep.sheared {
			t.Fatal("the main-hand shears did not shear the sheep")
		}
		if d := tr.inv.slots[tr.p.heldSlot()].dmg; d != 1 {
			t.Fatalf("the shears wore %d, want one shear's worth", d)
		}
	})
}
