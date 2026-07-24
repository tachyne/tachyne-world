// Package worldread is a stable, read-only public view of a tachyne world for
// out-of-process consumers — notably tachyne-map's renderer.
//
// The engine world is a pure function of (seed, edits), so a reader rebuilds
// any chunk from the seed plus an optional edit store, yielding block states,
// biomes, per-block sky/block light, and the heightmap — with no running
// engine and no write access. This is the ONLY surface tachyne-map imports; it
// deliberately exposes plain values (ints, strings, maps) and never leaks the
// engine's internal types.
//
// Sharding note: a reader is built from a seed and a world.Store, which is the
// same seam sharding swaps (gob FileStore today, a shared store later). The map
// therefore reads a world identically regardless of how many shards write it.
package worldread

import (
	"github.com/tachyne/tachyne-world/internal/anvil"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// MinY is the world floor: the world-Y of section 0, local block 0.
const MinY = worldgen.MinY

// Dim selects a dimension.
type Dim int

const (
	Overworld Dim = iota
	Nether
	End
)

// String returns the canonical dimension id used in tile paths.
func (d Dim) String() string {
	switch d {
	case Nether:
		return "nether"
	case End:
		return "end"
	default:
		return "overworld"
	}
}

// Reader is a read-only view of one dimension of a world. Safe for concurrent
// Chunk calls (the underlying world guards its own state).
type Reader struct {
	w   *world.World
	dim Dim
}

// Open builds a read-only reader for dim from a seed and an OPTIONAL edit store.
// Pass store=nil for terrain only (pure worldgen, no player edits), or
// world.NewFileStore(path) to include edits from a gob file (a missing file is
// treated as no edits). The store is only ever read — the reader never writes.
func Open(dim Dim, seed int64, store world.Store) (*Reader, error) {
	var (
		w   *world.World
		err error
	)
	switch dim {
	case Nether:
		w, err = world.NewNether(seed, store)
	case End:
		w, err = world.NewEnd(seed, store)
	default:
		w, err = world.NewWithStore(seed, store)
	}
	if err != nil {
		return nil, err
	}
	return &Reader{w: w, dim: dim}, nil
}

// OpenGob is Open with a gob FileStore at gobPath (a missing file yields
// terrain only).
func OpenGob(dim Dim, seed int64, gobPath string) (*Reader, error) {
	return Open(dim, seed, world.NewFileStore(gobPath))
}

// Dim reports the reader's dimension.
func (r *Reader) Dim() Dim { return r.dim }

// Sections is the world's column height in 16-block sections.
func (r *Reader) Sections() int { return r.w.Sections() }

// Ceiling is the exclusive top build limit (world-Y).
func (r *Reader) Ceiling() int { return r.w.Ceiling() }

// SetBlock applies a block change to this reader's own IN-MEMORY view of the
// world, so a live consumer (the map renderer) can follow the running engine by
// replaying its block-change events.
//
// It never writes to disk. Persistence happens only through the engine's Save
// path, which a Reader never calls — and a reader opened with a nil store has
// no store to save to at all. Cached light for the affected 3x3 chunk
// neighbourhood is dropped, so the next Chunk read reflects the change with
// correct lighting.
func (r *Reader) SetBlock(x, y, z int, state uint32) {
	r.w.SetBlock(x, y, z, state)
}

// Seed is the world seed this reader generates terrain from.
func (r *Reader) Seed() int64 { return r.w.Seed() }

// Chunk holds one chunk's render data.
//
// Section arrays are indexed YZX ((ly*16 + lz)*16 + lx) and run bottom-up from
// MinY; SkyLight/BlockLight share that layout. Biomes has one entry per
// section (index = section). Heightmap is indexed lz*16 + lx. In all
// accessors, ly is a 0-based index from the world floor (world-Y = MinY + ly),
// not an absolute world-Y.
type Chunk struct {
	CX, CZ     int
	Sections   [][4096]uint32
	SkyLight   [][4096]uint8
	BlockLight [][4096]uint8
	Biomes     []string
	Heightmap  [256]int16
}

// Chunk reads the render data for chunk (cx, cz), generating it from the seed
// and applying any edits. It is never nil for a valid coordinate.
func (r *Reader) Chunk(cx, cz int) *Chunk {
	ch := r.w.Chunk(int32(cx), int32(cz))
	ld := r.w.Light(int32(cx), int32(cz))
	return &Chunk{
		CX:         cx,
		CZ:         cz,
		Sections:   ch.Sections,
		SkyLight:   ld.Sky,
		BlockLight: ld.Block,
		Biomes:     ch.Biomes,
		Heightmap:  ch.Heightmap,
	}
}

// Height is the number of block layers in the column (sections × 16).
func (c *Chunk) Height() int { return len(c.Sections) * 16 }

// State returns the block-state id at local (lx, ly, lz), or 0 (air) if out of
// range. lx/lz are 0..15; ly is 0-based from the world floor.
func (c *Chunk) State(lx, ly, lz int) uint32 {
	if lx < 0 || lx > 15 || lz < 0 || lz > 15 || ly < 0 {
		return 0
	}
	sec := ly >> 4
	if sec >= len(c.Sections) {
		return 0
	}
	return c.Sections[sec][((ly&15)*16+lz)*16+lx]
}

// SkyLightAt returns the sky-light level (0..15) at local (lx, ly, lz).
func (c *Chunk) SkyLightAt(lx, ly, lz int) uint8 { return c.lightAt(c.SkyLight, lx, ly, lz) }

// BlockLightAt returns the block-light level (0..15) at local (lx, ly, lz).
func (c *Chunk) BlockLightAt(lx, ly, lz int) uint8 { return c.lightAt(c.BlockLight, lx, ly, lz) }

func (c *Chunk) lightAt(arr [][4096]uint8, lx, ly, lz int) uint8 {
	if arr == nil || lx < 0 || lx > 15 || lz < 0 || lz > 15 || ly < 0 {
		return 0
	}
	sec := ly >> 4
	if sec >= len(arr) {
		return 0
	}
	return arr[sec][((ly&15)*16+lz)*16+lx]
}

// Biome returns the biome name for the section containing ly.
func (c *Chunk) Biome(ly int) string {
	sec := ly >> 4
	if sec < 0 || sec >= len(c.Biomes) {
		return ""
	}
	return c.Biomes[sec]
}

// Decode resolves a canonical block-state id to its block name (e.g.
// "minecraft:oak_stairs") and property map. Unknown ids resolve to
// minecraft:air. This is the bridge from engine state ids to vanilla models.
func Decode(state uint32) (name string, props map[string]string) {
	return anvil.Decode(state)
}
