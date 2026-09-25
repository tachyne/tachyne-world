package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// JukeboxBlockEntity.onSongChanged: when the song ends the jukebox's signal
// drops, and its neighbours are told — a lamp beside it goes out.
func TestJukeboxSongEndUpdatesNeighbours(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, jukeboxState(true))
	w.SetBlock(x+1, y, z, lampOn)
	h.tick.Store(100)
	h.jukeboxes[simPos{blockPos: blockPos{x, y, z}}] = &jukebox{
		disc: invStack{item: itemByName["music_disc_13"], count: 1}, started: 40, length: 50}
	h.jukeboxTick(players)
	stepTicks(h, players, 8)
	if got := w.At(x+1, y, z); got != lampOff {
		t.Fatalf("the lamp beside a jukebox whose song ended is still %d", got)
	}
}

// A trapped chest in the Nether emits the count of its own viewers.
func TestTrappedChestSignalInItsOwnDimension(t *testing.T) {
	h := newHub(world.New(1))
	const x, y, z = 74, 80, 74
	trapped := worldgen.BlockID("trapped_chest")
	h.worldFor(dimNether).SetBlock(x, y, z, trapped)
	viewer := &tracked{p: newPlayer(9, "peek", [16]byte{}), gamemode: gmSurvival, dim: dimNether,
		winKind: winChest, winPos: simPos{dim: dimNether, blockPos: blockPos{x, y, z}}}
	h.playersRef = map[int32]*tracked{9: viewer}
	var got int
	h.inDim(dimNether, func() { got = h.ownSignal(x, y, z, trapped) })
	if got != 1 {
		t.Fatalf("a Nether trapped chest with one viewer emits %d, want 1", got)
	}
}
