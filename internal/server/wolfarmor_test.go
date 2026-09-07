package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The owner armours a tamed wolf; the armour soaks blows as durability and
// breaks when spent; shears take it off; a scute repairs an eighth.
func TestWolfArmor(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.spawnAnimal(players, entityWolf, 0, 0)
	if w == nil {
		t.Fatal("no wolf")
	}
	w.tamed, w.owner = true, pl.p.eid
	pl.inv.slots[0] = invStack{item: itemWolfArmor, count: 1}
	pl.p.setHotbarSlot(0, itemWolfArmor)
	pl.p.held = 0
	if !h.tryWolfArmor(players, pl, w) || w.armorSt.item != itemWolfArmor || pl.inv.slots[0].item != 0 {
		t.Fatalf("the owner should armour the wolf: armour=%+v held=%+v", w.armorSt, pl.inv.slots[0])
	}
	if w.armorValue() < wolfArmorPoints {
		t.Fatalf("armoured wolf armour value %v, want ≥ %d", w.armorValue(), wolfArmorPoints)
	}
	hp := w.health
	w.hurt(5)
	if w.health != hp || w.armorSt.dmg != 5 {
		t.Fatalf("the armour should soak the blow: health %v→%v, armour dmg %d", hp, w.health, w.armorSt.dmg)
	}
	// Repair while sitting with a scute.
	w.sitting = true
	pl.inv.slots[0] = invStack{item: itemArmadilloScute, count: 1}
	pl.p.setHotbarSlot(0, itemArmadilloScute)
	if !h.tryWolfArmor(players, pl, w) || w.armorSt.dmg != 0 {
		t.Fatalf("a scute should repair: dmg=%d", w.armorSt.dmg)
	}
	// Enough blows break it and the wolf is bare again.
	w.hurt(float64(wolfArmorMax()))
	if w.armorSt.item != 0 || w.armorNote != 2 {
		t.Fatalf("the armour should break: %+v note=%d", w.armorSt, w.armorNote)
	}
	h.wolfArmorNote(players, w)
	w.hurt(2)
	if w.health >= hp {
		t.Fatal("a bare wolf takes the blow")
	}
	// Shears take a worn piece off and drop it.
	w.armorSt = invStack{item: itemWolfArmor, count: 1, dmg: 3}
	pl.inv.slots[0] = invStack{item: itemShears, count: 1}
	pl.p.setHotbarSlot(0, itemShears)
	items := len(h.items)
	if !h.tryWolfArmor(players, pl, w) || w.armorSt.item != 0 || len(h.items) != items+1 {
		t.Fatal("shears should strip the armour and drop it")
	}
	// A stranger cannot: the wolf belongs to someone else now.
	w.owner = pl.p.eid + 1000
	pl.inv.slots[0] = invStack{item: itemWolfArmor, count: 1}
	pl.p.setHotbarSlot(0, itemWolfArmor)
	if h.tryWolfArmor(players, pl, w) {
		t.Fatal("only the owner may armour the wolf")
	}
}
