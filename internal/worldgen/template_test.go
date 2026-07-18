package worldgen

import "testing"

// TestTemplatesResolve checks every baked template resolves its whole palette to
// real states at all four rotations (only structure markers may be skipped).
func TestTemplatesResolve(t *testing.T) {
	if len(templates) == 0 {
		t.Fatal("no structure templates embedded")
	}
	for name, tmpl := range templates {
		for rot := 0; rot < 4; rot++ {
			for i, s := range tmpl.resolved[rot] {
				if s == tmplSkip {
					n := trimNS(tmpl.Palette[i].Name)
					if n != "structure_void" && n != "structure_block" && n != "jigsaw" {
						t.Errorf("%s rot%d palette[%d] %q resolved to skip", name, rot, i, n)
					}
				}
			}
		}
	}
}

// TestIglooFromTemplate stamps a real igloo and asserts its dome came from the
// vanilla template.
func TestIglooFromTemplate(t *testing.T) {
	g := NewGenerator(1)
	var ig Igloo
	for i := 0; i < 120 && !ig.Exists; i++ {
		for j := 0; j < 120; j++ {
			if c := g.IglooIn(i*iglooCell+160, j*iglooCell+160); c.Exists {
				ig = c
				break
			}
		}
	}
	if !ig.Exists {
		t.Skip("no igloo in the snowy search window")
	}
	ch := g.GenerateChunk(int32(ig.X>>4), int32(ig.Z>>4))
	snow := blockBase("snow_block")
	n := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b == snow {
				n++
			}
		}
	}
	t.Logf("igloo %d,%d,%d basement=%v depth=%d snow=%d", ig.X, ig.Y, ig.Z, ig.Basement, ig.Depth, n)
	if n == 0 {
		t.Fatal("igloo dome should stamp snow blocks from the real template")
	}
}
