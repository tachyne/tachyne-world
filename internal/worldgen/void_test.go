package worldgen

import "testing"

// The void is vanilla's "The Void" preset: air everywhere but the start
// platform — stone within checkerboard distance 16 of (8, 8), cobblestone
// at its centre, three above the bottom of the world — and the_void
// biome in every section. A chunk is its cells, cell for cell.
func TestVoidGenerator(t *testing.T) {
	g := NewVoidGenerator(1)
	for _, c := range []struct {
		x, y, z int
		want    uint32
	}{
		{8, MinY + 3, 8, Cobblestone},
		{-8, MinY + 3, -8, Stone},
		{24, MinY + 3, 24, Stone},
		{25, MinY + 3, 8, Air},
		{8, MinY + 3, -9, Air},
		{8, MinY + 2, 8, Air},
		{8, MinY + 4, 8, Air},
		{8, 64, 8, Air},
	} {
		if got := g.BlockAt(c.x, c.y, c.z); got != c.want {
			t.Errorf("BlockAt(%d,%d,%d) = %d, want %d", c.x, c.y, c.z, got, c.want)
		}
	}
	stone := 0
	for cx := int32(-2); cx <= 2; cx++ {
		for cz := int32(-2); cz <= 2; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for s, b := range ch.Biomes {
				if b != "minecraft:the_void" {
					t.Fatalf("chunk %d,%d section %d biome %q", cx, cz, s, b)
				}
			}
			for s := range ch.Sections {
				for i, st := range ch.Sections[s] {
					x, y, z := int(cx)*16+i%16, MinY+s*16+i/256, int(cz)*16+i/16%16
					if want := g.BlockAt(x, y, z); st != want {
						t.Fatalf("chunk %d,%d cell %d,%d,%d = %d, BlockAt says %d", cx, cz, x, y, z, st, want)
					}
					if st != Air {
						stone++
					}
				}
			}
		}
	}
	if stone != 33*33 {
		t.Errorf("%d platform blocks, want 33×33", stone)
	}
	if g.Height(8, 8) != MinY+4 || g.Height(100, 100) != MinY || g.SurfaceY(0, 0) != MinY+4 {
		t.Errorf("heights: platform %d, open void %d, surface %v", g.Height(8, 8), g.Height(100, 100), g.SurfaceY(0, 0))
	}
	if g.BiomeName(500, -500) != "minecraft:the_void" || g.CaveBiomeAt(0, -40, 0) != "minecraft:the_void" {
		t.Errorf("biome %q / %q", g.BiomeName(500, -500), g.CaveBiomeAt(0, -40, 0))
	}
	if _, _, ok := NewGenerator(1).LocateStructure("village", 0, 0, 2000); !ok {
		t.Fatal("the overworld has no village to compare with")
	}
	for _, name := range []string{"village", "stronghold", "mineshaft", "monument"} {
		if _, _, ok := g.LocateStructure(name, 0, 0, 2000); ok {
			t.Errorf("located %s in the void", name)
		}
	}
	if g.MineshaftIn(0, 0).Exists || g.SwampHutIn(0, 0).Exists || g.DesertTempleIn(0, 0).Exists {
		t.Error("a structure answered in the void")
	}
}
