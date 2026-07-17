package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// fishSetup arms a survival player with a fishing rod beside a 5×5 two-deep
// pool of water sources centered at (500, 199-200, 500), open sky above.
func fishSetup(t *testing.T, ench [2]enchApply) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			h.world.SetBlock(500+dx, 199, 500+dz, worldgen.WaterBase)
			h.world.SetBlock(500+dx, 200, 500+dz, worldgen.WaterBase)
			for y := 201; y <= 203; y++ {
				h.world.SetBlock(500+dx, y, 500+dz, 0)
			}
		}
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = 496.5, 201, 500.5
	pl.yaw, pl.pitch = -90, 35 // facing east, aiming down at the pool
	pl.p.setHotbarSlot(0, itemFishingRod)
	pl.inv.slots[0] = invStack{item: itemFishingRod, count: 1, ench: ench}
	return h, pl, map[int32]*tracked{1: pl}
}

// poolBobber plants an already-floating bobber mid-pool, as if a cast landed.
func poolBobber(h *hub, pl *tracked) *bobberEntity {
	b := &bobberEntity{eid: h.allocEID(), owner: pl.p.eid, x: 500.5, y: 200.85, z: 500.5,
		state: bobberBobbing, openWater: true}
	b.sx, b.sy, b.sz = b.x, b.y, b.z
	h.bobbers[pl.p.eid] = b
	return b
}

func TestCastCreatesBobber(t *testing.T) {
	h, pl, players := fishSetup(t, [2]enchApply{{id: enchLure, lvl: 2}, {id: enchLuckOfTheSea, lvl: 1}})
	h.useRod(players, pl)
	b := h.bobbers[pl.p.eid]
	if b == nil {
		t.Fatal("casting a rod should spawn a bobber")
	}
	if b.lureSpeed != 200 || b.luck != 1 {
		t.Fatalf("enchants not read at cast: lureSpeed=%d luck=%d", b.lureSpeed, b.luck)
	}
	if b.state != bobberFlying {
		t.Fatalf("a fresh cast should be flying, got state %d", b.state)
	}
	if math.Hypot(b.vx, b.vz) < 0.3 {
		t.Fatalf("cast velocity too weak: (%v, %v, %v)", b.vx, b.vy, b.vz)
	}
}

func TestBobberLandsAndFloats(t *testing.T) {
	h, pl, players := fishSetup(t, [2]enchApply{})
	h.useRod(players, pl)
	b := h.bobbers[pl.p.eid]
	for i := 0; i < 200 && b.state != bobberBobbing; i++ {
		h.updateBobbers(players)
	}
	if b.state != bobberBobbing {
		t.Fatalf("bobber never reached water: state=%d at (%v,%v,%v)", b.state, b.x, b.y, b.z)
	}
	for i := 0; i < 100; i++ { // settle: it should hover around the surface line
		h.updateBobbers(players)
	}
	if b.y < 200.3 || b.y > 201.3 {
		t.Fatalf("floating bobber should hover near y≈200.9, got %v", b.y)
	}
	if b.wait <= 0 && b.hookT <= 0 && b.nibble <= 0 {
		t.Fatal("floating bobber should have started the catch sequence")
	}
}

func TestSwitchingItemSnapsLine(t *testing.T) {
	h, pl, players := fishSetup(t, [2]enchApply{})
	poolBobber(h, pl)
	pl.p.setHotbarSlot(0, itemByName["stick"])
	h.updateBobbers(players)
	if h.bobbers[pl.p.eid] != nil {
		t.Fatal("putting the rod away should discard the bobber")
	}
}

func TestLureShortensWait(t *testing.T) {
	h, pl, _ := fishSetup(t, [2]enchApply{})
	b := poolBobber(h, pl)
	b.lureSpeed = 300 // Lure III
	for i := 0; i < 50; i++ {
		b.wait = 0
		h.catchingFish(nil, b)
		if b.wait > 300 { // 600 max roll − 300
			t.Fatalf("Lure III wait roll out of range: %d", b.wait)
		}
	}
}

func TestReelDuringNibbleLandsLoot(t *testing.T) {
	h, pl, players := fishSetup(t, [2]enchApply{})
	b := poolBobber(h, pl)
	b.nibble = 10
	b.openWater = false // fish or junk only
	h.reelBobber(players, pl, b)
	if h.bobbers[pl.p.eid] != nil {
		t.Fatal("reeling should discard the bobber")
	}
	got := false
	for i := 1; i < len(pl.inv.slots); i++ {
		if pl.inv.slots[i].count > 0 {
			got = true
		}
	}
	if !got {
		t.Fatal("reeling a nibble should land loot in the inventory")
	}
	if pl.inv.slots[0].dmg != 1 {
		t.Fatalf("a catch should cost the rod 1 durability, got %d", pl.inv.slots[0].dmg)
	}
	if len(h.orbs) != 1 {
		t.Fatalf("a catch should drop one XP orb, got %d", len(h.orbs))
	}
}

func TestReelBeachedCostsTwo(t *testing.T) {
	h, pl, players := fishSetup(t, [2]enchApply{})
	b := poolBobber(h, pl)
	b.state, b.grounded, b.nibble = bobberFlying, true, 0
	h.reelBobber(players, pl, b)
	if pl.inv.slots[0].dmg != 2 {
		t.Fatalf("reeling a beached bobber should cost 2 durability, got %d", pl.inv.slots[0].dmg)
	}
	if len(h.orbs) != 0 {
		t.Fatal("no XP without a bite")
	}
}

func TestReelHookedMobPullsIt(t *testing.T) {
	h, pl, players := fishSetup(t, [2]enchApply{})
	m := &mob{eid: 9, etype: entityCow, health: 10, x: 510.5, y: 200, z: 500.5}
	h.mobs[9] = m
	b := poolBobber(h, pl)
	b.state, b.hooked = bobberHooked, 9
	h.reelBobber(players, pl, b)
	if m.vx >= 0 { // player is west of the cow: the yank must point −x
		t.Fatalf("hooked mob should be pulled toward the player, vx=%v", m.vx)
	}
	if pl.inv.slots[0].dmg != 5 {
		t.Fatalf("reeling a hooked mob should cost 5 durability, got %d", pl.inv.slots[0].dmg)
	}
}

func TestOpenWaterDetection(t *testing.T) {
	h, pl, _ := fishSetup(t, [2]enchApply{})
	b := poolBobber(h, pl)
	if !h.bobberOpenWater(b) {
		t.Fatal("a clear 5×5 pool with open sky is open water")
	}
	stone := h.world.At(500, 100, 500) // any solid terrain state
	if !worldgen.Collides(stone) {
		stone = worldgen.BlockBase("stone")
	}
	h.world.SetBlock(502, 200, 502, stone) // a block breaking the surface layer
	if h.bobberOpenWater(b) {
		t.Fatal("a block in the surface 5×5 must break open water")
	}
}

func TestFishingLootPools(t *testing.T) {
	h, pl, _ := fishSetup(t, [2]enchApply{})
	b := poolBobber(h, pl)
	treasureIDs := map[int32]bool{
		itemByName["name_tag"]: true, itemByName["saddle"]: true,
		itemByName["nautilus_shell"]: true, itemByName["bow"]: true,
		itemEnchantedBook: true,
	}
	fishIDs := map[int32]bool{
		itemByName["cod"]: true, itemByName["salmon"]: true,
		itemByName["tropical_fish"]: true, itemByName["pufferfish"]: true,
	}

	b.openWater, b.luck = false, 0
	fish := 0
	for i := 0; i < 1000; i++ {
		st, isFish := h.rollFishingLoot(pl, b)
		if st.count == 0 {
			t.Fatal("fishing loot must never be empty")
		}
		if treasureIDs[st.item] {
			t.Fatalf("treasure (%d) must not bite outside open water", st.item)
		}
		if isFish != fishIDs[st.item] {
			t.Fatalf("isFish=%v disagrees with item %d", isFish, st.item)
		}
		if isFish {
			fish++
		}
	}
	if fish < 800 { // 85/95 ≈ 89% expected
		t.Fatalf("fish share collapsed without open water: %d/1000", fish)
	}

	b.openWater, b.luck = true, 3 // Luck of the Sea III: junk 4 / fish 82 / treasure 11
	treasure := 0
	for i := 0; i < 2000; i++ {
		if st, _ := h.rollFishingLoot(pl, b); treasureIDs[st.item] {
			treasure++
		}
	}
	if treasure < 50 {
		t.Fatalf("Luck of the Sea III in open water found almost no treasure: %d/2000", treasure)
	}
}

func TestJunkGearComesDamaged(t *testing.T) {
	h, pl, _ := fishSetup(t, [2]enchApply{})
	b := poolBobber(h, pl)
	boots := itemByName["leather_boots"]
	seen := false
	for i := 0; i < 500 && !seen; i++ {
		if st := h.rollFishJunk(b); st.item == boots {
			seen = true
			if st.dmg <= 0 || st.dmg > itemMaxDurability[boots] {
				t.Fatalf("junk boots should come used: dmg=%d of %d", st.dmg, itemMaxDurability[boots])
			}
		}
	}
	if !seen {
		t.Fatal("500 junk rolls never produced leather boots (weight 17%)")
	}
}

func TestFishAdvancementCriteria(t *testing.T) {
	var crit *advCriterion
	for i := range advTable {
		if advTable[i].id == "minecraft:husbandry/fishy_business" {
			for j := range advTable[i].criteria {
				if advTable[i].criteria[j].name == "cod" {
					crit = &advTable[i].criteria[j]
				}
			}
		}
	}
	if crit == nil {
		t.Fatal("fishy_business/cod criterion missing from the generated table")
	}
	if crit.unmatchable {
		t.Fatal("fishing_rod_hooked should be matchable now")
	}
	if !(advMatch{item: itemByName["cod"]}).criterion(crit) {
		t.Fatal("catching a cod should satisfy the cod criterion")
	}
	if (advMatch{item: itemByName["salmon"]}).criterion(crit) {
		t.Fatal("a salmon must not satisfy the cod criterion")
	}
}
