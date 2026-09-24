package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Breaking a full shulker box by hand, through the hub's own event order
// (the block change, then the drop): the box comes back with its contents
// and nothing scatters.
func TestShulkerBoxHandBreakRealPath(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pos := blockPos{0, 100, 0}
	box := worldgen.BlockID("shulker_box")
	c := &chest{}
	c.slots[0] = invStack{item: itemByName["diamond"], count: 5}
	h.chests[simPos{blockPos: pos}] = c
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.Air)
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: worldgen.Air, by: pl.p.eid, broken: box})
	h.dropShulkerBox(players, 0, box, pos) // what evDrop does for a shulker box
	var boxes, loose int
	for _, it := range h.items {
		switch {
		case it.item == itemByName["shulker_box"] && it.boxID != 0:
			boxes++
		case it.item == itemByName["diamond"]:
			loose++
		}
	}
	if boxes != 1 || loose != 0 {
		t.Errorf("hand-broken full shulker box: %d boxes with contents, %d loose diamond stacks; want 1 and 0", boxes, loose)
	}
}

// A blast takes the box the same way: setBlockAt removes it, then the
// explosion's loot drop carries the contents.
func TestShulkerBoxExplodedKeepsContents(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	h.playersRef = players
	pos := blockPos{0, 100, 0}
	box := worldgen.BlockID("shulker_box")
	h.world.SetBlock(pos.x, pos.y, pos.z, box)
	c := &chest{}
	c.slots[3] = invStack{item: itemByName["diamond"], count: 5}
	h.chests[simPos{blockPos: pos}] = c
	h.setBlockAt(players, 0, pos, worldgen.Air)
	h.dropExploded(players, 0, pos, box, 4, blastTNT)
	var boxes, loose int
	for _, it := range h.items {
		switch {
		case it.item == itemByName["shulker_box"] && it.boxID != 0:
			boxes++
		case it.item == itemByName["diamond"]:
			loose++
		}
	}
	if boxes != 1 || loose != 0 {
		t.Errorf("exploded full shulker box: %d boxes with contents, %d loose diamond stacks; want 1 and 0", boxes, loose)
	}
}

// A piston destroys a shulker box in its way (PushReaction.DESTROY) and the
// box drops with its contents, as Block.dropResources gives it its block
// entity. Holding the contents aside on removal had them vanish here: the
// piston rolled the drop before it cleared the cell.
func TestShulkerBoxBrokenByPistonKeepsContents(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	box := worldgen.BlockID("shulker_box")
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, box)
	c := &chest{}
	c.slots[2] = invStack{item: itemByName["diamond"], count: 5}
	h.chests[simPos{blockPos: blockPos{x + 1, y, z}}] = c
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	var boxes, loose int
	for _, it := range h.items {
		switch {
		case it.item == itemByName["shulker_box"] && it.boxID != 0:
			boxes++
		case it.item == itemByName["diamond"]:
			loose++
		}
	}
	if boxes != 1 || loose != 0 {
		t.Errorf("piston-broken shulker box: %d boxes with contents, %d loose diamond stacks; want 1 and 0", boxes, loose)
	}
}
