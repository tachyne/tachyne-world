package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A stalagmite saved as an edit on a floor the generator no longer lays
// breaks when its chunk comes into range, as vanilla breaks one that loses
// its floor (bug #27); one on stone stays, and a loose edit that is not a
// cave growth (a snow layer) is not this pass's to judge.
func TestChunkActivationSettlesFloatingGrowths(t *testing.T) {
	h := newHub(world.New(1))
	h.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs.json"))
	players := map[int32]*tracked{}
	h.tick.Store(1000)
	w := h.world
	w.ForceLoad(5, 5, 1)
	for x := 80; x <= 90; x++ {
		for z := 80; z <= 90; z++ {
			for y := 170; y <= 180; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	tip := dripstoneState(dripTip, true, false)
	w.SetBlock(82, 175, 82, tip)                                   // floating: air below
	w.SetBlock(84, 172, 84, worldgen.Stone)                        // a floor…
	w.SetBlock(84, 173, 84, tip)                                   // …with a stalagmite on it
	w.SetBlock(86, 176, 86, worldgen.BlockBase("snow"))            // floating snow: left alone
	h.reconcileMobChunks(players, map[[2]int32]bool{{5, 5}: true}) // chunk (5,5) comes into range
	if w.At(82, 175, 82) != worldgen.Air {
		t.Error("the floating stalagmite survived its chunk coming into range")
	}
	if w.At(84, 173, 84) != tip {
		t.Error("a stalagmite standing on stone was knocked down")
	}
	if w.At(86, 176, 86) == worldgen.Air {
		t.Error("the settle pass judged a snow layer: only cave growths are its business")
	}
}
