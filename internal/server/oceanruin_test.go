package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func findTestRuin(g *worldgen.Generator) (worldgen.OceanRuins, bool) {
	for i := -40; i < 40; i++ {
		for j := -40; j < 40; j++ {
			if r := g.OceanRuinsIn(i*320+160, j*320+160); r.Exists {
				return r, true
			}
		}
	}
	return worldgen.OceanRuins{}, false
}

// A player arriving at an ocean ruin seeds its drowned once (persistent, so
// they never despawn); its chest fills from underwater_ruin_big/small and
// its suspicious blocks brush from the warm/cold ocean-ruin archaeology
// tables.
func TestOceanRuinSeedsAndLoots(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	r, ok := findTestRuin(h.world.Gen())
	if !ok {
		t.Fatal("no ocean ruin found")
	}
	want := 0
	for _, p := range r.Pieces {
		want += len(p.Drowned)
	}
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = float64(r.X), 40, float64(r.Z)
	h.populateOceanRuins(players)
	drowned := 0
	for _, m := range h.mobs {
		if m.etype == entityDrowned {
			drowned++
			if !m.persistent {
				t.Error("a seeded drowned must be persistent")
			}
		}
	}
	if drowned != want {
		t.Errorf("seeded %d drowned, markers hold %d", drowned, want)
	}
	h.populateOceanRuins(players)
	again := 0
	for _, m := range h.mobs {
		if m.etype == entityDrowned {
			again++
		}
	}
	if again != drowned {
		t.Errorf("a second visit seeded more drowned (%d -> %d)", drowned, again)
	}

	for _, p := range r.Pieces {
		for _, c := range p.Chests {
			tbl, ok := h.structureChestTable(blockPos{c.X, c.Y, c.Z})
			if !ok || tbl != c.Table {
				t.Errorf("chest at %d,%d,%d routes to %q (%v), want %q", c.X, c.Y, c.Z, tbl, ok, c.Table)
			}
			if _, found := lootForChest(tbl); !found {
				t.Errorf("loot table %q is not baked", tbl)
			}
		}
		for _, s := range p.Sus {
			tbl, ok := h.brushLootTable(blockPos{s[0], s[1], s[2]})
			wantTbl := "archaeology/ocean_ruin_cold"
			if r.Warm {
				wantTbl = "archaeology/ocean_ruin_warm"
			}
			if !ok || tbl != wantTbl {
				t.Errorf("suspicious block at %v brushes %q (%v), want %q", s, tbl, ok, wantTbl)
			}
			if _, found := lootForChest(tbl); !found {
				t.Errorf("archaeology table %q is not baked", tbl)
			}
		}
	}
	if _, ok := h.structureChestTable(blockPos{r.X + 1, 0, r.Z + 1}); ok {
		t.Error("a cell that is not a ruin chest must not route")
	}
}

// The desert temple's cellar sand brushes from the desert_pyramid table.
func TestDesertTempleBrushTable(t *testing.T) {
	h := newHub(world.New(7))
	g := h.world.Gen()
	var d worldgen.DesertTemple
	for cx := 0; cx < 24000 && !d.Exists; cx += 336 {
		for cz := 0; cz < 24000 && !d.Exists; cz += 336 {
			d = g.DesertTempleIn(cx+168, cz+168)
		}
	}
	if !d.Exists {
		t.Skip("no desert temple in range")
	}
	if len(d.Sus) == 0 {
		t.Fatal("temple seeds no suspicious sand")
	}
	for _, s := range d.Sus {
		tbl, ok := h.brushLootTable(blockPos{s[0], s[1], s[2]})
		if !ok || tbl != "archaeology/desert_pyramid" {
			t.Errorf("cell %v brushes %q (%v)", s, tbl, ok)
		}
	}
	if _, found := lootForChest("archaeology/desert_pyramid"); !found {
		t.Error("desert_pyramid archaeology table is not baked")
	}
}

// Trail-ruins suspicious gravel brushes from the common and rare tables the
// capped rules appended, and both tables are baked.
func TestTrailRuinsBrushTables(t *testing.T) {
	h := newHub(world.New(1))
	g := h.world.Gen()
	var site worldgen.TrailRuins
	for i := -40; i < 40 && !site.Exists; i++ {
		for j := -40; j < 40 && !site.Exists; j++ {
			site = g.TrailRuinsIn(i*544+272, j*544+272)
		}
	}
	if !site.Exists {
		t.Fatal("no trail ruins found")
	}
	sus := g.TrailRuinsSus(site)
	if len(sus) == 0 {
		t.Fatal("site holds no suspicious gravel")
	}
	rare := 0
	for _, s := range sus {
		tbl, ok := h.brushLootTable(blockPos{s.X, s.Y, s.Z})
		if !ok || tbl != s.Table {
			t.Errorf("cell %d,%d,%d brushes %q (%v), want %q", s.X, s.Y, s.Z, tbl, ok, s.Table)
		}
		if _, found := lootForChest(tbl); !found {
			t.Errorf("archaeology table %q is not baked", tbl)
		}
		if tbl == "archaeology/trail_ruins_rare" {
			rare++
		}
	}
	if rare == 0 {
		t.Error("a site should hold some rare finds")
	}
}
