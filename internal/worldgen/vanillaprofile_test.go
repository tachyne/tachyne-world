package worldgen

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// vcProfile is the shape of a world's caves the way the 26.3 comparison
// measures it: on land columns (top terrain block at y >= 63), the share
// of cells that are cave air at each depth under the top terrain block; and
// over every column, what the ground under the top holds per 16-block band
// (air, water, lava per mille).
type vcProfile struct {
	land, sea int
	depthN    [65]int
	depthAir  [65]int
	band      map[string]*[4]int // key "land|-64": cells, air, water, lava
	bandOrder []string
	factors   [8]int // land columns' factor, by whole units
}

func (p *vcProfile) add(land bool, y int, b uint32) {
	k := fmt.Sprintf("%s %4d", map[bool]string{true: "land", false: "sea"}[land], floorDiv(y+64, 16)*16-64)
	c := p.band[k]
	if c == nil {
		c = &[4]int{}
		p.band[k] = c
		p.bandOrder = append(p.bandOrder, k)
	}
	c[0]++
	switch b {
	case Air:
		c[1]++
	case Water:
		c[2]++
	case Lava:
		c[3]++
	}
}

func (p *vcProfile) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "land factor by whole units %v\n", p.factors)
	fmt.Fprintf(&sb, "columns land %d sea %d\nland: cave air fraction by depth below the top terrain block\n", p.land, p.sea)
	for d := 1; d <= 64; d++ {
		fmt.Fprintf(&sb, " d=%2d %.4f", d, float64(p.depthAir[d])/float64(max(1, p.depthN[d])))
		if d%4 == 0 {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("underground cells by 16-band (air/water/lava per mille of the band)\n")
	for _, w := range []string{"land", "sea"} {
		for y := -64; y < 320; y += 16 {
			k := fmt.Sprintf("%s %4d", w, y)
			if c := p.band[k]; c != nil {
				fmt.Fprintf(&sb, " %s..%4d air %5.1f water %5.1f lava %5.1f\n", k, y+15,
					1000*float64(c[1])/float64(c[0]), 1000*float64(c[2])/float64(c[0]), 1000*float64(c[3])/float64(c[0]))
			}
		}
	}
	return sb.String()
}

// The land columns caveProfile counts: those whose top terrain block is in
// this range (TACHYNE_CAVE_PROFILE_TOP="lo,hi"), to compare like heights.
var vcProfileTopLo, vcProfileTopHi = 63, 1 << 20

// caveProfile measures a generator's terrain (no features) over chunks
// within r of (cx, cz).
func caveProfile(g *Generator, cx, cz, r int32) *vcProfile {
	p := &vcProfile{band: map[string]*[4]int{}}
	for x0 := (cx - r) * 16; x0 < (cx+r+1)*16; x0++ {
		for z0 := (cz - r) * 16; z0 < (cz+r+1)*16; z0++ {
			x, z := int(x0), int(z0)
			c := g.columnAt(x, z)
			col := make([]uint32, c.h-MinY)
			top := MinY
			for y := MinY; y < c.h; y++ {
				b := g.terrainCell(c, x, y, z)
				col[y-MinY] = b
				if b != Air && b != Water && b != Lava {
					top = y
				}
			}
			land := top >= 63
			if land && (top < vcProfileTopLo || top > vcProfileTopHi) {
				continue
			}
			if land {
				if g.vcaves != nil {
					p.factors[min(7, int(g.vcaves.factor(x, z, c.h)))]++
				}
				p.land++
				for d := 1; d <= 64 && top-d >= MinY; d++ {
					p.depthN[d]++
					if col[top-d-MinY] == Air {
						p.depthAir[d]++
					}
				}
			} else {
				p.sea++
			}
			for y := MinY; y < top; y++ {
				p.add(land, y, col[y-MinY])
			}
		}
	}
	return p
}

// TestVanillaCaveProfile logs the vanilla-caves world's profile beside the
// native one, for comparison with the 26.3 server's (the numbers in the
// report come from its region files; the engine's terrain is its own, so
// the comparison is of shapes, not cells). Set TACHYNE_CAVE_PROFILE to
// "radius,cx,cz" in chunks to run it, and TACHYNE_CAVE_PROFILE_CONST to
// measure with the constant factor.
func TestVanillaCaveProfile(t *testing.T) {
	r := os.Getenv("TACHYNE_CAVE_PROFILE")
	if r == "" {
		t.Skip("set TACHYNE_CAVE_PROFILE=<chunk radius>")
	}
	var rad, cx, cz int32
	fmt.Sscanf(r, "%d,%d,%d", &rad, &cx, &cz)
	g := NewGenerator(1)
	g.SetCaveMode(CavesVanilla)
	if r := os.Getenv("TACHYNE_CAVE_PROFILE_TOP"); r != "" {
		fmt.Sscanf(r, "%d,%d", &vcProfileTopLo, &vcProfileTopHi)
	}
	g.vcaves.constFactor = os.Getenv("TACHYNE_CAVE_PROFILE_CONST") != ""
	t.Logf("vanilla caves (constant factor %v), seed 1, %d chunks round %d,%d:\n%v", g.vcaves.constFactor, (2*rad+1)*(2*rad+1), cx, cz, caveProfile(g, cx, cz, rad))
}

// The aquifer's statistics in a generated vanilla-caves world, against the
// 26.3 server's seed-1 world (625 chunks round the origin, the same
// measure): under the sea the caves flood (the server: 21–54 water per
// mille of the ground at y 0–63, almost no dry cave above y=32), below
// y=-54 they are lava (8 per mille of the -64..-49 band), and under land
// the caves are mostly dry with a few local lakes (the server's inland
// land: at most 8 per mille in any band).
func TestVanillaAquiferStatistics(t *testing.T) {
	g := NewGenerator(1)
	g.SetCaveMode(CavesVanilla)
	p := caveProfile(g, 0, 0, 5)
	t.Logf("\n%v", p)
	per := func(k string, i int) float64 {
		c := p.band[k]
		if c == nil || c[0] == 0 {
			return -1
		}
		return 1000 * float64(c[i]) / float64(c[0])
	}
	if w, a := per("sea   48", 2), per("sea   48", 1); w < 20 || a > 1 {
		t.Errorf("sea caves at y 48..63: %.1f water, %.1f air per mille; vanilla's: 20.9 water, no air", w, a)
	}
	if w, a := per("sea   32", 2), per("sea   32", 1); w < 3*a {
		t.Errorf("sea caves at y 32..47: %.1f water, %.1f air per mille; vanilla's are flooded", w, a)
	}
	if l := per("sea  -64", 3) + per("land  -64", 3); l <= 0 {
		t.Errorf("no lava below y=-54")
	}
	for _, k := range p.bandOrder {
		if strings.HasPrefix(k, "land") && per(k, 2) > 20 {
			t.Errorf("land caves in band %s hold %.1f water per mille; vanilla's inland caves hold at most 8", k, per(k, 2))
		}
	}
}
