package worldgen

import "testing"

// A jungle temple stands in its jungle: mossy cobblestone at its corners,
// the two chests and the two dispensers where vanilla puts them.
func TestJungleTempleStamps(t *testing.T) {
	g := NewGenerator(5)
	var tp JungleTemple
	found := false
	for r := 0; r < 40 && !found; r++ {
		for cx := -r; cx <= r && !found; cx++ {
			for _, cz := range []int{-r, r} {
				if tt := g.JungleTempleIn(cx*512+8, cz*512+8); tt.Exists {
					tp, found = tt, true
					break
				}
			}
		}
	}
	if !found {
		t.Skip("no jungle temple within 40 cells for this seed")
	}
	read := func(x, y, z int) uint32 {
		ch := g.GenerateChunk(int32(x>>4), int32(z>>4))
		return sectionBlockAt(ch, x&15, y, z&15)
	}
	x, y, z := tp.world(0, 0, 0)
	if s := read(x, y, z); s != cobblestone && s != mossyCobblestone {
		t.Errorf("temple corner is %d", s)
	}
	for _, c := range tp.Chests() {
		if s := read(c[0], c[1], c[2]); s < ChestNorth || s > ChestNorth+23 {
			t.Errorf("no chest at %v: %d", c, s)
		}
	}
	for _, d := range tp.Dispensers() {
		if info, ok := InfoForState(read(d[0], d[1], d[2])); !ok || !info.HasProperty("triggered") {
			t.Errorf("no dispenser at %v", d)
		}
	}
}

// A swamp hut stands on its stilts with the cauldron inside.
func TestSwampHutStamps(t *testing.T) {
	g := NewGenerator(5)
	var hut SwampHut
	found := false
	for r := 0; r < 40 && !found; r++ {
		for cx := -r; cx <= r && !found; cx++ {
			for _, cz := range []int{-r, r} {
				if h := g.SwampHutIn(cx*512+8, cz*512+8); h.Exists {
					hut, found = h, true
					break
				}
			}
		}
	}
	if !found {
		t.Skip("no swamp hut within 40 cells for this seed")
	}
	x, y, z := hut.world(4, 2, 6)
	ch := g.GenerateChunk(int32(x>>4), int32(z>>4))
	if s := sectionBlockAt(ch, x&15, y, z&15); s != blockBase("cauldron") {
		t.Errorf("no cauldron in the hut: %d", s)
	}
	hx, hy, hz := hut.Home()
	ch = g.GenerateChunk(int32(hx>>4), int32(hz>>4))
	if s := sectionBlockAt(ch, hx&15, hy, hz&15); s != Air {
		t.Errorf("the witch's spot is not clear: %d", s)
	}
}

// Nether fossils lie in the soul sand valley: bone blocks appear across it.
func TestNetherFossilsInValley(t *testing.T) {
	g := NewNetherGenerator(7)
	if TemplateByName("nether_fossils/fossil_1") == nil {
		t.Fatal("nether fossil templates are not baked")
	}
	cx, cz, ok := findNetherBiomeChunk(g, "minecraft:soul_sand_valley")
	if !ok {
		t.Skip("no soul sand valley within 64 chunks")
	}
	lo, hi := BlockRange("bone_block")
	bones := 0
	for dcx := int32(-4); dcx <= 4 && bones == 0; dcx++ {
		for dcz := int32(-4); dcz <= 4 && bones == 0; dcz++ {
			ch := g.generateNetherChunk(cx+dcx, cz+dcz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if b >= lo && b <= hi {
						bones++
					}
				}
			}
		}
	}
	if bones == 0 {
		t.Error("no bone blocks in eighty-one valley chunks")
	}
}
