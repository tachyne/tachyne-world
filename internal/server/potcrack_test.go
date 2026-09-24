package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A pot shattered by a projectile drops its faces: the sherds it wears and
// a brick for each plain side (the loot table's dynamic sherds entry).
func TestCrackedPotDropsItsFaces(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pos := blockPos{0, 180, 0}
	h.world.SetBlock(0, 180, 0, worldgen.BlockID("decorated_pot"))
	angler := itemByName["angler_pottery_sherd"]
	h.potSherds.set(simPos{dim: 0, blockPos: pos}, potSherds{angler, 0, 0, 0})
	h.breakPotByProjectile(players, 0, pos)
	got := map[int32]int{}
	for _, it := range h.items {
		got[it.item] += it.count
	}
	if got[angler] != 1 || got[itemByName["brick"]] != 3 || got[itemDecoratedPot] != 0 {
		t.Errorf("shattered pot dropped %v, want one angler sherd and three bricks", got)
	}
	if !potCracksUnder(invStack{item: itemByName["iron_pickaxe"], count: 1}) || potCracksUnder(invStack{item: 0}) {
		t.Error("#breaks_decorated_pots: a pickaxe cracks a pot, a bare hand does not")
	}
	silky := invStack{item: itemByName["iron_pickaxe"], count: 1}
	silky.ench = enchSetLevel(silky.ench, enchSilkTouch, 1)
	if potCracksUnder(silky) {
		t.Error("Silk Touch cracked a pot")
	}
}
