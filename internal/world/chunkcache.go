package world

import (
	"encoding/binary"
	"fmt"

	"tachyne/internal/worldgen"
)

// ChunkCache persists generated (pre-edit) chunks so terrain isn't re-derived
// from noise on every restart or LRU eviction. It is a PURE CACHE, never
// authoritative: generation is deterministic by (seed, GenVersion), so a miss,
// a corrupt entry, or a dead backend just means "generate it again". Backends
// must therefore be failure-silent: return misses on error, drop writes.
type ChunkCache interface {
	Get(key string) ([]byte, bool)
	Put(key string, val []byte)
}

// cacheKey names a chunk uniquely across seeds and generator revisions.
func (w *World) cacheKey(cx, cz int32) string {
	dim := w.dimTag
	if dim == "" && w.noSky {
		dim = "n." // the nether shares the seed — keep its chunks distinct
	}
	return fmt.Sprintf("%sv%d.s%d.%d.%d", dim, worldgen.GenVersion, w.seed, cx, cz)
}

// SetChunkCache attaches a persistent chunk cache. Puts are asynchronous (a
// single writer goroutine drains a bounded queue, dropping when full) so the
// generate path never blocks on storage.
func (w *World) SetChunkCache(c ChunkCache) {
	w.chunkCache = c
	w.cachePuts = make(chan cachePut, 256)
	go func() {
		for p := range w.cachePuts {
			c.Put(p.key, p.val)
		}
	}()
}

type cachePut struct {
	key string
	val []byte
}

// ---- chunk (de)serialization ----------------------------------------------
//
// Format (little ceremony, decode-validated): magic byte 0xC1, then for each
// of the SectionCount sections: RLE runs of (varint length, varint state)
// summing to exactly 4096, then the section biome name (u8 len + bytes);
// finally the 256-entry heightmap as i16 pairs. Terrain is extremely runny
// (air/stone/deepslate), so RLE alone shrinks ~400 KB of sections to a few KB
// without spending CPU on real compression.

const chunkMagic = 0xC1

func encodeChunk(ch *worldgen.Chunk) []byte {
	buf := make([]byte, 0, 16*1024)
	buf = append(buf, chunkMagic)
	var tmp [binary.MaxVarintLen64]byte
	putUv := func(v uint64) {
		n := binary.PutUvarint(tmp[:], v)
		buf = append(buf, tmp[:n]...)
	}
	for s := range ch.Sections {
		sec := &ch.Sections[s]
		for i := 0; i < len(sec); {
			run := 1
			for i+run < len(sec) && sec[i+run] == sec[i] {
				run++
			}
			putUv(uint64(run))
			putUv(uint64(sec[i]))
			i += run
		}
		b := ch.Biomes[s]
		buf = append(buf, byte(len(b)))
		buf = append(buf, b...)
	}
	for _, h := range ch.Heightmap {
		buf = append(buf, byte(h), byte(h>>8))
	}
	return buf
}

// decodeChunk rebuilds a chunk, returning nil on ANY malformed input (the
// caller treats that as a miss and regenerates).
func decodeChunk(data []byte) *worldgen.Chunk {
	if len(data) < 1 || data[0] != chunkMagic {
		return nil
	}
	pos := 1
	getUv := func() (uint64, bool) {
		v, n := binary.Uvarint(data[pos:])
		if n <= 0 {
			return 0, false
		}
		pos += n
		return v, true
	}
	ch := &worldgen.Chunk{}
	for s := range ch.Sections {
		sec := &ch.Sections[s]
		filled := 0
		for filled < len(sec) {
			run, ok := getUv()
			if !ok || run == 0 || filled+int(run) > len(sec) {
				return nil
			}
			state, ok := getUv()
			if !ok {
				return nil
			}
			for i := 0; i < int(run); i++ {
				sec[filled+i] = uint32(state)
			}
			filled += int(run)
		}
		if pos >= len(data) {
			return nil
		}
		bl := int(data[pos])
		pos++
		if pos+bl > len(data) {
			return nil
		}
		ch.Biomes[s] = string(data[pos : pos+bl])
		pos += bl
	}
	if pos+len(ch.Heightmap)*2 > len(data) {
		return nil
	}
	for i := range ch.Heightmap {
		ch.Heightmap[i] = int16(uint16(data[pos]) | uint16(data[pos+1])<<8)
		pos += 2
	}
	return ch
}
