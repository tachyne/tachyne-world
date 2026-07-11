package world

import (
	"testing"

	"tachyne/internal/worldgen"
)

// TestEditOverlay: a SetBlock persists, is visible via Block, and appears in the
// regenerated chunk — while neighbouring blocks stay as generated.
func TestEditOverlay(t *testing.T) {
	w := New(1)
	const x, y, z = 5, 70, 9

	neighbour := w.Block(x+1, y, z)
	w.SetBlock(x, y, z, worldgen.Stone)

	if got := w.Block(x, y, z); got != worldgen.Stone {
		t.Fatalf("Block after edit = %d, want stone", got)
	}
	if got := w.Block(x+1, y, z); got != neighbour {
		t.Errorf("neighbour changed by the edit: %d -> %d", neighbour, got)
	}

	ch := w.Chunk(0, 0)
	sec := (y - worldgen.MinY) / 16
	ly := (y - worldgen.MinY) % 16
	if got := ch.Sections[sec][(ly*16+z)*16+x]; got != worldgen.Stone {
		t.Errorf("chunk section block = %d, want stone", got)
	}
}

// TestNegativeCoords: edits at negative world coordinates land in the right
// chunk (floor division, not truncation).
func TestNegativeCoords(t *testing.T) {
	w := New(1)
	w.SetBlock(-3, 64, -20, worldgen.Sand)
	if got := w.Block(-3, 64, -20); got != worldgen.Sand {
		t.Fatalf("Block at negative coords = %d, want sand", got)
	}
	// The edit must show up in chunk (-1, -2): -3>>4 = -1, -20>>4 = -2.
	ch := w.Chunk(-1, -2)
	lx, lz := -3-(-1*16), -20-(-2*16) // 13, 12
	sec := (64 - worldgen.MinY) / 16
	ly := (64 - worldgen.MinY) % 16
	if got := ch.Sections[sec][(ly*16+lz)*16+lx]; got != worldgen.Sand {
		t.Errorf("edit not found in chunk (-1,-2): got %d", got)
	}
}

// TestOutOfBounds: edits outside the world height are ignored, not panics.
func TestOutOfBounds(t *testing.T) {
	w := New(1)
	w.SetBlock(0, 5000, 0, worldgen.Stone) // above the world
	if got := w.Block(0, 5000, 0); got != worldgen.Air {
		t.Errorf("out-of-bounds block = %d, want air", got)
	}
}

// TestBlockSeesGeneratedTrees: point reads must include decorations. gen.BlockAt
// is terrain-only, so falling back to it made every generated tree read as air —
// punching a trunk "broke" hardness-0 air instantly and dropped no wood.
func TestBlockSeesGeneratedTrees(t *testing.T) {
	w := New(1)
	logs := map[uint32]bool{
		worldgen.OakLog: true, worldgen.SpruceLog: true, worldgen.BirchLog: true,
		worldgen.JungleLog: true, worldgen.AcaciaLog: true, worldgen.DarkOakLog: true,
		worldgen.CherryLog: true, worldgen.MangroveLog: true,
	}
	// Find any generated tree near spawn and confirm a point read of its trunk
	// base sees the log (not terrain-only air — the bug this guards against).
	for x := -80; x < 80; x++ {
		for z := -80; z < 80; z++ {
			if !w.Gen().TreeAt(x, z) {
				continue
			}
			y := w.Gen().Height(x, z) // trunk roots on the surface
			if logs[w.Block(x, y, z)] {
				return // point read included the trunk decoration
			}
			t.Fatalf("tree trunk at (%d,%d,%d) read as %d, not a log", x, y, z, w.Block(x, y, z))
		}
	}
	t.Fatal("no generated tree found near spawn to test")
}

// TestBlockMatchesChunkView: Block() must agree byte-for-byte with the chunk the
// client receives — one source of truth for "what is the world".
func TestBlockMatchesChunkView(t *testing.T) {
	w := New(1)
	w.SetBlock(3, 70, -30, worldgen.Stone) // an edit must show in both views too
	ch := w.Chunk(0, -2)                   // world x 0..15, z -32..-17
	for sec := 0; sec < worldgen.SectionCount; sec++ {
		for i, want := range ch.Sections[sec] {
			lx, lz, ly := i%16, (i/16)%16, i/256
			y := worldgen.MinY + sec*16 + ly
			if got := w.Block(lx, y, -32+lz); got != want {
				t.Fatalf("Block(%d,%d,%d)=%d but chunk carries %d", lx, y, -32+lz, got, want)
			}
		}
	}
}

// TestDropYUnderground: an item dropped in a tunnel rests on the tunnel floor,
// not teleported to the world surface (the coal-vanishes-when-mined bug).
func TestDropYUnderground(t *testing.T) {
	w := New(1)
	// Carve a 1-block pocket at y=30 with a solid floor below it.
	w.SetBlock(100, 30, 100, worldgen.Air)
	w.SetBlock(100, 29, 100, worldgen.Stone)
	if got := w.DropY(100, 30, 100); got != 30 {
		t.Fatalf("drop in a floored pocket should rest at 30, got %d", got)
	}
	// A pocket with air below: the drop falls to the next support.
	w.SetBlock(100, 29, 100, worldgen.Air)
	w.SetBlock(100, 28, 100, worldgen.Air)
	w.SetBlock(100, 27, 100, worldgen.Stone)
	if got := w.DropY(100, 30, 100); got != 28 {
		t.Fatalf("drop should fall to the support at 28, got %d", got)
	}
}

// TestDropYFallsThroughLeaves: tree-chop drops must not rest on the canopy.
func TestDropYFallsThroughLeaves(t *testing.T) {
	w := New(1)
	// Column: ground at y=64, leaves at y=70..72, break position at y=73.
	w.SetBlock(200, 64, 200, worldgen.Stone)
	for y := 65; y <= 69; y++ {
		w.SetBlock(200, y, 200, worldgen.Air)
	}
	for y := 70; y <= 72; y++ {
		w.SetBlock(200, y, 200, worldgen.OakLeaves)
	}
	if got := w.DropY(200, 73, 200); got != 65 {
		t.Fatalf("drop should fall through the canopy to the ground (65), got %d", got)
	}
}

// TestMigrateEdits: MigrateEdits rewrites every stored edit's block-state id
// through the supplied map, persists the change, and leaves untouched ids alone.
func TestMigrateEdits(t *testing.T) {
	dir := t.TempDir()
	store := NewFileStore(dir + "/world.gob")
	w, err := NewWithStore(1, store)
	if err != nil {
		t.Fatal(err)
	}
	w.SetBlock(5, 70, 5, 100) // "old" id 100 -> should map to 999
	w.SetBlock(6, 70, 5, 200) // unchanged by the map
	remap := func(s uint32) uint32 {
		if s == 100 {
			return 999
		}
		return s
	}
	n, err := w.MigrateEdits(remap)
	if err != nil || n != 1 {
		t.Fatalf("MigrateEdits n=%d err=%v, want 1 edit changed", n, err)
	}
	if got := w.Block(5, 70, 5); got != 999 {
		t.Errorf("migrated block = %d, want 999", got)
	}
	if got := w.Block(6, 70, 5); got != 200 {
		t.Errorf("untouched block changed to %d, want 200", got)
	}
	// Persisted: a fresh world over the same store sees the migrated id.
	w2, _ := NewWithStore(1, store)
	if got := w2.Block(5, 70, 5); got != 999 {
		t.Errorf("reloaded migrated block = %d, want 999 (not persisted)", got)
	}
}

// TestHeightmapTracksEdits: the chunk heightmap must include the edit
// overlay — the client gates precipitation on it (rain through built roofs).
func TestHeightmapTracksEdits(t *testing.T) {
	w := New(1)
	base := w.Chunk(0, 0).Heightmap[0] // column lx=0,lz=0
	w.SetBlock(0, 200, 0, 1)           // build a roof block high above terrain
	if got := w.Chunk(0, 0).Heightmap[0]; got != 200 {
		t.Fatalf("heightmap after build = %d, want 200 (terrain was %d)", got, base)
	}
	w.SetBlock(0, 200, 0, worldgen.Air) // tear it down again
	if got := w.Chunk(0, 0).Heightmap[0]; got != base {
		t.Fatalf("heightmap after removal = %d, want terrain %d", got, base)
	}
	// digging the natural surface must LOWER the column
	w.SetBlock(0, int(base), 0, worldgen.Air)
	if got := w.Chunk(0, 0).Heightmap[0]; got >= base {
		t.Fatalf("heightmap after digging surface = %d, want < %d", got, base)
	}
}
