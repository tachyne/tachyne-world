package worldgen

import (
	"strings"
	"testing"
)

// Camps appear in their biomes, built from their own variant's templates,
// with a looted container on the campsite and trees at the tree jigsaws.
func TestAbandonedCampsGenerate(t *testing.T) {
	g := NewGenerator(1)
	chestLo, _, _ := BlockRangeOK("chest")
	barrelLo, barrelHi, _ := BlockRangeOK("barrel")
	chestHi := chestLo + 23
	camps, trees, chests := 0, 0, 0
	variants := map[string]bool{}
	for cz := -6000; cz <= 6000 && camps < 12; cz += campCell {
		for cx := -6000; cx <= 6000 && camps < 12; cx += campCell {
			c := g.AbandonedCampIn(cx, cz)
			if !c.Exists {
				continue
			}
			camps++
			variants[c.Biome] = true
			pieces := g.AssembleAbandonedCamp(c)
			if len(pieces) < 2 {
				t.Errorf("camp at %d,%d (%s) assembled %d pieces", c.X, c.Z, c.Biome, len(pieces))
			}
			if !strings.Contains(pieces[0].Tmpl.name, "/tent/"+c.Biome+"/") {
				t.Errorf("camp in %s started from %s", c.Biome, pieces[0].Tmpl.name)
			}
			for _, p := range pieces {
				if p.Feature != "" {
					trees++
				}
			}
			for _, cc := range g.AbandonedCampChests(c.X, c.Z) {
				if !strings.Contains(cc.Table, "/abandoned_camp_") {
					t.Errorf("camp container routes to %q", cc.Table)
				}
				ch := g.GenerateChunk(int32(cc.X>>4), int32(cc.Z>>4))
				s := sectionBlockAt(ch, cc.X&15, cc.Y, cc.Z&15)
				name, _ := StateName(s)
				// a chest, a copper chest of any age, or a barrel
				if !(s >= chestLo && s <= chestHi) && !(s >= barrelLo && s <= barrelHi) && !strings.HasSuffix(name, "copper_chest") {
					t.Errorf("no chest or barrel at the camp's container cell %d,%d,%d (state %d)", cc.X, cc.Y, cc.Z, s)
				}
				chests++
			}
			if x, z, ok := g.LocateStructure("abandoned_camp_"+c.Biome, c.X, c.Z, 100); !ok || x != c.X || z != c.Z {
				t.Errorf("the %s camp's own locator missed it", c.Biome)
			}
		}
	}
	t.Logf("%d camps in %d variants %v, %d tree pieces, %d containers", camps, len(variants), variants, trees, chests)
	if camps == 0 || chests == 0 {
		t.Fatalf("%d camps, %d containers", camps, chests)
	}
	if trees == 0 {
		t.Error("no camp grew a tree piece")
	}
}
