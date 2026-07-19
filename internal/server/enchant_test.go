package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

var tDiamondSword = itemByName["diamond_sword"] // meleeDamage 7

// enchSetup: a survival player with an open enchanting window on real land.
func enchSetup(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	lx, lz := h.findLand(10, 10)
	pl.x, pl.z = float64(lx), float64(lz)
	pl.y = float64(h.world.SurfaceFeet(lx, lz))
	pl.winID, pl.winKind = 7, winEnchant
	pl.winPos = blockPos{lx, int(pl.y), lz}
	return h, pl, map[int32]*tracked{1: pl}
}

func TestEnchantTableRollsAndApplies(t *testing.T) {
	h, pl, players := enchSetup(t)
	pl.xpLevel = 30
	pl.enchSlots[0] = invStack{item: tDiamondSword, count: 1}
	pl.enchSlots[1] = invStack{item: itemLapisLazuli, count: 10}

	h.rollEnchOptions(pl)
	for i, o := range pl.enchOpts {
		if o.cost < 1 || (o.id != enchSharpness && o.id != enchLooting) || o.lvl < 1 {
			t.Fatalf("option %d not rolled for a sword: %+v", i, o)
		}
	}

	lvlBefore := pl.xpLevel
	rolled := pl.enchOpts[1].id
	h.handleEnchant(players, pl, 1) // middle option: 2 lapis + 2 levels
	if !pl.enchSlots[0].enchanted() || pl.enchSlots[0].enchLvl(rolled) == 0 {
		t.Fatalf("sword not enchanted with the rolled offer (%d): %+v", rolled, pl.enchSlots[0])
	}
	if pl.enchSlots[1].count != 8 {
		t.Fatalf("2 lapis should be consumed, left %d", pl.enchSlots[1].count)
	}
	if pl.xpLevel != lvlBefore-2 {
		t.Fatalf("2 levels should be paid, level %d→%d", lvlBefore, pl.xpLevel)
	}
	// Already enchanted → the follow-up roll disables every row.
	if pl.enchOpts != ([3]enchOption{}) {
		t.Fatalf("post-enchant options must be disabled: %+v", pl.enchOpts)
	}
}

func TestEnchantRejectedWithoutLevelsOrLapis(t *testing.T) {
	h, pl, players := enchSetup(t)
	pl.enchSlots[0] = invStack{item: tDiamondSword, count: 1}
	pl.enchSlots[1] = invStack{item: itemLapisLazuli, count: 10}
	pl.xpLevel = 0 // broke
	h.rollEnchOptions(pl)
	h.handleEnchant(players, pl, 0)
	if pl.enchSlots[0].enchanted() {
		t.Fatal("AUTHORITY: enchanting without the level requirement must be rejected")
	}
	pl.xpLevel = 30
	pl.enchSlots[1] = invStack{} // no lapis
	h.handleEnchant(players, pl, 0)
	if pl.enchSlots[0].enchanted() {
		t.Fatal("AUTHORITY: enchanting without lapis must be rejected")
	}
}

func TestUnenchantableItemRollsNothing(t *testing.T) {
	h, pl, _ := enchSetup(t)
	pl.enchSlots[0] = invStack{item: itemByName["rotten_flesh"], count: 3} // rotten flesh
	h.rollEnchOptions(pl)
	if pl.enchOpts != ([3]enchOption{}) {
		t.Fatalf("rotten flesh must not be enchantable: %+v", pl.enchOpts)
	}
}

func TestSharpnessAddsMeleeDamage(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players := map[int32]*tracked{1: pl}
	pl.inv.slots[0] = invStack{item: tDiamondSword, count: 1, ench: [2]enchApply{{id: enchSharpness, lvl: 3}}}
	pl.p.setHotbarSlot(0, tDiamondSword) // the connection-side held-item mirror
	m := &mob{eid: 2, etype: entityZombie, hostile: true, health: zombieHealth, x: 1.5, y: 70, z: 0.5}
	h.mobs[2] = m
	h.attackMob(players, 1, 2)
	// Diamond sword base 7 + Sharpness III (vanilla 0.5·3+0.5 = 2.0), no crit → 9.
	if want := zombieHealth - (7 + 2); m.health != want {
		t.Fatalf("sharpness 3 sword should deal 9: health %d, want %d", m.health, want)
	}
}

func TestProtectionReducesDamage(t *testing.T) {
	pl := testTracked()
	plain := pl.armorReduce(10)
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["wooden_axe"], count: 1, ench: [2]enchApply{{id: enchProtection, lvl: 4}}}
	}
	// Items without armorInfo give no armor points — isolating the EPF path:
	// 16 EPF = 64% off.
	prot := pl.armorReduce(10)
	if prot >= plain*0.4+0.01 {
		t.Fatalf("protection 4×4 should cut ~64%%: %v → %v", plain, prot)
	}
}

func TestEnchantPersistsAndNeverMergesWithPlain(t *testing.T) {
	// Store round-trip keeps the enchantment.
	st := newInvStore(t.TempDir() + "/inv.json")
	pl := testTracked()
	pl.inv.slots[3] = invStack{item: tDiamondSword, count: 1, ench: [2]enchApply{{id: enchSharpness, lvl: 5}, {id: enchUnbreaking, lvl: 2}}}
	st.save("Steve", pl)
	got := testTracked()
	newInvStore(st.path).loadInto(got, "Steve")
	if got.inv.slots[3].enchLvl(enchSharpness) != 5 || got.inv.slots[3].enchLvl(enchUnbreaking) != 2 {
		t.Fatalf("enchantments must survive persistence: %+v", got.inv.slots[3])
	}
	// An enchanted stack refuses to merge into a plain one.
	inv := &inventory{}
	inv.add(35, 10)
	changed, leftover := inv.addStack(invStack{item: 35, count: 5, ench: [2]enchApply{{id: enchEfficiency, lvl: 1}}})
	if leftover != 0 || len(changed) != 1 || changed[0] == 0 {
		t.Fatalf("enchanted stack must take its own slot: changed=%v leftover=%d", changed, leftover)
	}
}
