package worldgen

import "testing"

func findVillage(g *Generator) (Village, bool) {
	for x := -villageCell * 30; x <= villageCell*30; x += villageCell {
		for z := -villageCell * 30; z <= villageCell*30; z += villageCell {
			if v := g.VillageIn(x+8, z+8); v.Exists {
				return v, true
			}
		}
	}
	return Village{}, false
}

func TestVillageStamps(t *testing.T) {
	g := NewGenerator(7)
	v, ok := findVillage(g)
	if !ok {
		t.Fatal("no village in 61x61 cells — odds or flatness gate broken")
	}
	if len(v.Houses) < 4 {
		t.Fatalf("village should have 4+ houses, has %d", len(v.Houses))
	}
	// The well chunk carries cobble at the rim and a bell.
	ch := g.GenerateChunk(int32(v.X>>4), int32(v.Z>>4))
	rimX, rimZ := v.X-2, v.Z-2
	sec := (v.Y - MinY) / 16
	i := ((v.Y-MinY)%16*16+(rimZ&15))*16 + (rimX & 15)
	if (rimX>>4) == (v.X>>4) && (rimZ>>4) == (v.Z>>4) && ch.Sections[sec][i] != Cobblestone {
		t.Fatalf("well rim should be cobblestone, got %d", ch.Sections[sec][i])
	}
	// A house floor chunk has planks at its centre.
	h := v.Houses[0]
	hc := g.GenerateChunk(int32(h.X>>4), int32(h.Z>>4))
	fsec := (h.Y - 1 - MinY) / 16
	fi := ((h.Y-1-MinY)%16*16+(h.Z&15))*16 + (h.X & 15)
	if hc.Sections[fsec][fi] != OakPlanks {
		t.Fatalf("house floor should be planks, got %d", hc.Sections[fsec][fi])
	}
}

// TestHouseIsFurnishedAndRoofed: every house must have a bed, a real door, and
// a peaked stair roof — not the old empty flat-roofed box.
func TestHouseIsFurnishedAndRoofed(t *testing.T) {
	g := NewGenerator(7)
	v, ok := findVillage(g)
	if !ok {
		t.Skip("no village rolled")
	}
	// Scan the house's chunk neighbourhood for the new features.
	h := v.Houses[0]
	beds, doors, stairs := 0, 0, 0
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			ch := g.GenerateChunk(int32(h.X>>4)+dcx, int32(h.Z>>4)+dcz)
			for _, sec := range ch.Sections {
				for _, b := range sec {
					switch b {
					case BedFootNorth, BedHeadNorth:
						beds++
					case OakDoorLowerS, OakDoorUpperS:
						doors++
					case OakStairsNorth, OakStairsSouth:
						stairs++
					}
				}
			}
		}
	}
	if beds == 0 {
		t.Error("house has no bed")
	}
	if doors == 0 {
		t.Error("house has no door")
	}
	if stairs == 0 {
		t.Error("house has a flat roof (no peaked stairs)")
	}
}

func TestVillageOnlyOnFlatDryLand(t *testing.T) {
	g := NewGenerator(7)
	v, ok := findVillage(g)
	if !ok {
		t.Skip("no village rolled")
	}
	if v.Y <= SeaLevel+1 {
		t.Fatalf("village at y=%d is underwater", v.Y)
	}
}
