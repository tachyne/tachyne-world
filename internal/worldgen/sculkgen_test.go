package worldgen

import "testing"

// TestDeepDarkGeneratesSculk searches for a deep_dark chunk and asserts it grew
// sculk (and, over the search, at least one can_summon shrieker exists so the
// Warden path has a natural trigger).
func TestDeepDarkGeneratesSculk(t *testing.T) {
	g := NewGenerator(1)
	foundDeep, sculkChunks, shriekers := 0, 0, 0
	for cx := int32(0); cx < 40 && shriekers == 0; cx++ {
		for cz := int32(0); cz < 40; cz++ {
			ch := g.GenerateChunk(cx, cz)
			deep := false
			for _, b := range ch.Biomes {
				if b == "minecraft:deep_dark" {
					deep = true
					break
				}
			}
			if !deep {
				continue
			}
			foundDeep++
			sculk, shr := 0, 0
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					switch {
					case b == wgSculk:
						sculk++
					case b >= wgSculkShrieker && b < wgSculkShrieker+8:
						shr++
					}
				}
			}
			if sculk > 0 {
				sculkChunks++
			}
			shriekers += shr
			if shriekers > 0 {
				break
			}
		}
	}
	t.Logf("deep_dark chunks: %d, with sculk: %d, shriekers seen: %d", foundDeep, sculkChunks, shriekers)
	if foundDeep == 0 {
		t.Skip("no deep_dark chunk in the search window (biome noise) — cannot assert")
	}
	if sculkChunks == 0 {
		t.Fatal("deep_dark chunks generated NO sculk floor")
	}
}

// caveWorld is a scratch view of a deepslate cave round (x, z): floor at
// y0, air from y0+1 to y0+4, a ceiling above.
func caveWorld(g *Generator, x, y0, z int) *sculkWorld {
	v := &owRegion{g: g, baseX: x &^ 15, baseZ: z &^ 15, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	for dx := -20; dx <= 20; dx++ {
		for dz := -20; dz <= 20; dz++ {
			for y := y0 - 3; y <= y0+8; y++ {
				s := Deepslate
				if y > y0 && y < y0+5 {
					s = Air
				}
				v.capture[[3]int{x + dx, y, z + dz}] = s
			}
		}
	}
	return &sculkWorld{view: v, orig: map[[3]int]uint32{}}
}

// SculkPatchFeature on a cave floor: the charges turn floor into sculk and
// hang veins about it, all within twelve blocks (and a little) of the
// start, and sensors or shriekers stand only on sculk.
func TestSculkPatchSpreads(t *testing.T) {
	g := NewGenerator(1)
	x, y0, z := 2000, -50, 2000
	sculk, veins, growths := 0, 0, 0
	for seed := int64(0); seed < 8; seed++ {
		w := caveWorld(g, x, y0, z)
		if !w.patch([3]int{x, y0 + 1, z}, newTreeRNG(seed, x, z)) {
			t.Fatal("a patch refused an air cell on a cave floor")
		}
		for c, s := range w.view.capture {
			if o, set := w.orig[c]; !set || o == s {
				continue // the fixture's own cells, or a cell set back as it was
			}
			if dx, dz := c[0]-x, c[2]-z; dx*dx+dz*dz > 14*14 {
				t.Fatalf("seed %d: sculk %d at %v, beyond the patch's reach", seed, s, c)
			}
			switch {
			case s == wgSculk:
				sculk++
				if w.orig[c] != Deepslate {
					t.Fatalf("seed %d: sculk replaced %d", seed, w.orig[c])
				}
			case isVein(s):
				veins++
				if !veinAnyFace(s) {
					t.Fatalf("seed %d: a vein with no face at %v", seed, c)
				}
			case s == sculkSensorState || s == sculkShriekerState:
				growths++
				if w.get([3]int{c[0], c[1] - 1, c[2]}) != wgSculk {
					t.Fatalf("seed %d: a growth at %v not on sculk", seed, c)
				}
			}
		}
	}
	if sculk == 0 || veins == 0 {
		t.Fatalf("patches made %d sculk and %d veins", sculk, veins)
	}
	t.Logf("8 patches: %d sculk, %d veins, %d sensors/shriekers", sculk, veins, growths)
}

// MultifaceGrowthFeature for sculk_vein: an air cell beside a deepslate
// wall grows a vein on that wall's face.
func TestSculkVeinGrowth(t *testing.T) {
	g := NewGenerator(1)
	x, y0, z := 2000, -50, 2000
	w := caveWorld(g, x, y0, z)
	p := [3]int{x, y0 + 1, z} // on the floor
	w.vein(p, newTreeRNG(3, x, z))
	s := w.get(p)
	if !isVein(s) {
		t.Fatalf("no vein at the origin (got %d)", s)
	}
	for d := 0; d < 6; d++ {
		if veinHasFace(s, d) && !IsFaceSturdy(w.get(sculkStep(p, d)), sculkOpp(d)) {
			t.Errorf("a vein face %s against nothing", sculkDirName[d])
		}
	}
	w2 := caveWorld(g, x, y0, z)
	w2.view.capture[[3]int{x, y0 + 2, z}] = Deepslate
	w2.vein([3]int{x, y0 + 2, z}, newTreeRNG(3, x, z))
	if isVein(w2.get([3]int{x, y0 + 2, z})) {
		t.Error("a vein grew inside rock")
	}
}

// deepDarkPlan is the first origin chunk with a sculk plan for seed 1.
func deepDarkPlan(t *testing.T, g *Generator) (int32, int32, *sculkPlan) {
	t.Helper()
	for cx := int32(0); cx < 40; cx++ {
		for cz := int32(0); cz < 40; cz++ {
			if p := g.sculkIn(cx, cz); p != nil {
				return cx, cz, p
			}
		}
	}
	t.Skip("no deep-dark sculk plan near the origin")
	return 0, 0, nil
}

// A patch with a player's build or dig in its box is rolled back: the cell
// is in no patch the new plan keeps.
func TestSculkBuildGuard(t *testing.T) {
	cx, cz, p := deepDarkPlan(t, NewGenerator(1))
	var at [3]int
	for c, cell := range p.cells {
		if cell.s == wgSculk {
			at = c
			break
		}
	}
	for name, e := range map[string]uint32{"build": BlockBase("stone_bricks"), "dug": Air} {
		g := NewGenerator(1)
		setTestEdits(g, map[[3]int]uint32{at: e})
		if q := g.sculkIn(cx, cz); q != nil {
			if _, ok := q.cells[at]; ok {
				t.Errorf("%s at %v: the sculk there is still planned", name, at)
			}
		}
	}
}

// Sculk goes only onto cells still as the plan found them: a block stamped
// there since (an ancient city's, a player's) keeps its place.
func TestSculkKeepsLaterBlocks(t *testing.T) {
	g := NewGenerator(1)
	cx, cz, p := deepDarkPlan(t, g)
	for c, cell := range p.cells {
		if floorDiv16(c[0]) != int(cx) || floorDiv16(c[2]) != int(cz) {
			continue
		}
		ch := g.GenerateChunk(cx, cz)
		lx, lz := c[0]-int(cx)*16, c[2]-int(cz)*16
		if got := sectionBlockAt(ch, lx, c[1], lz); got != cell.s && got == cell.orig {
			t.Errorf("planned sculk cell %v left as %d", c, got)
		}
		setSectionBlock(ch, lx, c[1], lz, Cobblestone, true)
		g.placeSculk(ch, cx, cz)
		if got := sectionBlockAt(ch, lx, c[1], lz); got != Cobblestone {
			t.Errorf("sculk overwrote a later block at %v (got %d)", c, got)
		}
		return
	}
	t.Skip("the plan wrote nothing in its own chunk")
}
