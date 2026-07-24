package worldread

import "testing"

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
