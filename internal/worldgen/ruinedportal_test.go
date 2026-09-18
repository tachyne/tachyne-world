package worldgen

import "testing"

// A jungle portal stands overgrown: vines on its masonry and jungle leaves
// on its netherrack; a desert one lies partly buried; an ocean one stands
// on the sea floor with magma for lava.
func TestRuinedPortalVariants(t *testing.T) {
	g := NewGenerator(9)
	found := map[string]RuinedPortal{}
	for r := 0; r < 60 && len(found) < 3; r++ {
		for cx := -r; cx <= r && len(found) < 3; cx++ {
			for _, cz := range []int{-r, r} {
				p := g.RuinedPortalIn(cx*portalCell+8, cz*portalCell+8)
				if !p.Exists {
					continue
				}
				b := g.BiomeName(p.X, p.Z)
				switch {
				case (b == "minecraft:jungle" || b == "minecraft:bamboo_jungle" || b == "minecraft:sparse_jungle") && found["jungle"].Exists == false:
					found["jungle"] = p
				case b == "minecraft:desert" && found["desert"].Exists == false:
					found["desert"] = p
				case portalBiomeSets[b] != nil && portalSetupsFor(b)[0].placement == plOceanFloor && b != "minecraft:swamp" && b != "minecraft:mangrove_swamp" && found["ocean"].Exists == false:
					found["ocean"] = p
				}
			}
		}
	}
	count := func(p RuinedPortal, want func(uint32) bool) int {
		n := 0
		for cx := int32((p.X - 16) >> 4); cx <= int32((p.X+32)>>4); cx++ {
			for cz := int32((p.Z - 16) >> 4); cz <= int32((p.Z+32)>>4); cz++ {
				ch := g.GenerateChunk(cx, cz)
				for s := range ch.Sections {
					for _, b := range ch.Sections[s] {
						if want(b) {
							n++
						}
					}
				}
			}
		}
		return n
	}
	if p, ok := found["jungle"]; ok {
		if !p.Props.vines || !p.Props.overgrown {
			t.Errorf("jungle portal props %+v", p.Props)
		}
		if count(p, func(b uint32) bool { return in(b, rangeOf("vine")) }) == 0 {
			t.Error("no vines on the jungle portal")
		}
		if count(p, func(b uint32) bool { return in(b, rangeOf("jungle_leaves")) }) == 0 {
			t.Error("no leaves over the jungle portal's netherrack")
		}
	} else {
		t.Log("no jungle portal found within reach")
	}
	if p, ok := found["desert"]; ok {
		if p.Props.placement != plPartlyBuried || p.Y >= g.Height(p.X, p.Z) {
			t.Errorf("desert portal not buried: Y %d surface %d", p.Y, g.Height(p.X, p.Z))
		}
	}
	if p, ok := found["ocean"]; ok {
		if p.Y >= SeaLevel {
			t.Errorf("ocean portal above the sea: Y %d", p.Y)
		}
		if count(p, func(b uint32) bool { return b == rpMagma }) == 0 {
			t.Error("no magma at the ocean portal")
		}
	}
	// the block ageing and rules on a synthetic stamp: mossy masonry appears
	tmpl := TemplateByName("ruined_portal/portal_1")
	if tmpl == nil {
		t.Fatal("no portal template")
	}
	ch := NewChunk(SectionCount)
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			setSectionBlock(ch, x, 70, z, Stone, true)
		}
	}
	p := RuinedPortal{X: 100002, Y: 71, Z: 100002, Tmpl: "ruined_portal/portal_1", Exists: true, Props: portalProps{mossiness: 0.8, placement: plLand}}
	g.stampRuinedPortalVariant(ch, 6250, 6250, p, tmpl)
	mossy, netherrack := 0, 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b == rpMossyBricks || in(b, rpMossyStairs) || in(b, rpMossySlab) || in(b, rpMossyWall) {
				mossy++
			}
			if b == rpNetherrack || b == rpMagma {
				netherrack++
			}
		}
	}
	if mossy == 0 || netherrack == 0 {
		t.Errorf("aged stamp: mossy %d netherrack %d", mossy, netherrack)
	}
}

// Half the portals are mirrored front-to-back: the mirror flips the
// template's X within its footprint before the rotation, and its chests
// with it.
func TestRuinedPortalMirror(t *testing.T) {
	g := NewGenerator(3)
	mirrored, plain := 0, 0
	for i := -40; i < 40 && (mirrored == 0 || plain == 0); i++ {
		for j := -40; j < 40; j++ {
			p := g.RuinedPortalIn(i*portalCell+8, j*portalCell+8)
			if !p.Exists {
				continue
			}
			if p.Mir == mirFB {
				mirrored++
			} else {
				plain++
			}
			tmpl := TemplateByName(p.Tmpl)
			for k, c := range tmpl.Chests {
				rx, ry, rz := tmpl.placePos(c[0], c[1], c[2], p.Rot, p.Mir)
				if got := p.Chests[k]; got != [3]int{p.X + rx, p.Y + ry, p.Z + rz} {
					t.Fatalf("chest %d at %v, want the mirrored placement", k, got)
				}
			}
		}
	}
	if mirrored == 0 || plain == 0 {
		t.Fatalf("portals: %d mirrored, %d plain — want both", mirrored, plain)
	}
	tm := &Template{Size: [3]int{5, 1, 3}}
	if x, _, z := tm.placePos(0, 0, 0, 0, mirFB); x != 4 || z != 0 {
		t.Fatalf("front-back mirror of the corner gave %d,%d", x, z)
	}
	if x, _, z := tm.placePos(0, 0, 0, 1, mirFB); x != 2 || z != 4 {
		t.Fatalf("mirror then clockwise turn gave %d,%d", x, z)
	}
}
