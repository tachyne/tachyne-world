package server

import "testing"

// TestSpawnCapMatchesVanilla — one player's ±8 spawn ring is 17×17 = 289
// chunks, so spawnCap must yield each category's exact maxInstancesPerChunk
// (vanilla canSpawnForCategoryGlobal): creatures 10, monsters 70, etc. The old
// bug fed the radius-12 view window (~625 chunks) here, doubling every cap.
func TestSpawnCapMatchesVanilla(t *testing.T) {
	onePlayerRing := (2*spawnDistChunks + 1) * (2*spawnDistChunks + 1) // 289
	if onePlayerRing != spawnChunkArea {
		t.Fatalf("one player's spawn ring (%d) must equal the magic number (%d)", onePlayerRing, spawnChunkArea)
	}
	for cat, want := range categoryCap {
		if got := spawnCap(cat, onePlayerRing); got != want {
			t.Errorf("cat %d: one-player cap %d, want vanilla maxInstancesPerChunk %d", cat, got, want)
		}
	}
	// Two players' worth of spawn ring → double the cap.
	if got := spawnCap(catCreature, 2*spawnChunkArea); got != 2*categoryCap[catCreature] {
		t.Errorf("two-player creature cap %d, want %d", got, 2*categoryCap[catCreature])
	}
}
