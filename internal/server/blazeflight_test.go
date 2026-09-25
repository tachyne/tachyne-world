package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Blaze.customServerAiStep: a blaze lifts itself toward a target above it.
func TestBlazeRisesToAHigherTarget(t *testing.T) {
	h, players := mobBlocksHub()
	shaft(h.world, 0, 0, 180, 200, worldgen.Stone, worldgen.Air)
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z, pl.dim = 0.5, 196, 0.5, dimNether // worldFor falls back to the one test world
	players[1] = pl
	b := h.spawnHostileYIn(players, entityBlaze, dimNether, 0.5, 180, 0.5)
	peak := b.y
	runMobs(h, players, 40, func() {
		peak = max(peak, b.y)
	})
	if peak < 186 {
		t.Fatalf("the blaze should rise toward the player above it: peak y=%.2f", peak)
	}
}

// Blaze.aiStep: with nothing to rise to, a blaze sinks slowly instead of
// dropping.
func TestBlazeFallsSlowly(t *testing.T) {
	h, players := mobBlocksHub()
	shaft(h.world, 0, 0, 180, 200, worldgen.Stone, worldgen.Air)
	b := h.spawnHostileYIn(players, entityBlaze, dimNether, 0.5, 195, 0.5)
	b.y = 195
	runMobs(h, players, 1, nil)
	if b.y <= 180 {
		t.Fatalf("a blaze should not drop straight to the floor: y=%.2f", b.y)
	}
	runMobs(h, players, 200, nil)
	if b.y != 180 {
		t.Fatalf("the blaze should settle on the floor in the end: y=%.2f", b.y)
	}
	if b.health < blazeHealth {
		t.Fatalf("a slow fall of fifteen blocks should not hurt: health %v", b.health)
	}
}
