package server

import (
	"testing"
)

// /item modify through the dispatcher: inline functions (one, and a list),
// a component patch, a block container, the modifier tail of /item … from,
// the item argument with components, and the refusals — an unknown
// modifier id, a function the engine does not run, an empty slot.
func TestItemModify(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "item replace entity alice hotbar.0 with stone 10")
	s.handleCommand(alice, "item replace entity alice hotbar.1 with air")
	s.handleCommand(alice, `item replace entity alice hotbar.3 with diamond_sword[damage=7]`)
	settle(t, h, logs, "M0")
	s.handleCommand(alice, `item modify entity alice hotbar.0 {function:"minecraft:set_count",count:5,add:true}`)
	s.handleCommand(alice, `item modify entity alice hotbar.0 [{function:"set_name",name:"Rock"},{function:"set_lore",lore:["hard",{text:"grey"}]}]`)
	s.handleCommand(alice, `item modify entity alice hotbar.3 {function:"set_components",components:{"minecraft:unbreakable":{},"!minecraft:damage":{}}}`)
	s.handleCommand(alice, `item replace entity alice hotbar.2 from entity alice hotbar.0 {function:"set_count",count:2}`)
	s.handleCommand(alice, `setblock 4 150 4 chest{Items:[{Slot:0b,id:"minecraft:book",count:1}]}`)
	s.handleCommand(alice, `item modify block 4 150 4 container.0 {function:"set_enchantments",enchantments:{"minecraft:mending":1}}`)
	s.handleCommand(alice, "item modify entity alice hotbar.1 {function:\"set_count\",count:3}")
	s.handleCommand(alice, "item modify entity alice hotbar.0 minecraft:no_such_modifier")
	s.handleCommand(alice, `item modify entity alice hotbar.0 {function:"minecraft:copy_custom_data"}`)
	settle(t, h, logs, "M1")
	a := linesBetween(logs["alice"], "M0", "M1")
	for _, want := range []string{
		"Modified 1 slot(s) on alice",
		"Modified 1 slot(s) at 4, 150, 4",
		"The target does not have slot hotbar.1",
		"Can't find element 'minecraft:no_such_modifier' in registry 'minecraft:item_modifier'",
		"The loot function 'minecraft:copy_custom_data' is not supported here",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		rock := tr.inv.slots[0]
		if rock.item != itemByName["stone"] || rock.count != 15 || rock.name != "Rock" || rock.tags.lore != "hard\ngrey" {
			t.Errorf("hotbar.0 %+v", rock)
		}
		if two := tr.inv.slots[2]; two.item != itemByName["stone"] || two.count != 2 || two.name != "Rock" {
			t.Errorf("hotbar.2 from hotbar.0 with set_count 2: %+v", two)
		}
		if sw := tr.inv.slots[3]; sw.item != itemByName["diamond_sword"] || sw.dmg != 0 || !sw.tags.unbreakable {
			t.Errorf("hotbar.3 %+v", sw)
		}
		c := h.chests[simPos{blockPos: blockPos{4, 150, 4}}]
		if c == nil || c.slots[0].item != itemEnchantedBook || c.slots[0].enchLvl(enchByName["mending"]) != 1 {
			t.Errorf("chest slot 0 %+v", c)
		}
	})
}
