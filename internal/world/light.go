package world

import (
	"container/list"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Sky-light propagation. The client does not compute lighting itself, so the
// server ships a light level (0–15) for every block. This replaces the old
// "everything is full daylight" stopgap, which left caves and interiors lit.
//
// The model is the classic two-phase flood fill:
//
//  1. Direct skylight: in every column, light falls straight down at full
//     strength (15) through transparent blocks until it meets something opaque.
//  2. Propagation: that light then spreads to neighbouring blocks, losing one
//     level per step (plus any translucency), so it seeps sideways into
//     overhangs and through cave mouths but fades out deep underground.
//
// Light can travel up to 15 blocks, so a block's light depends on terrain up to
// 15 away — across chunk borders. To get borders right (and avoid dark seams at
// cave entrances), we flood-fill the chunk together with its eight neighbours
// and return only the centre. One ring of neighbours is exactly enough: a
// 16-wide centre plus 16-wide neighbours covers the full 15-block reach.

// LightData holds sky- and block-light levels (0–15) for one chunk — one value
// per block, in the same YZX order as chunk block data. The wire layer packs
// these into the nibble arrays of a Chunk Data packet. Sky light comes from the
// open sky; block light comes from emitters (torches, lava, glowstone, …).
type LightData struct {
	Sky   [][4096]uint8
	Block [][4096]uint8
}

const regionW = 48 // 3 chunks wide: centre + a one-chunk ring

// ri indexes a region buffer at (rx, rz, yi); yi is height above the world floor.
func ri(rx, rz, yi int) int { return (yi*regionW+rz)*regionW + rx }

// propagate runs the flood fill: every queued cell pushes light to its six
// neighbours, losing one level per step plus the destination's translucency,
// and never entering an opaque cell. Used for both sky and block light — they
// differ only in how the queue is seeded.
func propagate(op, level []uint8, queue []int32, effTop int) {
	for head := 0; head < len(queue); head++ {
		i := int(queue[head])
		l := level[i]
		if l <= 1 {
			continue
		}
		yi := i / (regionW * regionW)
		rem := i % (regionW * regionW)
		rz := rem / regionW
		rx := rem % regionW

		spread := func(rx, rz, yi int) {
			j := ri(rx, rz, yi)
			if op[j] >= worldgen.Opaque {
				return
			}
			if nl := l - 1 - op[j]; nl > level[j] {
				level[j] = nl
				queue = append(queue, int32(j))
			}
		}
		if rx > 0 {
			spread(rx-1, rz, yi)
		}
		if rx < regionW-1 {
			spread(rx+1, rz, yi)
		}
		if rz > 0 {
			spread(rx, rz-1, yi)
		}
		if rz < regionW-1 {
			spread(rx, rz+1, yi)
		}
		if yi > 0 {
			spread(rx, rz, yi-1)
		}
		if yi < effTop-1 {
			spread(rx, rz, yi+1)
		}
	}
}

// Light computes sky and block light for the chunk at (cx, cz), accounting for
// terrain and edits in the chunk and its neighbours. Caves are dark unless a
// player places an emitter (torch, glowstone, …).
// BlockLightAt returns the block-light level (0..15) at a world coordinate, used by
// mob spawning to keep hostiles out of torch-lit areas. It computes the block's
// chunk light, so call it sparingly (e.g. once per spawn attempt), not per tick.
func (w *World) BlockLightAt(x, y, z int) uint8 {
	_, b := w.LightAt(x, y, z)
	return b
}

// LightAt returns (sky, block) light at a world coordinate from one chunk
// light computation. Like BlockLightAt, computing the chunk's light is the
// expensive part — callers doing several lookups in one chunk should use
// Light() directly and index it themselves.
func (w *World) LightAt(x, y, z int) (uint8, uint8) {
	if y < worldgen.MinY || y >= w.Ceiling() {
		return 0, 0
	}
	cx, cz := floorDiv(x, 16), floorDiv(z, 16)
	ld := w.Light(int32(cx), int32(cz))
	lx, lz := x-cx*16, z-cz*16
	sec, ly := (y-worldgen.MinY)/16, (y-worldgen.MinY)%16
	i := (ly*16+lz)*16 + lx
	return ld.Sky[sec][i], ld.Block[sec][i]
}

// lightCacheEntry is a computed chunk's light plus its LRU node.
type lightCacheEntry struct {
	ld   *LightData
	elem *list.Element
}

// lightCacheCap: byte-budgeted like the generator cache (a chunk's light is
// Sections×4096×2 bytes ≈ 0.2 MB at vanilla height). 48 MB ≈ 225 chunks —
// more than one player's tracked window, so steady-state spawning and chunk
// sends hit the cache.
func (w *World) lightCacheCap() int {
	n := (48 << 20) / (w.Sections() * 4096 * 2)
	if n < 64 {
		n = 64
	}
	return n
}

// invalidateLight drops cached light for the chunk containing (x,z) and its
// neighbours (light floods across chunk borders from a 3×3 read).
func (w *World) invalidateLight(x, z int) {
	cx, cz, _, _ := chunkOf(x, z)
	w.lightMu.Lock()
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			key := chunkPos{int32(cx) + dx, int32(cz) + dz}
			if e, ok := w.lightCache[key]; ok {
				w.lightLRU.Remove(e.elem)
				delete(w.lightCache, key)
			}
		}
	}
	w.lightMu.Unlock()
}

// Light returns the chunk's computed light, cached until an edit invalidates
// it (light is a pure function of terrain + edits).
func (w *World) Light(cx, cz int32) *LightData {
	key := chunkPos{cx, cz}
	w.lightMu.Lock()
	if e, ok := w.lightCache[key]; ok {
		w.lightLRU.MoveToFront(e.elem)
		w.lightMu.Unlock()
		return e.ld
	}
	w.lightMu.Unlock()

	ld := w.computeLight(cx, cz) // flood outside the lock (concurrent builders)

	w.lightMu.Lock()
	defer w.lightMu.Unlock()
	if e, ok := w.lightCache[key]; ok { // lost a race; keep the existing entry
		return e.ld
	}
	w.lightCache[key] = lightCacheEntry{ld: ld, elem: w.lightLRU.PushFront(key)}
	for len(w.lightCache) > w.lightCacheCap() {
		oldest := w.lightLRU.Back()
		if oldest == nil {
			break
		}
		w.lightLRU.Remove(oldest)
		delete(w.lightCache, oldest.Value.(chunkPos))
	}
	return ld
}

func (w *World) computeLight(cx, cz int32) *LightData {
	// Assemble the 3×3 neighbourhood: the shared cached generator output plus a
	// small snapshot of each chunk's edits. Reading the cached chunks directly
	// (instead of a full edited copy per neighbour) avoids nine ~0.4 MB copies
	// per lit chunk — the difference between cheap and GC-thrashing during a join.
	var gen [3][3]*worldgen.Chunk
	var ed [3][3]map[int]uint32
	for dz := 0; dz < 3; dz++ {
		for dx := 0; dx < 3; dx++ {
			ncx, ncz := cx-1+int32(dx), cz-1+int32(dz)
			gen[dz][dx] = w.generated(ncx, ncz)
			ed[dz][dx] = w.editsCopy(ncx, ncz)
		}
	}
	blockAt := func(rx, rz, yi int) uint32 {
		cdz, cdx := rz/16, rx/16
		lx, lz := rx%16, rz%16
		if m := ed[cdz][cdx]; m != nil {
			// edit keys are localIndex(lx,y,lz) = (y-MinY)*256 + lz*16 + lx; yi = y-MinY.
			if s, ok := m[yi*256+lz*16+lx]; ok {
				return s
			}
		}
		return gen[cdz][cdx].Sections[yi/16][((yi%16)*16+lz)*16+lx]
	}

	// Cap the flood fill just above the tallest block in the neighbourhood:
	// everything higher is open air at full daylight, so computing it is wasted
	// work — and for typical terrain that's most of the 384-block column. Edits
	// can raise the surface (player towers), so account for them too.
	maxY := worldgen.MinY - 1
	for dz := 0; dz < 3; dz++ {
		for dx := 0; dx < 3; dx++ {
			if h := gen[dz][dx].MaxHeight(); h > maxY {
				maxY = h
			}
			for k := range ed[dz][dx] {
				if y := k/256 + worldgen.MinY; y > maxY {
					maxY = y
				}
			}
		}
	}
	effTop := maxY - worldgen.MinY + 2 // one open-air layer above the tallest block
	if h := w.Ceiling() - worldgen.MinY; effTop > h {
		effTop = h
	}
	if effTop < 1 {
		effTop = 1
	}

	n := effTop * regionW * regionW
	op := make([]uint8, n)  // per-block opacity (0, 1, or Opaque)
	sky := make([]uint8, n) // resolved sky-light level
	blk := make([]uint8, n) // resolved block-light level

	// One scan: record opacity, and seed the block-light queue from any emitter.
	var blkQueue []int32
	for yi := 0; yi < effTop; yi++ {
		for rz := 0; rz < regionW; rz++ {
			for rx := 0; rx < regionW; rx++ {
				state := blockAt(rx, rz, yi)
				i := ri(rx, rz, yi)
				op[i] = worldgen.LightFilterFast(state)
				if e := worldgen.LightEmissionFast(state); e > 0 {
					blk[i] = e
					blkQueue = append(blkQueue, int32(i))
				}
			}
		}
	}

	// Seed sky light: full strength straight down each column until something
	// stops it. Those cells seed the sky propagation queue.
	skyQueue := make([]int32, 0, regionW*regionW*8)
	if !w.noSky { // the nether has no sky to seed from
		for rz := 0; rz < regionW; rz++ {
			for rx := 0; rx < regionW; rx++ {
				for yi := effTop - 1; yi >= 0; yi-- {
					i := ri(rx, rz, yi)
					if op[i] != 0 {
						break // opaque or translucent: direct skylight stops here
					}
					sky[i] = 15
					skyQueue = append(skyQueue, int32(i))
				}
			}
		}
	}

	propagate(op, sky, skyQueue, effTop)
	propagate(op, blk, blkQueue, effTop)

	// Extract the centre chunk (region columns 16..31) into section arrays. Cells
	// at or above effTop weren't computed: open sky (sky 15, block 0).
	sections := w.Sections()
	out := &LightData{
		Sky:   make([][4096]uint8, sections),
		Block: make([][4096]uint8, sections),
	}
	for s := 0; s < sections; s++ {
		for ly := 0; ly < 16; ly++ {
			yi := s*16 + ly
			for lz := 0; lz < 16; lz++ {
				for lx := 0; lx < 16; lx++ {
					idx := (ly*16+lz)*16 + lx
					if yi < effTop {
						ci := ri(16+lx, 16+lz, yi)
						out.Sky[s][idx] = sky[ci]
						out.Block[s][idx] = blk[ci]
					} else if !w.noSky {
						out.Sky[s][idx] = 15
					}
				}
			}
		}
	}
	return out
}
