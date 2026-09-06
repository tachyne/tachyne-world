package worldgen

import "testing"

// A bastion assembles from the vanilla start pool into a real structure: many
// pieces, chests that name their own vanilla tables, mob pieces to seed, and
// the pools' rule processors degrade the masonry deterministically.
func TestBastionAssembles(t *testing.T) {
	g := NewGenerator(7)
	b := Bastion{X: 1000, Y: bastionY, Z: 1000, Exists: true}
	pieces := g.AssembleBastion(b)
	if len(pieces) < 8 {
		t.Fatalf("bastion assembled only %d pieces", len(pieces))
	}
	proc := 0
	for _, p := range pieces {
		if p.Proc != "" {
			if processors[p.Proc] == nil {
				t.Errorf("piece %s names an unbaked processor list %q", p.Tmpl.name, p.Proc)
			}
			proc++
		}
	}
	if proc == 0 {
		t.Error("no piece carries a processor list")
	}
	chests := g.BastionChests(b)
	if len(chests) == 0 {
		t.Fatal("a bastion has loot chests")
	}
	for _, c := range chests {
		switch c.Table {
		case "chests/bastion_other", "chests/bastion_treasure", "chests/bastion_bridge", "chests/bastion_hoglin_stable":
		default:
			t.Errorf("chest at %d,%d,%d has table %q", c.X, c.Y, c.Z, c.Table)
		}
	}
	if len(g.BastionMobs(b)) == 0 {
		t.Error("a bastion seeds piglins/hoglins from its mob pieces")
	}
	// Processor rules: 30% of polished blackstone bricks crack, position-seeded.
	rules := processors["bastion_generic_degradation"]
	if len(rules) == 0 {
		t.Fatal("bastion_generic_degradation not baked")
	}
	bricks, _ := BlockRange("polished_blackstone_bricks")
	crackedLo, crackedHi := BlockRange("cracked_polished_blackstone_bricks")
	cracked, n := 0, 0
	for x := 0; x < 40; x++ {
		for z := 0; z < 40; z++ {
			n++
			out := applyRules(rules, bricks, x, 40, z, 0, 33, 0)
			if out >= crackedLo && out <= crackedHi {
				cracked++
			} else if out != bricks {
				t.Fatalf("unexpected output state %d", out)
			}
			if applyRules(rules, bricks, x, 40, z, 0, 33, 0) != out {
				t.Fatal("processor rolls must be deterministic per position")
			}
		}
	}
	if frac := float64(cracked) / float64(n); frac < 0.2 || frac > 0.4 {
		t.Errorf("cracked %.2f of the bricks, want about 0.3", frac)
	}
}

// Bastions sit in the Nether's qualifying biomes only, and the site query is
// stable across the cell.
func TestBastionSiting(t *testing.T) {
	g := NewGenerator(3)
	found := 0
	for cx := -20; cx < 20; cx++ {
		for cz := -20; cz < 20; cz++ {
			b := g.BastionIn(cx*bastionCell+8, cz*bastionCell+8)
			if !b.Exists {
				continue
			}
			found++
			if b.Y != bastionY || g.netherBiome(b.X, b.Z) == "minecraft:basalt_deltas" {
				t.Errorf("bad site %+v", b)
			}
			if b2 := g.BastionIn(b.X, b.Z); b2 != b {
				t.Errorf("site query unstable: %+v vs %+v", b, b2)
			}
		}
	}
	if found == 0 {
		t.Error("no bastion in 1600 cells")
	}
}
