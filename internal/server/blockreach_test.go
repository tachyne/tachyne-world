package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// handleUseItemOn and handleBlockBreakAction measure against
// block_interaction_range + 1: a click or a dig nine blocks out does nothing
// at the default reach, and works once the attribute reaches that far.
func TestBlockReachFollowsTheAttribute(t *testing.T) {
	blockReachChecked = true
	defer func() { blockReachChecked = false }()
	s, h, p := breakPlaceServer(t) // creative: 4.5 + 0.5
	w := s.world
	y := int(p.y)
	clearAirBox(w, 9, y+1, 0, 2)
	w.SetBlock(9, y, 0, worldgen.Stone)
	w.SetBlock(10, y, 0, worldgen.Stone)
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)

	s.handlePlace(p, placeBody(10, y, 0, 1))
	if w.Block(10, y+1, 0) != worldgen.Air {
		t.Fatal("a click past the reach placed a block")
	}
	s.handleDig(p, digBody(digStartBreak, 9, y, 0))
	if w.Block(9, y, 0) != worldgen.Stone {
		t.Fatal("a dig past the reach broke the block")
	}

	onHub(t, h, func() { h.playersRef[p.eid].playerAttrs().SetBase(attr.BlockInteractionRange, 12) })
	for i := 0; i < 200 && (p.reachMirror.Load() == nil || p.reachMirror.Load().reach < 12); i++ {
		time.Sleep(10 * time.Millisecond) // the hub's tick mirrors the reach
	}
	s.handlePlace(p, placeBody(10, y, 0, 1))
	if w.Block(10, y+1, 0) != worldgen.Stone {
		t.Fatal("a raised block_interaction_range should reach the click")
	}
	s.handleDig(p, digBody(digStartBreak, 9, y, 0))
	if w.Block(9, y, 0) == worldgen.Stone {
		t.Fatal("a raised block_interaction_range should reach the dig")
	}
}

// A click whose hit point lies outside the clicked block is dropped.
func TestUseItemOnHitPointInsideTheBlock(t *testing.T) {
	if !hitInBlock(0.5) || !hitInBlock(1) || !hitInBlock(0) {
		t.Fatal("a face point is inside the block")
	}
	if hitInBlock(1.6) || hitInBlock(-0.6) {
		t.Fatal("a point past the block is outside it")
	}
}

// Stands answer only within entity_interaction_range + 3.
func TestArmorStandNeedsEntityReach(t *testing.T) {
	h, players, st := standFixture(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 12.5, 180, 0.5
	h.hitStand(players, pl, st)
	h.tick.Add(1)
	h.hitStand(players, pl, st)
	if h.armorStands[st.eid] == nil {
		t.Fatal("a stand broke from twelve blocks away")
	}
	pl.playerAttrs().SetBase(attr.EntityInteractionRange, 12)
	h.hitStand(players, pl, st)
	h.tick.Add(1)
	h.hitStand(players, pl, st)
	if h.armorStands[st.eid] != nil {
		t.Fatal("a raised entity_interaction_range should reach the stand")
	}
}
