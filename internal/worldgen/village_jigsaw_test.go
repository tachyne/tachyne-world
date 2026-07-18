package worldgen

import (
	"strings"
	"testing"
)

// TestJigsawAssemblesVillage stress-tests the assembler on the largest jigsaw
// structure: a plains village must grow from its town-centre start pool into a
// substantial connected settlement — a town centre, streets, and houses drawn
// from the real vanilla templates. (Assembly only; the live village still uses
// the hand-built layout until the villager economy is re-integrated onto the
// jigsaw pieces — see the structure-template task.)
func TestJigsawAssemblesVillage(t *testing.T) {
	g := NewGenerator(1)
	rng := newJigsawRNG(1, 500, 500)
	pieces := g.AssembleJigsaw("village/plains/town_centers", 500, 68, 500, rng, 6)
	if len(pieces) < 8 {
		t.Fatalf("a plains village should assemble many pieces, got %d", len(pieces))
	}

	names := map[string]int{}
	for i := range pieces {
		for n, tp := range templates {
			if tp == pieces[i].Tmpl {
				names[n]++
			}
		}
	}
	tc, streets, houses := 0, 0, 0
	for n, c := range names {
		switch {
		case strings.Contains(n, "town_center"):
			tc += c
		case strings.Contains(n, "street"):
			streets += c
		case strings.Contains(n, "house"):
			houses += c
		}
	}
	t.Logf("plains village: %d pieces (town_centers=%d streets=%d houses=%d, %d distinct)",
		len(pieces), tc, streets, houses, len(names))
	if tc == 0 {
		t.Fatal("village must contain its town centre")
	}
	if streets == 0 || houses == 0 {
		t.Fatal("village should branch into streets and houses")
	}
}
