package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// simTickFixture is a ticking spot high above the seed-1 terrain.
func simTickFixture(t *testing.T) (*hub, *world.World, map[int32]*tracked, blockPos) {
	t.Helper()
	w := world.New(1)
	h := newTestHub(w)
	pos := blockPos{3000, 180, 3000}
	w.ForceLoad(pos.x, pos.z, 2)
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.Stone)
	return h, w, map[int32]*tracked{}, pos
}

// LevelChunkTicks.schedule: a second schedule for a (pos, block) already
// pending is ignored, and the first keeps its trigger tick; a different
// block at the cell gets a tick of its own.
func TestBlockTickRescheduleWhilePendingIsIgnored(t *testing.T) {
	h, w, _, pos := simTickFixture(t)
	w.SetBlock(pos.x, pos.y, pos.z, frostedIceMin)
	now := h.tick.Load()
	if !h.scheduleBlockTickIn(0, pos, 30) {
		t.Fatal("the first schedule was refused")
	}
	if h.scheduleBlockTickIn(0, pos, 5) {
		t.Error("a second schedule for the same block was added while one is pending")
	}
	key := simPos{dim: 0, blockPos: pos}
	if m := h.simTicks[key]; m.due != now+30 {
		t.Errorf("pending tick due %d, want %d (the first one kept)", m.due, now+30)
	}
	if !h.hasBlockTickIn(0, pos, frostedIceMin+1) {
		t.Error("an older age of the same block should count as the same block")
	}
	live, _, _ := worldgen.BlockRangeOK("tube_coral_block")
	w.SetBlock(pos.x, pos.y, pos.z, live)
	if h.hasBlockTickIn(0, pos, live) {
		t.Error("frosted ice's tick counted as the coral's")
	}
	if !h.scheduleBlockTickIn(0, pos, 5) {
		t.Error("a schedule for another block at the cell was ignored")
	}
}

// A neighbour's change reaching frosted ice through the simulation queue
// does not age it (only its scheduled tick does); it books a tick if the
// ice has none.
func TestNeighbourReactionDoesNotAgeFrostedIce(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	h.dayTime.Store(6000) // noon: bright enough that a tick would try to melt it
	w.SetBlock(pos.x, pos.y, pos.z, frostedIceMin)
	h.scheduleIn(0, pos, 1) // a neighbour reaction
	stepTicks(h, players, 1)
	if got := w.At(pos.x, pos.y, pos.z); got != frostedIceMin {
		t.Fatalf("a neighbour reaction aged the frosted ice to %d", got)
	}
	if !h.hasBlockTickIn(0, pos, frostedIceMin) {
		t.Error("ice with no tick pending was not booked one")
	}
}

// Frost Walker's write is the ice's onPlace: it books the first melt
// attempt 60-120 ticks out.
func TestFrostedIceOnPlaceBooksItsTick(t *testing.T) {
	h, _, players, pos := simTickFixture(t)
	now := h.tick.Load()
	h.setBlockAt(players, 0, pos, frostedIceMin)
	m, ok := h.simTicks[simPos{dim: 0, blockPos: pos}]
	if !ok {
		t.Fatal("placing frosted ice booked no tick")
	}
	if m.due < now+frostedFirstMin || m.due > now+frostedFirstMin+frostedFirstSpan-1 {
		t.Errorf("first tick due in %d, want 60-120", m.due-now)
	}
}

// CoralBlock.updateShape: a neighbour's change to a dry coral books its die
// tick; the coral bleaches on that tick, not on the change.
func TestCoralReactionBooksDieTickInsteadOfBleaching(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	live, _, _ := worldgen.BlockRangeOK("tube_coral_block")
	dead, _, _ := worldgen.BlockRangeOK("dead_tube_coral_block")
	w.SetBlock(pos.x, pos.y, pos.z, live)
	h.scheduleIn(0, pos, 1) // a neighbour reaction
	stepTicks(h, players, 1)
	if w.At(pos.x, pos.y, pos.z) != live {
		t.Fatal("a neighbour reaction bleached the coral at once")
	}
	if !h.hasBlockTickIn(0, pos, live) {
		t.Fatal("the reaction booked no die tick")
	}
	stepTicks(h, players, coralDieMin+coralDieVar)
	if w.At(pos.x, pos.y, pos.z) != dead {
		t.Error("the dry coral did not bleach on its die tick")
	}
}

// ServerLevel.tickBlock: a tick runs only for the block it was scheduled
// for. A coral swapped for another before its die tick is not bleached by
// the old one; the newcomer's own change books its own tick.
func TestBlockTickSkippedWhenItsBlockIsGone(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	tube, _, _ := worldgen.BlockRangeOK("tube_coral_block")
	brain, _, _ := worldgen.BlockRangeOK("brain_coral_block")
	w.SetBlock(pos.x, pos.y, pos.z, tube)
	h.scheduleCoralDeath(0, pos)
	m, ok := h.simTicks[simPos{dim: 0, blockPos: pos}]
	if !ok {
		t.Fatal("the dry coral booked no die tick")
	}
	w.SetBlock(pos.x, pos.y, pos.z, brain)
	stepTicks(h, players, int(m.due-h.tick.Load()))
	if w.At(pos.x, pos.y, pos.z) != brain {
		t.Fatal("the tube coral's tick bleached the brain coral that replaced it")
	}
	if !h.hasBlockTickIn(0, pos, brain) {
		t.Error("the stale tick's update should reach the brain coral as a change and book its own tick")
	}
}

// An eyeblossom switches on its scheduled tick, never on a neighbour's
// change.
func TestEyeblossomSwitchesOnlyOnItsTick(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	h.dayTime.Store(15000)                                        // night: a tick would open it
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.BlockBase("dirt")) // ground a flower stands on
	w.SetBlock(pos.x, pos.y, pos.z, closedEyeblossom)
	h.scheduleIn(0, pos, 1) // a neighbour reaction
	stepTicks(h, players, 1)
	if w.At(pos.x, pos.y, pos.z) != closedEyeblossom {
		t.Fatal("a neighbour reaction switched the eyeblossom")
	}
	h.scheduleBlockTickIn(0, pos, 3)
	stepTicks(h, players, 3)
	if w.At(pos.x, pos.y, pos.z) != openEyeblossom {
		t.Error("the eyeblossom's own tick did not open it at night")
	}
}
