package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// JukeboxBlock.getSignal: a playing jukebox is a redstone source in its own
// right, a full 15 to every side. That is a different thing from the
// comparator reading, which says WHICH disc is in it — the engine had the
// comparator and not the signal.
func TestPlayingJukeboxPowersRedstone(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	const x, y, z = 60, 180, 60
	for dx := -1; dx <= 1; dx++ {
		w.SetBlock(x+dx, y-1, z, worldgen.Stone)
		w.SetBlock(x+dx, y, z, worldgen.Air)
	}
	w.SetBlock(x, y, z, worldgen.BlockBase("jukebox"))
	pos := simPos{blockPos: blockPos{x, y, z}}
	h.tick.Store(100)

	// Empty: no signal.
	if got := h.emitPower(x, y, z, x+1, y, z); got != 0 {
		t.Errorf("an empty jukebox emits %d, want 0", got)
	}

	// Playing: 15.
	h.jukeboxes[pos] = &jukebox{disc: invStack{item: int32(itemByName["music_disc_cat"]), count: 1},
		started: 50, length: 1000}
	if got := h.emitPower(x, y, z, x+1, y, z); got != 15 {
		t.Errorf("a playing jukebox emits %d, want 15", got)
	}
	if !h.isSignalSource(w.At(x, y, z)) {
		t.Error("a jukebox is not counted as a signal source at all")
	}

	// Finished: back to nothing, without the disc being taken out.
	h.tick.Store(2000)
	if got := h.emitPower(x, y, z, x+1, y, z); got != 0 {
		t.Errorf("a finished jukebox still emits %d, want 0", got)
	}
}
