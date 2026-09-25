package server

import (
	"testing"
)

// /give with item components through the dispatcher: enchantments (both
// spellings of the key), a custom name with spaces, damage and a potion; an
// unknown enchantment and an unsupported component are refused.
func TestGiveItemComponents(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, `give bob diamond_sword[enchantments={sharpness:5,"minecraft:looting":3},custom_name="The Big One",damage=10] 1`)
	s.handleCommand(alice, `give bob minecraft:potion[potion_contents={potion:"minecraft:strong_healing"}] 2`)
	s.handleCommand(alice, `give bob book[stored_enchantments={mending:1}]`)
	s.handleCommand(alice, `give bob diamond_sword[enchantments={nonsense:1}]`)
	s.handleCommand(alice, `give bob diamond_sword[max_damage=5]`)
	settle(t, h, logs, "G1")
	a := linesBetween(logs["alice"], "", "G1")
	if !hasLine(a, "Can't find element 'minecraft:nonsense' of type 'minecraft:enchantment'") {
		t.Errorf("no refusal of an unknown enchantment: %q", a)
	}
	if !hasLine(a, "The 'minecraft:max_damage' component is not supported here") {
		t.Errorf("no refusal of an unsupported component: %q", a)
	}
	onHub(t, h, func() {
		var bob *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" {
				bob = tr
			}
		}
		var sword, potion, book invStack
		for _, st := range bob.inv.slots {
			switch st.item {
			case itemByName["diamond_sword"]:
				sword = st
			case itemByName["potion"]:
				potion.potion, potion.count = st.potion, potion.count+st.count
			case itemEnchantedBook:
				book = st
			}
		}
		if sword.enchLvl(enchByName["sharpness"]) != 5 || sword.enchLvl(enchByName["looting"]) != 3 ||
			sword.name != "The Big One" || sword.dmg != 10 {
			t.Errorf("sword %+v", sword)
		}
		if potion.count != 2 || potion.potion != potionByVanillaName["strong_healing"] {
			t.Errorf("potion %+v", potion)
		}
		if book.enchLvl(enchByName["mending"]) != 1 {
			t.Errorf("book %+v", book)
		}
	})
}

func TestParseSNBT(t *testing.T) {
	v, err := parseSNBT(`{a:1b, "b c":[I;1,2], d:'it\'s', e:1.5f, f:[{x:true}], minecraft:g:3}`)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(map[string]any)
	if m["a"] != int64(1) || m["e"] != 1.5 || m["minecraft:g"] != int64(3) {
		t.Errorf("parsed %#v", m)
	}
	if l := m["b c"].([]any); len(l) != 2 {
		t.Errorf("array %#v", m["b c"])
	}
	if _, err := parseSNBT(`{a:1`); err == nil {
		t.Error("an unclosed compound parsed")
	}
}
