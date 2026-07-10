package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-common/render770"
	"tachyne/internal/world"
)

func TestEquipmentPacketShape(t *testing.T) {
	armor := [4]invStack{
		{item: itemByName["stone_shovel"], count: 1},  // helmet (head = armor[0])
		{item: itemByName["stone_pickaxe"], count: 1}, // chestplate
		{}, // no leggings
		{item: itemByName["stone_hoe"], count: 1}, // boots
	}
	pkt := render770.Equipment(equipEv(9, invStack{item: itemByName["diamond_sword"], count: 1}, invStack{}, armor))
	r := bytes.NewReader(pkt.Body)
	if eid, _ := protocol.ReadVarInt(r); eid != 9 {
		t.Fatalf("eid = %d", eid)
	}
	// Seven entries, slots 0..6, every one but the last with the high bit set.
	// The body slot (6) is empty here (only the happy-ghast harness fills it).
	wantSlots := []byte{0, 1, 2, 3, 4, 5, 6}
	wantItems := []int32{itemByName["diamond_sword"], 0, itemByName["stone_hoe"], 0, itemByName["stone_pickaxe"], itemByName["stone_shovel"], 0} // main, off, feet, legs, chest, head, body
	for i, slot := range wantSlots {
		marker, err := r.ReadByte()
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if i < len(wantSlots)-1 && marker&0x80 == 0 {
			t.Fatalf("entry %d should have the continuation bit", i)
		}
		if i == len(wantSlots)-1 && marker&0x80 != 0 {
			t.Fatal("last entry must not have the continuation bit")
		}
		if marker&0x7f != slot {
			t.Fatalf("entry %d slot = %d, want %d", i, marker&0x7f, slot)
		}
		count, _ := protocol.ReadVarInt(r)
		if wantItems[i] == 0 {
			if count != 0 {
				t.Fatalf("entry %d should be empty, count=%d", i, count)
			}
			continue
		}
		item, _ := protocol.ReadVarInt(r)
		if item != wantItems[i] {
			t.Fatalf("entry %d item = %d, want %d", i, item, wantItems[i])
		}
		protocol.ReadVarInt(r) // add comps
		protocol.ReadVarInt(r) // remove comps
	}
	if r.Len() != 0 {
		t.Fatalf("%d trailing bytes", r.Len())
	}
}

func TestLeaveKeepsArmorWorn(t *testing.T) {
	h := newHub(world.New(1))
	h.invs = newInvStore(t.TempDir() + "/inv.json")
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.armor[0] = invStack{item: itemByName["stone_shovel"], count: 1, dmg: 5} // worn helmet
	pl.offhand = invStack{item: itemByName["wheat_seeds"], count: 7}

	h.onLeave(players, pl.p)
	// Armor must NOT be folded into the main inventory anymore…
	for i, s := range pl.inv.slots {
		if s.item == itemByName["stone_shovel"] {
			t.Fatalf("helmet was reclaimed into inventory slot %d", i)
		}
	}
	// …and a rejoin restores the worn loadout exactly.
	rejoined := testTracked()
	h.invs.loadInto(rejoined, "tester")
	if rejoined.armor[0] != (invStack{item: itemByName["stone_shovel"], count: 1, dmg: 5}) {
		t.Fatalf("worn helmet should persist across relog, got %+v", rejoined.armor[0])
	}
	if rejoined.offhand != (invStack{item: itemByName["wheat_seeds"], count: 7}) {
		t.Fatalf("offhand should persist across relog, got %+v", rejoined.offhand)
	}
}
