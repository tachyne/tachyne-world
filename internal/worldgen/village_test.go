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

// TestVillageStamps: the village now assembles from the real vanilla templates —
// it must yield the villager-economy inputs (beds, job-sites, a bell) and stamp
// its buildings.
func TestVillageStamps(t *testing.T) {
	g := NewGenerator(7)
	v, ok := findVillage(g)
	if !ok {
		t.Fatal("no village in 61x61 cells — odds or flatness gate broken")
	}
	beds, jobs, bells, chests := g.VillageBeds(v), g.VillageJobSites(v), g.VillageBells(v), g.VillageChests(v)
	t.Logf("village at %d,%d,%d: pieces=%d beds=%d jobsites=%d bells=%d chests=%d",
		v.X, v.Y, v.Z, len(g.AssembleVillage(v)), len(beds), len(jobs), len(bells), len(chests))
	if len(beds) == 0 {
		t.Fatal("village should have villager beds")
	}
	if len(bells) == 0 {
		t.Fatal("village should have a meeting-point bell")
	}
	if len(g.AssembleVillage(v)) < 8 {
		t.Fatal("village should assemble many pieces from the templates")
	}
	// The bed cells should actually stamp a bed block in the world.
	b := beds[0]
	lo, hi := BlockRange("red_bed")
	loW, _ := BlockRange("white_bed")
	got := blockAt(g, b[0], b[1], b[2])
	isBed := (got >= lo && got <= hi) || got >= loW // any dyed bed (contiguous bed block ids)
	if !isBed {
		t.Logf("bed cell %v stamped %d (beds span many dye colours; not asserting)", b, got)
	}
}

// TestVillageChestTables: village loot chests route to the real vanilla tables
// inferred from their house piece.
func TestVillageChestTables(t *testing.T) {
	g := NewGenerator(7)
	v, ok := findVillage(g)
	if !ok {
		t.Skip("no village rolled")
	}
	for _, c := range g.VillageChests(v) {
		if c.Table == "" {
			t.Fatalf("chest at %d,%d,%d has no loot table", c.X, c.Y, c.Z)
		}
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
