package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func farmerSetup(t *testing.T) (*hub, *mob, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -6; x <= 8; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 40.5, 180, 0.5
	h.dayTime.Store(3000) // mid-morning: the work segment
	h.tick.Store(1000)
	v := h.spawnMob(players, entityVillager, 0.5, 180, 0.5)
	if v == nil {
		t.Fatal("villager spawn returned nil")
	}
	v.profession = profFarmer
	return h, v, players
}

// A farmer harvests a mature wheat plot, pockets the drops, and sows the
// seed back on the bare farmland.
func TestFarmerHarvestsAndReplants(t *testing.T) {
	h, v, players := farmerSetup(t)
	w := h.world
	wheat := cropRanges[0]
	w.SetBlock(3, 179, 0, farmlandMin)
	w.SetBlock(3, 180, 0, wheat[1]) // mature
	if !h.farmerStep(players, v) || v.farmPos != (blockPos{3, 180, 0}) {
		t.Fatalf("the farmer did not pick the ripe plot: %+v", v.farmPos)
	}
	if v.vx <= 0 {
		t.Error("the farmer is not walking to the plot")
	}
	v.x, v.z = 3.5, 0.5
	h.farmerStep(players, v)
	if isCropState(w.At(3, 180, 0)) {
		t.Fatal("the ripe wheat was not harvested")
	}
	if len(h.items) == 0 {
		t.Fatal("the harvest dropped nothing")
	}
	// The drops are at its feet: it pockets them.
	for i := 0; i < 8 && len(h.items) > 0; i++ {
		for _, it := range h.items {
			it.noPickupUntil = 0
		}
		h.villagerPickupStep(players, v)
	}
	if villagerCount(v, int32(itemByName["wheat_seeds"])) == 0 {
		t.Fatalf("no seeds in its pockets: %v", v.hoard)
	}
	// Back on the bare farmland it sows.
	for i := 0; i < 3 && w.At(3, 180, 0) == worldgen.Air; i++ {
		h.farmerStep(players, v)
	}
	if got := w.At(3, 180, 0); got != wheat[0] {
		t.Errorf("plot state %d after sowing, want young wheat %d", got, wheat[0])
	}
	// Off duty, nothing happens.
	h.dayTime.Store(12000)
	v.farmPos, v.farmNext = blockPos{}, 0
	w.SetBlock(3, 180, 0, wheat[1])
	if h.farmerStep(players, v) {
		t.Error("a farmer worked its field in the evening")
	}
	h.dayTime.Store(3000)
	h.rules.MobGriefing = false
	if h.farmerStep(players, v) {
		t.Error("a farmer worked its field with mob griefing off")
	}
}

// A farmer with bone meal feeds a growing crop.
func TestFarmerUsesBonemeal(t *testing.T) {
	h, v, players := farmerSetup(t)
	w := h.world
	wheat := cropRanges[0]
	w.SetBlock(3, 179, 0, farmlandMin)
	w.SetBlock(3, 180, 0, wheat[0])
	v.hoard = []invStack{{item: itemBoneMeal, count: 2}}
	if !h.farmerBonemealStep(players, v) || v.bmPos != (blockPos{3, 180, 0}) {
		t.Fatalf("the farmer did not pick the young crop: %+v", v.bmPos)
	}
	v.x, v.z = 3.5, 0.5
	h.farmerBonemealStep(players, v)
	if got := w.At(3, 180, 0); got == wheat[0] {
		t.Error("the crop did not grow")
	}
	if villagerCount(v, itemBoneMeal) != 1 {
		t.Errorf("bone meal left %d, want 1", villagerCount(v, itemBoneMeal))
	}
}

// Villagers pocket what they want and leave the rest; pockets hold eight
// stacks.
func TestVillagerPicksUpWantedItems(t *testing.T) {
	h, v, players := farmerSetup(t)
	stick := h.spawnItem(players, int32(itemByName["stick"]), 1, 1.5, 180, 0.5)
	bread := h.spawnItem(players, int32(itemByName["bread"]), 3, 1.5, 180, 0.5)
	stick.noPickupUntil, bread.noPickupUntil = 0, 0
	h.villagerPickupStep(players, v)
	if villagerCount(v, int32(itemByName["bread"])) != 3 {
		t.Errorf("bread pocketed: %d, want 3", villagerCount(v, int32(itemByName["bread"])))
	}
	if _, still := h.items[stick.eid]; !still {
		t.Error("a stick was pocketed")
	}
	if h.villagerPickupStep(players, v) {
		t.Error("nothing wanted is left, yet the villager is busy")
	}
	for i := 0; i < villagerInvSlots; i++ {
		villagerAddItem(v, invStack{item: int32(itemByName["wheat"]), count: 64})
	}
	if villagerInvRoom(v, int32(itemByName["carrot"])) != 0 || villagerWants(v, int32(itemByName["carrot"])) {
		t.Error("full pockets still want more")
	}
}
