package world

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Per-block light updates, after vanilla's LevelLightEngine. The first read
// of a chunk's light runs the full flood fill (light.go); after that, an edit
// does not throw the cached light away. It relights the cells around the
// edited one, the way vanilla's LightEngine.checkBlock does:
//
//   - checkNode looks at the edited cell. If it held more light than it can
//     have now, the cell drops to 0 and a DECREASE entry carries its old
//     level outward; otherwise a "pull light in" entry asks its neighbours to
//     push light back into it. An emitter queues an INCREASE from its
//     emission.
//   - propagateDecrease zeroes every neighbour dimmer than the level that
//     left (it may have been lit through the cell), passing the decrease on;
//     a neighbour as bright or brighter is lit independently, and is queued
//     to push its light back into the darkened cells.
//   - propagateIncrease floods from the queued cells, a step costing
//     max(1, filter) and never entering an opaque cell — the full fill's rule.
//
// Sky light adds the per-column sources (SkyLightEngine / ChunkSkyLightSources):
// every cell from the top of the world down to the first block that is not
// fully clear is a level-15 source. An edit that closes a column removes the
// sources below it, one that opens a column adds them, and both then run the
// same decrease/increase passes. A stored sky level of 15 is always a source
// (propagation loses at least one level a step), so the stored light itself
// says where a column's sources end.
//
// Only cells whose light can change are touched, and they all lie within 14
// blocks (horizontally) of the edit, so reads stay within 15: the window of
// chunks covering ±15 blocks is all an edit ever needs. When any of those
// chunks has no cached light, the edit falls back to dropping the cached 3×3
// (the old behaviour); the next read recomputes it with the full fill.
//
// Cached LightData is copy-on-write: an edit copies a chunk's sky or block
// arrays the first time it changes one of them and publishes the copy, so a
// LightData already handed to a reader (a chunk being encoded, a spawn check)
// never changes under it.

// lightReach is how far (in blocks, horizontally) an edit's light update can
// read: cells change within 14, and their neighbours are read.
const lightReach = 15

// lightVerSlots sizes World.lightVer. A collision only makes a raced flood
// fill skip the cache once.
const lightVerSlots = 1024

func lightVerSlot(cx, cz int32) int {
	h := uint32(cx)*73856093 ^ uint32(cz)*19349663
	return int(h % lightVerSlots)
}

// Queue entries: LightEngine.QueueEntry's packing — the level in bits 0..3, a
// bit per direction to propagate in (4..9), and the "increase from emission"
// flag (11). Directions follow vanilla's Direction order.
const (
	dirDown = iota
	dirUp
	dirNorth // -z
	dirSouth // +z
	dirWest  // -x
	dirEast  // +x
)

const (
	qLevelMask    = 0xF
	qDirsAll      = 0x3F << 4
	qFromEmission = 1 << 11
)

func qDir(d int) uint32 { return 1 << (d + 4) }

func oppositeDir(d int) int { return d ^ 1 }

func qDecreaseAll(level int) uint32 { return qDirsAll | uint32(level&qLevelMask) }

func qDecreaseSkip(level, skip int) uint32 {
	return (qDirsAll &^ qDir(skip)) | uint32(level&qLevelMask)
}

func qIncreaseEmission(level int) uint32 {
	return qDirsAll | qFromEmission | uint32(level&qLevelMask)
}

func qIncreaseSkip(level, skip int) uint32 {
	return (qDirsAll &^ qDir(skip)) | uint32(level&qLevelMask)
}

func qIncreaseOnly(level, dir int) uint32 { return qDir(dir) | uint32(level&qLevelMask) }

// qPullLightIn is PULL_LIGHT_IN_ENTRY: a decrease of level 1 zeroes nothing,
// so it only asks every lit neighbour to push its light back in.
var qPullLightIn = qDecreaseAll(1)

type lightQEntry struct {
	node int32
	data uint32
}

// lightWindow is the chunks around one edit, addressed as one block box: node
// = (yi*sz + bz)*sx + bx, with bx/bz from the window's min corner and yi the
// height above the world floor.
type lightWindow struct {
	noSky    bool
	wx0, wz0 int // world coords of the window's min corner
	ncx      int // chunks across (x)
	sx, sz   int // blocks across
	h        int // column height in blocks

	gen   []*worldgen.Chunk
	edits []map[int]uint32 // live overlays: read only with World.mu read-locked
	ld    []*LightData     // the chunk's light; replaced by a copy on first write
	owned []uint8          // bit 0: Sky copied, bit 1: Block copied, bit 2: struct copied

	dec, inc []lightQEntry
}

func (lw *lightWindow) node(bx, bz, yi int) int32 { return int32((yi*lw.sz+bz)*lw.sx + bx) }

func (lw *lightWindow) decode(n int32) (bx, bz, yi int) {
	i := int(n)
	bx = i % lw.sx
	i /= lw.sx
	return bx, i % lw.sz, i / lw.sz
}

// cell locates a node: its chunk (window index), section and in-section index.
func (lw *lightWindow) cell(n int32) (ci, sec, idx, local int) {
	bx, bz, yi := lw.decode(n)
	lx, lz := bx&15, bz&15
	ci = (bz>>4)*lw.ncx + (bx >> 4)
	return ci, yi >> 4, ((yi&15)*16+lz)*16 + lx, yi*256 + lz*16 + lx
}

func (lw *lightWindow) state(n int32) uint32 {
	ci, sec, idx, local := lw.cell(n)
	if m := lw.edits[ci]; m != nil {
		if s, ok := m[local]; ok {
			return s
		}
	}
	return lw.gen[ci].Sections[sec][idx]
}

func (lw *lightWindow) get(sky bool, n int32) int {
	ci, sec, idx, _ := lw.cell(n)
	if sky {
		return int(lw.ld[ci].Sky[sec][idx])
	}
	return int(lw.ld[ci].Block[sec][idx])
}

func (lw *lightWindow) set(sky bool, n int32, v int) {
	ci, sec, idx, _ := lw.cell(n)
	lw.own(ci, sky)
	if sky {
		lw.ld[ci].Sky[sec][idx] = uint8(v)
	} else {
		lw.ld[ci].Block[sec][idx] = uint8(v)
	}
}

// own makes the chunk's sky or block arrays private to this update before the
// first write: the published LightData stays as it was for its readers.
func (lw *lightWindow) own(ci int, sky bool) {
	bit := uint8(2)
	if sky {
		bit = 1
	}
	if lw.owned[ci]&bit != 0 {
		return
	}
	if lw.owned[ci]&4 == 0 {
		cp := *lw.ld[ci] // shares both slices until one is copied below
		lw.ld[ci] = &cp
		lw.owned[ci] |= 4
	}
	if sky {
		s := make([][4096]uint8, len(lw.ld[ci].Sky))
		copy(s, lw.ld[ci].Sky)
		lw.ld[ci].Sky = s
	} else {
		b := make([][4096]uint8, len(lw.ld[ci].Block))
		copy(b, lw.ld[ci].Block)
		lw.ld[ci].Block = b
	}
	lw.owned[ci] |= bit
}

// neighbor is the node one step in dir, or false off the window or the world.
func (lw *lightWindow) neighbor(n int32, dir int) (int32, bool) {
	bx, bz, yi := lw.decode(n)
	switch dir {
	case dirDown:
		yi--
	case dirUp:
		yi++
	case dirNorth:
		bz--
	case dirSouth:
		bz++
	case dirWest:
		bx--
	case dirEast:
		bx++
	}
	if bx < 0 || bx >= lw.sx || bz < 0 || bz >= lw.sz || yi < 0 || yi >= lw.h {
		return 0, false
	}
	return lw.node(bx, bz, yi), true
}

func (lw *lightWindow) enqueueDecrease(n int32, data uint32) {
	lw.dec = append(lw.dec, lightQEntry{n, data})
}

func (lw *lightWindow) enqueueIncrease(n int32, data uint32) {
	lw.inc = append(lw.inc, lightQEntry{n, data})
}

// checkBlockNode is BlockLightEngine.checkNode for the edited cell.
func (lw *lightWindow) checkBlockNode(n int32) {
	emission := int(worldgen.LightEmissionFast(lw.state(n)))
	old := lw.get(false, n)
	if emission < old {
		lw.set(false, n, 0)
		lw.enqueueDecrease(n, qDecreaseAll(old))
	} else {
		lw.enqueueDecrease(n, qPullLightIn)
	}
	if emission > 0 {
		lw.enqueueIncrease(n, qIncreaseEmission(emission))
	}
}

// checkSkyNode is SkyLightEngine.checkNode for the edited cell: move the
// column's sources to the new lowest one, then relight the cell itself.
func (lw *lightWindow) checkSkyNode(n int32) {
	bx, bz, yi := lw.decode(n)
	// The cells above are unchanged, so they are all sources (the column
	// above is clear) exactly when the one directly above stores 15.
	aboveOpen := yi == lw.h-1 || lw.get(true, lw.node(bx, bz, yi+1)) == 15
	lowest := lw.h // no source in the column at or below yi
	if aboveOpen {
		lowest = yi + 1
		for y := yi; y >= 0 && worldgen.LightFilterFast(lw.state(lw.node(bx, bz, y))) == 0; y-- {
			lowest = y
		}
		lw.removeSourcesBelow(bx, bz, lowest)
		lw.addSourcesAbove(bx, bz, lowest)
	}
	if aboveOpen && yi >= lowest {
		lw.enqueueDecrease(n, qDecreaseSkip(15, dirUp))
		lw.enqueueIncrease(n, qIncreaseSkip(15, dirUp))
		return
	}
	if old := lw.get(true, n); old > 0 {
		lw.set(true, n, 0)
		lw.enqueueDecrease(n, qDecreaseAll(old))
	} else {
		lw.enqueueDecrease(n, qPullLightIn)
	}
}

// removeSourcesBelow clears the column's sources under its new lowest one.
func (lw *lightWindow) removeSourcesBelow(bx, bz, lowest int) {
	for y := lowest - 1; y >= 0; y-- {
		n := lw.node(bx, bz, y)
		if lw.get(true, n) != 15 {
			return
		}
		lw.set(true, n, 0)
		if y == lowest-1 {
			lw.enqueueDecrease(n, qDecreaseAll(15))
		} else {
			lw.enqueueDecrease(n, qDecreaseSkip(15, dirUp))
		}
	}
}

// addSourcesAbove raises every cell from the new lowest source up to the old
// one to 15.
func (lw *lightWindow) addSourcesAbove(bx, bz, lowest int) {
	for y := lowest; y < lw.h; y++ {
		n := lw.node(bx, bz, y)
		if lw.get(true, n) == 15 {
			return
		}
		lw.set(true, n, 15)
		lw.enqueueIncrease(n, qIncreaseSkip(15, dirUp))
	}
}

// propagateDecrease is LightEngine.propagateDecrease (block light also
// re-seeds a zeroed emitter from its emission; the sky has no emitters).
func (lw *lightWindow) propagateDecrease(sky bool, n int32, data uint32) {
	oldFrom := int(data & qLevelMask)
	for dir := dirDown; dir <= dirEast; dir++ {
		if data&qDir(dir) == 0 {
			continue
		}
		to, ok := lw.neighbor(n, dir)
		if !ok {
			continue
		}
		toLevel := lw.get(sky, to)
		if toLevel == 0 {
			continue
		}
		if toLevel <= oldFrom-1 {
			emission := 0
			if !sky {
				emission = int(worldgen.LightEmissionFast(lw.state(to)))
			}
			lw.set(sky, to, 0)
			if emission < toLevel {
				lw.enqueueDecrease(to, qDecreaseSkip(toLevel, oppositeDir(dir)))
			}
			if emission > 0 {
				lw.enqueueIncrease(to, qIncreaseEmission(emission))
			}
		} else {
			lw.enqueueIncrease(to, qIncreaseOnly(toLevel, oppositeDir(dir)))
		}
	}
}

// propagateIncrease is LightEngine.propagateIncrease with the full fill's
// step rule: cost max(1, filter), opaque cells never entered.
func (lw *lightWindow) propagateIncrease(sky bool, n int32, data uint32, fromLevel int) {
	for dir := dirDown; dir <= dirEast; dir++ {
		if data&qDir(dir) == 0 {
			continue
		}
		to, ok := lw.neighbor(n, dir)
		if !ok {
			continue
		}
		toLevel := lw.get(sky, to)
		if fromLevel-1 <= toLevel {
			continue
		}
		filter := int(worldgen.LightFilterFast(lw.state(to)))
		if filter >= worldgen.Opaque {
			continue
		}
		cost := filter
		if cost < 1 {
			cost = 1
		}
		if nl := fromLevel - cost; nl > toLevel {
			lw.set(sky, to, nl)
			if nl > 1 {
				lw.enqueueIncrease(to, qIncreaseSkip(nl, oppositeDir(dir)))
			}
		}
	}
}

// run drains the queues: every decrease first, then every increase (an
// increase whose cell has since changed level is stale and skipped).
func (lw *lightWindow) run(sky bool) {
	for i := 0; i < len(lw.dec); i++ {
		e := lw.dec[i]
		lw.propagateDecrease(sky, e.node, e.data)
	}
	lw.dec = lw.dec[:0]
	for i := 0; i < len(lw.inc); i++ {
		e := lw.inc[i]
		fromLevel := lw.get(sky, e.node)
		target := int(e.data & qLevelMask)
		if e.data&qFromEmission != 0 && fromLevel < target {
			lw.set(sky, e.node, target)
			fromLevel = target
		}
		if fromLevel == target {
			lw.propagateIncrease(sky, e.node, e.data, fromLevel)
		}
	}
	lw.inc = lw.inc[:0]
}

// relightLocked brings the cached light up to date with a changed cell at
// (x, y, z), whose previous state was old (or, when !oldKnown, the generated
// block). The new state is already in the overlay. The caller holds lightMu
// and not mu.
func (w *World) relightLocked(x, y, z int, old uint32, oldKnown bool) {
	cx, cz := int32(floorDiv(x, 16)), int32(floorDiv(z, 16))
	for dz := int32(-1); dz <= 1; dz++ { // a flood fill reading this cell is now stale
		for dx := int32(-1); dx <= 1; dx++ {
			w.lightVer[lightVerSlot(cx+dx, cz+dz)]++
		}
	}

	cx0, cx1 := int32(floorDiv(x-lightReach, 16)), int32(floorDiv(x+lightReach, 16))
	cz0, cz1 := int32(floorDiv(z-lightReach, 16)), int32(floorDiv(z+lightReach, 16))
	ncx, ncz := int(cx1-cx0+1), int(cz1-cz0+1)
	lw := &lightWindow{
		noSky: w.noSky,
		wx0:   int(cx0) * 16,
		wz0:   int(cz0) * 16,
		ncx:   ncx,
		sx:    ncx * 16,
		sz:    ncz * 16,
		h:     w.Sections() * 16,
		gen:   make([]*worldgen.Chunk, ncx*ncz),
		edits: make([]map[int]uint32, ncx*ncz),
		ld:    make([]*LightData, ncx*ncz),
		owned: make([]uint8, ncx*ncz),
	}
	for dz := 0; dz < ncz; dz++ {
		for dx := 0; dx < ncx; dx++ {
			e, ok := w.lightCache[chunkPos{cx0 + int32(dx), cz0 + int32(dz)}]
			if !ok {
				// Light for a chunk in reach was never computed or has been
				// evicted: nothing to relight from. Drop the rest; the next
				// read runs the full fill.
				w.dropLightLocked(cx, cz)
				w.lightDropped++
				return
			}
			lw.ld[dz*ncx+dx] = e.ld
		}
	}
	for dz := 0; dz < ncz; dz++ { // before mu: generating reads the edits
		for dx := 0; dx < ncx; dx++ {
			lw.gen[dz*ncx+dx] = w.generated(cx0+int32(dx), cz0+int32(dz))
		}
	}
	n := lw.node(x-lw.wx0, z-lw.wz0, y-worldgen.MinY)
	if !oldKnown {
		ci, sec, idx, _ := lw.cell(n)
		old = lw.gen[ci].Sections[sec][idx]
	}

	w.mu.RLock()
	for dz := 0; dz < ncz; dz++ {
		for dx := 0; dx < ncx; dx++ {
			lw.edits[dz*ncx+dx] = w.edits[chunkPos{cx0 + int32(dx), cz0 + int32(dz)}]
		}
	}
	cur := lw.state(n)
	// hasDifferentLightProperties: only a change of filter or emission moves
	// light; the sky only sees the filter.
	oldFilter, newFilter := worldgen.LightFilterFast(old), worldgen.LightFilterFast(cur)
	blockChanged := oldFilter != newFilter || worldgen.LightEmissionFast(old) != worldgen.LightEmissionFast(cur)
	if blockChanged {
		lw.checkBlockNode(n)
		lw.run(false)
	}
	if !lw.noSky && oldFilter != newFilter {
		lw.checkSkyNode(n)
		lw.run(true)
	}
	w.mu.RUnlock()

	for dz := 0; dz < ncz; dz++ {
		for dx := 0; dx < ncx; dx++ {
			i := dz*ncx + dx
			if lw.owned[i] == 0 {
				continue
			}
			key := chunkPos{cx0 + int32(dx), cz0 + int32(dz)}
			e := w.lightCache[key]
			e.ld = lw.ld[i]
			w.lightCache[key] = e
		}
	}
	w.lightRelit++
}

// dropAllLight forgets every cached chunk light (a bulk rewrite of the
// overlay, which no per-cell relight follows).
func (w *World) dropAllLight() {
	w.lightMu.Lock()
	w.lightCache = make(map[chunkPos]lightCacheEntry)
	w.lightLRU.Init()
	for i := range w.lightVer {
		w.lightVer[i]++
	}
	w.lightMu.Unlock()
}

// lightStats reports edits relit in place and edits that dropped the cache.
func (w *World) lightStats() (relit, dropped uint64) {
	w.lightMu.Lock()
	defer w.lightMu.Unlock()
	return w.lightRelit, w.lightDropped
}
