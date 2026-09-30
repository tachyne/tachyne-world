package worldgen

import "testing"

func TestOceanStructuresGenerate(t *testing.T) {
	g := NewGenerator(1)
	var ship Shipwreck
	var mon Monument
	for i := 0; i < 90 && (!ship.Exists || !mon.Exists); i++ {
		for j := 0; j < 90; j++ {
			if !ship.Exists {
				if s := g.ShipwreckIn(i*shipwreckCell+160, j*shipwreckCell+160); s.Exists {
					ship = s
				}
			}
			if !mon.Exists {
				if m := g.MonumentIn(i*monumentCell+224, j*monumentCell+224); m.Exists {
					mon = m
				}
			}
		}
	}

	if ship.Exists {
		ch := g.GenerateChunk(int32(ship.X>>4), int32(ship.Z>>4))
		planks, chests := 0, 0
		for s := range ch.Sections {
			for _, b := range ch.Sections[s] {
				switch b {
				case OakPlanks:
					planks++
				case ChestNorth:
					chests++
				}
			}
		}
		t.Logf("shipwreck %d,%d,%d planks=%d north-facing chests=%d, loot chests reported %d", ship.X, ship.Y, ship.Z, planks, chests, len(ship.Chests))
		if planks == 0 {
			t.Error("shipwreck hull should survive generation")
		}
		// The loot chests the lookup reports stand where it says (any facing,
		// waterlogged or not; they need not be in the origin chunk).
		for _, c := range ship.Chests {
			cc := g.GenerateChunk(int32(c.X>>4), int32(c.Z>>4))
			if s := sectionBlockAt(cc, c.X&15, c.Y, c.Z&15); !isChestState(s) {
				t.Errorf("shipwreck chest %v missing (got %d)", c, s)
			}
		}
	}

	if mon.Exists {
		// The core room (and its gold) sits in one of the grid's first four
		// columns, chosen per monument, so the whole footprint is scanned.
		bricks, gold := 0, 0
		for cx := (mon.X - 30) >> 4; cx <= (mon.X+30)>>4; cx++ {
			for cz := (mon.Z - 30) >> 4; cz <= (mon.Z+30)>>4; cz++ {
				ch := g.GenerateChunk(int32(cx), int32(cz))
				for s := range ch.Sections {
					for _, b := range ch.Sections[s] {
						switch b {
						case blockBase("prismarine_bricks"):
							bricks++
						case blockBase("gold_block"):
							gold++
						}
					}
				}
			}
		}
		t.Logf("monument %d,%d,%d bricks=%d gold=%d", mon.X, mon.Y, mon.Z, bricks, gold)
		if bricks == 0 || gold == 0 {
			t.Error("monument shell + gold core should survive generation")
		}
	}
}
