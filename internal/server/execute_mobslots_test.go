package server

import (
	"testing"
)

// Mobs hold items too: /item writes a zombie's hand and armour (a chest
// slot refuses a block), /execute if items reads them back, a villager's
// inventory is mob.inventory.*, a donkey's chest is horse.chest and its
// contents horse.*, and `this` in a slot source is a mob running the
// command.
func TestExecuteIfItemsOnMobs(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("summon zombie 3 100 3 {PersistenceRequired:1b,NoAI:1b}")
	run("summon villager 5 100 5 {NoAI:1b}")
	if got := run("summon donkey 7 100 7 {NoAI:1b}"); !hasLine(got, "Summoned new Donkey") {
		t.Errorf("summon donkey: %q", got)
	}
	run("item replace entity @e[type=zombie] weapon.mainhand with diamond_sword")
	run("item replace entity @e[type=zombie] armor.chest with iron_chestplate")
	if got := run("item replace entity @e[type=zombie] armor.legs with stone"); hasLine(got, "Replaced 1 slot(s) on Zombie") {
		t.Errorf("a legs slot took stone: %q", got)
	}
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityVillager {
				m.hoard = []invStack{{item: itemWheat, count: 9}, {item: int32(itemByName["bread"]), count: 2}}
			}
		}
	})
	run("item replace entity @e[type=donkey] horse.chest with chest")
	run("item replace entity @e[type=donkey] horse.2 with emerald 4")
	for _, c := range []struct{ cmd, want string }{
		{"execute if items entity @e[type=zombie] weapon.mainhand diamond_sword", "Test passed. Count: 1"},
		{"execute if items entity @e[type=zombie] armor.* iron_chestplate", "Test passed. Count: 1"},
		{"execute if items entity @e[type=zombie] armor.legs *", "Test failed"},
		{"execute if items entity @e[type=villager] mob.inventory.* wheat", "Test passed. Count: 9"},
		{"execute if items entity @e[type=villager] mob.inventory.1 bread", "Test passed. Count: 2"},
		{"execute if items entity @e[type=donkey] horse.chest chest", "Test passed. Count: 1"},
		{"execute if items entity @e[type=donkey] horse.* emerald", "Test passed. Count: 4"},
		{"execute if slots entity @e[type=zombie] weapon.*", "Test passed. Count: 2"},
		{`execute as @e[type=zombie] if items entity alice {type:"slot_range",slots:"weapon.mainhand",source:"this"} diamond_sword`, "Test passed. Count: 1"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
	onHub(t, h, func() {
		for _, m := range h.mobs {
			if m.etype == entityZombie && (m.gear[1].item != int32(itemByName["iron_chestplate"]) || m.gear[2].item != 0) {
				t.Errorf("the zombie's chest and legs: %v %v", m.gear[1], m.gear[2])
			}
		}
	})
}

// The contents slot source reaches into a stack's container: a shulker
// box's slots up to its last filled one, a bundle's stacks, a crossbow's
// loaded projectiles; a component that holds no items is refused.
func TestExecuteContentsSlotSource(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	onHub(t, h, func() {
		tr := h.playersRef[ps["alice"].eid]
		box := invStack{item: int32(itemByName["shulker_box"]), count: 1, boxID: h.boxes.mint()}
		var c chest
		c.slots[0] = invStack{item: int32(itemByName["diamond"]), count: 5}
		c.slots[3] = invStack{item: int32(itemByName["diamond"]), count: 2}
		h.boxes.set(box.boxID, c)
		tr.inv.slots[0] = box
		bundle := invStack{item: int32(itemByName["bundle"]), count: 1, bundleID: h.newBundleID()}
		h.bundles.set(bundle.bundleID, []invStack{{item: int32(itemByName["arrow"]), count: 7}})
		tr.inv.slots[1] = bundle
		tr.inv.slots[2] = invStack{item: int32(itemByName["crossbow"]), count: 1, load: xbowLoad{item: int32(itemByName["arrow"]), n: 3}}
	})
	src := func(slot, comp string) string {
		return `{type:"contents",slot_source:{type:"slot_range",slots:"` + slot + `"},component:"` + comp + `"}`
	}
	for _, c := range []struct{ cmd, want string }{
		{"execute if items entity alice " + src("hotbar.0", "container") + " diamond", "Test passed. Count: 7"},
		{"execute if slots entity alice " + src("hotbar.0", "container"), "Test passed. Count: 4"},
		{"execute if items entity alice " + src("hotbar.1", "bundle_contents") + " arrow", "Test passed. Count: 7"},
		{"execute if items entity alice " + src("hotbar.2", "charged_projectiles") + " arrow", "Test passed. Count: 3"},
		{"execute if items entity alice " + src("hotbar.*", "container") + " arrow", "Test failed"},
		{"execute if slots entity alice " + src("hotbar.0", "damage"), "Failed to parse structure: No items in component"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
}

// Enchantment and potion sets in item predicates may name tags.
func TestItemPredicateEnchantmentPotionTags(t *testing.T) {
	sword := invStack{item: int32(itemByName["diamond_sword"]), count: 1}
	sword.ench[0] = enchApply{id: enchByName["sharpness"], lvl: 3}
	pot := invStack{item: int32(itemByName["potion"]), count: 1, potion: potWater}
	for _, c := range []struct {
		arg  string
		st   invStack
		want bool
	}{
		{"*[enchantments~[{enchantments:\"#minecraft:exclusive_set/damage\"}]]", sword, true},
		{"*[enchantments~[{enchantments:\"#minecraft:exclusive_set/mining\"}]]", sword, false},
		{"*[potion_contents~{potions:\"#minecraft:douses_fire\"}]", pot, true},
		{"*[potion_contents~{potions:\"#minecraft:tradeable\"}]", pot, false},
	} {
		test, msg := parseItemPredicate(c.arg)
		if msg != "" {
			t.Errorf("%s: %s", c.arg, msg)
			continue
		}
		if got := test(c.st); got != c.want {
			t.Errorf("%s on %v: %v, want %v", c.arg, c.st, got, c.want)
		}
	}
	if _, msg := parseItemPredicate("*[enchantments~[{enchantments:\"#minecraft:nope\"}]]"); msg == "" {
		t.Error("an unknown enchantment tag was accepted")
	}
}

// type=#tag selects the entity types the tag holds (vanilla's tags, merged
// with the data packs'), type=!#tag the rest; an unknown tag holds none.
func TestSelectorTypeTag(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	run := exRunner(t, s, h, logs, ps["alice"])
	run("summon skeleton 3 100 3 {NoAI:1b}")
	run("summon stray 4 100 4 {NoAI:1b}")
	run("summon zombie 5 100 5 {NoAI:1b}")
	for _, c := range []struct{ cmd, want string }{
		{"execute if entity @e[type=#minecraft:skeletons]", "Test passed. Count: 2"},
		{"execute if entity @e[type=#skeletons,type=!stray]", "Test passed. Count: 1"},
		{"execute if entity @e[type=!#minecraft:skeletons]", "Test passed. Count: 4"},
		{"execute if entity @e[type=#minecraft:no_such_tag]", "Test failed"},
	} {
		if got := run(c.cmd); !hasLine(got, c.want) {
			t.Errorf("%s: want %q, heard %q", c.cmd, c.want, got)
		}
	}
}
