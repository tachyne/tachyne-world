package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func eastState(t *testing.T, base uint32) uint32 {
	t.Helper()
	info, ok := worldgen.InfoForState(base)
	if !ok || !info.HasProperty("facing") {
		t.Fatal("block has no facing")
	}
	return worldgen.SetProperty(info, base, "facing", "east")
}

// TestDispenserBucketEmpty guards the #56 regression: a water/lava bucket must
// still pour its fluid (buckets are no longer placeable via BlockForItem).
func TestDispenserBucketEmpty(t *testing.T) {
	h := newHub(world.New(1))
	state := eastState(t, dispenserMin)
	pos, front := blockPos{5, 70, 5}, blockPos{6, 70, 5}
	for _, tc := range []struct {
		bucket int32
		fluid  uint32
	}{{itemBucketH2O, worldgen.WaterBase}, {itemBucketLav, worldgen.LavaBase}} {
		h.world.SetBlock(pos.x, pos.y, pos.z, state)
		h.world.SetBlock(front.x, front.y, front.z, worldgen.Air)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: tc.bucket, count: 1}
		h.bins[pos] = b
		h.ejectFromBin(nil, pos, state)
		if got := h.world.At(front.x, front.y, front.z); got != tc.fluid {
			t.Fatalf("bucket %d: front block %d, want fluid %d", tc.bucket, got, tc.fluid)
		}
		if b.slots[0].item != itemBucket {
			t.Fatalf("bucket %d: slot should hold an empty bucket, got %d", tc.bucket, b.slots[0].item)
		}
	}
}

func TestDispenserBucketPickup(t *testing.T) {
	h := newHub(world.New(1))
	state := eastState(t, dispenserMin)
	pos, front := blockPos{5, 70, 5}, blockPos{6, 70, 5}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	h.world.SetBlock(front.x, front.y, front.z, worldgen.WaterBase)
	b := &bin{slots: make([]invStack, 9)}
	b.slots[0] = invStack{item: itemBucket, count: 1}
	h.bins[pos] = b
	h.ejectFromBin(nil, pos, state)
	if h.world.At(front.x, front.y, front.z) != worldgen.Air {
		t.Fatal("scooping should clear the source ahead")
	}
	if b.slots[0].item != itemBucketH2O {
		t.Fatalf("empty bucket should become a water bucket, got %d", b.slots[0].item)
	}
}

func TestDropperPushesToContainer(t *testing.T) {
	h := newHub(world.New(1))
	state := eastState(t, dropperMin)
	pos, front := blockPos{5, 70, 5}, blockPos{6, 70, 5}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	dropper := &bin{slots: make([]invStack, 9)}
	dropper.slots[0] = invStack{item: int32(itemByName["stone"]), count: 5}
	h.bins[pos] = dropper
	dst := &chest{}
	h.chests[front] = dst

	h.ejectFromBin(nil, pos, state)
	if dropper.slots[0].count != 4 {
		t.Fatalf("dropper should move exactly one item, count now %d", dropper.slots[0].count)
	}
	moved := 0
	for _, s := range dst.slots {
		moved += s.count
	}
	if moved != 1 {
		t.Fatalf("target container should have received 1 item, got %d", moved)
	}
}

func TestDropperTossesWithoutContainer(t *testing.T) {
	h := newHub(world.New(1))
	state := eastState(t, dropperMin)
	pos, front := blockPos{5, 70, 5}, blockPos{6, 70, 5}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	h.world.SetBlock(front.x, front.y, front.z, worldgen.Air)
	dropper := &bin{slots: make([]invStack, 9)}
	dropper.slots[0] = invStack{item: int32(itemByName["stone"]), count: 5}
	h.bins[pos] = dropper
	h.items = map[int32]*itemEntity{}
	h.ejectFromBin(nil, pos, state)
	if len(h.items) != 1 {
		t.Fatalf("a dropper with no container ahead should toss the item, got %d drops", len(h.items))
	}
}

func TestDispenserRandomSlot(t *testing.T) {
	h := newHub(world.New(1))
	state := eastState(t, dispenserMin)
	pos, front := blockPos{5, 70, 5}, blockPos{6, 70, 5}
	// Two different items in two slots; over many fires both should be picked.
	picks := map[int32]int{}
	stone, dirt := int32(itemByName["stone"]), int32(itemByName["dirt"])
	for i := 0; i < 200; i++ {
		h.world.SetBlock(pos.x, pos.y, pos.z, state)
		h.world.SetBlock(front.x, front.y, front.z, worldgen.Air)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: stone, count: 1}
		b.slots[4] = invStack{item: dirt, count: 1}
		h.bins[pos] = b
		h.items = map[int32]*itemEntity{}
		h.ejectFromBin(nil, pos, state)
		// whichever slot emptied is the one that was picked
		if b.slots[0].count == 0 {
			picks[stone]++
		}
		if b.slots[4].count == 0 {
			picks[dirt]++
		}
	}
	if picks[stone] == 0 || picks[dirt] == 0 {
		t.Fatalf("random slot selection should hit both slots, got %v", picks)
	}
}
