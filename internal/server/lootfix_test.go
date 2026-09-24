package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Tall grass and large ferns: shears cut two of the small kind, a bare hand
// gets wheat seeds one time in eight from the lower half and nothing from
// the upper; snow layers give a snowball a layer, or the layers themselves
// to shears or Silk Touch; a chorus flower drops nothing.
func TestSpecialBlockDrops(t *testing.T) {
	h := newHub(world.New(1))
	if ds, ok := h.specialBlockDrops(tallGrassHi, int32(itemShears), false); !ok || len(ds) != 1 || ds[0].item != itemShortGrass || ds[0].count != 2 {
		t.Fatalf("shears on tall grass: %+v ok=%v", ds, ok)
	}
	if ds, ok := h.specialBlockDrops(largeFernLo, int32(itemShears), false); !ok || ds[0].item != itemFernItem || ds[0].count != 2 {
		t.Fatalf("shears on a large fern: %+v", ds)
	}
	seeds, plants := 0, 0
	for i := 0; i < 4000; i++ {
		for _, d := range h.rollDrops(tallGrassHi) {
			switch d.item {
			case itemWheatSeeds:
				seeds++
			default:
				plants++
			}
		}
		if ds := h.rollDrops(tallGrassLo); len(ds) != 0 {
			t.Fatalf("the upper half drops nothing: %+v", ds)
		}
	}
	if plants != 0 || seeds < 350 || seeds > 650 {
		t.Fatalf("bare-handed tall grass: %d seeds in 4000 (want ~500), %d plant items", seeds, plants)
	}
	three := snowLayer1 + 2
	if ds, ok := h.specialBlockDrops(three, 0, false); !ok || ds[0].item != itemSnowball || ds[0].count != 3 {
		t.Fatalf("three snow layers by hand: %+v", ds)
	}
	if ds, _ := h.specialBlockDrops(three, 0, true); ds[0].item != itemSnowLayer || ds[0].count != 3 {
		t.Fatalf("three snow layers with Silk Touch: %+v", ds)
	}
	if ds := h.rollDrops(three); ds[0].item != itemSnowball || ds[0].count != 3 {
		t.Fatalf("snow in the fallback path: %+v", ds)
	}
	if ds, ok := h.specialBlockDrops(worldgen.BlockBase("chorus_flower"), 0, false); !ok || len(ds) != 1 || ds[0].item != itemChorusFlower {
		t.Fatalf("a chorus flower a player breaks drops itself: %+v", ds)
	}
	if _, ok := h.specialBlockDrops(worldgen.Stone, 0, false); ok {
		t.Fatal("stone is not special")
	}
}

// Guardians drop by vanilla's table: shards, then cod or crystals or
// nothing, and the elder adds a sponge for a player's kill and a tide
// template one time in five.
func TestGuardianLoot(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	g := h.spawnMob(players, entityGuardian, 0.5, 40, 0.5)
	e := h.spawnMob(players, entityElderGuardian, 0.5, 40, 0.5)
	count := func(m *mob, n int) map[int32]int {
		c := map[int32]int{}
		for i := 0; i < n; i++ {
			for _, d := range h.mobLoot(m) {
				c[d.item] += d.count
			}
		}
		return c
	}
	c := count(g, 3000)
	if c[itemPrismarineCrystals] < 900 || c[itemCodItem] < 900 || c[itemWetSponge] != 0 || c[itemTideTemplate] != 0 {
		t.Fatalf("guardian loot over 3000: %v", c)
	}
	c = count(e, 3000)
	if c[itemWetSponge] != 0 {
		t.Fatal("no sponge unless a player made the kill")
	}
	if c[itemTideTemplate] < 450 || c[itemTideTemplate] > 750 {
		t.Fatalf("tide template one in five: %d of 3000", c[itemTideTemplate])
	}
	e.hitByPlayer = true
	if c = count(e, 100); c[itemWetSponge] != 100 {
		t.Fatalf("a player's elder kill always pays a sponge: %v", c)
	}
	g.burning = true
	if c = count(g, 3000); c[itemCodItem] != 0 || c[itemCookedCodItem] < 900 {
		t.Fatalf("a burning guardian drops cooked cod: %v", c)
	}
}

// A creeper a skeleton shot drops a music disc, a burning sheep's mutton
// comes cooked, and a turtle struck by lightning leaves a bowl.
func TestSkeletonKillDiscBurningSheepAndLightningTurtle(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	creeper := h.spawnMob(players, entityCreeper, 0.5, 40, 0.5)
	skel := h.spawnMob(players, entitySkeleton, 5.5, 40, 0.5)
	discs := 0
	for i := 0; i < 20; i++ {
		for _, d := range h.mobLoot(creeper) {
			if d.item != itemGunpowder {
				discs++
			}
		}
	}
	if discs != 0 {
		t.Fatal("no disc without a skeleton's kill")
	}
	creeper.lastAttacker = skel.eid
	discs = 0
	for i := 0; i < 20; i++ {
		for _, d := range h.mobLoot(creeper) {
			if d.item != itemGunpowder {
				discs++
			}
		}
	}
	if discs != 20 {
		t.Fatalf("a skeleton's kill always drops a disc: %d of 20", discs)
	}
	sheep := h.spawnMob(players, entitySheep, 0.5, 40, 0.5)
	sheep.burning = true
	cooked := false
	for _, d := range h.mobLoot(sheep) {
		if d.item == itemCookedMutton {
			cooked = true
		}
		if d.item == itemMutton {
			t.Fatal("a burning sheep's mutton is cooked")
		}
	}
	if !cooked {
		t.Fatal("no cooked mutton")
	}
	turtle := h.spawnMob(players, entityTurtle, 0.5, 40, 0.5)
	turtle.hurtKind(1, dtLightningBolt)
	bowl := false
	for _, d := range h.mobLoot(turtle) {
		if d.item == itemBowlItem {
			bowl = true
		}
	}
	if !bowl {
		t.Fatal("lightning on a turtle leaves a bowl")
	}
}
