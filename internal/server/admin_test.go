package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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
	h.damageOf(players, pl, 1000, dtGeneric)
	if !pl.dead || pl.inv.slots[0].count != 5 || pl.xpLevel != 7 || len(h.items) != 0 {
		t.Fatalf("keepInventory must skip the death stake: inv=%v lvl=%d items=%d", pl.inv.slots[0], pl.xpLevel, len(h.items))
	}
}

// Player.hurtServer's difficulty branch, per damage type. Easy is NOT a half:
// it is min(f/2 + 1, f), which leaves a 1-damage hit at 1 and softens a big
// one by rather less than half. Peaceful erases a scaled hit entirely, and a
// type that never scales is untouched at every difficulty.
func TestDifficultyScalesByDamageType(t *testing.T) {
	h := newHub(world.New(1))
	for _, tc := range []struct {
		name  string
		dt    dmgType
		byMob bool
		diff  int
		in    float32
		want  float32
	}{
		{"a zombie's bite on hard", dtMobAttack, true, diffHard, 4, 6},
		{"a zombie's bite on easy", dtMobAttack, true, diffEasy, 4, 3},
		{"a small bite on easy stays whole", dtMobAttack, true, diffEasy, 1, 1},
		{"a zombie's bite on normal", dtMobAttack, true, diffNormal, 4, 4},
		{"a zombie's bite on peaceful", dtMobAttack, true, diffPeaceful, 4, 0},
		{"a player's blow never scales", dtPlayerAttack, false, diffHard, 4, 4},
		{"a fall a mob did not cause", dtFall, false, diffHard, 4, 4},
		{"an explosion always scales", dtExplosion, false, diffHard, 4, 6},
		{"a sonic boom always scales", dtSonicBoom, false, diffEasy, 10, 6},
		{"starving has no living cause", dtStarve, false, diffHard, 1, 1},
	} {
		h.rules.Difficulty = tc.diff
		if got := h.difficultyScaled(tc.in, tc.dt, tc.byMob); got != tc.want {
			t.Errorf("%s: %v → %v, want %v", tc.name, tc.in, got, tc.want)
		}
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
