package world

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Stored heightmaps (vanilla's Heightmap, the four the live world keeps):
// per loaded chunk, the first free y above the highest block of each kind in
// every column. They are built the first time a loaded chunk is asked (a
// top-down scan of the generated base plus the edits, Heightmap.
// primeHeightmaps) and then maintained by SetBlock and RevertEdit exactly as
// Heightmap.update does, so a column is never rescanned from the sky. They
// live with the chunk in the memory cache and go when the LRU drops it.
//
// hmMu serialises every build and update. Its lock order is hmMu, then
// w.mu or genMu; nothing takes hmMu while holding either, and the LRU's
// eviction never touches it (dropping the cache entry drops the maps).

// HeightmapType names one of the live-world heightmaps.
type HeightmapType uint8

const (
	WorldSurface HeightmapType = iota
	OceanFloor
	MotionBlocking
	MotionBlockingNoLeaves
	numHeightmaps
)

// hmBit is the worldgen.HeightmapFlags bit for a heightmap.
var hmBit = [numHeightmaps]uint8{
	WorldSurface:           worldgen.HMWorldSurface,
	OceanFloor:             worldgen.HMOceanFloor,
	MotionBlocking:         worldgen.HMMotionBlocking,
	MotionBlockingNoLeaves: worldgen.HMMotionBlockingNoLeaves,
}

// chunkHeightmaps is a chunk's four heightmaps, each indexed lz*16+lx: the
// first free y (one above the highest matching block), MinY for a column
// with none — Heightmap.getFirstAvailable.
type chunkHeightmaps [numHeightmaps][256]int32

// Heightmap is getHeight(type, x, z) for a LOADED chunk: the y one above the
// highest block of the heightmap's kind in the column. ok=false when the
// chunk is not in memory; it never loads or generates one.
func (w *World) Heightmap(t HeightmapType, x, z int) (y int, ok bool) {
	if t >= numHeightmaps {
		return 0, false
	}
	cx, cz, lx, lz := chunkOf(x, z)
	w.hmMu.Lock()
	defer w.hmMu.Unlock()
	hm := w.heightmapsLocked(chunkPos{int32(cx), int32(cz)})
	if hm == nil {
		return 0, false
	}
	return int(hm[t][lz*16+lx]), true
}

// HeightAt is Heightmap with a fallback: a column in a chunk that is not
// loaded is scanned cell by cell with the same rule (which loads the chunk,
// as vanilla's getHeight does).
func (w *World) HeightAt(t HeightmapType, x, z int) int {
	if y, ok := w.Heightmap(t, x, z); ok {
		return y
	}
	bit := hmBit[t%numHeightmaps]
	for y := w.Ceiling() - 1; y >= worldgen.MinY; y-- {
		if worldgen.HeightmapFlags(w.At(x, y, z))&bit != 0 {
			return y + 1
		}
	}
	return worldgen.MinY
}

// heightmapsLocked returns a loaded chunk's heightmaps, building them on the
// first ask; nil when the chunk is not loaded. hmMu must be held.
func (w *World) heightmapsLocked(key chunkPos) *chunkHeightmaps {
	w.genMu.Lock()
	e, ok := w.cache[key]
	w.genMu.Unlock()
	if !ok || e.ch == nil {
		return nil
	}
	if e.hm != nil {
		return e.hm
	}
	hm := w.primeHeightmaps(key, e.ch)
	w.genMu.Lock()
	if cur, ok := w.cache[key]; ok && cur.ch == e.ch {
		cur.hm = hm
		w.cache[key] = cur
	}
	w.genMu.Unlock()
	return hm
}

// primeHeightmaps is Heightmap.primeHeightmaps over the generated base with
// the edit overlay applied: each column scanned down from its highest
// non-air cell until every heightmap has found its block.
func (w *World) primeHeightmaps(key chunkPos, base *worldgen.Chunk) *chunkHeightmaps {
	// Where to start: above the highest section of the base holding anything
	// but air, and above the highest edit in each column.
	topBase := worldgen.MinY - 1
	for s := len(base.Sections) - 1; s >= 0 && topBase < worldgen.MinY; s-- {
		for _, v := range &base.Sections[s] {
			if v != worldgen.Air {
				topBase = worldgen.MinY + s*16 + 15
				break
			}
		}
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	edits := w.edits[key]
	var topEdit [256]int
	for i := range topEdit {
		topEdit[i] = worldgen.MinY - 1
	}
	for idx := range edits {
		lx, y, lz := splitIndex(idx)
		if c := lz*16 + lx; y > topEdit[c] {
			topEdit[c] = y
		}
	}
	hm := new(chunkHeightmaps)
	for c := 0; c < 256; c++ {
		lx, lz := c&15, c>>4
		for t := range hm {
			hm[t][c] = worldgen.MinY
		}
		remaining := uint8(worldgen.HMWorldSurface | worldgen.HMOceanFloor | worldgen.HMMotionBlocking | worldgen.HMMotionBlockingNoLeaves)
		for y := max(topBase, topEdit[c]); y >= worldgen.MinY && remaining != 0; y-- {
			st, ok := edits[localIndex(lx, y, lz)]
			if !ok {
				sec, ly := (y-worldgen.MinY)/16, (y-worldgen.MinY)%16
				if sec >= len(base.Sections) {
					continue
				}
				st = base.Sections[sec][(ly*16+lz)*16+lx]
			}
			f := worldgen.HeightmapFlags(st) & remaining
			if f == 0 {
				continue
			}
			for t := HeightmapType(0); t < numHeightmaps; t++ {
				if f&hmBit[t] != 0 {
					hm[t][c] = int32(y + 1)
				}
			}
			remaining &^= f
		}
	}
	return hm
}

// heightmapUpdate is Heightmap.update for every heightmap of a loaded chunk
// after the block at (x,y,z) became state: a matching block at or above the
// column's first free cell raises it; a non-matching one where the top block
// was drops it to the next matching block below.
func (w *World) heightmapUpdate(x, y, z int, state uint32) {
	cx, cz, lx, lz := chunkOf(x, z)
	key := chunkPos{int32(cx), int32(cz)}
	w.hmMu.Lock()
	defer w.hmMu.Unlock()
	w.genMu.Lock()
	e, ok := w.cache[key]
	w.genMu.Unlock()
	if !ok || e.hm == nil {
		return // not loaded, or never asked: it is built fresh when it is
	}
	hm := e.hm
	c := lz*16 + lx
	flags := worldgen.HeightmapFlags(state)
	for t := HeightmapType(0); t < numHeightmaps; t++ {
		first := int(hm[t][c])
		if y <= first-2 {
			continue
		}
		bit := hmBit[t]
		if flags&bit != 0 {
			if y >= first {
				hm[t][c] = int32(y + 1)
			}
			continue
		}
		if first-1 != y {
			continue
		}
		hm[t][c] = worldgen.MinY
		for yy := y - 1; yy >= worldgen.MinY; yy-- {
			if worldgen.HeightmapFlags(w.At(x, yy, z))&bit != 0 {
				hm[t][c] = int32(yy + 1)
				break
			}
		}
	}
}
