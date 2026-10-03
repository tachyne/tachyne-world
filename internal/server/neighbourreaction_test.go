package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Vanilla's neighborChanged/updateShape run inside the change that caused
// them: water a player pours beside concrete powder sets it in the same
// tick, not the next.
func TestNeighbourReactionRunsInTheSameTick(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	powder := worldgen.BlockBase("white_concrete_powder")
	w.SetBlock(pos.x, pos.y, pos.z, powder)
	wp := blockPos{pos.x + 1, pos.y, pos.z}
	w.SetBlock(wp.x, wp.y, wp.z, worldgen.WaterBase)
	h.onBlock(players, evBlock{dim: 0, x: wp.x, y: wp.y, z: wp.z, state: worldgen.WaterBase, placed: true})
	if got := w.At(pos.x, pos.y, pos.z); got != worldgen.ConcreteFor(powder) {
		t.Fatalf("the powder is %d after the water's neighbour update, want concrete %d at once", got, worldgen.ConcreteFor(powder))
	}
}

// FrogspawnBlock.updateShape: a world-driven change (the bus, a plugin)
// that takes the water from under a clutch removes it inside that change.
func TestFrogspawnGoesWithItsWaterAtOnce(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.WaterBase)
	w.SetBlock(pos.x, pos.y, pos.z, frogspawnBlock)
	h.setBlockLive(players, 0, pos.x, pos.y-1, pos.z, worldgen.Stone)
	if got := w.At(pos.x, pos.y, pos.z); got != worldgen.Air {
		t.Fatalf("the clutch is still %d after its water went", got)
	}
}

// LeavesBlock.updateShape does not recompute a leaf's distance: it
// schedules the leaf's tick a tick out, which does — so a felled trunk
// still sends the change through the canopy a leaf a tick.
func TestLeafReactionSchedulesItsTick(t *testing.T) {
	h, w, players, pos := simTickFixture(t)
	base := worldgen.BlockBase("oak_leaves")
	leaf := base
	for s := base; s < base+4; s++ { // distance 1, not persistent, not waterlogged
		if _, d, persistent, ok := leafInfo(s); ok && d == 1 && !persistent && !worldgen.IsWaterlogged(s) {
			leaf = s
		}
	}
	log := worldgen.BlockBase("oak_log")
	lp := blockPos{pos.x - 1, pos.y, pos.z}
	w.SetBlock(lp.x, lp.y, lp.z, log)
	w.SetBlock(pos.x, pos.y, pos.z, leaf)
	w.SetBlock(lp.x, lp.y, lp.z, worldgen.Air)
	h.onBlock(players, evBlock{dim: 0, x: lp.x, y: lp.y, z: lp.z, state: worldgen.Air, broken: log})
	if got := w.At(pos.x, pos.y, pos.z); got != leaf {
		t.Fatalf("the leaf recomputed inside the change (%d), not on its tick", got)
	}
	due, ok := h.blockTickDue(0, pos, leaf)
	if !ok || due != h.tick.Load()+1 {
		t.Fatalf("the leaf's tick is due %d (pending %v), want a tick out", due, ok)
	}
	stepTicks(h, players, 1)
	if _, d, _, _ := leafInfo(w.At(pos.x, pos.y, pos.z)); d == 1 {
		t.Error("the leaf's tick did not move its distance off the felled log")
	}
}

// One tick list for every block: a tick for the fire burning in a cell and
// one for the sand that falls into it are two ticks (LevelChunkTicks keys
// them by position and block), so the fire's pending tick cannot swallow
// the sand's.
func TestBlockTicksKeyedByBlock(t *testing.T) {
	h, w, _, pos := simTickFixture(t)
	w.SetBlock(pos.x, pos.y, pos.z, worldgen.BlockBase("fire"))
	if !h.scheduleBlockTickIn(0, pos, 30) {
		t.Fatal("the fire's tick was refused")
	}
	sand := worldgen.BlockBase("sand")
	w.SetBlock(pos.x, pos.y, pos.z, sand)
	if !h.scheduleBlockTickIn(0, pos, 2) {
		t.Fatal("the sand's tick was refused because the fire's is pending")
	}
	if !h.hasBlockTickIn(0, pos, sand) || !h.hasBlockTickIn(0, pos, worldgen.BlockBase("fire")) {
		t.Error("the cell should hold both ticks")
	}
}
