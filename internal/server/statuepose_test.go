package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The statue's loot table copies copper_golem_pose onto the item (copy_state
// into block_state), and BlockItem.updateBlockStateFromTag puts it back on
// the placed block: a sitting statue broken and set down again still sits.
func TestStatueKeepsPoseThroughBreakAndPlace(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	statue := worldgen.BlockBase("weathered_copper_golem_statue")
	sitting := withStatuePose(statue, 2)
	if statuePoseOf(sitting) != 2 {
		t.Fatalf("pose round trip: %d", statuePoseOf(sitting))
	}
	var x, y, z int
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		x, y, z = int(tr.x)+3, int(tr.y), int(tr.z)
		w.SetBlock(x, y, z, sitting)
		for id := range h.items {
			delete(h.items, id)
		}
	})
	// A player's break: the edit, then the loot roll (the session's order).
	w.SetBlock(x, y, z, worldgen.Air)
	h.post(evBlock{x: x, y: y, z: z, state: worldgen.Air, by: p.eid, broken: sitting})
	h.post(evDrop{x: x, y: y, z: z, state: sitting, by: p.eid, held: uint16(itemByName["iron_pickaxe"])})
	var drop invStack
	onHub(t, h, func() {
		for _, it := range h.items {
			if base, ok := protocol.BlockForItem(it.item); ok && isGolemStatue(base) {
				drop = it.stack()
			}
		}
	})
	if drop.item == 0 {
		t.Fatal("the statue dropped nothing")
	}
	if drop.golemPose != 2 {
		t.Fatalf("the dropped statue carries pose %d, want 2 (sitting)", drop.golemPose)
	}
	// The pose survives a save.
	if unpackStack(packStack(drop)).golemPose != 2 {
		t.Fatal("the pose did not survive packStack")
	}
	// Placed from the offhand (the main hand holds something else), the
	// statue comes back sitting.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemByName["stone"], count: 1}
		drop.count = 1
		tr.offhand = drop
	})
	w.SetBlock(x, y, z, statue) // the session's prediction: the default pose
	h.post(evBlock{x: x, y: y, z: z, state: statue, by: p.eid, placed: true})
	onHub(t, h, func() {})
	onHub(t, h, func() {
		if got := w.Block(x, y, z); statuePoseOf(got) != 2 {
			t.Fatalf("the placed statue has pose %d, want 2 (sitting)", statuePoseOf(got))
		}
	})
}
