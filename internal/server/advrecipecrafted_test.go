package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// RecipeCraftedTrigger gives each ingredient predicate its own input: a
// decorated pot crafted from one sherd and three bricks does not earn
// "Careful Restoration"; four sherds do. Driven through the crafting grid's
// take path, where the trigger fires.
func TestPotFromSherdsNeedsFourSherds(t *testing.T) {
	const adv, crit = "minecraft:adventure/craft_decorated_pot_using_only_sherds", "pot_crafted_using_only_sherds"
	h := newHub(world.New(1))
	pl, players := craftPlayer(h)
	pl.adv = advState{}
	brick := int32(itemByName["brick"])
	sherds := []int32{int32(itemByName["angler_pottery_sherd"]), int32(itemByName["arms_up_pottery_sherd"]),
		int32(itemByName["blade_pottery_sherd"]), int32(itemByName["brewer_pottery_sherd"])}
	craft := func(faces [4]int32) {
		for i := range pl.craft {
			pl.craft[i] = invStack{}
		}
		for i, slot := range []int{1, 3, 5, 7} {
			pl.craft[slot] = invStack{item: faces[i], count: 1}
		}
		if res, _ := h.craftResult(pl.craft[:9], 3); res.item != itemDecoratedPot {
			t.Fatalf("the grid makes %+v, not a decorated pot", res)
		}
		pl.cursor = invStack{}
		h.takeCraftResult(players, pl, 0)
	}
	got := func() bool { _, ok := pl.adv[adv][crit]; return ok }

	craft([4]int32{sherds[0], brick, brick, brick})
	if got() {
		t.Fatal("one sherd and three bricks earned the only-sherds advancement")
	}
	craft([4]int32{sherds[0], sherds[0], sherds[0], brick})
	if got() {
		t.Fatal("three sherds and a brick earned the only-sherds advancement")
	}
	craft([4]int32{sherds[0], sherds[1], sherds[2], sherds[3]})
	if !got() {
		t.Fatal("four sherds did not earn the only-sherds advancement")
	}
}
