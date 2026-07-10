package worldgen

import "testing"

func BenchmarkGenerateChunk(b *testing.B) {
	g := NewGenerator(1)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		// Walk fresh coordinates so nothing is memoized between iterations.
		g.GenerateChunk(int32(i%512), int32(i/512+100))
	}
}
