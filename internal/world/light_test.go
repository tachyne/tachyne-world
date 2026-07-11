package world

import (
	"testing"

	"tachyne/internal/worldgen"
)

// chunkBlock reads a center-chunk block state at in-chunk (lx, y, lz).
func chunkBlock(ch *worldgen.Chunk, lx, y, lz int) uint32 {
	yi := y - worldgen.MinY
	return ch.Sections[yi/16][((yi%16)*16+lz)*16+lx]
}

func skyLevel(ld *LightData, lx, y, lz int) uint8 {
	yi := y - worldgen.MinY
	return ld.Sky[yi/16][((yi%16)*16+lz)*16+lx]
}

func blockLevel(ld *LightData, lx, y, lz int) uint8 {
	yi := y - worldgen.MinY
	return ld.Block[yi/16][((yi%16)*16+lz)*16+lx]
}

// TestBlockLightFromEmitter: an emitter placed in a dark underground pocket lights
// its neighbours (block light), even where there's no sky light. Glowstone (15)
// in air at depth: the adjacent block should read 14, and a few blocks away dimmer.
func TestBlockLightFromEmitter(t *testing.T) {
	w := New(1)
	const lx, lz = 8, 8
	surface := int(w.SurfaceY(lx, lz))
	y := surface - 8 // underground, no sky light here

	// Carve a little air pocket and drop glowstone (state 6042) one block over.
	w.SetBlock(lx, y, lz, worldgen.Air)
	w.SetBlock(lx+1, y, lz, worldgen.Air)
	w.SetBlock(lx+2, y, lz, worldgen.Air)
	w.SetBlock(lx, y, lz, worldgen.BlockID("glowstone")) // glowstone emits 15

	ld := w.Light(0, 0)
	if got := skyLevel(ld, lx+1, y, lz); got != 0 {
		t.Fatalf("expected no sky light underground, got %d", got)
	}
	if got := blockLevel(ld, lx+1, y, lz); got != 14 {
		t.Errorf("block light adjacent to glowstone = %d, want 14", got)
	}
	if got := blockLevel(ld, lx+2, y, lz); got != 13 {
		t.Errorf("block light two blocks from glowstone = %d, want 13", got)
	}
}

// TestSkyLightDirectColumns: every block with an unobstructed view of the sky
// (only transparent blocks above it) must be at full daylight (15). This is the
// core guarantee — open ground is never dark.
func TestSkyLightDirectColumns(t *testing.T) {
	w := New(1)
	ch := w.Chunk(0, 0)
	ld := w.Light(0, 0)

	top := worldgen.MinY + worldgen.SectionCount*16 - 1
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for y := top; y >= worldgen.MinY; y-- {
				if worldgen.SkyOpacity(chunkBlock(ch, lx, y, lz)) != 0 {
					break // sky is now obstructed; below here may be shadowed
				}
				if got := skyLevel(ld, lx, y, lz); got != 15 {
					t.Fatalf("open-sky block at (%d,%d,%d) = %d, want 15", lx, y, lz, got)
				}
			}
		}
	}
}

// TestSkyLightDeepIsDark: well below the surface the world should be dark — the
// regression guard against the old "ship full daylight everywhere" behaviour.
func TestSkyLightDeepIsDark(t *testing.T) {
	w := New(1)
	ld := w.Light(0, 0)

	lit, total := 0, 0
	for s := 0; s < 3; s++ { // bottom 3 sections: world y -64..-17
		for i := range ld.Sky[s] {
			total++
			if ld.Sky[s][i] > 0 {
				lit++
			}
		}
	}
	if lit*100/total > 5 {
		t.Fatalf("deep underground is %d%% lit, expected near-total darkness", lit*100/total)
	}
}

// TestSkyLightDeterministic: lighting is a pure function of the world, so the
// same chunk relights identically (re-streaming stays stable).
func TestSkyLightDeterministic(t *testing.T) {
	a := New(7).Light(2, -3)
	b := New(7).Light(2, -3)
	if len(a.Sky) != len(b.Sky) {
		t.Fatal("light data section counts differ")
	}
	for s := range a.Sky {
		if a.Sky[s] != b.Sky[s] || a.Block[s] != b.Block[s] {
			t.Fatal("sky light is not reproducible for the same seed/chunk")
		}
	}
}

// TestSkyLightSealedRoomIsDark: carving an air pocket inside solid rock and
// roofing it leaves the interior unlit (no torches yet), confirming light does
// not leak through opaque blocks.
func TestSkyLightSealedRoomIsDark(t *testing.T) {
	w := New(1)
	// Find a column whose surface is solid land, then a depth that is solid
	// stone all around to host a sealed pocket.
	const lx, lz = 8, 8
	wx, wz := lx, lz
	surface := int(w.SurfaceY(wx, wz))
	y := surface - 8 // comfortably underground
	// Hollow a 1-block pocket; its six neighbours stay solid generation rock.
	w.SetBlock(wx, y, wz, worldgen.Air)

	ld := w.Light(0, 0)
	if got := skyLevel(ld, lx, y, lz); got != 0 {
		t.Fatalf("sealed underground pocket lit at level %d, want 0", got)
	}
}

func TestBlockLightAt(t *testing.T) {
	w := New(1)
	w.SetBlock(0, 70, 0, worldgen.BlockID("glowstone")) // glowstone (emits 15)
	if got := w.BlockLightAt(0, 70, 0); got == 0 {
		t.Fatalf("block light at a glowstone should be >0, got %d", got)
	}
	if got := w.BlockLightAt(60, 70, 60); got != 0 {
		t.Fatalf("block light far from any emitter should be 0, got %d", got)
	}
}

// TestLightCacheInvalidation: chunk light is cached until an edit in the 3×3
// neighbourhood invalidates it — a placed torch must be visible to the next
// light read (mob spawning trusts this for its torch-protection rule).
func TestLightCacheInvalidation(t *testing.T) {
	w := New(1)
	surf := w.SurfaceFeet(40, 40)
	// Prime the cache with a read, then place a glowing block.
	if _, b := w.LightAt(40, surf, 40); b != 0 {
		t.Fatalf("unlit surface block light = %d, want 0", b)
	}
	w.SetBlock(40, surf, 40, worldgen.BlockID("glowstone"))
	if _, b := w.LightAt(40, surf+1, 40); b == 0 {
		t.Fatal("placed glowstone invisible to cached light — invalidation broken")
	}
	// Repeat reads serve from cache and stay correct.
	if _, b := w.LightAt(40, surf+1, 40); b == 0 {
		t.Fatal("cached re-read lost the glowstone light")
	}
}
