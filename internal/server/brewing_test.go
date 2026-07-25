package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func brewSetup(t *testing.T) (*hub, map[int32]*tracked, blockPos, *bin) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	pos := blockPos{10, 70, 10}
	h.world.SetBlock(pos.x, pos.y, pos.z, brewStandMax)
	b := &bin{slots: make([]invStack, 5)}
	h.bins[pos] = b
	return h, players, pos, b
}

func TestBrewWaterToAwkwardToStrength(t *testing.T) {
	h, players, _, b := brewSetup(t)
	b.slots[0] = potionStack(potWater)
	b.slots[1] = potionStack(potWater)
	b.slots[3] = invStack{item: itemNetherWart, count: 2}
	b.slots[4] = invStack{item: itemBlazePowder, count: 2}
	for i := 0; i < brewTicks/survivalTickN+1; i++ {
		h.updateBrewing(players)
	}
	if b.slots[0].potion != potAwkward || b.slots[1].potion != potAwkward {
		t.Fatalf("water+wart should brew awkward: %+v %+v", b.slots[0], b.slots[1])
	}
	if b.slots[3].count != 1 || b.slots[4].count != 1 {
		t.Fatalf("one wart + one powder consumed: ing=%d fuel=%d", b.slots[3].count, b.slots[4].count)
	}
	// Second stage: awkward + blaze powder ingredient → strength.
	b.slots[3] = invStack{item: itemBlazePowder, count: 1}
	for i := 0; i < brewTicks/survivalTickN+1; i++ {
		h.updateBrewing(players)
	}
	if b.slots[0].potion != potStrength || b.slots[0].name != "Potion of Strength" {
		t.Fatalf("awkward+powder should brew strength: %+v", b.slots[0])
	}
}

func TestBrewNeedsFuelAndValidIngredient(t *testing.T) {
	h, players, pos, b := brewSetup(t)
	b.slots[0] = potionStack(potWater)
	b.slots[3] = invStack{item: itemNetherWart, count: 1}
	// No fuel: nothing happens.
	for i := 0; i < 30; i++ {
		h.updateBrewing(players)
	}
	if b.slots[0].potion != potWater {
		t.Fatal("brewing without blaze powder must not proceed")
	}
	if h.brewProg[pos] != 0 {
		t.Fatal("no progress without fuel")
	}
	// Wrong ingredient on water: idle.
	b.slots[4] = invStack{item: itemBlazePowder, count: 1}
	b.slots[3] = invStack{item: itemSugarBrew, count: 1}
	for i := 0; i < 30; i++ {
		h.updateBrewing(players)
	}
	if b.slots[0].potion != potWater {
		t.Fatal("sugar on a water bottle is not a recipe")
	}
}

func TestDrinkPotionAppliesEffectAndReturnsBottle(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.inv.slots[0] = potionStack(potFireRes)
	h.drinkPotion(nil, pl, 0)
	if pl.inv.slots[0].item != itemGlassBottle {
		t.Fatalf("drinking should return the bottle: %+v", pl.inv.slots[0])
	}
	if pl.hasEffect(effFireRes) == 0 {
		t.Fatal("fire resistance should be active after drinking")
	}
}

// Nether wart advances on a RANDOM tick with a 1-in-10 roll
// (NetherWartBlock.randomTick), not on a self-rearming schedule. The old
// scheduled path ignored the randomTickSpeed gamerule and ran ~3x too fast.
func TestWartGrowsOnRandomTick(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := blockPos{5, 70, 5}
	h.world.SetBlock(pos.x, pos.y-1, pos.z, worldgen.SoulSand)
	h.world.SetBlock(pos.x, pos.y, pos.z, netherWartMin)

	for i := 0; i < 500 && h.world.At(pos.x, pos.y, pos.z) < netherWartMax; i++ {
		h.tickWart(players, 0, pos.x, pos.y, pos.z, h.world.At(pos.x, pos.y, pos.z))
	}
	if got := h.world.At(pos.x, pos.y, pos.z); got != netherWartMax {
		t.Fatalf("wart reached state %d, want max %d", got, netherWartMax)
	}
}

// The 1-in-10 gate must actually gate: a single tick rarely advances.
func TestWartAdvancesAtVanillaRate(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	const trials = 4000
	advanced := 0
	for i := 0; i < trials; i++ {
		x, y, z := 100+i%40, 70, 100+i/40
		h.world.SetBlock(x, y, z, netherWartMin)
		h.tickWart(players, 0, x, y, z, netherWartMin)
		if h.world.At(x, y, z) != netherWartMin {
			advanced++
		}
	}
	if rate := float64(advanced) / trials; rate < 0.07 || rate > 0.13 {
		t.Errorf("wart advance rate %.3f, want ~0.10 (1 in 10)", rate)
	}
}

// Growth must honour the randomTickSpeed gamerule, which the scheduled path
// bypassed entirely — including 0, which has to stop growth dead.
func TestWartRespectsRandomTickSpeedZero(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.RandomTicks = 0
	players := map[int32]*tracked{}
	p := testTracked()
	p.x, p.y, p.z, p.dim = 8, 70, 8, 0
	players[1] = p

	h.world.SetBlock(8, 69, 8, worldgen.SoulSand)
	h.world.SetBlock(8, 70, 8, netherWartMin)
	for i := 0; i < 2000; i++ {
		h.runRandomTicks(players)
	}
	if got := h.world.At(8, 70, 8); got != netherWartMin {
		t.Errorf("wart grew to %d with randomTickSpeed=0", got)
	}
}

func TestWildWartGeneratesOnSoulSand(t *testing.T) {
	g := worldgen.NewNetherGenerator(7)
	found := false
	for cx := int32(-6); cx <= 6 && !found; cx++ {
		for cz := int32(-6); cz <= 6 && !found; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if b >= worldgen.NetherWart && b <= worldgen.NetherWart+3 {
						found = true
						break
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("no wild nether wart in 13x13 chunks")
	}
}
