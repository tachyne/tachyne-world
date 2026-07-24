package worldread

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSetBlockInMemoryOnly pins the contract a live map depends on: an applied
// block change is visible to the next Chunk read, and NOTHING is written to
// disk (the engine owns the world files; a reader must never touch them).
func TestSetBlockInMemoryOnly(t *testing.T) {
	dir := t.TempDir()
	gobPath := filepath.Join(dir, "world.gob") // does not exist = no edits
	r, err := OpenGob(Overworld, 1, gobPath)
	if err != nil {
		t.Fatalf("OpenGob: %v", err)
	}

	// High above terrain, so the column is reliably air.
	const wx, wy, wz = 5, 200, 5
	ly := wy - MinY
	if got := r.Chunk(0, 0).State(wx, ly, wz); got != 0 {
		t.Fatalf("expected air at y=%d, got state %d", wy, got)
	}

	const stone = uint32(1)
	r.SetBlock(wx, wy, wz, stone)

	if got := r.Chunk(0, 0).State(wx, ly, wz); got != stone {
		t.Errorf("after SetBlock: state = %d, want %d", got, stone)
	}
	if name, _ := Decode(stone); name == "minecraft:air" || name == "" {
		t.Errorf("state %d decoded to %q, expected a real block", stone, name)
	}
	if _, err := os.Stat(gobPath); !os.IsNotExist(err) {
		t.Errorf("SetBlock wrote to disk (%s exists) — a reader must never persist", gobPath)
	}
}

func TestOpenReadDecode(t *testing.T) {
	// Terrain-only (nil store) view of the classic world (seed 1).
	r, err := Open(Overworld, 1, nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r.Sections() <= 0 {
		t.Fatalf("sections = %d, want > 0", r.Sections())
	}

	ch := r.Chunk(0, 0)
	if got := len(ch.Sections); got != r.Sections() {
		t.Fatalf("chunk sections = %d, want %d", got, r.Sections())
	}
	if len(ch.SkyLight) == 0 || len(ch.BlockLight) == 0 {
		t.Fatalf("light not populated: sky=%d block=%d", len(ch.SkyLight), len(ch.BlockLight))
	}
	if len(ch.Biomes) != r.Sections() {
		t.Fatalf("biomes = %d, want %d", len(ch.Biomes), r.Sections())
	}

	// Scan column (0,0) from the top for the first solid block.
	var surface uint32
	surfaceY := 0
	for ly := ch.Height() - 1; ly >= 0; ly-- {
		if s := ch.State(0, ly, 0); s != 0 {
			surface, surfaceY = s, ly
			break
		}
	}
	if surface == 0 {
		t.Fatal("column (0,0) is entirely air")
	}
	name, _ := Decode(surface)
	if name == "" || name == "minecraft:air" {
		t.Fatalf("surface block (ly=%d) decoded to %q", surfaceY, name)
	}
	if n, _ := Decode(0); n != "minecraft:air" {
		t.Errorf("Decode(0) = %q, want minecraft:air", n)
	}
	t.Logf("seed 1 chunk (0,0): surface %s at world-Y %d, %d sections",
		name, MinY+surfaceY, r.Sections())
}
