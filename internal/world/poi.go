package world

import (
	"sort"
	"sync"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The point-of-interest index: vanilla's PoiManager, for the blocks villagers
// claim (beds, workstations, bells). Each chunk's list is built lazily from
// its generated blocks with the edits laid over them, and dropped when an
// edit lands in that chunk. It only ever reads loaded chunks: a query never
// generates anything, so the hub can ask it.
//
// What counts as a point of interest, and of what kind, is the caller's
// (SetPOIKinds): the world keeps positions and a kind byte, nothing more.

// POI is one point of interest.
type POI struct {
	X, Y, Z int
	Kind    uint8
}

type poiIndex struct {
	mu     sync.Mutex
	kinds  []uint8 // by state id; 0 = not a point of interest
	chunks map[chunkPos][]POI
}

// SetPOIKinds installs the classifier: the kind of point of interest a block
// state is, 0 for none. Call once, before any query.
func (w *World) SetPOIKinds(kind func(state uint32) uint8) {
	max := worldgen.MaxState()
	t := make([]uint8, max+1)
	for s := uint32(0); s <= max; s++ {
		t[s] = kind(s)
	}
	w.poi.mu.Lock()
	w.poi.kinds = t
	w.poi.chunks = map[chunkPos][]POI{}
	w.poi.mu.Unlock()
}

// HasPOIKinds reports whether a classifier is installed.
func (w *World) HasPOIKinds() bool {
	w.poi.mu.Lock()
	defer w.poi.mu.Unlock()
	return w.poi.kinds != nil
}

// poiKind is the kind of a state (0 when no classifier is set).
func (w *World) poiKind(s uint32) uint8 {
	if int(s) < len(w.poi.kinds) {
		return w.poi.kinds[s]
	}
	return 0
}

// POIsInChunk returns a loaded chunk's points of interest; ok is false when
// the chunk is not loaded (nothing is generated to answer).
func (w *World) POIsInChunk(cx, cz int32) ([]POI, bool) {
	key := chunkPos{cx, cz}
	w.poi.mu.Lock()
	if w.poi.kinds == nil {
		w.poi.mu.Unlock()
		return nil, true
	}
	if l, ok := w.poi.chunks[key]; ok {
		w.poi.mu.Unlock()
		return l, true
	}
	w.poi.mu.Unlock()
	if !w.Loaded(cx, cz) {
		return nil, false
	}
	ch := w.generated(cx, cz) // loaded: a cache hit, never a generation
	w.mu.RLock()
	edits := w.edits[key]
	var out []POI
	for sec := range ch.Sections {
		baseY := worldgen.MinY + sec*16
		for i, s := range ch.Sections[sec] {
			if w.poiKind(s) == 0 {
				continue
			}
			ly, lz, lx := i/256, (i/16)%16, i%16
			y := baseY + ly
			if _, edited := edits[localIndex(lx, y, lz)]; edited {
				continue // the edit decides this cell below
			}
			out = append(out, POI{int(cx)*16 + lx, y, int(cz)*16 + lz, w.poiKind(s)})
		}
	}
	for idx, s := range edits {
		if k := w.poiKind(s); k != 0 {
			lx, y, lz := splitIndex(idx)
			out = append(out, POI{int(cx)*16 + lx, y, int(cz)*16 + lz, k})
		}
	}
	w.mu.RUnlock()
	w.poi.mu.Lock()
	w.poi.chunks[key] = out
	w.poi.mu.Unlock()
	return out, true
}

// POIsNear returns the points of interest within r of a block (by squared
// distance, as vanilla's getInRange) in loaded chunks, closest first, keeping
// those want accepts.
func (w *World) POIsNear(x, y, z, r int, want func(POI) bool) []POI {
	var out []POI
	for cx := (x - r) >> 4; cx <= (x+r)>>4; cx++ {
		for cz := (z - r) >> 4; cz <= (z+r)>>4; cz++ {
			l, _ := w.POIsInChunk(int32(cx), int32(cz))
			for _, p := range l {
				dx, dy, dz := p.X-x, p.Y-y, p.Z-z
				if dx*dx+dy*dy+dz*dz <= r*r && (want == nil || want(p)) {
					out = append(out, p)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		di := sq(out[i].X-x) + sq(out[i].Y-y) + sq(out[i].Z-z)
		dj := sq(out[j].X-x) + sq(out[j].Y-y) + sq(out[j].Z-z)
		if di != dj {
			return di < dj
		}
		if out[i].X != out[j].X {
			return out[i].X < out[j].X
		}
		if out[i].Y != out[j].Y {
			return out[i].Y < out[j].Y
		}
		return out[i].Z < out[j].Z
	})
	return out
}

func sq(v int) int { return v * v }

// poiInvalidate drops a chunk's list after an edit in it.
func (w *World) poiInvalidate(cx, cz int32) {
	w.poi.mu.Lock()
	if w.poi.chunks != nil {
		delete(w.poi.chunks, chunkPos{cx, cz})
	}
	w.poi.mu.Unlock()
}
