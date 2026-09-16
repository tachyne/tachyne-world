package worldgen

import "testing"

// The nether's ores: gold and quartz veins in the rock, ancient debris
// buried and never touching air.
func TestNetherOresAndDebris(t *testing.T) {
	g := NewNetherGenerator(11)
	gold, quartz, debris, exposed := 0, 0, 0, 0
	for cx := int32(-4); cx <= 4; cx++ {
		for cz := int32(-4); cz <= 4; cz++ {
			ch := g.generateNetherChunk(cx, cz)
			for s := range ch.Sections {
				for i, b := range ch.Sections[s] {
					switch b {
					case NetherGoldOre:
						gold++
					case NetherQuartzOre:
						quartz++
					case AncientDebris:
						debris++
						lx, ly, lz := i%16, MinY+s*16+i/256, (i/16)%16
						for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
							nx, nz := lx+o[0], lz+o[2]
							if nx < 0 || nx > 15 || nz < 0 || nz > 15 {
								continue
							}
							if sectionBlockAt(ch, nx, ly+o[1], nz) == Air {
								exposed++
							}
						}
					}
				}
			}
		}
	}
	if gold == 0 || quartz == 0 {
		t.Errorf("gold %d quartz %d in 81 chunks", gold, quartz)
	}
	if debris == 0 {
		t.Error("no ancient debris in 81 chunks")
	}
	if exposed > 0 {
		t.Errorf("%d ancient debris faces open to air", exposed)
	}
}

// A basalt deltas chunk is basalt and blackstone, with lava deltas or
// columns about; a soul sand valley grows basalt pillars somewhere near.
func TestNetherDeltasAndValleyFeatures(t *testing.T) {
	g := NewNetherGenerator(11)
	if cx, cz, ok := findNetherBiomeChunk(g, "minecraft:basalt_deltas"); ok {
		basalt, magma := 0, 0
		for dcx := int32(-1); dcx <= 1; dcx++ {
			for dcz := int32(-1); dcz <= 1; dcz++ {
				ch := g.generateNetherChunk(cx+dcx, cz+dcz)
				for s := range ch.Sections {
					for _, b := range ch.Sections[s] {
						switch b {
						case Basalt, Blackstone:
							basalt++
						case MagmaBlock:
							magma++
						}
					}
				}
			}
		}
		if basalt < 500 {
			t.Errorf("basalt+blackstone in nine delta chunks: %d", basalt)
		}
		if magma == 0 {
			t.Error("no magma (delta rims or ore) in the deltas")
		}
	} else {
		t.Log("no basalt deltas within 64 chunks for this seed")
	}
	if cx, cz, ok := findNetherBiomeChunk(g, "minecraft:soul_sand_valley"); ok {
		basalt := 0
		for dcx := int32(-2); dcx <= 2; dcx++ {
			for dcz := int32(-2); dcz <= 2; dcz++ {
				ch := g.generateNetherChunk(cx+dcx, cz+dcz)
				for s := range ch.Sections {
					for _, b := range ch.Sections[s] {
						if b == Basalt {
							basalt++
						}
					}
				}
			}
		}
		if basalt == 0 {
			t.Error("no basalt pillars in twenty-five valley chunks")
		}
	}
}
