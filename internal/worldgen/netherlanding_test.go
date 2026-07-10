package worldgen

import "testing"

func TestNetherLandingIsWalkable(t *testing.T) {
	g := NewNetherGenerator(7)
	found := 0
	for _, c := range [][2]int{{0, 0}, {100, -50}, {-200, 300}, {50, 50}, {-80, -80}} {
		x, y, z, ok := g.NetherLanding(c[0], c[1])
		if !ok {
			continue
		}
		found++
		for ax := -1; ax <= 1; ax++ {
			for az := -1; az <= 1; az++ {
				if f := g.netherBlock(x+ax, y-1, z+az); f == Air || f == Lava {
					t.Fatalf("landing (%d,%d,%d): floor at (%d,%d) is %d", x, y, z, ax, az, f)
				}
				for ay := 0; ay < 3; ay++ {
					if b := g.netherBlock(x+ax, y+ay, z+az); b != Air {
						t.Fatalf("landing (%d,%d,%d): headroom blocked at +%d", x, y, z, ay)
					}
				}
			}
		}
	}
	if found == 0 {
		t.Fatal("no natural landing found at any test point — scan too strict")
	}
}
