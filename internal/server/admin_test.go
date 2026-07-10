package server

import (
	"testing"

	"tachyne/internal/world"
)

func TestPeacefulClearsAndBlocksHostiles(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	z := h.spawnHostile(players, entityZombie, 5, 5)
	cow := h.spawnAnimal(players, entityCow, 8, 8)
	h.applyRule(players, evSetRule{rule: "difficulty", num: diffPeaceful})
	if _, ok := h.mobs[z.eid]; ok {
		t.Fatal("peaceful must clear hostiles")
	}
	if _, ok := h.mobs[cow.eid]; !ok {
		t.Fatal("peaceful keeps the animals")
	}
	h.dayTime.Store(14000) // night — but peaceful blocks spawning
	before := len(h.mobs)
	for i := 0; i < 30; i++ {
		h.updateHostiles(players)
	}
	if len(h.mobs) != before {
		t.Fatal("peaceful must block hostile spawning")
	}
}

func TestKeepInventoryGamerule(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	pl.inv.slots[0] = invStack{item: 35, count: 5}
	pl.xpLevel = 7
	h.applyRule(players, evSetRule{rule: "keepInventory", on: true})
	h.damage(players, pl, 1000)
	if !pl.dead || pl.inv.slots[0].count != 5 || pl.xpLevel != 7 || len(h.items) != 0 {
		t.Fatalf("keepInventory must skip the death stake: inv=%v lvl=%d items=%d", pl.inv.slots[0], pl.xpLevel, len(h.items))
	}
}

func TestDifficultyScalesMobDamage(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffHard
	if h.diffMult() != 1.5 {
		t.Fatal("hard = 1.5×")
	}
	h.rules.Difficulty = diffEasy
	if h.diffMult() != 0.5 {
		t.Fatal("easy = 0.5×")
	}
}

func TestGiveByName(t *testing.T) {
	if itemByName["diamond_sword"] != tDiamondSword {
		t.Fatalf("name table wrong: diamond_sword=%d", itemByName["diamond_sword"])
	}
	if _, ok := summonable["creeper"]; !ok {
		t.Fatal("creeper must be summonable")
	}
}
