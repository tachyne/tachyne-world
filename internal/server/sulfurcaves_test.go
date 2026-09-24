package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Down in the sulfur caves the world reports the biome, and the natural
// spawner draws from its pool: sulfur cubes at weight 100 in packs of two
// to four among the cave monsters, bats in eights.
func TestSulfurCavesSpawnPool(t *testing.T) {
	w := world.New(5)
	g := w.Gen()
	x, y, z, found := 0, 0, 0, false
	for r := 0; r < 200 && !found; r++ {
		for cx := -r; cx <= r && !found; cx++ {
			for _, cz := range []int{-r, r} {
				for _, p := range [2][2]int{{cx, cz}, {cz, cx}} {
					px, pz := p[0]*16+8, p[1]*16+8
					sec := (w.GroundY(px, pz) - 60 - worldgen.MinY) / 16
					py := worldgen.MinY + sec*16 + 8
					if sec >= 0 && g.CaveBiomeAt(px, py, pz) == "minecraft:sulfur_caves" {
						x, y, z, found = px, py, pz, true
						break
					}
				}
				if found {
					break
				}
			}
		}
	}
	if !found {
		t.Fatal("no sulfur caves section within 200 chunks of the origin for seed 5")
	}
	if got := w.BiomeAt3D(x, y, z); got != "minecraft:sulfur_caves" {
		t.Fatalf("BiomeAt3D(%d,%d,%d) = %q, want minecraft:sulfur_caves", x, y, z, got)
	}
	if got := w.BiomeAt3D(x, w.SurfaceFeet(x, z), z); got == "minecraft:sulfur_caves" {
		t.Errorf("the surface over (%d,%d) reports sulfur_caves", x, z)
	}
	h := newHub(w)
	cube, ok := entityByName["sulfur_cube"]
	if !ok {
		t.Fatal("sulfur_cube has no entity id")
	}
	hasCube := false
	for _, e := range h.spawnPool(0, catMonster, x, y, z) {
		if e.etype == cube {
			hasCube = e.weight == 100 && e.min == 2 && e.max == 4
		}
	}
	if !hasCube {
		t.Errorf("the monster pool at (%d,%d,%d) lacks sulfur_cube ×100 in packs of 2–4: %+v", x, y, z, h.spawnPool(0, catMonster, x, y, z))
	}
	bats := h.spawnPool(0, catAmbient, x, y, z)
	if len(bats) != 1 || bats[0].etype != entityBat || bats[0].weight != 10 || bats[0].min != 8 {
		t.Errorf("the ambient pool in the sulfur caves: %+v, want bats ×10 in eights", bats)
	}
}
