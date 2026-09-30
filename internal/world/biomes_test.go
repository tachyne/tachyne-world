package world

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A biome override is per quart: BiomeAt3D answers it for the quart's
// blocks only, a section shows it once most of its quarts have it, and an
// override back to the generated biome is dropped.
func TestBiomeOverrides(t *testing.T) {
	w := New(1)
	gen := w.BiomeAt3D(1, 70, 1)
	if !w.SetBiome(1, 70, 1, "minecraft:the_void") {
		t.Fatal("the first override must change the biome")
	}
	if got := w.BiomeAt3D(3, 71, 3); got != "minecraft:the_void" {
		t.Errorf("same quart reads %q", got)
	}
	if got := w.BiomeAt3D(4, 70, 1); got == "minecraft:the_void" {
		t.Error("the next quart must keep its biome")
	}
	if w.SetBiome(2, 69, 2, "minecraft:the_void") {
		t.Error("setting the biome a quart already has changes nothing")
	}
	sec := (70 - worldgen.MinY) / 16
	if got := w.SectionBiomes(0, 0)[sec]; got == "minecraft:the_void" {
		t.Error("one quart of 64 must not recolour its section")
	}
	for x := 0; x < 16; x += 4 {
		for z := 0; z < 16; z += 4 {
			for y := 64; y < 80; y += 4 {
				w.SetBiome(x, y, z, "minecraft:the_void")
			}
		}
	}
	if got := w.SectionBiomes(0, 0)[sec]; got != "minecraft:the_void" {
		t.Errorf("a filled section shows %q", got)
	}
	if got := w.Chunk(0, 0).Biomes[sec]; got != "minecraft:the_void" {
		t.Errorf("the chunk a viewer is sent shows %q", got)
	}
	if len(w.BiomeOverrides()) != 64 {
		t.Errorf("%d overrides, want 64", len(w.BiomeOverrides()))
	}
	w.SetBiome(1, 70, 1, gen)
	if w.BiomeAt3D(1, 70, 1) != gen || len(w.BiomeOverrides()) != 63 {
		t.Error("an override back to the generated biome must be dropped")
	}
	w2 := New(1)
	w2.LoadBiomeOverrides(w.BiomeOverrides())
	if w2.BiomeAt3D(8, 72, 8) != "minecraft:the_void" {
		t.Error("loaded overrides must apply")
	}
}
