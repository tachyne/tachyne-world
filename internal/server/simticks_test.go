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
	if due, _ := h.blockTickDue(0, pos, frostedIceMin); due != now+30 {
		t.Errorf("pending tick due %d, want %d (the first one kept)", due, now+30)
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
	due, ok := h.blockTickDue(0, pos, frostedIceMin)
	if !ok {
		t.Fatal("placing frosted ice booked no tick")
	}
	if due < now+frostedFirstMin || due > now+frostedFirstMin+frostedFirstSpan-1 {
		t.Errorf("first tick due in %d, want 60-120", due-now)
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
// the old one, whose tick just drops.
func TestBlockTickSkippedWhenItsBlockIsGone(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	tube, _, _ := worldgen.BlockRangeOK("tube_coral_block")
	brain, _, _ := worldgen.BlockRangeOK("brain_coral_block")
	w.SetBlock(pos.x, pos.y, pos.z, tube)
	h.scheduleCoralDeath(0, pos)
	due, ok := h.blockTickDue(0, pos, tube)
	if !ok {
		t.Fatal("the dry coral booked no die tick")
	}
	w.SetBlock(pos.x, pos.y, pos.z, brain)
	stepTicks(h, players, int(due-h.tick.Load()))
	if w.At(pos.x, pos.y, pos.z) != brain {
		t.Fatal("the tube coral's tick bleached the brain coral that replaced it")
	}
	if _, ok := h.blockTickDue(0, pos, tube); ok {
		t.Error("the tube coral's tick is still pending after its trigger tick")
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

// runBlockTickNow jumps the clock to the trigger tick of the tick pending
// for the block at pos and runs that game tick, as the hub would.
func runBlockTickNow(t *testing.T, h *hub, players map[int32]*tracked, dim int, pos blockPos) {
	t.Helper()
	w := h.worldFor(dim)
	due, ok := h.blockTickDue(dim, pos, w.At(pos.x, pos.y, pos.z))
	if !ok {
		t.Fatalf("no tick pending at %v", pos)
	}
	w.ForceLoad(pos.x, pos.z, 2)
	h.tick.Store(due - 1)
	stepTicks(h, players, 1)
}

// LecternBlock.signalPageChange asks for the 2-tick tick that ends the
// pulse; a page turned while it is pending leaves it, so the pulse ends two
// ticks after the first turn, not the last.
func TestLecternPulseKeepsItsFirstTick(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	pos := blockPos{x + 3, y, z}
	w.SetBlock(pos.x, pos.y, pos.z, setBoolProp(worldgen.BlockBase("lectern"), "powered", false))
	h.lecternPulse(players, simPos{blockPos: pos})
	if !boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
		t.Fatal("a page turn did not power the lectern")
	}
	stepTicks(h, players, 1)
	h.lecternPulse(players, simPos{blockPos: pos})
	stepTicks(h, players, 1)
	if boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
		t.Error("the second turn pushed the pulse's end back")
	}
}

// A button's unpress is its scheduled tick: a neighbour's update in the
// middle of the press does not release it, and it pops up on time.
func TestButtonUnpressIsItsTick(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	pos := blockPos{x + 3, y, z}
	btn := worldgen.BlockBase("stone_button")
	info, _ := worldgen.InfoForState(btn)
	btn = setBoolProp(worldgen.SetProperty(info, worldgen.SetProperty(info, btn, "face", "floor"), "facing", "north"), "powered", false)
	w.SetBlock(pos.x, pos.y, pos.z, btn)
	ticks, _, _, _ := buttonKind(btn)
	h.inDim(0, func() { h.pressButton(players, pos, btn, nil) })
	if !boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
		t.Fatal("the button did not press")
	}
	due, ok := h.blockTickDue(0, pos, btn)
	if !ok || due != h.tick.Load()+uint64(ticks) {
		t.Fatalf("the unpress is due %d (pending %v), want %d out", due, ok, ticks)
	}
	stepTicks(h, players, ticks-1)
	h.notifyAround(players, 0, pos)
	if !boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
		t.Fatal("the button came up early")
	}
	stepTicks(h, players, 1)
	if boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
		t.Error("the button stayed down past its press")
	}
}
