package worldgen

import "testing"

// TestDesertTempleQuery checks the placement query in isolation — deterministic,
// desert-only, four distinct chest slots. (Stamping is exercised in-engine; a
// chunk-gen block assertion here would depend on cross-machine float
// determinism of the terrain height, which musl vs glibc don't guarantee.)
func TestDesertTempleQuery(t *testing.T) {
	g := NewGenerator(7)
	var d DesertTemple
	found := false
	for cx := 0; cx < 24000 && !found; cx += templeCell {
		for cz := 0; cz < 24000; cz += templeCell {
			if q := g.DesertTempleIn(cx+templeCell/2, cz+templeCell/2); q.Exists {
				d, found = q, true
				break
			}
		}
	}
	if !found {
		t.Skip("no desert temple within scan range for this seed")
	}
	// Deterministic: a second query for the same cell returns the same temple.
	if q := g.DesertTempleIn(d.X, d.Z); q != d {
		t.Fatalf("DesertTempleIn not deterministic: %+v vs %+v", q, d)
	}
	// Desert-only.
	if name := g.BiomeName(d.X, d.Z); name != "minecraft:desert" {
		t.Fatalf("temple sited off-desert: %s", name)
	}
	// Four distinct chest positions.
	seen := map[[3]int]bool{}
	for _, c := range d.Chests() {
		seen[c] = true
	}
	if len(seen) != 4 {
		t.Fatalf("expected 4 distinct chests, got %d", len(seen))
	}
}

// TestRuinedPortalStamps finds an actually-stamped crying-obsidian frame block in
// generated chunks and confirms the query agrees it's a portal — the robust
// stamp-vs-query pattern (cf. TestDungeonQueryMatchesStamp), independent of
// cross-machine height float determinism.
func TestRuinedPortalStamps(t *testing.T) {
	g := NewGenerator(7)
	x, _, z, ok := findWith(g, 24, func(b uint32) bool { return b == CryingObsidian })
	if !ok {
		t.Skip("no ruined portal within scan radius for this seed")
	}
	p := g.RuinedPortalIn(x, z)
	if !p.Exists {
		t.Fatalf("crying obsidian at (%d,%d) but RuinedPortalIn empty", x, z)
	}
	// The found block must lie within the (rotated) template footprint.
	t0 := TemplateByName(p.Tmpl)
	sx, sz := t0.Size[0], t0.Size[2]
	if p.Rot&1 == 1 {
		sx, sz = sz, sx
	}
	if x < p.X-1 || x > p.X+sx || z < p.Z-1 || z > p.Z+sz {
		t.Fatalf("crying obsidian (%d,%d) outside portal %q footprint at (%d,%d) size %dx%d",
			x, z, p.Tmpl, p.X, p.Z, sx, sz)
	}
}
