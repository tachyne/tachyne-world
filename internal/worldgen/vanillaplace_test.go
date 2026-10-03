package worldgen

import (
	"bufio"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The facts in testdata/vanilla_place_seed1.txt were printed by a 26.3
// server's own classes for seed 1 (WorldgenRandom, the structure
// placements, ChunkGeneratorStructureState's rings over a plains-only
// biome source, the overworld biome source's possible biomes and
// FeatureSorter's per-step order); the placement code must reproduce them.

func vpOracle(t *testing.T) map[string][][]string {
	t.Helper()
	f, err := os.Open("testdata/vanilla_place_seed1.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][][]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) > 0 {
			out[fs[0]] = append(out[fs[0]], fs[1:])
		}
	}
	return out
}

func atoi64(t *testing.T, s string) int64 {
	t.Helper()
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestVanillaWorldgenRandomMatchesServer(t *testing.T) {
	o := vpOracle(t)
	r := newVWLegacy(0)
	r.setLargeFeatureWithSalt(1, -3, 7, 10387312)
	got := []string{itoa32(r.nextIntN(26)), itoa32(r.nextIntN(26)), strconv.FormatInt(r.nextLong(), 10),
		strconv.FormatUint(uint64(math.Float32bits(r.nextFloat())), 10), strconv.FormatUint(math.Float64bits(r.nextDouble()), 10)}
	if strings.Join(got, " ") != strings.Join(o["LWS"][0], " ") {
		t.Errorf("setLargeFeatureWithSalt draws %v, server %v", got, o["LWS"][0])
	}
	r.setLargeFeatureSeed(1, 5, -9)
	got = []string{itoa32(r.nextIntN(1000)), itoa32(r.nextIntN(7)), strconv.FormatInt(r.nextLong(), 10)}
	if strings.Join(got, " ") != strings.Join(o["LFS"][0], " ") {
		t.Errorf("setLargeFeatureSeed draws %v, server %v", got, o["LFS"][0])
	}
	x := newVWXoroshiro(42)
	fs := o["FS"]
	n := 0
	for i, ds := range o["DS"] {
		cx, cz := int32(atoi64(t, ds[0])), int32(atoi64(t, ds[1]))
		seed := x.setDecorationSeed(1, cx, cz)
		if seed != atoi64(t, ds[2]) {
			t.Errorf("decoration seed (%d,%d) = %d, server %s", cx, cz, seed, ds[2])
		}
		for step := 0; step < 11; step += 3 {
			for idx := 0; idx < 40; idx += 13 {
				x.setFeatureSeed(seed, idx, step)
				a1, a2, a3 := x.nextIntN(16), x.nextIntN(16), x.nextIntN(7)
				f := x.nextFloat()
				d := x.nextDouble()
				l := x.nextLong()
				b := strconv.FormatBool(x.nextBool())
				in := x.nextInt()
				got := []string{itoa32(a1), itoa32(a2), itoa32(a3), strconv.FormatUint(uint64(math.Float32bits(f)), 10),
					strconv.FormatUint(math.Float64bits(d), 10), strconv.FormatInt(l, 10), b, itoa32(in)}
				want := fs[n][2:]
				if strings.Join(got, " ") != strings.Join(want, " ") {
					t.Errorf("chunk %d step %d index %d: draws %v, server %v", i, step, idx, got, want)
				}
				n++
			}
		}
	}
}

func itoa32(v int32) string { return strconv.Itoa(int(v)) }

func TestVanillaStructurePlacementMatchesServer(t *testing.T) {
	o := vpOracle(t)
	v := newVanillaStructs(1, "overworld", func(int, int, int) string { return "plains" }, nil)
	v.fixed = "plains" // the oracle's FixedBiomeSource
	all := mustVPData()
	checked := 0
	for _, pc := range o["PC"] {
		s := all.Sets[pc[0]]
		var got []string
		for gx := int32(-3); gx <= 3; gx++ {
			for gz := int32(-3); gz <= 3; gz++ {
				x, z := v.potentialChunk(s, gx*s.Spacing, gz*s.Spacing)
				got = append(got, itoa32(x)+","+itoa32(z))
			}
		}
		if strings.Join(got, " ") != strings.Join(pc[1:], " ") {
			t.Errorf("%s: potential chunks\n got %v\nwant %v", pc[0], got, pc[1:])
		}
		checked++
	}
	for _, sc := range o["SC"] {
		s := all.Sets[sc[0]]
		var got []string
		for cx := int32(-200); cx <= 200 && len(got) < 40; cx++ {
			for cz := int32(-200); cz <= 200 && len(got) < 40; cz++ {
				if v.isStructureChunk(s, cx, cz) {
					got = append(got, itoa32(cx)+","+itoa32(cz))
				}
			}
		}
		if strings.Join(got, " ") != strings.Join(sc[1:], " ") {
			t.Errorf("%s: structure chunks\n got %v\nwant %v", sc[0], got, sc[1:])
		}
		checked++
	}
	if checked < 38 {
		t.Errorf("only %d sets checked", checked)
	}
	for _, ring := range o["RING"] {
		s := all.Sets[ring[0]]
		got := v.ringPositions(s, 1)
		var gs []string
		for _, p := range got {
			gs = append(gs, itoa32(p[0])+","+itoa32(p[1]))
		}
		if strings.Join(gs, " ") != strings.Join(ring[1:], " ") {
			t.Errorf("%s: rings\n got %v\nwant %v", ring[0], gs, ring[1:])
		}
	}
}

func TestVanillaFeatureOrderMatchesServer(t *testing.T) {
	o := vpOracle(t)
	d := mustVPData()
	if strings.Join(o["BIOMES"][0], " ") != strings.Join(d.Possible["overworld"], " ") {
		t.Fatalf("possible biomes\n got %v\nwant %v", d.Possible["overworld"], o["BIOMES"][0])
	}
	steps, err := vpStepFeatures(d.Possible["overworld"], d.Biomes)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range o["STEP"] {
		i := int(atoi64(t, st[0]))
		if strings.Join(steps[i], " ") != strings.Join(st[1:], " ") {
			t.Errorf("step %d\n got %v\nwant %v", i, steps[i], st[1:])
		}
	}
	if len(o["STEP"]) != len(steps) {
		t.Errorf("%d steps, server %d", len(steps), len(o["STEP"]))
	}
}

// vpSynthLevel is the oracle's synthetic world (Place2.java): a terrain
// height field with sea to y=63, stone/deepslate under grass or sand, and
// one biome per chunk.
type vpSynthLevel struct{}

var vpSynthBiomes = []string{"plains", "forest", "desert", "ocean", "swamp", "taiga", "jungle", "badlands",
	"dark_forest", "river", "snowy_plains", "meadow", "lush_caves", "cherry_grove", "mangrove_swamp", "warm_ocean"}

func vpSynthH(x, z int) int { return 50 + vpMod(x*7+z*13+(x>>4)*29, 30) }

func vpMod(a, b int) int { return ((a % b) + b) % b }

func (vpSynthLevel) Block(x, y, z int) uint32 {
	if y < -64 || y >= 320 {
		return Air
	}
	if y == -64 {
		return Bedrock
	}
	h := vpSynthH(x, z)
	switch {
	case y < h-1:
		if y < 0 {
			return Deepslate
		}
		return Stone
	case y == h-1:
		if h > 63 {
			return GrassBlock
		}
		return Sand
	case y < 63:
		return Water
	}
	return Air
}

func (vpSynthLevel) Height(hm HeightmapType, x, z int) int {
	h := vpSynthH(x, z)
	if hm == HeightOceanFloor || hm == HeightOceanFloorWG {
		return h
	}
	return max(h, 63)
}

func (vpSynthLevel) Biome(x, y, z int) string { return vpSynthChunkBiome(x, z) }

func vpSynthChunkBiome(x, z int) string {
	return vpSynthBiomes[vpMod((x>>4)*3+(z>>4)*5, len(vpSynthBiomes))]
}

func TestVanillaDecorationPositionsMatchServer(t *testing.T) {
	o := vpOracle2(t)
	dec, err := newVanillaDecor(1, "overworld")
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int32][]string{}
	var order [][2]int32
	for _, p := range o["P"] {
		k := [2]int32{int32(atoi64(t, p[0])), int32(atoi64(t, p[1]))}
		if _, ok := want[k]; !ok {
			order = append(order, k)
		}
		want[k] = append(want[k], strings.Join(p[2:], " "))
	}
	for _, c := range order {
		var biomes []string
		seen := map[string]bool{}
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				b := vpSynthChunkBiome(int(c[0])*16+dx*16, int(c[1])*16+dz*16)
				if !seen[b] {
					seen[b] = true
					biomes = append(biomes, b)
				}
			}
		}
		var got []string
		var cur *vpPlacedFeature
		var line []string
		var n int
		var rng *vwRandom
		flush := func() {
			if cur == nil {
				return
			}
			got = append(got, strings.Join(append(line, "n="+strconv.Itoa(n), "next="+itoa32(rng.nextInt())), " "))
		}
		// Every placed feature the chunk tries prints a line, placed or not:
		// walk them through a hook on the feature loop.
		dec.decorateTrace(c[0], c[1], vpSynthLevel{}, biomes, func(step, gi int, pf *vpPlacedFeature, r *vwRandom) {
			flush()
			cur, rng, n = pf, r, 0
			line = []string{strconv.Itoa(step), strconv.Itoa(gi), pf.Name}
		}, func(p *vpPlacement) {
			if n < 64 {
				line = append(line, strconv.Itoa(p.pos.x)+","+strconv.Itoa(p.pos.y)+","+strconv.Itoa(p.pos.z))
			}
			n++
		})
		flush()
		w := want[c]
		for i := 0; i < max(len(w), len(got)); i++ {
			var g, ww string
			if i < len(got) {
				g = got[i]
			}
			if i < len(w) {
				ww = w[i]
			}
			if g != ww {
				t.Errorf("chunk %v:\n got %s\nwant %s", c, g, ww)
				break
			}
		}
	}
}

func vpOracle2(t *testing.T) map[string][][]string {
	t.Helper()
	f, err := os.Open("testdata/vanilla_place2_seed1.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][][]string{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) > 0 {
			out[fs[0]] = append(out[fs[0]], fs[1:])
		}
	}
	return out
}

func TestVanillaRingsAndNetherOrderMatchServer(t *testing.T) {
	o := vpOracle2(t)
	nb := []string{"plains", "ocean", "desert", "river", "forest", "deep_ocean", "jungle", "savanna", "taiga", "badlands", "dark_forest", "snowy_plains"}
	v := newVanillaStructs(1, "overworld", func(qx, qy, qz int) string {
		return nb[vpMod(qx*31+qz*17+(qx>>3)*7+(qz>>5)*3, len(nb))]
	}, nil)
	got := v.ringPositions(mustVPData().Sets["strongholds"], 1)
	var gs []string
	for _, p := range got {
		gs = append(gs, itoa32(p[0])+","+itoa32(p[1]))
	}
	if strings.Join(gs, " ") != strings.Join(o["RING2"][0], " ") {
		t.Errorf("rings\n got %v\nwant %v", gs, o["RING2"][0])
	}
	d := mustVPData()
	if strings.Join(o["NBIOMES"][0], " ") != strings.Join(d.Possible["nether"], " ") {
		t.Errorf("nether biomes %v, server %v", d.Possible["nether"], o["NBIOMES"][0])
	}
	steps, err := vpStepFeatures(d.Possible["nether"], d.Biomes)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range o["NSTEP"] {
		i := int(atoi64(t, st[0]))
		if strings.Join(steps[i], " ") != strings.Join(st[1:], " ") {
			t.Errorf("nether step %d\n got %v\nwant %v", i, steps[i], st[1:])
		}
	}
}
