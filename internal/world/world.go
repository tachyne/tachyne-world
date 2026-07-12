// Package world is the mutable game world: procedurally generated terrain plus
// a persistent overlay of block edits. Only the edits (diffs from generation)
// are stored, so memory scales with how much players change, not world size.
// Safe for concurrent use by multiple connections.
package world

import (
	"container/list"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

type chunkPos = [2]int32

// genCacheBudget bounds the generated-chunk cache BY MEMORY, not count.
// Terrain generation (with lighting reading a 3×3 neighbourhood) is the join
// bottleneck, so we memoise the deterministic, pre-edit generator output —
// but a vanilla-height chunk is ~0.4 MB while a tall (earth true-scale, 108
// sections) one is ~1.8 MB, so a fixed count of 1024 would balloon from
// ~400 MB to ~1.8 GB and swap the box (it did). cacheCap() derives the entry
// count from the world's actual chunk size.
const (
	genCacheBudget      = 256 << 20 // main-world cached generator output
	genCacheBudgetMinor = 64 << 20  // nether/End: toured rarely, cheap to regen
	genCacheMin         = 128       // floor: lighting reads 3×3, keep a useful window
)

// cacheCap is the LRU entry limit for this world's chunk size. The budget is
// PER WORLD — the engine runs three (overworld/nether/End), so the sum must
// fit the pod's memory limit with GC headroom: a portal transit fills two
// caches at once and 3×400 MB OOM-killed the 1Gi world pod the first time a
// player toured the nether.
func (w *World) cacheCap() int {
	budget := genCacheBudget
	if w.noSky { // nether/End
		budget = genCacheBudgetMinor
	}
	bytesPerChunk := w.Sections() * 4096 * 4
	n := budget / bytesPerChunk
	if n < genCacheMin {
		n = genCacheMin
	}
	return n
}

// cacheEntry is a generated chunk plus its node in the LRU list.
type cacheEntry struct {
	ch   *worldgen.Chunk
	elem *list.Element
}

// World wraps the terrain generator with an edit overlay and a generated-chunk
// cache. The cache is valid forever (generation is a pure function of seed), so
// edits are always applied as a fresh layer on top of a cached base.
type World struct {
	gen   *worldgen.Generator
	seed  int64
	mu    sync.RWMutex
	edits map[chunkPos]map[int]uint32 // chunk -> local block index -> state

	genMu sync.Mutex
	cache map[chunkPos]cacheEntry
	lru   *list.List // front = most recently used; values are chunkPos

	// Computed chunk light, LRU'd like the generator output but INVALIDATED
	// by edits (light is a pure function of terrain + edits). Natural mob
	// spawning reads light on every attempt; without this cache each read
	// was a fresh 3×3 flood fill.
	lightMu    sync.Mutex
	lightCache map[chunkPos]lightCacheEntry
	lightLRU   *list.List

	store  Store       // nil = in-memory only (no persistence)
	noSky  bool        // nether/End: sky light is zero everywhere
	dimTag string      // chunk-cache key prefix for non-overworld dims
	dirty  atomic.Bool // edits changed since the last successful Save

	chunkCache ChunkCache    // persistent generated-chunk cache (nil = off)
	cachePuts  chan cachePut // async write queue for the cache (drop when full)
}

func New(seed int64) *World {
	return &World{
		gen:        worldgen.NewGenerator(seed),
		seed:       seed,
		edits:      make(map[chunkPos]map[int]uint32),
		cache:      make(map[chunkPos]cacheEntry),
		lru:        list.New(),
		lightCache: make(map[chunkPos]lightCacheEntry),
		lightLRU:   list.New(),
	}
}

// SetEarth switches this world's terrain to a real elevation model (earth
// mode, see worldgen/earth.go). Must be called at boot, before any chunk is
// generated. The chunk-cache key gains an "E.<name>.<vscale>." prefix so
// earth chunks never collide with noise chunks (or other regions/scales) for
// the same seed.
func (w *World) SetEarth(name string, vscale float64) (*worldgen.EarthDEM, error) {
	dem, err := worldgen.LoadEarthDEM(name, vscale)
	if err != nil {
		return nil, err
	}
	w.gen.SetEarth(dem)
	// Prepend (don't overwrite): a tall world's "H<sections>." tag composes
	// with the earth tag so every (region, vscale, height) keys distinctly.
	w.dimTag = fmt.Sprintf("E.%s.%g.%s", name, vscale, w.dimTag)
	return dem, nil
}

// SetCeiling raises the world's top build limit (tall worlds — earth mode at
// true vertical scale). Must be called at boot, before any chunk is generated.
// The chunk-cache key gains an "H<sections>." prefix so tall chunks never
// collide with vanilla-height ones.
func (w *World) SetCeiling(maxY int) {
	w.gen.SetCeiling(maxY)
	if s := w.gen.SectionCount(); s != worldgen.SectionCount {
		w.dimTag = fmt.Sprintf("H%d.%s", s, w.dimTag)
	}
}

// Sections is the world's column height in 16-block sections.
func (w *World) Sections() int { return w.gen.SectionCount() }

// Ceiling is the world's exclusive top build limit (world Y).
func (w *World) Ceiling() int { return w.gen.Ceiling() }

// NewEnd builds the End world for a seed (End generator, no sky light).
func NewEnd(seed int64, store Store) (*World, error) {
	w := New(seed)
	w.gen = worldgen.NewEndGenerator(seed)
	w.noSky = true
	w.dimTag = "e."
	w.store = store
	if store != nil {
		edits, err := store.Load()
		if err != nil {
			return nil, err
		}
		w.edits = edits
	}
	return w, nil
}

// NewNether builds the nether world for a seed: nether-mode generator and no
// sky light (the dimension has no sky).
func NewNether(seed int64, store Store) (*World, error) {
	w := New(seed)
	w.gen = worldgen.NewNetherGenerator(seed)
	w.noSky = true
	w.store = store
	if store != nil {
		edits, err := store.Load()
		if err != nil {
			return nil, err
		}
		w.edits = edits
	}
	return w, nil
}

// NewWithStore builds a world backed by store, loading any previously persisted
// edits so placed/broken blocks survive a restart. Call Save (periodically and
// on shutdown) to persist subsequent changes.
func NewWithStore(seed int64, store Store) (*World, error) {
	w := New(seed)
	w.store = store
	if store != nil {
		edits, err := store.Load()
		if err != nil {
			return nil, err
		}
		w.edits = edits
	}
	return w, nil
}

// EditCount reports how many block edits are currently held (placed/broken
// blocks). Useful at boot to confirm persisted edits reloaded.
func (w *World) EditCount() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	n := 0
	for _, m := range w.edits {
		n += len(m)
	}
	return n
}

// Save persists the current edits if anything changed since the last save. It
// snapshots under the read lock so it never races concurrent SetBlock calls.
// A no-op when there's no store or no changes.
func (w *World) Save() error {
	if w.store == nil || !w.dirty.Load() {
		return nil
	}
	w.mu.RLock()
	snapshot := make(map[chunkPos]map[int]uint32, len(w.edits))
	for key, m := range w.edits {
		cp := make(map[int]uint32, len(m))
		for idx, state := range m {
			cp[idx] = state
		}
		snapshot[key] = cp
	}
	w.mu.RUnlock()

	// Clear dirty before writing: a concurrent edit during the write re-sets it,
	// so we never miss a change (at worst we save it again next time).
	w.dirty.Store(false)
	if err := w.store.Save(snapshot); err != nil {
		w.dirty.Store(true) // write failed — keep it dirty so we retry
		return err
	}
	return nil
}

// MigrateEdits rewrites every persisted edit's block-state id through remap and
// saves the result. It exists for a one-time id-space migration when the
// canonical block-state numbering changes (a version bump): the caller supplies
// a pure old→new id function. Returns the number of edits whose id changed. Call
// once at startup BEFORE any chunk is served (edits are applied over a cached
// base at serve time, so an already-served chunk would keep the old ids).
func (w *World) MigrateEdits(remap func(state uint32) uint32) (int, error) {
	w.mu.Lock()
	n := 0
	for _, blocks := range w.edits {
		for idx, state := range blocks {
			if ns := remap(state); ns != state {
				blocks[idx] = ns
				n++
			}
		}
	}
	w.mu.Unlock()
	if n == 0 {
		return 0, nil
	}
	w.dirty.Store(true)
	return n, w.Save()
}

// ForEachEdit visits every persisted block edit (world coordinates + state).
// Used at boot to rebuild derived indexes (e.g. the lightning-rod set) that
// are otherwise only maintained incrementally as blocks change.
func (w *World) ForEachEdit(fn func(x, y, z int, state uint32)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	for key, blocks := range w.edits {
		for idx, state := range blocks {
			lx, y, lz := splitIndex(idx)
			fn(int(key[0])*16+lx, y, int(key[1])*16+lz, state)
		}
	}
}

// generated returns the cached, pre-edit generator output for a chunk, building
// and caching it on a miss. The returned chunk is shared and must NOT be mutated
// by callers — apply edits onto a copy (see Chunk) or read through an overlay.
func (w *World) generated(cx, cz int32) *worldgen.Chunk {
	key := chunkPos{cx, cz}

	w.genMu.Lock()
	if e, ok := w.cache[key]; ok {
		w.lru.MoveToFront(e.elem)
		w.genMu.Unlock()
		return e.ch
	}
	w.genMu.Unlock()

	// Persistent cache first: a hit skips noise/caves/features entirely. On a
	// miss (or decode failure — the cache is never trusted), generate and queue
	// an async write-back.
	var ch *worldgen.Chunk
	if w.chunkCache != nil {
		if data, ok := w.chunkCache.Get(w.cacheKey(cx, cz)); ok {
			ch = decodeChunk(data, w.Sections())
		}
	}
	if ch == nil {
		ch = w.gen.GenerateChunk(cx, cz) // generate outside the lock
		if w.chunkCache != nil {
			select {
			case w.cachePuts <- cachePut{key: w.cacheKey(cx, cz), val: encodeChunk(ch)}:
			default: // queue full — it's a cache, dropping is fine
			}
		}
	}

	w.genMu.Lock()
	defer w.genMu.Unlock()
	if e, ok := w.cache[key]; ok { // lost a race; keep the existing entry
		return e.ch
	}
	w.cache[key] = cacheEntry{ch: ch, elem: w.lru.PushFront(key)}
	for len(w.cache) > w.cacheCap() {
		oldest := w.lru.Back()
		if oldest == nil {
			break
		}
		w.lru.Remove(oldest)
		delete(w.cache, oldest.Value.(chunkPos))
	}
	return ch
}

// editsCopy returns a snapshot of a chunk's edit overlay, or nil if it has none.
// A copy lets lighting read edits lock-free without racing SetBlock.
func (w *World) editsCopy(cx, cz int32) map[int]uint32 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	m := w.edits[chunkPos{cx, cz}]
	if len(m) == 0 {
		return nil
	}
	cp := make(map[int]uint32, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

// EditedBlock is one player edit in a chunk: its in-chunk position and state.
type EditedBlock struct {
	LX, Y, LZ int
	State     uint32
}

// EditedBlocks returns the player edits in a chunk, decoded to in-chunk positions.
// Block entities (chests, beds, signs) exist only as edits — the generator places
// none — so the server scans these to build the chunk packet's block-entity list.
func (w *World) EditedBlocks(cx, cz int32) []EditedBlock {
	w.mu.RLock()
	defer w.mu.RUnlock()
	m := w.edits[chunkPos{cx, cz}]
	if len(m) == 0 {
		return nil
	}
	out := make([]EditedBlock, 0, len(m))
	for idx, state := range m {
		lx, y, lz := splitIndex(idx)
		out = append(out, EditedBlock{lx, y, lz, state})
	}
	return out
}

// EditedChunks lists every chunk that holds player edits — the scan surface for
// boot-time sweeps (e.g. extinguishing furnaces orphaned by a restart).
func (w *World) EditedChunks() [][2]int32 {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([][2]int32, 0, len(w.edits))
	for pos := range w.edits {
		out = append(out, [2]int32{pos[0], pos[1]})
	}
	return out
}

// SurfaceY reports a safe spawn height at a column.
func (w *World) SurfaceY(x, z int) float64 { return w.gen.SurfaceY(x, z) }

// Gen exposes the terrain generator (pure queries: dungeons, heights).
func (w *World) Gen() *worldgen.Generator { return w.gen }

// Seed is the world's generation seed.
func (w *World) Seed() int64 { return w.seed }

// NearestEdited finds the closest edited block within a horizontal radius of
// (x,z) whose state satisfies pred. Portals and other placed structures exist
// only as edits, so this scans the (small) edit overlay rather than raw
// terrain — a 128-block search costs a few map lookups per edited chunk.
func (w *World) NearestEdited(x, y, z, radius int, pred func(uint32) bool) (int, int, int, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	bestD := radius*radius + 1
	var bx, by, bz int
	found := false
	minCX, maxCX := (x-radius)>>4, (x+radius)>>4
	minCZ, maxCZ := (z-radius)>>4, (z+radius)>>4
	for cp, m := range w.edits {
		if int(cp[0]) < minCX || int(cp[0]) > maxCX || int(cp[1]) < minCZ || int(cp[1]) > maxCZ {
			continue
		}
		for idx, state := range m {
			if !pred(state) {
				continue
			}
			ly := idx/256 + worldgen.MinY
			lz := (idx % 256) / 16
			lx := idx % 16
			wx, wz := int(cp[0])*16+lx, int(cp[1])*16+lz
			d := (wx-x)*(wx-x) + (wz-z)*(wz-z)
			if d < bestD {
				bestD, bx, by, bz, found = d, wx, ly, wz, true
			}
		}
	}
	return bx, by, bz, found
}

// BiomeAt reports the biome identifier at a world column.
func (w *World) BiomeAt(x, z int) string { return w.gen.BiomeName(x, z) }

// GroundY is the terrain surface height (top of the solid ground, where a land
// mob's feet rest) — unlike SurfaceY it is NOT clamped to sea level, so over
// oceans it's the seafloor, not the water surface.
func (w *World) GroundY(x, z int) int { return w.gen.Height(x, z) }

// IsLand reports whether a column's ground stands above sea level (so land mobs
// can tell water apart from walkable ground).
func (w *World) IsLand(x, z int) bool { return w.gen.Height(x, z) > worldgen.SeaLevel }

// Walkable reports whether a land mob can step into a column: dry land with no
// tree trunk rooted in it (so cows walk around trees instead of through them).
func (w *World) Walkable(x, z int) bool {
	if w.noSky {
		// Cavern worlds have no sea level or trees: a column is walkable when
		// its mob floor is a real standable surface that isn't a lava bath.
		y := w.MobFeet(x, z)
		if !w.inBounds(y) || worldgen.IsLava(w.Block(x, y, z)) || worldgen.IsLava(w.Block(x, y-1, z)) {
			return false
		}
		return w.mobStandable(x, y-1, z)
	}
	// Test the actual floor, not the raw height: `Height > SeaLevel` froze
	// every mob standing at EXACTLY sea level (beaches, coastal flats — where
	// spawn often is), because no neighbouring column ever qualified. A column
	// is walkable when the mob's resting spot is dry: water columns have water
	// AT the feet (fluids aren't standable, so MobFeet bottoms out on the
	// seabed with water above), beaches have air at the feet and sand below.
	y := w.MobFeet(x, z)
	if !w.inBounds(y) || worldgen.IsWater(w.Block(x, y, z)) || worldgen.IsWater(w.Block(x, y-1, z)) {
		return false
	}
	return !w.gen.TreeAt(x, z)
}

// standable reports whether a block is solid enough for a mob to rest on top of.
// It uses blocks.json's bounding box (worldgen.Collides): full/partial-cube blocks
// hold a mob up; pass-through blocks (air, fluids, plants, torches, rails, signs,
// redstone) do not. Reads through the edit overlay.
func (w *World) standable(x, y, z int) bool {
	if !w.inBounds(y) {
		return false
	}
	return worldgen.Collides(w.Block(x, y, z))
}

// SurfaceFeet returns the world-y a mob's feet rest at in a column: one above the
// highest standable block. Unlike GroundY it consults the edit overlay, so a mob
// climbs onto a placed block and drops when the block under it is dug out. It is
// also robust to GroundY's exact convention: it corrects up or down to the real
// surface either way.
func (w *World) SurfaceFeet(x, z int) int {
	y := w.gen.Height(x, z) // start at the generated surface, then correct for edits
	for w.inBounds(y) && w.standable(x, y, z) {
		y++ // blocks were placed here — climb above them
	}
	for y > worldgen.MinY && !w.standable(x, y-1, z) {
		y-- // ground was dug out — drop to what we can stand on
	}
	return y
}

// DropY returns where a dropped item rests when it appears at (x, y, z): it
// falls from y to the first supporting block in the column. Underground drops
// stay on the tunnel floor where the block was mined — NOT teleported to the
// world surface (SurfaceFeet), which is only right for surface plants. Leaves
// don't support items: tree-chopping drops fall through the canopy to the
// ground instead of collecting on top of the tree, out of pickup reach.
func (w *World) DropY(x, y, z int) int {
	for y > worldgen.MinY {
		below := w.Block(x, y-1, z)
		if worldgen.Collides(below) && !worldgen.IsLeaves(below) {
			break
		}
		y--
	}
	return y
}

// TallObstacle reports whether the block a mob would stand on in this column is a
// fence, fence gate or wall. These have a 1.5-block collision height in vanilla,
// so a non-flying mob cannot step or climb over a single one — it must go around.
// Reads through the edit overlay, so player-placed fences pen mobs in. Because a
// mob never stands on a fence (MobFeet excludes fence-tops), the fence sits in the
// mob's own feet cell — that's the cell to test, not the block below it.
func (w *World) TallObstacle(x, z int) bool {
	s := w.Block(x, w.MobFeet(x, z), z)
	// A closed door is a wall to mobs regardless of geometry (doors are never
	// floors, so MobFeet always lands AT the door block); open doors pass.
	return worldgen.IsTallCollision(s) || worldgen.IsClosedDoor(s)
}

// ClosedWoodenDoorFeet reports whether the block at a mob's feet in this column
// is a closed wooden door — the cell a door-using mob (villager) may plan
// through and open. Iron/copper doors are excluded (mobs can't operate them).
func (w *World) ClosedWoodenDoorFeet(x, z int) bool {
	s := w.Block(x, w.MobFeet(x, z), z)
	return worldgen.IsClosedDoor(s) && worldgen.IsWoodenDoor(s)
}

// mobStandable is standable for a walking mob: a fence/wall/fence-gate is NOT a
// floor (it blocks passage, it doesn't hold a mob up), so a mob is never perched
// on top of one — placing a fence on a mob leaves it at the fence's base, free to
// walk out, instead of teleporting it onto the fence where it would be stuck.
func (w *World) mobStandable(x, y, z int) bool {
	if !w.inBounds(y) {
		return false
	}
	s := w.Block(x, y, z)
	return worldgen.Collides(s) && !worldgen.IsTallCollision(s) && !worldgen.IsDoor(s) &&
		!worldgen.IsThinFloor(s) // a carpet is not a floor a block high — mobs stand at its cell
}

// MobFeet is SurfaceFeet for a walking mob: the resting y one above the highest
// block it can actually stand on, treating fences/walls/gates as non-floors. Used
// for mob seating + step tests so a mob is never lifted onto a fence.
func (w *World) MobFeet(x, z int) int {
	y := w.gen.Height(x, z)
	limit := w.Ceiling()
	if w.noSky {
		// Cavern worlds: a solid start must never climb out through the
		// ceiling shell — that parks mobs floating in the void above it.
		limit = y + 8
	}
	for w.inBounds(y) && y < limit && w.mobStandable(x, y, z) {
		y++
	}
	for y > worldgen.MinY && !w.mobStandable(x, y-1, z) {
		y--
	}
	return y
}

// MobFeetFrom is MobFeet relative to a mob's CURRENT height instead of the
// column surface: climb out if a block was placed into the mob (bounded — a
// buried mob stays put rather than teleporting out through a mountain), then
// descend to the nearest floor below. This is what lets cave mobs exist:
// seating against MobFeet hoisted every underground mob to the surface.
func (w *World) MobFeetFrom(x, z, yHint int) int {
	y := yHint
	for climbed := 0; w.inBounds(y) && w.mobStandable(x, y, z); climbed++ {
		if climbed >= 8 {
			return yHint // fully buried — stay (suffocation is the caller's business)
		}
		y++
	}
	for y > worldgen.MinY && !w.mobStandable(x, y-1, z) {
		y--
	}
	return y
}

// Spawnable reports whether a mob may spawn standing in this column: dry land with
// clear body space over a legal floor — never on or inside a fence/wall/gate or
// under an overhang. Edit-overlay aware, so player-built hazards are rejected.
// Movement uses Walkable+TallObstacle; spawning is stricter (the standing spot
// itself must be legal, not merely reachable).
func (w *World) Spawnable(x, z int) bool {
	if !w.Walkable(x, z) {
		return false
	}
	feet := w.MobFeet(x, z)
	return !worldgen.Collides(w.Block(x, feet, z)) && !worldgen.Collides(w.Block(x, feet+1, z))
}

func (w *World) inBounds(y int) bool {
	return y >= worldgen.MinY && y < w.Ceiling()
}

// localIndex packs an in-chunk (lx, y, lz) into a single index.
func localIndex(lx, y, lz int) int { return (y-worldgen.MinY)*256 + lz*16 + lx }

func splitIndex(idx int) (lx, y, lz int) {
	y = idx/256 + worldgen.MinY
	rem := idx % 256
	return rem % 16, y, rem / 16
}

func chunkOf(x, z int) (cx, cz, lx, lz int) {
	cx, cz = floorDiv(x, 16), floorDiv(z, 16)
	return cx, cz, x - cx*16, z - cz*16
}

// SetBlock records a persistent edit at a world coordinate.
func (w *World) SetBlock(x, y, z int, state uint32) {
	if !w.inBounds(y) {
		return
	}
	cx, cz, lx, lz := chunkOf(x, z)
	idx := localIndex(lx, y, lz)
	key := chunkPos{int32(cx), int32(cz)}

	w.mu.Lock()
	m := w.edits[key]
	if m == nil {
		m = make(map[int]uint32)
		w.edits[key] = m
	}
	m[idx] = state
	w.mu.Unlock()
	w.dirty.Store(true)
	w.invalidateLight(x, z) // cached chunk light is stale for this 3×3
}

// Block returns the block state at a world coordinate: an edit if one exists,
// otherwise the generated block.
// Block reads one block: edits first, then the cached generated chunk. It MUST
// see the same world the chunk packets carry — including decorations (trees,
// grass, flowers), which only exist in generated chunks. It previously fell back
// to gen.BlockAt (terrain+caves only, no features), which made the server treat
// every generated tree as air: punching a trunk "broke" hardness-0 air instantly
// and dropped nothing, while the client plainly saw a log. One source of truth.
func (w *World) Block(x, y, z int) uint32 {
	return w.At(x, y, z)
}

// At returns the block at a world coord using the generated-chunk CACHE (no
// per-call terrain noise) plus the edit overlay — cheap enough for the random
// ticker, which reads thousands of blocks per tick. Unlike Block it sees feature
// decoration (it reads the assembled chunk), which is what growth wants.
func (w *World) At(x, y, z int) uint32 {
	if !w.inBounds(y) {
		return worldgen.Air
	}
	cx, cz, lx, lz := chunkOf(x, z)
	w.mu.RLock()
	if m := w.edits[chunkPos{int32(cx), int32(cz)}]; m != nil {
		if s, ok := m[localIndex(lx, y, lz)]; ok {
			w.mu.RUnlock()
			return s
		}
	}
	w.mu.RUnlock()

	ch := w.generated(int32(cx), int32(cz)) // shared cached chunk, no copy
	sec := (y - worldgen.MinY) / 16
	ly := (y - worldgen.MinY) % 16
	return ch.Sections[sec][(ly*16+lz)*16+lx]
}

// Chunk returns a fresh chunk: a copy of the cached generated base with any
// persistent edits applied on top. The copy keeps the shared cached chunk
// immutable.
func (w *World) Chunk(cx, cz int32) *worldgen.Chunk {
	base := w.generated(cx, cz)
	ch := new(worldgen.Chunk)
	*ch = *base // copy the generated terrain so edits don't touch the cache

	w.mu.RLock()
	var touched [256]bool
	edited := false
	for idx, state := range w.edits[chunkPos{cx, cz}] {
		lx, y, lz := splitIndex(idx)
		sec := (y - worldgen.MinY) / 16
		ly := (y - worldgen.MinY) % 16
		ch.Sections[sec][(ly*16+lz)*16+lx] = state
		touched[lz*16+lx] = true
		edited = true
	}
	w.mu.RUnlock()
	if edited {
		// keep the client-facing heightmap honest about player builds — the
		// client gates precipitation rendering on it, so without this rain
		// and snow fall through built roofs on freshly loaded chunks
		ch.RecomputeHeightmapColumns(&touched)
	}
	return ch
}

// floorDiv divides rounding toward negative infinity (for negative coords).
func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}
