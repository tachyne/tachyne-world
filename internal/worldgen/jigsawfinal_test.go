package worldgen

import "testing"

// A placed piece's jigsaw blocks become their final_state (vanilla's
// JigsawReplacementProcessor) — they used to be skipped, leaving holes.
func TestJigsawBlocksBecomeTheirFinalState(t *testing.T) {
	g := NewGenerator(1)
	for name, tmpl := range templates {
		for _, j := range tmpl.Jigsaws {
			want, ok := jigsawFinalState(j.Final)
			if !ok || want == Air {
				continue
			}
			// Place the piece so this jigsaw lands inside chunk (0,0).
			ch := NewChunk(g.sections)
			p := PlacedPiece{Tmpl: tmpl, OX: 8 - j.Pos[0], OY: 100 - j.Pos[1], OZ: 8 - j.Pos[2]}
			g.StampPieces(ch, 0, 0, []PlacedPiece{p})
			if got := sectionBlockAt(ch, 8, 100, 8); got != want {
				t.Fatalf("%s: jigsaw cell holds %d, want its final state %s (%d)", name, got, j.Final, want)
			}
			return
		}
	}
	t.Fatal("no template with a solid final_state")
}

// A terrain_matching street follows the ground: each column of it lands at
// that column's surface, however the ground slopes under the piece.
func TestStreetsFollowTheGround(t *testing.T) {
	g := NewGenerator(1)
	for cz := -3000; cz <= 3000; cz += villageCell {
		for cx := -3000; cx <= 3000; cx += villageCell {
			v := g.VillageIn(cx, cz)
			if !v.Exists {
				continue
			}
			for _, p := range g.AssembleVillage(v) {
				if !p.TerrainMatch {
					continue
				}
				for _, b := range p.Tmpl.Blocks {
					st := p.Tmpl.resolved[p.Rot&3][b[3]]
					if b[1] != 0 || st == Air || st == tmplSkip {
						continue
					}
					rx, _, rz := p.Tmpl.rotatePos(b[0], b[1], b[2], p.Rot)
					x, z := p.OX+rx, p.OZ+rz
					y := g.SurfaceWG(x, z) - 1
					ch := g.GenerateChunk(int32(x>>4), int32(z>>4))
					if sectionBlockAt(ch, x&15, y, z&15) == Air {
						t.Errorf("street %s at %d,%d: nothing at the surface (y %d)", p.Tmpl.name, x, z, y)
					}
					return // one checked street cell is enough
				}
			}
		}
	}
	t.Fatal("no village street found")
}
