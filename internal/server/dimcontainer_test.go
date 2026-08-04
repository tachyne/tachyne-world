package server

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Block-entity storage used to be keyed by x/y/z alone, so the same
// coordinates in two dimensions named ONE container. Everything here is a
// regression guard on that: a Nether build must be its own.

func dimStoreHub(t *testing.T) (*hub, map[int32]*tracked, *tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	over, nether := testTracked(), testTracked()
	nether.p.eid = 2
	nether.dim = dimNether
	players := map[int32]*tracked{over.p.eid: over, nether.p.eid: nether}
	h.playersRef = players
	return h, players, over, nether
}

func TestNetherChestIsNotTheOverworldChest(t *testing.T) {
	h, _, over, nether := dimStoreHub(t)
	pos := blockPos{10, 70, 10}
	h.world.SetBlock(pos.x, pos.y, pos.z, chestStateMin)
	h.nether.SetBlock(pos.x, pos.y, pos.z, chestStateMin)

	h.openChest(over, pos.x, pos.y, pos.z)
	if over.viewChest == nil {
		t.Fatal("the overworld chest did not open")
	}
	over.viewChest.slots[0] = invStack{item: 1, count: 5}

	h.openChest(nether, pos.x, pos.y, pos.z)
	if nether.viewChest == nil {
		t.Fatal("the Nether chest did not open")
	}
	if nether.viewChest.slots[0].count != 0 {
		t.Error("the Nether chest is showing the overworld chest's contents")
	}
	if nether.viewChest == over.viewChest {
		t.Error("both dimensions share one chest object")
	}
}

func TestNetherFurnaceIsNotTheOverworldFurnace(t *testing.T) {
	h, _, over, nether := dimStoreHub(t)
	pos := blockPos{-3, 40, 8}
	h.world.SetBlock(pos.x, pos.y, pos.z, furnaceStateMin)
	h.nether.SetBlock(pos.x, pos.y, pos.z, furnaceStateMin)

	h.openFurnace(over, pos.x, pos.y, pos.z)
	h.furnaces[simPos{blockPos: pos}].slots[furnaceInput] = invStack{item: 1, count: 3}

	h.openFurnace(nether, pos.x, pos.y, pos.z)
	if f := h.furnaces[simPos{dim: dimNether, blockPos: pos}]; f == nil {
		t.Fatal("the Nether furnace was not created")
	} else if f.slots[furnaceInput].count != 0 {
		t.Error("the Nether furnace inherited the overworld furnace's input")
	}
}

// A beacon outside the overworld used to be dropped on the floor by the
// dim != 0 early return in onBlock, so it never opened or ticked.
func TestBeaconRegistersOutsideTheOverworld(t *testing.T) {
	h, players, _, _ := dimStoreHub(t)
	h.nether.SetBlock(4, 40, 4, beaconState)
	h.onBlock(players, evBlock{dim: dimNether, x: 4, y: 40, z: 4, state: beaconState})
	if h.beacons[simPos{dim: dimNether, blockPos: blockPos{4, 40, 4}}] == nil {
		t.Error("a Nether beacon was never registered")
	}
	if h.beacons[simPos{blockPos: blockPos{4, 40, 4}}] != nil {
		t.Error("it registered against the overworld instead")
	}
}

// Mining in the Nether used to post the loot into the overworld: the drop
// helper had no dimension to work with and defaulted to dim 0.
func TestBlockDropsStayInTheirDimension(t *testing.T) {
	h, players, _, _ := dimStoreHub(t)
	h.spawnBlockDrop(players, dimNether, itemByName["cobblestone"], 1, 6, 40, 6)
	if len(h.items) == 0 {
		t.Fatal("mining dropped nothing at all")
	}
	for _, it := range h.items {
		if it.dim != dimNether {
			t.Errorf("a block mined in the Nether dropped into dim %d", it.dim)
		}
	}
}

// containers.json keys carry the dimension, and files written before they did
// read back as the overworld — where every one of those containers stood.
func TestContainerStoreKeysCarryTheDimension(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	s := newContainerStore(path)
	over := simPos{blockPos: blockPos{1, 2, 3}}
	nether := simPos{dim: dimNether, blockPos: blockPos{1, 2, 3}}
	oc, nc := &chest{}, &chest{}
	oc.slots[0] = invStack{item: 11, count: 1}
	nc.slots[0] = invStack{item: 22, count: 2}
	s.recordChests(map[simPos]*chest{over: oc, nether: nc})
	s.flush()

	got := newContainerStore(path).loadChests()
	if c := got[over]; c == nil || c.slots[0].item != 11 {
		t.Errorf("overworld chest: %+v", c)
	}
	if c := got[nether]; c == nil || c.slots[0].item != 22 {
		t.Errorf("Nether chest: %+v", c)
	}
	if len(got) != 2 {
		t.Errorf("%d chests restored, want the two distinct ones", len(got))
	}
}

func TestContainerStoreReadsPreDimensionKeys(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	legacy := map[string]any{"chests": map[string][]containerRow{
		"7,64,-8": {slotRow(0, invStack{item: 42, count: 9})},
	}}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	got := newContainerStore(path).loadChests()
	c := got[simPos{blockPos: blockPos{7, 64, -8}}]
	if c == nil {
		t.Fatal("a chest saved before the dimension column was lost")
	}
	if c.slots[0].item != 42 || c.slots[0].count != 9 {
		t.Errorf("contents mangled: %+v", c.slots[0])
	}
}

// The banner and campfire render views only rewrite the entries they touch, so
// their pre-dimension keys are migrated when the file is read — otherwise a
// banner placed before this change would render plain forever.
func TestRenderViewsMigratePreDimensionKeys(t *testing.T) {
	dir := t.TempDir()
	bp, cp := dir+"/banners.json", dir+"/campfires.json"
	bd, _ := json.Marshal(map[string][]map[string]string{
		"3,70,3": {{"pattern": "minecraft:stripe_bottom", "color": "red"}},
	})
	cd, _ := json.Marshal(map[string]campfireItems{
		"5,70,5": {Items: [4]string{"minecraft:beef"}},
	})
	if err := os.WriteFile(bp, bd, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, cd, 0o644); err != nil {
		t.Fatal(err)
	}
	if ls := newBannerStore(bp).get(dimOverworld, 3, 70, 3); len(ls) != 1 {
		t.Errorf("banner layers %v — a pre-dimension banner was lost", ls)
	}
	if ci, ok := newCampfireStore(cp).get(dimOverworld, 5, 70, 5); !ok || ci.Items[0] == "" {
		t.Errorf("campfire %+v ok=%v — a pre-dimension campfire was lost", ci, ok)
	}
}
