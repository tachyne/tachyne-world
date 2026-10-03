package worldgen

import "testing"

// /place structure nether_fossil lays a fossil out from any Nether chunk
// (every biome accepted), dropped onto a floor above the lava sea; outside
// the Nether there is no column to drop it on.
func TestPlaceNetherFossilStamp(t *testing.T) {
	if TemplateByName("nether_fossils/fossil_1") == nil {
		t.Skip("no fossil templates")
	}
	g := NewNetherGenerator(1)
	placed := 0
	for i := 0; i < 12 && placed == 0; i++ {
		x, z := i*64, -i*48
		st, ok := g.PlaceStructureStamp("minecraft:nether_fossil", x, z)
		if !ok {
			continue
		}
		f := g.netherFossilFrom(x>>4<<4, z>>4<<4, true)
		if !f.Exists || f.Y <= NetherLavaSea || f.X < st.X0 || f.X > st.X1 || f.Z < st.Z0 || f.Z > st.Z1 {
			t.Fatalf("fossil %+v outside its stamp %+v", f, st)
		}
		lo, hi := BlockRange("bone_block")
		for cx := int32(st.X0 >> 4); cx <= int32(st.X1>>4); cx++ {
			for cz := int32(st.Z0 >> 4); cz <= int32(st.Z1>>4); cz++ {
				ch := NewChunk(g.sections)
				st.Stamp(ch, cx, cz)
				for _, sec := range ch.Sections {
					for _, s := range sec {
						if s >= lo && s <= hi {
							placed++
						}
					}
				}
			}
		}
	}
	if placed == 0 {
		t.Fatal("no fossil laid a bone block")
	}
	if _, ok := NewGenerator(1).PlaceStructureStamp("nether_fossil", 0, 0); ok {
		t.Error("an Overworld generator placed a nether fossil")
	}
}

// The Nether's ore features grow in netherrack: an ore_quartz blob, and
// ancient debris that never touches air.
func TestPlaceNetherOres(t *testing.T) {
	g := NewNetherGenerator(1)
	rock := func(x, y, z int) uint32 {
		if y > 80 {
			return Air
		}
		return Netherrack
	}
	st, ok := g.PlaceFeatureStamp("minecraft:ore_quartz", 8, 40, 8, 5, rock)
	if !ok {
		t.Fatal("ore_quartz is not a /place feature")
	}
	ch := netherrackChunk(g)
	st.Stamp(ch, 0, 0)
	quartz := 0
	for _, sec := range ch.Sections {
		for _, s := range sec {
			if s == NetherQuartzOre {
				quartz++
			}
		}
	}
	if quartz == 0 {
		t.Error("the quartz blob placed no ore")
	}
	debris := 0
	for seed := int64(0); seed < 8; seed++ {
		cells := scatteredOre(newTreeRNG(seed, 0, 0), 0, 79, 0, 3, rock)
		for c, s := range cells {
			if s != AncientDebris {
				t.Fatalf("scattered ore placed %d", s)
			}
			if c[1]+1 > 80 {
				t.Fatalf("debris at %v beside the air above", c)
			}
			debris++
		}
	}
	if debris == 0 {
		t.Error("eight debris placements placed nothing")
	}
	for _, n := range []string{"ore_magma", "ore_soul_sand", "ore_nether_gold", "ore_gravel_nether", "ore_blackstone",
		"ore_ancient_debris_large", "ore_ancient_debris_small", "sculk_patch_ancient_city"} {
		found := false
		for _, m := range PlaceFeatureNames() {
			found = found || m == n
		}
		if !found {
			t.Errorf("%s is not a /place feature", n)
		}
	}
}
