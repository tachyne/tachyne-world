package worldgen

import "testing"

// aboveGround counts state s in the chunk's columns above their terrain
// height (Height-1 is the natural floor).
func aboveGround(g *Generator, ch *Chunk, cx, cz int32, s uint32) (n int, at [3]int) {
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			for y := g.Height(wx, wz); y < g.Height(wx, wz)+60; y++ {
				if sectionBlockAt(ch, lx, y, lz) == s {
					n++
					at = [3]int{wx, y, wz}
				}
			}
		}
	}
	return n, at
}

// The ice spikes biome stands packed-ice spikes on its snow, and a build
// in a spike's box keeps that spike out.
func TestIceSpikesGrowAndGuard(t *testing.T) {
	seed, cx, cz := findBiomeSeed(t, "minecraft:ice_spikes")
	g := NewGenerator(seed)
	var bcx, bcz int32
	best, at := 0, [3]int{}
	for dcx := int32(-2); dcx <= 2; dcx++ {
		for dcz := int32(-2); dcz <= 2; dcz++ {
			n, a := aboveGround(g, g.GenerateChunk(cx+dcx, cz+dcz), cx+dcx, cz+dcz, PackedIce)
			if n > best {
				best, at, bcx, bcz = n, a, cx+dcx, cz+dcz
			}
		}
	}
	if best == 0 {
		t.Fatal("no packed ice above the ground in 25 chunks of ice spikes")
	}
	t.Logf("seed %d: %d packed ice above ground in chunk %d,%d", seed, best, bcx, bcz)
	g2 := NewGenerator(seed)
	setEditOverlay(g2, map[[3]int]uint32{at: BlockBase("oak_planks")})
	n, _ := aboveGround(g2, g2.GenerateChunk(bcx, bcz), bcx, bcz, PackedIce)
	if n >= best {
		t.Errorf("a plank in a spike left %d packed ice (was %d)", n, best)
	}
}

// The old-growth taigas get mossy cobblestone boulders; a build beside one
// keeps it out.
func TestForestRocksAndGuard(t *testing.T) {
	seed, cx, cz := findBiomeSeed(t, "minecraft:old_growth_pine_taiga")
	g := NewGenerator(seed)
	var bcx, bcz int32
	best, at := 0, [3]int{}
	for dcx := int32(-2); dcx <= 2; dcx++ {
		for dcz := int32(-2); dcz <= 2; dcz++ {
			n, a := aboveGround(g, g.GenerateChunk(cx+dcx, cz+dcz), cx+dcx, cz+dcz, MossyCobblestone)
			if n > best {
				best, at, bcx, bcz = n, a, cx+dcx, cz+dcz
			}
		}
	}
	if best == 0 {
		t.Fatal("no forest rocks in 25 chunks of old-growth pine taiga")
	}
	g2 := NewGenerator(seed)
	setEditOverlay(g2, map[[3]int]uint32{{at[0], at[1] + 1, at[2]}: BlockBase("oak_planks")})
	n, _ := aboveGround(g2, g2.GenerateChunk(bcx, bcz), bcx, bcz, MossyCobblestone)
	if n >= best {
		t.Errorf("a plank on a rock left %d mossy cobblestone (was %d)", n, best)
	}
}

// Blue ice grows only against packed ice, under the frozen oceans' bergs.
func TestBlueIce(t *testing.T) {
	seed, cx, cz := findBiomeSeed(t, "minecraft:frozen_ocean")
	g := NewGenerator(seed)
	total := 0
	for dcx := int32(-4); dcx <= 4; dcx++ {
		for dcz := int32(-4); dcz <= 4; dcz++ {
			ch := g.GenerateChunk(cx+dcx, cz+dcz)
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					for y := 30; y <= 62; y++ {
						if sectionBlockAt(ch, lx, y, lz) == BlueIce {
							total++
						}
					}
				}
			}
		}
	}
	t.Logf("seed %d: %d blue ice below the waterline in 81 chunks", seed, total)
	// A patch placed straight into the view: water beside packed ice.
	view := &owRegion{g: g, baseX: 0, baseZ: 0, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			for y := 40; y <= 50; y++ {
				view.capture[[3]int{x, y, z}] = Water
			}
			view.capture[[3]int{x, 51, z}] = PackedIce
		}
	}
	ch := NewChunk(g.sections)
	g.blueIce(chunkCell{ch, 0, 0}, view, 1, 50, 1)
	n := 0
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			for y := 40; y <= 55; y++ {
				if sectionBlockAt(ch, x, y, z) == BlueIce {
					n++
				}
			}
		}
	}
	if n < 2 {
		t.Errorf("a blue ice seed under packed ice spread to %d blocks", n)
	}
	ch = NewChunk(g.sections)
	delete(view.capture, [3]int{1, 51, 1})
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			view.capture[[3]int{x, 51, z}] = Water
		}
	}
	g.blueIce(chunkCell{ch, 0, 0}, view, 1, 50, 1)
	if sectionBlockAt(ch, 1, 50, 1) == BlueIce {
		t.Error("blue ice grew with no packed ice beside it")
	}
}
