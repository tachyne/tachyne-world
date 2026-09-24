package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bone meal on a berry bush still growing is the bone meal's, not a pick;
// from age 2 an empty hand picks, and a bearing cave vine is always a pick.
func TestBerryPlantsClaimOnlyPicks(t *testing.T) {
	for _, tc := range []struct {
		age  uint32
		held int32
		want bool
	}{{0, 0, false}, {1, 0, false}, {2, 0, true}, {3, 0, true},
		{0, itemBoneMeal, false}, {2, itemBoneMeal, false}, {3, itemBoneMeal, true}} {
		if got := berriesClaimClick(berryBase+tc.age, tc.held); got != tc.want {
			t.Errorf("bush age %d held %d: claims=%v, want %v", tc.age, tc.held, got, tc.want)
		}
	}
	bare := worldgen.StateWith("cave_vines", map[string]string{"age": "0", "berries": "false"})
	ripe := worldgen.StateWith("cave_vines", map[string]string{"age": "0", "berries": "true"})
	if berriesClaimClick(bare, itemBoneMeal) || !berriesClaimClick(ripe, itemBoneMeal) {
		t.Error("a cave vine claims the click exactly when it bears berries")
	}
}

// A sign placed into a water source keeps the water.
func TestSignPlacedInWaterWaterlogs(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	sign := worldgen.BlockBase("oak_sign")
	info, _ := worldgen.InfoForState(sign)
	dry := worldgen.SetProperty(info, sign, "waterlogged", "false")
	w.SetBlock(0, 200, 0, worldgen.WaterBase)
	if got := waterlogPlaced(w, 0, 200, 0, dry); worldgen.GetProperty(info, got, "waterlogged") != "true" {
		t.Error("a sign placed in water came out dry")
	}
	w.SetBlock(0, 200, 0, worldgen.Air)
	if got := waterlogPlaced(w, 0, 200, 0, dry); got != dry {
		t.Error("a sign placed in air was changed")
	}
}

// A cobweb holds a zombie to a quarter of its step; a spider walks through.
func TestCobwebHoldsMobsButNotSpiders(t *testing.T) {
	for _, tc := range []struct {
		etype int
		slow  bool
	}{{entityZombie, true}, {entitySpider, false}} {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		h.world.ForceLoad(0, 0, 2)
		for x := -2; x <= 8; x++ {
			h.world.SetBlock(x, 179, 0, worldgen.BlockBase("stone"))
			h.world.SetBlock(x, 180, 0, cobwebState)
		}
		m := h.spawnMob(players, tc.etype, 0.5, 180, 0.5)
		if !h.cobwebSlow(m.dim, m.x, m.y, m.z) {
			t.Fatal("fixture: the mob is not in the web")
		}
		if sf := h.webFactor(m); (sf < 1) != tc.slow {
			t.Errorf("%s in a web: factor %v", advEntityName[tc.etype], sf)
		}
	}
}

// A leaf block placed by hand is persistent and takes its distance from its
// neighbours: a hedge next to nothing woody never decays.
func TestPlacedLeavesArePersistent(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	p.setHotbarSlot(0, itemByName["oak_leaves"])
	p.held = 0
	x, y, z := 3, 70, 3
	w.SetBlock(x, y, z, worldgen.BlockBase("stone"))
	w.SetBlock(x, y+1, z, worldgen.Air)
	w.SetBlock(x+1, y+1, z, worldgen.BlockBase("oak_log"))
	s.handlePlace(p, placeBody(x, y, z, 1))
	base, d, persistent, ok := leafInfo(w.Block(x, y+1, z))
	if !ok || !persistent || d != 1 || base == 0 {
		t.Fatalf("placed leaf: ok=%v persistent=%v distance=%d, want a persistent leaf at distance 1", ok, persistent, d)
	}
}
