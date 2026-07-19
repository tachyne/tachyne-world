package worldgen

import "sync"

// Woodland mansion worldgen integration: a deterministic site in dark-forest on
// flat ground, the grid+placer assembly cached per site (a mansion spans many
// chunks), stamped per chunk via Template.StampAt.

const (
	mansionCell = 1024
	mansionOdds = 0.7 // of dark-forest cells
)

// Mansion is a placed mansion site (or the zero value). X,Y,Z is the placer's
// base blockPos (the mansion extends around and above it).
type Mansion struct {
	X, Y, Z int
	Exists  bool
}

// MansionIn returns the mansion owning (wx,wz)'s cell, on flat dark-forest land.
func (g *Generator) MansionIn(wx, wz int) Mansion {
	ox, oz := cellOrigin(wx, mansionCell), cellOrigin(wz, mansionCell)
	if hash01(g.seed, ox, oz, 0x3A00) >= mansionOdds {
		return Mansion{}
	}
	x := ox + 128 + int(hash01(g.seed, ox, oz, 0x3A01)*float64(mansionCell-256))
	z := oz + 128 + int(hash01(g.seed, ox, oz, 0x3A02)*float64(mansionCell-256))
	if b := g.BiomeName(x, z); b != "minecraft:dark_forest" && b != "minecraft:pale_garden" {
		return Mansion{}
	}
	// Flatness + dry-land gate (a mansion is huge — pick a level clearing).
	lo, hi := 1<<30, -(1 << 30)
	for _, d := range [][2]int{{0, 0}, {32, 0}, {-32, 0}, {0, 32}, {0, -32}, {24, 24}, {-24, -24}} {
		h := g.Height(x+d[0], z+d[1])
		if h < lo {
			lo = h
		}
		if h > hi {
			hi = h
		}
	}
	if hi-lo > 8 || lo <= SeaLevel {
		return Mansion{}
	}
	return Mansion{X: x, Y: g.Height(x, z), Z: z, Exists: true}
}

type mansionKey struct {
	seed int64
	x, z int
}

var (
	manCache = map[mansionKey][]mansionPiece{}
	manMu    sync.Mutex
)

// AssembleMansion runs (and caches) the grid solver + placer for a site.
func (g *Generator) AssembleMansion(m Mansion) []mansionPiece {
	k := mansionKey{g.seed, m.X, m.Z}
	manMu.Lock()
	p, ok := manCache[k]
	manMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, m.X, m.Z)
	mg := newMansionGrid(rng)
	pl := &mansionPlacer{rng: rng}
	pl.createMansion([3]int{m.X, m.Y - 1, m.Z}, 0, mg)
	manMu.Lock()
	manCache[k] = pl.pieces
	manMu.Unlock()
	return pl.pieces
}

// stampMansion stamps the mansion pieces overlapping this chunk.
func (g *Generator) stampMansion(ch *Chunk, cx, cz int32) {
	m := g.MansionIn(int(cx)*16+8, int(cz)*16+8)
	if !m.Exists {
		return
	}
	for _, pc := range g.AssembleMansion(m) {
		if t := TemplateByName("woodland_mansion/" + pc.tmpl); t != nil {
			t.StampAt(ch, cx, cz, pc.pos[0], pc.pos[1], pc.pos[2], pc.rot, pc.mir)
		}
	}
}

// MansionMob is a placed illager spawn: X,Y,Z world + Type (0=evoker,
// 1=vindicator, 2=allay).
type MansionMob struct {
	X, Y, Z, Type int
}

// MansionMobs returns the world positions + types of the mansion's illager
// spawn markers (the server seeds the mobs when a player approaches).
func (g *Generator) MansionMobs(m Mansion) []MansionMob {
	var out []MansionMob
	for _, pc := range g.AssembleMansion(m) {
		t := TemplateByName("woodland_mansion/" + pc.tmpl)
		if t == nil {
			continue
		}
		for _, s := range t.MobSpawns {
			tx, ty, tz := transformPos(s[0], s[1], s[2], pc.rot, pc.mir)
			out = append(out, MansionMob{pc.pos[0] + tx, pc.pos[1] + ty, pc.pos[2] + tz, s[3]})
		}
	}
	return out
}

// MansionChests returns the world positions of the mansion's loot chests.
func (g *Generator) MansionChests(m Mansion) [][3]int {
	var out [][3]int
	for _, pc := range g.AssembleMansion(m) {
		t := TemplateByName("woodland_mansion/" + pc.tmpl)
		if t == nil {
			continue
		}
		for _, c := range t.Chests {
			tx, ty, tz := transformPos(c[0], c[1], c[2], pc.rot, pc.mir)
			out = append(out, [3]int{pc.pos[0] + tx, pc.pos[1] + ty, pc.pos[2] + tz})
		}
	}
	return out
}
