package world

import "testing"

func BenchmarkChunkFirstTouch(b *testing.B) {
	// End-to-end cost of a brand-new chunk as the server pays it: generation
	// (through the cache) + the 3x3-neighbourhood light computation.
	w := New(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		cx, cz := int32(i%512), int32(i/512+1000)
		w.Light(cx, cz)
	}
}

func BenchmarkChunkFirstTouchWarmDiskCache(b *testing.B) {
	// Same as ChunkFirstTouch, but the persistent chunk cache already holds
	// every chunk (as after a restart): generation cost should vanish, leaving
	// decode + lighting.
	dir := b.TempDir()
	cache := NewDirCache(dir)
	w := New(1)
	g := w.gen
	for i := 0; i < 2200; i++ { // pre-populate beyond what the bench touches
		cx, cz := int32(i%512), int32(i/512+1000)
		for dx := int32(-1); dx <= 1; dx++ {
			for dz := int32(-1); dz <= 1; dz++ {
				key := w.cacheKey(cx+dx, cz+dz)
				if _, ok := cache.Get(key); !ok {
					cache.Put(key, encodeChunk(g.GenerateChunk(cx+dx, cz+dz)))
				}
			}
		}
	}
	w2 := New(1)
	w2.SetChunkCache(NewDirCache(dir))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N && i < 2200; i++ {
		cx, cz := int32(i%512), int32(i/512+1000)
		w2.Light(cx, cz)
	}
}
