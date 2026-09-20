package server

import (
	"math/rand"
	"testing"
)

// The trial chamber's equipment tables dress their mobs in trimmed armour
// wearing three protections at once: set_components carries the trim and
// set_enchantments a whole set, both of which the baker used to drop.
func TestTrialEquipmentIsTrimmedAndEnchanted(t *testing.T) {
	tbl, ok := lootForChest("equipment/trial_chamber_melee")
	if !ok {
		t.Fatal("the melee equipment table should be baked")
	}
	h := newHub(nil)
	r := rand.New(rand.NewSource(7))
	trimmed, protected, weapons := 0, 0, 0
	for i := 0; i < 200; i++ {
		ctx := &lootCtx{rng: r.Intn, randf: r.Float64}
		for _, st := range h.evalChestStacks(tbl, ctx, 0) {
			if _, isArmor := armorInfo[st.item]; !isArmor {
				if st.enchLvl(enchByName["sharpness"]) > 0 || st.enchLvl(enchByName["knockback"]) > 0 {
					weapons++
				}
				continue
			}
			if st.trimMat != 0 && st.trimPat != 0 {
				trimmed++
			}
			if st.enchLvl(enchByName["protection"]) == 4 &&
				st.enchLvl(enchByName["fire_protection"]) == 4 &&
				st.enchLvl(enchByName["projectile_protection"]) == 4 {
				protected++
			}
		}
	}
	if trimmed == 0 {
		t.Error("no armour piece came out trimmed")
	}
	if protected == 0 {
		t.Error("no armour piece came out with all three protections")
	}
	if weapons == 0 {
		t.Error("the melee table should still hand out an enchanted weapon")
	}
}

// SetEnchantmentsFunction's `add` raises the level already on the item; the
// default replaces it, and a plain book becomes an enchanted one.
func TestSetEnchantmentsAddAndReplace(t *testing.T) {
	sharp := enchByName["sharpness"]
	st := invStack{item: itemBook, count: 1}
	st = applySetEnchantments(&lootFn{Enchs: []lootEnch{{Ench: "sharpness", Lvl: 3}}}, st)
	if st.item != itemEnchantedBook || st.enchLvl(sharp) != 3 {
		t.Fatalf("a plain book should become an enchanted book at level 3, got item %d level %d", st.item, st.enchLvl(sharp))
	}
	st = applySetEnchantments(&lootFn{Add: true, Enchs: []lootEnch{{Ench: "sharpness", Lvl: 2}}}, st)
	if st.enchLvl(sharp) != 5 {
		t.Fatalf("add should raise 3 by 2, got %d", st.enchLvl(sharp))
	}
	st = applySetEnchantments(&lootFn{Enchs: []lootEnch{{Ench: "sharpness", Lvl: 1}}}, st)
	if st.enchLvl(sharp) != 1 {
		t.Fatalf("replace should pin it at 1, got %d", st.enchLvl(sharp))
	}
}
