package worldgen

import "math"

// The End's obsidian spikes and exit podium, as vanilla builds them.
//
// EndSpikeFeature: ten spikes on a ring of 42 at angles i·36° (floor of
// 42·cos and 42·sin of 2(−π + iπ/10)), each taking a size 0–9 from a
// shuffle seeded by the world seed (the low 16 bits of the seed's first
// nextLong, as EndSpikeFeature's cache key): radius 2 + size/3, height 76 +
// 3·size, and the sizes 1 and 2 guarded by an iron-bar cage. Obsidian runs
// from the world floor to the height inside radius²+1; the square round
// the spike above y=65 up to height+10 is cleared; bedrock caps the spike
// under the crystal, which sits in fire.
//
// EndPodiumFeature: the exit portal's bedrock bowl, rim, pillar and four
// torches, pre-placed inactive (no portal) at the island's top at 0,0 as
// the dragon fight does on a new world; the server stamps the active one
// (portal) when the dragon dies and the inactive one when it is summoned
// again.
//
// Build guard: a spike a player touched — built on or dug into, old or new
// — keeps the old engine pillar (radius 3, height 76 + 7i mod 28, no cage),
// and the podium is not pre-placed where anyone built or dug round 0,0
// (the old exit dais stays as its edits left it).

// EndSpike is one obsidian pillar.
type EndSpike struct {
	X, Z, Radius, Height int
	Guarded              bool
	Legacy               bool // the old engine pillar, kept for a player who touched it
}

// CrystalY is the height of the spike's end crystal: on the bedrock cap
// (vanilla), or straight on the obsidian (the old pillars).
func (s EndSpike) CrystalY() int {
	if s.Legacy {
		return s.Height
	}
	return s.Height + 1
}

// javaRandom is java.util.Random / SingleThreadedRandomSource: the 48-bit
// LCG vanilla seeds the spike shuffle with.
type javaRandom struct{ seed int64 }

func newJavaRandom(seed int64) *javaRandom {
	return &javaRandom{(seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *javaRandom) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.seed >> (48 - bits))
}

func (r *javaRandom) nextInt(bound int32) int32 {
	if bound&(bound-1) == 0 {
		return int32((int64(bound) * int64(r.next(31))) >> 31)
	}
	for {
		bits := r.next(31)
		val := bits % bound
		if bits-val+(bound-1) >= 0 {
			return val
		}
	}
}

func (r *javaRandom) nextLong() int64 {
	hi := r.next(32)
	lo := r.next(32)
	return int64(hi)<<32 + int64(lo)
}

// vanillaEndSpikes is EndSpikeFeature.getSpikesForLevel for a world seed.
func vanillaEndSpikes(worldSeed int64) []EndSpike {
	key := newJavaRandom(worldSeed).nextLong() & 0xFFFF
	r := newJavaRandom(key)
	var sizes [EndPillars]int
	for i := range sizes {
		sizes[i] = i
	}
	for i := len(sizes); i > 1; i-- {
		j := int(r.nextInt(int32(i)))
		sizes[i-1], sizes[j] = sizes[j], sizes[i-1]
	}
	out := make([]EndSpike, EndPillars)
	for i := range out {
		a := 2 * (-math.Pi + math.Pi/10*float64(i))
		size := sizes[i]
		out[i] = EndSpike{
			X:       int(math.Floor(42 * math.Cos(a))),
			Z:       int(math.Floor(42 * math.Sin(a))),
			Radius:  2 + size/3,
			Height:  76 + size*3,
			Guarded: size == 1 || size == 2,
		}
	}
	return out
}

// legacyEndSpike is the old engine pillar i.
func legacyEndSpike(i int) EndSpike {
	return EndSpike{
		X:      int(EndPillarRing * cos01(float64(i)/EndPillars)),
		Z:      int(EndPillarRing * sin01(float64(i)/EndPillars)),
		Radius: 3,
		Height: 76 + (i*7)%28,
		Legacy: true,
	}
}

// box is the cells a spike stands in, for the build guard.
func (s EndSpike) box() (x0, y0, z0, x1, y1, z1 int) {
	r := max(s.Radius, 2) // the cage reaches two out
	return s.X - r, EndSurfaceY - 8, s.Z - r, s.X + r, s.Height + 10, s.Z + r
}

// EndSpikes is the End's ten pillars as this world has them (nil outside
// the End): vanilla's, but the old pillar where a player touched either.
func (g *Generator) EndSpikes() []EndSpike {
	if !g.end {
		return nil
	}
	g.spikeMu.Lock()
	defer g.spikeMu.Unlock()
	if g.spikes != nil {
		return g.spikes
	}
	spikes := vanillaEndSpikes(g.seed ^ 0xE4D) // NewEndGenerator's seed back to the world's
	for i := range spikes {
		old := legacyEndSpike(i)
		if g.touchedIn(old.box()) || g.touchedIn(spikes[i].box()) {
			spikes[i] = old
		}
	}
	g.spikes = spikes
	return spikes
}

// forgetEndSpikes drops the cached layout: the edit overlay changed.
func (g *Generator) forgetEndSpikes() {
	g.spikeMu.Lock()
	g.spikes = nil
	g.spikeMu.Unlock()
}

// endSpikeReach bounds the spikes (ring 42, radius ≤ 5, cage 2): a column
// farther out never meets one.
const endSpikeReach = 50

// endSpikeCell is the block a spike puts at (x, y, z), if any: obsidian
// in the spike, air in the cleared square above y=65.
func endSpikeCell(spikes []EndSpike, x, y, z int) (uint32, bool) {
	for _, s := range spikes {
		dx, dz := x-s.X, z-s.Z
		if s.Legacy {
			if dx*dx+dz*dz <= 9 && y >= EndSurfaceY-8 && y < s.Height {
				return Obsidian, true
			}
			continue
		}
		if dx < -s.Radius || dx > s.Radius || dz < -s.Radius || dz > s.Radius || y > s.Height+10 {
			continue
		}
		if dx*dx+dz*dz <= s.Radius*s.Radius+1 && y < s.Height {
			return Obsidian, true
		}
		if y > 65 {
			return Air, true
		}
	}
	return 0, false
}

// endSpikeTops stamps each vanilla spike's cage (when guarded), bedrock cap
// and fire into the chunk.
func (g *Generator) endSpikeTops(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	put := func(x, y, z int, s uint32) { setSectionBlock(ch, x-baseX, y, z-baseZ, s, true) }
	for _, s := range g.EndSpikes() {
		if s.Legacy || s.X+2 < baseX || s.X-2 > baseX+15 || s.Z+2 < baseZ || s.Z-2 > baseZ+15 {
			continue
		}
		if s.Guarded {
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					for dy := 0; dy <= 3; dy++ {
						xSide, zSide, top := absInt(dx) == 2, absInt(dz) == 2, dy == 3
						if !xSide && !zSide && !top {
							continue
						}
						xEdge := dx == -2 || dx == 2 || top
						zEdge := dz == -2 || dz == 2 || top
						put(s.X+dx, s.Height+dy, s.Z+dz, ironBars(xEdge && dz != -2, xEdge && dz != 2, zEdge && dx != -2, zEdge && dx != 2))
					}
				}
			}
		}
		put(s.X, s.Height, s.Z, Bedrock)
		put(s.X, s.Height+1, s.Z, endFire)
	}
}

var endFire = BlockID("fire")

// ironBars is iron bars with the given connections (north, south, west,
// east), dry.
func ironBars(n, s, w, e bool) uint32 {
	b := func(v bool) string {
		if v {
			return "true"
		}
		return "false"
	}
	return withProps("iron_bars", "north", b(n), "south", b(s), "west", b(w), "east", b(e), "waterlogged", "false")
}

// PodiumCell is one block of the exit podium.
type PodiumCell struct {
	X, Y, Z int
	State   uint32
}

// EndPodium is EndPodiumFeature at (ox, oy, oz), in placing order (later
// cells win): the bowl — bedrock inside the rim and end stone out to it
// below the origin's level, air above it, the rim ring of bedrock at it and
// the portal (active) or air (inactive) inside — then the four-high bedrock
// pillar and a wall torch on each side of its third block.
func EndPodium(ox, oy, oz int, active bool) []PodiumCell {
	var out []PodiumCell
	for z := oz - 4; z <= oz+4; z++ {
		for y := oy - 1; y <= oy+32; y++ {
			for x := ox - 4; x <= ox+4; x++ {
				dx, dy, dz := x-ox, y-oy, z-oz
				d2 := float64(dx*dx + dy*dy + dz*dz)
				inside := d2 < 2.5*2.5
				if !inside && d2 >= 3.5*3.5 {
					continue
				}
				var s uint32
				switch {
				case y < oy && inside:
					s = Bedrock
				case y < oy:
					s = EndStone
				case y > oy:
					s = Air
				case !inside:
					s = Bedrock
				case active:
					s = EndPortalBlock
				default:
					s = Air
				}
				out = append(out, PodiumCell{x, y, z, s})
			}
		}
	}
	for k := 0; k < 4; k++ {
		out = append(out, PodiumCell{ox, oy + k, oz, Bedrock})
	}
	for _, t := range [4]struct {
		name   string
		dx, dz int
	}{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}} {
		out = append(out, PodiumCell{ox + t.dx, oy + 2, oz + t.dz, withProps("wall_torch", "facing", t.name)})
	}
	return out
}

// EndExitPortal is where the exit podium stands: the island's top block at
// 0,0 as generated (EnderDragonFight's MOTION_BLOCKING_NO_LEAVES height
// there, one down).
func (g *Generator) EndExitPortal() (x, y, z int) {
	from := EndSurfaceY + 16
	if g.vEnd() != nil {
		from = vdmH - 1 // the vanilla island's top, wherever it stands
	}
	for y := from; y > MinY+1; y-- {
		if g.endBlockCol(0, y, 0, 0, 0, false) == EndStone {
			return 0, y, 0
		}
	}
	return 0, EndSurfaceY, 0
}

// stampEndPodium pre-places the inactive podium, unless a player built or
// dug round it.
func (g *Generator) stampEndPodium(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	if baseX > 4 || baseX+15 < -4 || baseZ > 4 || baseZ+15 < -4 {
		return
	}
	ox, oy, oz := g.EndExitPortal()
	if g.touchedIn(ox-4, oy-1, oz-4, ox+4, oy+32, oz+4) {
		return
	}
	for _, c := range EndPodium(ox, oy, oz, false) {
		setSectionBlock(ch, c.X-baseX, c.Y, c.Z-baseZ, c.State, true)
	}
}
