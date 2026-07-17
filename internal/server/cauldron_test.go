package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// bucketSetup: a survival player at (496.5,70,500.5) facing east (+x), with
// the given stack in hotbar slot 0.
func bucketSetup(t *testing.T, held invStack) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 496.5, 200, 500.5
	pl.yaw = -90 // facing +x
	pl.p.setHotbarSlot(0, held.item)
	pl.inv.slots[0] = held
	return h, pl, map[int32]*tracked{1: pl}
}

func TestBucketScoopAndPour(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucket, count: 1})
	h.world.SetBlock(499, 201, 500, worldgen.WaterBase) // eye-height source ahead
	h.bucketFill(players, pl, 0)
	if h.world.At(499, 201, 500) != worldgen.Air {
		t.Fatal("scooping should remove the source block")
	}
	if pl.inv.slots[0].item != itemBucketH2O {
		t.Fatalf("scoop should fill the bucket, got item %d", pl.inv.slots[0].item)
	}
	h.bucketEmpty(players, pl, 0, 499, 201, 500)
	if h.world.At(499, 201, 500) != worldgen.WaterBase {
		t.Fatal("pouring should place a water source")
	}
	if pl.inv.slots[0].item != itemBucket {
		t.Fatalf("pouring should empty the bucket, got item %d", pl.inv.slots[0].item)
	}
}

func TestBucketScoopIgnoresFlowingAndStopsAtSolid(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucket, count: 1})
	h.world.SetBlock(498, 201, 500, worldgen.WaterBase+3) // flowing: ray passes through
	h.world.SetBlock(499, 201, 500, worldgen.BlockBase("stone"))
	h.world.SetBlock(500, 201, 500, worldgen.WaterBase) // a source BEHIND the wall
	h.bucketFill(players, pl, 0)
	if pl.inv.slots[0].item != itemBucket {
		t.Fatal("no source before the wall — the bucket must stay empty")
	}
	if h.world.At(500, 201, 500) != worldgen.WaterBase {
		t.Fatal("the source behind the wall must be untouched")
	}
}

func TestBucketWaterEvaporatesInNether(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucketH2O, count: 1})
	pl.dim = 1
	h.bucketEmpty(players, pl, 0, 499, 201, 500)
	if h.worldFor(1).At(499, 201, 500) == worldgen.WaterBase {
		t.Fatal("water must not survive the nether")
	}
	if pl.inv.slots[0].item != itemBucket {
		t.Fatal("the bucket still empties when the water boils off")
	}
}

func TestBucketStackSplitsOnScoop(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucket, count: 3})
	h.world.SetBlock(499, 201, 500, worldgen.WaterBase)
	h.bucketFill(players, pl, 0)
	if pl.inv.slots[0].item != itemBucket || pl.inv.slots[0].count != 2 {
		t.Fatalf("stack should shrink to 2 empties, got %d×%d", pl.inv.slots[0].count, pl.inv.slots[0].item)
	}
	found := false
	for i := 1; i < len(pl.inv.slots); i++ {
		if pl.inv.slots[i].item == itemBucketH2O {
			found = true
		}
	}
	if !found {
		t.Fatal("the filled bucket should land elsewhere in the inventory")
	}
}

func cauldronAt(h *hub, x, y, z int, state uint32) { h.world.SetBlock(x, y, z, state) }

func TestCauldronBucketCycle(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucketH2O, count: 1})
	cauldronAt(h, 499, 200, 500, cauldronState)
	h.useCauldron(players, pl, 0, 499, 200, 500)
	if h.world.At(499, 200, 500) != waterCauldronBase+2 {
		t.Fatalf("water bucket should fill the cauldron to 3, state=%d", h.world.At(499, 200, 500))
	}
	if pl.inv.slots[0].item != itemBucket {
		t.Fatal("the bucket should be empty after filling the cauldron")
	}
	h.useCauldron(players, pl, 0, 499, 200, 500) // scoop it back out
	if h.world.At(499, 200, 500) != cauldronState {
		t.Fatal("an empty bucket should drain a full water cauldron")
	}
	if pl.inv.slots[0].item != itemBucketH2O {
		t.Fatal("draining should hand back a water bucket")
	}
}

func TestCauldronBottleCycle(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemGlassBottle, count: 1})
	cauldronAt(h, 499, 200, 500, waterCauldronBase+2) // full
	h.useCauldron(players, pl, 0, 499, 200, 500)
	if h.world.At(499, 200, 500) != waterCauldronBase+1 {
		t.Fatalf("a bottle takes one level, state=%d", h.world.At(499, 200, 500))
	}
	if pl.inv.slots[0].item != itemPotion || pl.inv.slots[0].potion != potWater {
		t.Fatalf("bottle should become a water bottle, got %+v", pl.inv.slots[0])
	}
	pl.p.setHotbarSlot(0, itemPotion)
	h.useCauldron(players, pl, 0, 499, 200, 500) // pour it back
	if h.world.At(499, 200, 500) != waterCauldronBase+2 {
		t.Fatal("a water bottle should raise the level back to 3")
	}
	if pl.inv.slots[0].item != itemGlassBottle {
		t.Fatal("pouring the bottle should hand back glass")
	}
}

func TestCauldronLavaBucketOut(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucket, count: 1})
	cauldronAt(h, 499, 200, 500, lavaCauldronState)
	h.useCauldron(players, pl, 0, 499, 200, 500)
	if h.world.At(499, 200, 500) != cauldronState || pl.inv.slots[0].item != itemBucketLav {
		t.Fatalf("lava cauldron should yield a lava bucket, state=%d item=%d",
			h.world.At(499, 200, 500), pl.inv.slots[0].item)
	}
}

func TestCauldronBannerWash(t *testing.T) {
	banner := invStack{item: itemByName["white_banner"], count: 1}
	banner.pats[0] = bannerLayer{patPlus1: 3, color: 1}
	banner.pats[1] = bannerLayer{patPlus1: 5, color: 2}
	h, pl, players := bucketSetup(t, banner)
	cauldronAt(h, 499, 200, 500, waterCauldronBase) // level 1
	h.useCauldron(players, pl, 0, 499, 200, 500)
	if pl.inv.slots[0].patCount() != 1 {
		t.Fatalf("washing should strip the top layer, %d layers left", pl.inv.slots[0].patCount())
	}
	if h.world.At(499, 200, 500) != cauldronState {
		t.Fatal("washing from level 1 should empty the cauldron")
	}
}

func TestCauldronRainFill(t *testing.T) {
	h, pl, players := bucketSetup(t, invStack{item: itemBucket, count: 1})
	_ = pl
	pos := blockPos{499, 200, 500}
	cauldronAt(h, pos.x, pos.y, pos.z, cauldronState)
	for i := 0; i < 2000 && h.world.At(pos.x, pos.y, pos.z) != waterCauldronBase+2; i++ {
		h.cauldronPrecip(players, pos, h.world.At(pos.x, pos.y, pos.z), false)
	}
	if h.world.At(pos.x, pos.y, pos.z) != waterCauldronBase+2 {
		t.Fatalf("rain should eventually fill the cauldron to 3, state=%d", h.world.At(pos.x, pos.y, pos.z))
	}
}
