package worldgen

import (
	"math"
	"testing"
)

// The values below were sampled from the 26.3 server itself (seed 1): its
// NormalNoise built from the noise files and seeded through RandomState's
// positional factory, its density functions through
// RandomState.sampleBlockValueUncached, its carvers through
// CaveWorldCarver.carve, and its java.util.Random and Mth tables. Bits must
// match exactly.

func TestVanillaNoiseMatchesVanilla(t *testing.T) {
	w := vnWorldPositional(1)
	if w.lo != 0xF1A7ACB2676359FE || w.hi != 0x5988F20331D7EB8A {
		t.Fatalf("world positional factory %x %x", w.lo, w.hi)
	}
	cases := []struct {
		name    string
		x, y, z int
		bits    uint32
	}{
		{"cave_cheese", 0, 0, 0, 1057313497},
		{"cave_cheese", 100, -30, -250, 3204242647},
		{"cave_cheese", -1234, 64, 5678, 3183266556},
		{"noodle", 0, 0, 0, 1025081816},
		{"noodle", 100, -30, -250, 3208171822},
		{"noodle", -1234, 64, 5678, 1044598072},
		{"pillar", 0, 0, 0, 3188604624},
		{"pillar", 100, -30, -250, 1026517357},
		{"pillar", -1234, 64, 5678, 1000292912},
	}
	noises := map[string]*vnNormal{}
	for _, c := range cases {
		n := noises[c.name]
		if n == nil {
			n = newVNNormal(w, "minecraft:"+c.name, vcNoiseParams[c.name])
			noises[c.name] = n
		}
		got := n.get(float64(c.x), float64(c.y), float64(c.z))
		if math.Float32bits(got) != c.bits {
			t.Errorf("%s at %d,%d,%d = %v, vanilla %v", c.name, c.x, c.y, c.z, got, math.Float32frombits(c.bits))
		}
	}
}

// noodleAt is the noodle function at any cell by InterpolatedFunction's
// point sampler (Mth.lerp3 over the cell's corners), as the oracle sampled it.
func (v *vanillaCaves) noodleAt(x, y, z int) float32 {
	xi, yi, zi := ((x%4)+4)%4, ((y%8)+8)%8, ((z%4)+4)%4
	x0, y0, z0 := x-xi, y-yi, z-zi
	var c [2][2][2][4]float32
	for dx := 0; dx < 2; dx++ {
		for dy := 0; dy < 2; dy++ {
			for dz := 0; dz < 2; dz++ {
				n, th, a, b := v.noodleCorner(x0+4*dx, y0+8*dy, z0+4*dz)
				c[dx][dy][dz] = [4]float32{n, th, a, b}
			}
		}
	}
	at := func(k int) float32 {
		if xi == 0 && yi == 0 && zi == 0 {
			return c[0][0][0][k]
		}
		return vnLerp3(float32(xi)/4, float32(yi)/8, float32(zi)/4,
			c[0][0][0][k], c[1][0][0][k], c[0][1][0][k], c[1][1][0][k],
			c[0][0][1][k], c[1][0][1][k], c[0][1][1][k], c[1][1][1][k])
	}
	n, th, a, b := at(0), at(1), at(2), at(3)
	if n >= -1000000 && n < 0 {
		return 64
	}
	return th + 1.5*max(vcAbs(a), vcAbs(b))
}

func TestVanillaCaveDensityMatchesVanilla(t *testing.T) {
	v := newVanillaCaves(1)
	cases := []struct {
		fn      string
		x, y, z int
		bits    uint32
	}{
		{"entrances", 0, 0, 0, 1051982397},
		{"entrances", 4, -8, 12, 1049275144},
		{"entrances", 100, -30, -250, 1052609176},
		{"entrances", -1234, 64, 5678, 1050247815},
		{"entrances", 37, -41, -19, 1039220094},
		{"entrances", 512, 16, 512, 1048940723},
		{"entrances", -77, 120, 333, 1030213029},
		{"entrances", 8, -60, 8, 1043247383},
		{"entrances", 9, -59, 9, 1042866772},
		{"entrances", 2000, 40, -3000, 1043526441},
		{"entrances", -5, -5, -5, 1054723531},
		{"entrances", 33, 7, -65, 1044643219},
		{"entrances", 250, 200, -250, 1053730843},
		{"entrances", 16, -64, 16, 1027098191},
		{"entrances", 12, 312, 4, 1054168367},
		{"roughness", 0, 0, 0, 1015039725},
		{"roughness", 4, -8, 12, 1016446978},
		{"roughness", 100, -30, -250, 999546241},
		{"roughness", -1234, 64, 5678, 3132289247},
		{"roughness", 37, -41, -19, 1015028809},
		{"roughness", 512, 16, 512, 1020428177},
		{"roughness", -77, 120, 333, 1015976850},
		{"roughness", 8, -60, 8, 1018608472},
		{"roughness", 9, -59, 9, 1018731908},
		{"roughness", 2000, 40, -3000, 1019896504},
		{"roughness", -5, -5, -5, 1011674462},
		{"roughness", 33, 7, -65, 1017443888},
		{"roughness", 250, 200, -250, 1019113903},
		{"roughness", 16, -64, 16, 3151033913},
		{"roughness", 12, 312, 4, 1013296536},
		{"spaghetti2D", 0, 0, 0, 1037296570},
		{"spaghetti2D", 4, -8, 12, 1048583601},
		{"spaghetti2D", 100, -30, -250, 1065353216},
		{"spaghetti2D", -1234, 64, 5678, 1065353216},
		{"spaghetti2D", 37, -41, -19, 1065353216},
		{"spaghetti2D", 512, 16, 512, 1065353216},
		{"spaghetti2D", -77, 120, 333, 1065353216},
		{"spaghetti2D", 8, -60, 8, 1065353216},
		{"spaghetti2D", 9, -59, 9, 1065353216},
		{"spaghetti2D", 2000, 40, -3000, 1065353216},
		{"spaghetti2D", -5, -5, -5, 1034073095},
		{"spaghetti2D", 33, 7, -65, 1065353216},
		{"spaghetti2D", 250, 200, -250, 1065353216},
		{"spaghetti2D", 16, -64, 16, 1065353216},
		{"spaghetti2D", 12, 312, 4, 1065353216},
		{"pillars", 0, 0, 0, 3189608936},
		{"pillars", 4, -8, 12, 3181765168},
		{"pillars", 100, -30, -250, 3146023217},
		{"pillars", -1234, 64, 5678, 3207734134},
		{"pillars", 37, -41, -19, 3174211149},
		{"pillars", 512, 16, 512, 3183687691},
		{"pillars", -77, 120, 333, 3193982336},
		{"pillars", 8, -60, 8, 3194987635},
		{"pillars", 9, -59, 9, 3192002497},
		{"pillars", 2000, 40, -3000, 3203651361},
		{"pillars", -5, -5, -5, 3172235527},
		{"pillars", 33, 7, -65, 986593894},
		{"pillars", 250, 200, -250, 3197830476},
		{"pillars", 16, -64, 16, 3193717683},
		{"pillars", 12, 312, 4, 1042287068},
		{"noodle", 0, 0, 0, 1053863268},
		{"noodle", 4, -8, 12, 3178709140},
		{"noodle", 100, -30, -250, 1115684864},
		{"noodle", -1234, 64, 5678, 1043031809},
		{"noodle", 37, -41, -19, 1115684864},
		{"noodle", 512, 16, 512, 1024033546},
		{"noodle", -77, 120, 333, 1115684864},
		{"noodle", 8, -60, 8, 1115684864},
		{"noodle", 9, -59, 9, 1115684864},
		{"noodle", 2000, 40, -3000, 1052464402},
		{"noodle", -5, -5, -5, 1050761655},
		{"noodle", 33, 7, -65, 1052808471},
		{"noodle", 250, 200, -250, 1049099922},
		{"noodle", 16, -64, 16, 1115684864},
		{"noodle", 12, 312, 4, 1115684864},
	}
	for _, c := range cases {
		var got float32
		switch c.fn {
		case "entrances":
			got = v.entrances(c.x, c.y, c.z, v.roughness(c.x, c.y, c.z))
		case "roughness":
			got = v.roughness(c.x, c.y, c.z)
		case "spaghetti2D":
			got = v.spaghetti2D(c.x, c.y, c.z)
		case "pillars":
			got = v.pillars(c.x, c.y, c.z)
		case "noodle":
			got = v.noodleAt(c.x, c.y, c.z)
		}
		if math.Float32bits(got) != c.bits {
			t.Errorf("%s at %d,%d,%d = %v, vanilla %v", c.fn, c.x, c.y, c.z, got, math.Float32frombits(c.bits))
		}
	}
}

func TestVanillaCarverRandomMatchesVanilla(t *testing.T) {
	r := vcLargeFeatureRandom(1, 3, -7)
	if f := r.nextFloat(); math.Float32bits(f) != 1063771370 {
		t.Errorf("nextFloat after setLargeFeatureSeed = %v", f)
	}
	if n := r.nextInt(15); n != 7 {
		t.Errorf("nextInt(15) = %d, vanilla 7", n)
	}
	if l := r.nextLong(); l != 7642557276404951346 {
		t.Errorf("nextLong = %d", l)
	}
	pi := vcPiF
	angle := pi * 17 / 90 // float arithmetic, step by step, as (float) Math.PI * 17 / 90
	for _, c := range []struct {
		got  float32
		bits int32
	}{
		{mthSin(1.2345), 1064413252},
		{mthCos(-2.5), -1085467599},
		{mthSin(float64(angle)), 1057957019},
	} {
		if int32(math.Float32bits(c.got)) != c.bits {
			t.Errorf("Mth table value %v, vanilla %v", c.got, math.Float32frombits(uint32(c.bits)))
		}
	}
}

// The cave carvers carve exactly vanilla's cells into a chunk: the count and
// an FNV-1a hash over the sorted (y+64)<<8 | z<<4 | x keys.
func TestVanillaCaveCarversMatchVanilla(t *testing.T) {
	g := NewGenerator(1)
	v := newVanillaCaves(1)
	for _, c := range []struct {
		cx, cz int32
		cells  int
		hash   uint64
	}{
		{0, 0, 1727, 16690477506301416211},
		{3, -7, 833, 14908065212686819682},
		{-12, 5, 2152, 12461034448356214552},
		{40, 40, 973, 17378266758060416300},
	} {
		mask := make([]uint64, g.sections*16*256/64)
		v.carveCaves(g, c.cx, c.cz, mask)
		n, h := 0, uint64(1469598103934665603)
		for i := 0; i < len(mask)*64; i++ {
			if mask[i>>6]&(1<<(i&63)) != 0 {
				n++
				h ^= uint64(i)
				h *= 1099511628211
			}
		}
		if n != c.cells || h != c.hash {
			t.Errorf("chunk %d,%d: %d cells hash %d, vanilla %d cells hash %d", c.cx, c.cz, n, h, c.cells, c.hash)
		}
	}
}

// The interpolation steps up each cell the way vanilla's volume sampler
// does, and agrees with the corners at the corners.
func TestVanillaCellInterpolation(t *testing.T) {
	var out [128]float32
	vcFillCell(1, 2, 3, 4, 5, 6, 7, 8, &out)
	if out[0] != 1 {
		t.Fatalf("corner 000 = %v", out[0])
	}
	// x=2, z=1, y=4: lerp z (0.25), then x (0.5), then up y half way.
	lo := vnLerp(0.5, vnLerp(0.25, 1, 5), vnLerp(0.25, 2, 6))
	hi := vnLerp(0.5, vnLerp(0.25, 3, 7), vnLerp(0.25, 4, 8))
	want := lo
	step := (hi - lo) * 0.125
	for i := 0; i < 4; i++ {
		want += step
	}
	if got := out[(2*4+1)*8+4]; got != want {
		t.Fatalf("interpolated %v, want %v", got, want)
	}
}

// legacyCarve is the engine's own cave carve as it stood before the cave
// choice existed, copied here so a native world is shown to carve exactly
// as before.
func legacyCarve(g *Generator, b uint32, wx, wy, wz, colH int) uint32 {
	if g.earth != nil {
		return b
	}
	if wy < caveMinY || !carveable(b) {
		return b
	}
	ceil := colH
	underwater := colH <= SeaLevel+1
	if underwater {
		ceil = colH - caveSeaFloorGap
	}
	if wy >= ceil {
		return b
	}
	thr := caveThreshold
	if !underwater {
		if d := ceil - 1 - wy; d < caveSurfaceTaper {
			thr *= caveSurfaceFrac + (1-caveSurfaceFrac)*float64(d)/caveSurfaceTaper
		}
	}
	a := g.caveA.Noise3(float64(wx)/64, float64(wy)/40, float64(wz)/64)
	c := g.caveB.Noise3(float64(wx)/64, float64(wy)/40, float64(wz)/64)
	if a*a+c*c < thr {
		return Air
	}
	return b
}

// A native world — the default, and every world made before the choice —
// carves exactly as it always has, and its chunks are the same whether the
// mode was set or left alone.
func TestNativeCavesUnchanged(t *testing.T) {
	g := NewGenerator(7)
	if g.CaveMode() != CavesNative || g.vcaves != nil {
		t.Fatalf("a new generator is %v (vanilla state %v)", g.CaveMode(), g.vcaves != nil)
	}
	for x := -40; x < 40; x += 3 {
		for z := -40; z < 40; z += 5 {
			c := g.columnAt(x, z)
			for y := MinY; y < c.h; y++ {
				b := c.block(y)
				if got, want := g.carve(b, x, y, z, c.h), legacyCarve(g, b, x, y, z, c.h); got != want {
					t.Fatalf("carve(%d,%d,%d) = %d, before the choice %d", x, y, z, got, want)
				}
			}
		}
	}
	set := NewGenerator(7)
	set.SetCaveMode(CavesNative)
	for _, p := range [][2]int32{{0, 0}, {5, -3}} {
		if !g.GenerateChunk(p[0], p[1]).Equal(set.GenerateChunk(p[0], p[1])) {
			t.Fatalf("chunk %v differs once the native mode is set explicitly", p)
		}
	}
}

// A vanilla world's caves: through GenerateChunk, the chunk is cut where the
// mask says (air, or lava below y=-54), the mask agrees with BlockAt, and
// the world differs from a native one.
func TestVanillaCavesGenerate(t *testing.T) {
	g := NewGenerator(1)
	g.SetCaveMode(CavesVanilla)
	if g.CaveMode() != CavesVanilla || g.vcaves == nil {
		t.Fatal("vanilla mode not set")
	}
	native := NewGenerator(1)
	open, lava, below, differ := 0, 0, 0, 0
	for _, p := range [][2]int32{{0, 0}, {6, -4}, {-9, 11}} {
		ch := g.GenerateChunk(p[0], p[1])
		nch := native.GenerateChunk(p[0], p[1])
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				x, z := int(p[0])*16+lx, int(p[1])*16+lz
				h := g.Height(x, z)
				for y := caveMinY; y < h-8; y++ {
					below++
					s := sectionBlockAt(ch, lx, y, lz)
					if s != sectionBlockAt(nch, lx, y, lz) {
						differ++
					}
					if !g.vcaves.open(g, x, y, z) {
						continue
					}
					open++
					if bs := g.BlockAt(x, y, z); bs != Air && bs != Lava && carveable(g.columnAt(x, z).block(y)) {
						t.Fatalf("BlockAt(%d,%d,%d) = %d in an open vanilla cave cell", x, y, z, bs)
					}
					if y < vanillaLavaLevel && g.BlockAt(x, y, z) == Lava {
						lava++
					}
				}
			}
		}
	}
	t.Logf("vanilla caves: %d of %d cells under the ground open (%.1f%%), %d lava, %d cells differ from native",
		open, below, 100*float64(open)/float64(below), lava, differ)
	if open == 0 || open*50 < below {
		t.Errorf("vanilla caves open %d of %d cells under the ground", open, below)
	}
	if differ == 0 {
		t.Error("a vanilla world's underground matches the native one")
	}
}

// Dungeons find their spots in vanilla's caves: MonsterRoomFeature wants a
// solid floor and ceiling and one to five two-high gaps at floor level, and
// vanilla's cheese, spaghetti and noodle caves and carver tunnels offer far
// more of those than the engine's own tunnels (about four rooms in 625
// chunks round the origin). For scale: the 26.3 server itself, seed 1,
// generated 12 dungeon spawners (7 zombie, 2 skeleton, 2 spider below y=0;
// 1 zombie above) in the same 625 chunks. The engine's terrain is not
// vanilla's, so the counts can only be compared, not matched.
func TestVanillaCavesMonsterRooms(t *testing.T) {
	count := func(g *Generator) int {
		n := 0
		for cx := int32(-12); cx <= 12; cx++ {
			for cz := int32(-12); cz <= 12; cz++ {
				n += len(g.monsterRooms(cx, cz))
			}
		}
		return n
	}
	native := NewGenerator(1)
	vanilla := NewGenerator(1)
	vanilla.SetCaveMode(CavesVanilla)
	nn, vn := count(native), count(vanilla)
	t.Logf("monster rooms in 625 chunks: native %d, vanilla caves %d", nn, vn)
	if vn <= nn {
		t.Errorf("vanilla caves give %d monster rooms in 625 chunks, no more than native's %d", vn, nn)
	}
}

func TestParseCaveMode(t *testing.T) {
	for s, want := range map[string]CaveMode{"native": CavesNative, "vanilla": CavesVanilla} {
		if m, ok := ParseCaveMode(s); !ok || m != want || m.String() != s {
			t.Errorf("ParseCaveMode(%q) = %v %v", s, m, ok)
		}
	}
	if _, ok := ParseCaveMode("tachyne"); ok {
		t.Error("ParseCaveMode accepted tachyne")
	}
	// The Nether and the End have no choice.
	n := NewNetherGenerator(1)
	n.SetCaveMode(CavesVanilla)
	if n.CaveMode() != CavesNative || n.vcaves != nil {
		t.Error("the Nether took vanilla caves")
	}
}
