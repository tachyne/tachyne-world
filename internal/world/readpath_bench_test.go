package world

import "testing"

// The hot read path: one cached chunk, blocks read at random within it — the
// shape of the random ticker and of every mob step.
func BenchmarkAtCached(b *testing.B) {
	w := New(1)
	w.generated(0, 0)
	w.SetBlock(3, 70, 3, 1) // one edit so the overlay path is exercised too
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.At(i&15, 60+(i&31), (i>>4)&15)
	}
}

func BenchmarkAtCachedParallel(b *testing.B) {
	w := New(1)
	w.generated(0, 0)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			w.At(i&15, 60+(i&31), (i>>4)&15)
			i++
		}
	})
}

func BenchmarkReaderAt(b *testing.B) {
	w := New(1)
	w.SetBlock(3, 70, 3, 1)
	rd := w.Reader(0, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rd.At(i&15, 60+(i&31), (i>>4)&15)
	}
}
