package world

import (
	"sync"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Biome overrides: what /fillbiome writes. Vanilla keeps a chunk's biomes
// per quart (a 4×4×4 cell, QuartPos) and /fillbiome rewrites quarts, so the
// overrides are kept per quart too, and BiomeAt3D — what spawning,
// precipitation and the rest read — answers from them first.
//
// The chunk the client is sent carries one biome per section (the attach
// ChunkHeader), so a section shows the biome most of its 64 quarts have:
// a fill covering a section whole shows exactly; a partial one shows where
// it covers the larger part.

// Quart is a quart's coordinates: a block position divided by four, floored.
type Quart = [3]int32

// QuartOf is the quart holding a block (QuartPos.fromBlock on each axis).
func QuartOf(x, y, z int) Quart { return Quart{int32(x >> 2), int32(y >> 2), int32(z >> 2)} }

// biomeOverlay holds the overrides by chunk. The zero value is empty and
// ready.
type biomeOverlay struct {
	mu sync.RWMutex
	m  map[chunkPos]map[Quart]string
}

func quartChunk(q Quart) chunkPos { return chunkPos{q[0] >> 2, q[2] >> 2} }

func (b *biomeOverlay) get(x, y, z int) (string, bool) {
	q := QuartOf(x, y, z)
	b.mu.RLock()
	defer b.mu.RUnlock()
	s, ok := b.m[quartChunk(q)][q]
	return s, ok
}

func (b *biomeOverlay) set(q Quart, biome string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.m == nil {
		b.m = map[chunkPos]map[Quart]string{}
	}
	k := quartChunk(q)
	if b.m[k] == nil {
		b.m[k] = map[Quart]string{}
	}
	b.m[k][q] = biome
}

func (b *biomeOverlay) clear(q Quart) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := quartChunk(q)
	if m := b.m[k]; m != nil {
		delete(m, q)
		if len(m) == 0 {
			delete(b.m, k)
		}
	}
}

// applyTo sets each overridden section of a chunk copy to the biome most of
// its quarts have.
func (b *biomeOverlay) applyTo(ch *worldgen.Chunk, cx, cz int32) {
	b.mu.RLock()
	m := b.m[chunkPos{cx, cz}]
	if len(m) == 0 {
		b.mu.RUnlock()
		return
	}
	perSec := map[int]map[string]int{}
	for q, biome := range m {
		sec := (int(q[1])*4 - worldgen.MinY) / 16
		if sec < 0 || sec >= len(ch.Biomes) {
			continue
		}
		if perSec[sec] == nil {
			perSec[sec] = map[string]int{}
		}
		perSec[sec][biome]++
	}
	b.mu.RUnlock()
	for sec, counts := range perSec {
		n := 0
		for _, c := range counts {
			n += c
		}
		base := ch.Biomes[sec]
		best, bestN := base, 64-n // the quarts left as generated; they win a tie
		for biome, c := range counts {
			if c > bestN || c == bestN && best != base && biome < best {
				best, bestN = biome, c
			}
		}
		ch.Biomes[sec] = best
	}
}

// SetBiome overrides the biome of the quart holding (x,y,z) and reports
// whether the biome there changed. An override back to what generation put
// there is dropped rather than kept.
func (w *World) SetBiome(x, y, z int, biome string) bool {
	if !w.inBounds(y) {
		return false
	}
	cur := w.BiomeAt3D(x, y, z)
	q := QuartOf(x, y, z)
	if biome == w.generatedBiome3D(x, y, z) {
		w.bio.clear(q)
	} else {
		w.bio.set(q, biome)
	}
	return cur != biome
}

// BiomeOverrides is every override, for saving.
func (w *World) BiomeOverrides() map[Quart]string {
	w.bio.mu.RLock()
	defer w.bio.mu.RUnlock()
	out := map[Quart]string{}
	for _, m := range w.bio.m {
		for q, b := range m {
			out[q] = b
		}
	}
	return out
}

// LoadBiomeOverrides installs saved overrides (at boot, before any chunk is
// served).
func (w *World) LoadBiomeOverrides(m map[Quart]string) {
	for q, b := range m {
		w.bio.set(q, b)
	}
}

// SectionBiomes is the biome each section of a chunk shows its viewers,
// bottom to top — what the chunk carries, overrides applied.
func (w *World) SectionBiomes(cx, cz int32) []string {
	base := w.generated(cx, cz)
	ch := &worldgen.Chunk{Biomes: append([]string(nil), base.Biomes...)}
	w.bio.applyTo(ch, cx, cz)
	return ch.Biomes
}
