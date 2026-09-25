package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// spawnOffspringFromSpawnEgg: an egg used on its own kind makes a baby for
// the ageable villager, squid and dolphin (getBreedOffspring) and, through
// Mob.setBaby, for the zombie family, piglins and zoglins; a piglin brute
// cannot be a baby, so its egg does nothing to it.
func TestSpawnEggOffspringGaps(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	eggOf := map[int]int32{}
	for item, et := range spawnEggEntity {
		eggOf[et] = item
	}
	babies := func(etype int) int {
		n := 0
		for _, m := range h.mobs {
			if m.etype == etype && m.baby {
				n++
			}
		}
		return n
	}
	for _, et := range []int{entityVillager, entitySquid, entityGlowSquid, entityDolphin, entityZombie, entityHusk,
		entityDrowned, entityZombieVillager, entityZombifiedPiglin, entityPiglin, entityZoglin} {
		parent := h.spawnMob(players, et, 0.5, 200, 0.5)
		parent.baby = false
		before := babies(et)
		pl.inv.slots[pl.p.heldSlot()] = invStack{item: eggOf[et], count: 2}
		if !h.interactMob(players, pl, parent, false) || babies(et) != before+1 {
			t.Errorf("%s: its own egg should make a baby", entityNameByID[et])
			continue
		}
		if pl.inv.slots[pl.p.heldSlot()].count != 1 {
			t.Errorf("%s: the egg should be spent", entityNameByID[et])
		}
		for _, m := range h.mobs {
			if m.etype == et && m.baby && m != parent && m.wearsAnything() {
				t.Errorf("%s: the baby came out wearing or holding something", entityNameByID[et])
			}
		}
	}
	brute := h.spawnMob(players, entityPiglinBrute, 0.5, 200, 0.5)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: eggOf[entityPiglinBrute], count: 1}
	h.interactMob(players, pl, brute, false)
	if babies(entityPiglinBrute) != 0 {
		t.Error("a piglin brute cannot be a baby")
	}
}
