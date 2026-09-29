package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestPistonMachineSurvivesRestart: a button drives a sticky piston that
// pushes a block out and pulls it back. A player watches (the chunk is sent
// to them), presses the button, and the server restarts. Every block of the
// machine must read as it did before the restart, and the machine must work
// again after it.
//
// Vanilla keeps a chunk's blocks in the chunk and saves a copy of each
// section (SerializableChunkData.copyOf), so a block that went back to the
// state it had earlier is saved like any other. The engine keeps generation
// plus an edit layer, and a chunk sent to a player was built on the cached
// generated chunk's own section storage: the edits were written into
// generation itself. A block that then went back to the state it had when
// the chunk was sent (a button released, a piston retracted, a block pulled
// home) matched that "generation", so SetBlock dropped its edit, and the
// restart or the next time the chunk left memory put terrain where it stood.
// That is how a player's lift lost its walls, torches, dust and observers,
// and the water behind it came through.
func TestPistonMachineSurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.gob")
	w, err := world.NewWithStore(1, world.NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	x, y, z := 8, 180, 8 // the piston; built high, clear of terrain
	w.ForceLoad(x, z, 2)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	h.ticks.sprintLeft = 1 << 30 // as fast as the loop runs
	startHub(t, h)

	var (
		button = withProps(t, worldgen.BlockBase("stone_button"), map[string]string{"face": "wall", "facing": "west", "powered": "false"})
		piston = withProps(t, worldgen.BlockBase("sticky_piston"), map[string]string{"facing": "east", "extended": "false"})
		planks = worldgen.BlockBase("oak_planks")
	)
	cells := map[blockPos]uint32{
		{x - 2, y, z}: button, // on the stone's west face
		{x - 1, y, z}: worldgen.Stone,
		{x, y, z}:     piston,
		{x + 1, y, z}: planks, // pushed to x+2 and pulled back
	}
	for xx := x - 3; xx <= x+3; xx++ {
		cells[blockPos{xx, y - 1, z}] = worldgen.Stone // a floor for the button's stone and the planks
	}
	onHub(t, h, func() {
		for p, s := range cells {
			w.SetBlock(p.x, p.y, p.z, s)
		}
		w.Chunk(int32(x>>4), int32(z>>4)) // a player's view receives the chunk
	})
	press := func(h *hub) {
		t.Helper()
		onHub(t, h, func() { h.pressButton(h.playersRef, blockPos{x - 2, y, z}, button, nil) })
		pushed := false
		for i := 0; i < 60; i++ {
			onHub(t, h, func() { pushed = pushed || h.worldFor(0).Block(x+2, y, z) == planks })
		}
		if !pushed {
			t.Fatal("the button never made the piston push the planks")
		}
	}
	check := func(w *world.World, when string) {
		t.Helper()
		for p, want := range cells {
			if got := w.Block(p.x, p.y, p.z); got != want {
				gn, _ := worldgen.StateName(got)
				wn, _ := worldgen.StateName(want)
				t.Errorf("%s: (%d,%d,%d) reads %s, want %s", when, p.x, p.y, p.z, gn, wn)
			}
		}
		if got := w.Block(x+2, y, z); got != worldgen.Air {
			t.Errorf("%s: the cell the planks were pushed into is not empty again", when)
		}
	}
	press(h)
	check(w, "after the press")
	onHub(t, h, func() {
		if err := w.Save(); err != nil {
			t.Fatal(err)
		}
	})

	// The restart: the saved edits load into a new world and a new hub
	// boots on them (run: the container sweeps and the redstone sweep).
	w2, err := world.NewWithStore(1, world.NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	w2.ForceLoad(x, z, 2)
	check(w2, "after the restart")
	h2 := newTestHub(w2)
	h2.rules.DoMobSpawning = false
	h2.ticks.sprintLeft = 1 << 30
	startHub(t, h2)
	stepHub(t, h2, 20) // the boot sweep's updates run
	check(w2, "after the boot sweep")
	press(h2)
	check(w2, "after a press on the restarted server")
}
