package worldgen

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// The vanilla generator's surface and carvers against the 26.3 server.
// testdata/vanilla_surface_seed1.txt.gz is a Java oracle's output
// (SurfOracle: the server jar's own NoiseBasedChunkGenerator, its private
// doFill, buildSurface and generateCarvers run by reflection on a
// ProtoChunk, seed 1, the overworld settings and biome source): every
// column of six chunks as run-length block names top down, after the
// surface ("S") and after the carvers ("K"); and ("B") the noise biome of
// each quart the surface's fuzzed lookup can reach, and ("Q") the biomes
// the surface pass's lookups returned, in order — vanilla's biome search
// (Climate.RTree) starts from its previous answer, so a quart inside two
// overlapping parameter boxes answers by the order it is asked in; the test
// replays that sequence as the biome source, which also checks that the
// engine asks in vanilla's order.

type vtOracleBiomes map[[3]int]string

func (b vtOracleBiomes) BiomeAt(qx, qy, qz int) string {
	s, ok := b[[3]int{qx, qy, qz}]
	if !ok {
		panic(fmt.Sprintf("biome at quart %d,%d,%d is not in the oracle's data", qx, qy, qz))
	}
	return s
}

type vtOracleSurface struct {
	cols   map[string]string // "S cx cz x z" → runs
	biomes vtOracleBiomes
	seqs   map[[2]int32][]string // the surface pass's lookups, in order
	chunks [][2]int32
}

// vtSeqBiomes replays a recorded sequence of answers.
type vtSeqBiomes struct {
	seq []string
	i   int
}

func (b *vtSeqBiomes) BiomeAt(int, int, int) string {
	if b.i >= len(b.seq) {
		b.i++
		return "minecraft:the_void"
	}
	s := b.seq[b.i]
	b.i++
	return s
}

func loadVTOracleSurface(t *testing.T, path string) *vtOracleSurface {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	o := &vtOracleSurface{cols: map[string]string{}, biomes: vtOracleBiomes{}, seqs: map[[2]int32][]string{}}
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch f[0] {
		case "S", "K":
			o.cols[strings.Join(f[:5], " ")] = strings.Join(f[5:], " ")
		case "Q":
			cx, _ := strconv.Atoi(f[1])
			cz, _ := strconv.Atoi(f[2])
			var seq []string
			for _, r := range f[3:] {
				name, n, _ := strings.Cut(r, "*")
				k, _ := strconv.Atoi(n)
				for ; k > 0; k-- {
					seq = append(seq, "minecraft:"+name)
				}
			}
			o.seqs[[2]int32{int32(cx), int32(cz)}] = seq
		case "B":
			cx, _ := strconv.Atoi(f[1])
			cz, _ := strconv.Atoi(f[2])
			o.chunks = append(o.chunks, [2]int32{int32(cx), int32(cz)})
			i := 3
			for qx := 4*cx - 1; qx <= 4*cx+4; qx++ {
				for qz := 4*cz - 1; qz <= 4*cz+4; qz++ {
					for qy := -16; qy <= 79; qy++ {
						o.biomes[[3]int{qx, qy, qz}] = "minecraft:" + f[i]
						i++
					}
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return o
}

// vtColumnRuns is a chunk column as the oracle writes it.
func vtColumnRuns(ch *Chunk, x, z int) string {
	var sb strings.Builder
	last, n := "", 0
	for y := 319; y >= MinY; y-- {
		s, _ := StateName(ch.getGen(x, y, z))
		if s == last {
			n++
			continue
		}
		if last != "" {
			fmt.Fprintf(&sb, "%s*%d ", last, n)
		}
		last, n = s, 1
	}
	fmt.Fprintf(&sb, "%s*%d", last, n)
	return sb.String()
}

func TestVanillaSurfaceMatchesServer(t *testing.T) {
	o := loadVTOracleSurface(t, "testdata/vanilla_surface_seed1.txt.gz")
	gen, err := newVTGen("minecraft:overworld", 1)
	if err != nil {
		t.Fatal(err)
	}
	mat, err := newVMSystem(gen)
	if err != nil {
		t.Fatal(err)
	}
	w := &vtWorld{seed: 1, gen: gen, mat: mat, zoom: vtZoomSeed(1), biomes: o.biomes, carvers: true}
	for _, c := range o.chunks {
		ch := NewChunk(SectionCount)
		nc := gen.fillChunk(ch, c[0], c[1], nil)
		seq := &vtSeqBiomes{seq: o.seqs[c]}
		w.biomes = seq
		mat.buildSurface(ch, c[0], c[1], nc, func(x, y, z int) string { return w.biomeAt(x, y, z, true) })
		if seq.i != len(seq.seq) {
			t.Errorf("chunk %d,%d: the surface asked %d biomes, vanilla %d", c[0], c[1], seq.i, len(seq.seq))
		}
		w.biomes = o.biomes
		compare := func(stage string) {
			bad := 0
			for x := 0; x < 16; x++ {
				for z := 0; z < 16; z++ {
					key := fmt.Sprintf("%s %d %d %d %d", stage, c[0], c[1], x, z)
					want, ok := o.cols[key]
					if !ok {
						t.Fatalf("%s: not in the oracle's data", key)
					}
					if got := vtColumnRuns(ch, x, z); got != want {
						if bad < 4 {
							t.Errorf("%s:\n got  %s\n want %s", key, got, want)
						}
						bad++
					}
				}
			}
			if bad > 0 {
				t.Errorf("chunk %d,%d after %s: %d of 256 columns differ", c[0], c[1], stage, bad)
			}
		}
		compare("S")
		w.carve(ch, c[0], c[1], nc)
		compare("K")
	}
}
