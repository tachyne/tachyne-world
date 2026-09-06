package worldgen

import "testing"

// An End city grows from the house tower into towers and bridges; its
// chests name the treasure table and its templates carry the sentries.
func TestEndCityAssembles(t *testing.T) {
	g := NewGenerator(11)
	found := 0
	for seed := int64(1); seed <= 6; seed++ {
		c := EndCity{X: 1300, Y: 60, Z: 100 * int(seed), Rot: int(seed) & 3, Exists: true}
		pieces := g.AssembleEndCity(c)
		if len(pieces) < 4 {
			t.Fatalf("city %d assembled only %d pieces", seed, len(pieces))
		}
		found += len(pieces)
		for _, p := range pieces {
			if p.x1 <= p.OX || p.z1 <= p.OZ {
				t.Fatalf("piece %s has an empty footprint %+v", p.Tmpl.name, p)
			}
		}
		if cs := g.EndCityChests(c); len(cs) > 0 && cs[0].Table != "chests/end_city_treasure" {
			t.Errorf("chest table %q", cs[0].Table)
		}
	}
	if found < 40 {
		t.Errorf("six cities made only %d pieces in total; the generators should branch", found)
	}
	// Sentries are baked from the templates' DATA markers.
	sentries := 0
	for _, name := range []string{"end_city/tower_top", "end_city/fat_tower_top", "end_city/ship", "end_city/third_roof", "end_city/second_roof", "end_city/base_roof"} {
		if tm := TemplateByName(name); tm != nil {
			for _, m := range tm.Mobs {
				if m.Type == "shulker" {
					sentries++
				}
			}
		}
	}
	if sentries == 0 {
		t.Error("no shulker sentry markers baked")
	}
	ship := TemplateByName("end_city/ship")
	elytra := false
	for _, m := range ship.Mobs {
		if m.Type == "elytra_frame" {
			elytra = true
		}
	}
	if !elytra {
		t.Error("the ship carries the elytra frame marker")
	}
}

// rotateOffset is vanilla's zero-pivot rotation, and placed() maps a
// rotated piece onto our non-negative footprint without moving its blocks.
func TestEndCityRotation(t *testing.T) {
	if x, z := rotateOffset(1, 0, 1); x != 0 || z != 1 {
		t.Errorf("clockwise 90 of (1,0) = (%d,%d), want (0,1)", x, z)
	}
	if x, z := rotateOffset(1, 0, 3); x != 0 || z != -1 {
		t.Errorf("counter-clockwise 90 of (1,0) = (%d,%d), want (0,-1)", x, z)
	}
	tm := TemplateByName("end_city/base_floor")
	for rot := 0; rot < 4; rot++ {
		p := (&endPiece{tmpl: tm, x: 100, y: 60, z: 200, rot: rot, overwrite: true}).placed()
		// The template's origin block (0,0,0) must land at the vanilla
		// position whatever the rotation.
		rx, _, rz := tm.rotatePos(0, 0, 0, rot)
		if p.OX+rx != 100 || p.OZ+rz != 200 {
			t.Errorf("rot %d: origin lands at (%d,%d), want (100,200)", rot, p.OX+rx, p.OZ+rz)
		}
	}
}

func TestEndCitySiting(t *testing.T) {
	g := NewGenerator(5)
	found := 0
	for cx := -8; cx < 8; cx++ {
		for cz := -8; cz < 8; cz++ {
			c := g.EndCityIn(cx*endCityCell+8, cz*endCityCell+8)
			if !c.Exists {
				continue
			}
			found++
			if g.endBiome(c.X, c.Z) != "minecraft:end_highlands" || c.Y < 40 {
				t.Errorf("bad site %+v", c)
			}
		}
	}
	if found == 0 {
		t.Error("no End city in 256 cells")
	}
}

// A generated End chunk over a city carries the city's purpur.
func TestEndCityStamps(t *testing.T) {
	g := NewGenerator(5)
	var c EndCity
	for cx := -8; cx < 8 && !c.Exists; cx++ {
		for cz := -8; cz < 8 && !c.Exists; cz++ {
			c = g.EndCityIn(cx*endCityCell+8, cz*endCityCell+8)
		}
	}
	if !c.Exists {
		t.Skip("no city")
	}
	ch := g.generateEndChunk(int32(floorDiv(c.X, 16)), int32(floorDiv(c.Z, 16)))
	lo, hi := BlockRange("purpur_block")
	found := 0
	for _, sec := range ch.Sections {
		for _, st := range sec {
			if st >= lo && st <= hi {
				found++
			}
		}
	}
	if found == 0 {
		t.Error("the city's chunk holds no purpur")
	}
}
