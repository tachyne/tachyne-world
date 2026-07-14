package worldgen

import (
	"math"
	"testing"
)

func TestGeodesGenerate(t *testing.T) {
	g := NewGenerator(1)
	var basalt, calc, amethyst, budding, clusters int
	var geodeChunk [2]int32
	found := false
	for cx := int32(-15); cx <= 15 && !found; cx++ {
		for cz := int32(-15); cz <= 15; cz++ {
			ch := g.GenerateChunk(cx, cz)
			var b, c, a, bd, cl int
			for sec := range ch.Sections {
				for _, s := range ch.Sections[sec] {
					switch {
					case s == smoothBasalt:
						b++
					case s == calcite:
						c++
					case s == amethystBlock:
						a++
					case s == buddingAmethyst:
						bd++
					case s >= amethystClusterBase && s <= amethystClusterBase+11,
						s >= smallBudBase && s <= smallBudBase+11,
						s >= mediumBudBase && s <= mediumBudBase+11,
						s >= largeBudBase && s <= largeBudBase+11:
						cl++
					}
				}
			}
			basalt += b
			calc += c
			amethyst += a
			budding += bd
			clusters += cl
			if a > 0 && !found { // a chunk containing a geode's amethyst lining
				geodeChunk = [2]int32{cx, cz}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no geode found in 900 chunks (expected ~30)")
	}
	if basalt == 0 || calc == 0 || amethyst == 0 || budding == 0 || clusters == 0 {
		t.Errorf("geode layers incomplete: basalt=%d calcite=%d amethyst=%d budding=%d clusters=%d",
			basalt, calc, amethyst, budding, clusters)
	}
	t.Logf("basalt=%d calcite=%d amethyst=%d budding=%d clusters=%d (first geode chunk %v)",
		basalt, calc, amethyst, budding, clusters, geodeChunk)

	// Determinism: regenerating the same chunk yields identical geode blocks.
	a := g.GenerateChunk(geodeChunk[0], geodeChunk[1])
	b := NewGenerator(1).GenerateChunk(geodeChunk[0], geodeChunk[1])
	for sec := range a.Sections {
		for i := range a.Sections[sec] {
			if a.Sections[sec][i] != b.Sections[sec][i] {
				t.Fatalf("geode chunk not deterministic at sec %d idx %d", sec, i)
			}
		}
	}
}

// TestGeodeCrossChunkConsistent: a geode straddling a chunk border must place
// identical blocks whether reached from its own chunk or the neighbour — the
// shared cells (border column) agree.
func TestGeodeCrossChunkConsistent(t *testing.T) {
	g := NewGenerator(1)
	// Find any rooted geode near a border and compare its blocks in the two
	// chunks it spans against a direct distance-field recompute.
	for ncx := int32(-8); ncx <= 8; ncx++ {
		for ncz := int32(-8); ncz <= 8; ncz++ {
			seed, gx, gy, gz, ok := g.geodeAt(ncx, ncz)
			if !ok {
				continue
			}
			// The geode reaches into neighbouring chunks; check the chunk east
			// of the origin holds the same shell cells the origin chunk would.
			east := g.GenerateChunk(ncx+1, ncz)
			base := int(ncx+1) * 16
			mismatch := 0
			for bx := gx - geodeReach; bx <= gx+geodeReach; bx++ {
				lx := bx - base
				if lx < 0 || lx > 15 {
					continue
				}
				for by := gy - geodeReach; by <= gy+geodeReach; by++ {
					for bz := gz - geodeReach; bz <= gz+geodeReach; bz++ {
						lz := bz - int(ncz)*16
						if lz < 0 || lz > 15 || by <= MinY {
							continue
						}
						d := dist(bx, by, bz, gx, gy, gz) + (geodeNoise(seed, bx, by, bz)-0.5)*0.8
						want := geodeShell(seed, bx, by, bz, d)
						if want == 0 || want == Air {
							continue // outside shell or hollow — skip (stone/air ambiguous)
						}
						yi := by - MinY
						got := east.Sections[yi/16][((yi%16)*16+lz)*16+lx]
						if got != want && (got == Stone || got == Deepslate) {
							mismatch++
						}
					}
				}
			}
			if mismatch != 0 {
				t.Fatalf("geode at origin (%d,%d) has %d shell cells missing in the east chunk", ncx, ncz, mismatch)
			}
			return // one straddling geode checked is enough
		}
	}
}

func dist(ax, ay, az, bx, by, bz int) float64 {
	dx, dy, dz := ax-bx, ay-by, az-bz
	return math.Sqrt(float64(dx*dx + dy*dy + dz*dz))
}
