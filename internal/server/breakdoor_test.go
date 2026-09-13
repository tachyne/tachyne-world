package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestZombieBreaksDoor: on hard, a door-breaking zombie stopped by a closed
// wooden door beats it down in two hundred and forty ticks; on normal it
// leaves it alone.
func TestZombieBreaksDoor(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.rules.Difficulty = diffHard
	w := h.worldFor(0)
	door := worldgen.BlockBase("oak_door")
	info, _ := worldgen.InfoForState(door)
	lower := worldgen.SetProperty(info, door, "half", "lower")
	lower = worldgen.SetProperty(info, lower, "open", "false")
	lower = worldgen.SetProperty(info, lower, "powered", "false")
	upper := worldgen.SetProperty(info, lower, "half", "upper")
	w.SetBlock(1, 179, 0, worldgen.Stone)
	w.SetBlock(1, 180, 0, lower)
	w.SetBlock(1, 181, 0, upper)
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	z.breaksDoors, z.hasTarget = true, true
	pos, ok := h.doorAhead(z, 1.5, 0.5)
	if !ok || pos != (blockPos{1, 180, 0}) {
		t.Fatalf("door ahead: %v %+v", ok, pos)
	}
	for i := 0; i < 130 && worldgen.IsDoor(w.At(1, 180, 0)); i++ {
		if !h.zombieBeatsDoor(players, z, pos) && worldgen.IsDoor(w.At(1, 180, 0)) {
			t.Fatalf("the zombie should keep beating: tick %d", z.doorTicks)
		}
	}
	if worldgen.IsDoor(w.At(1, 180, 0)) || w.At(1, 181, 0) != worldgen.Air {
		t.Fatalf("the door should be gone: %d / %d", w.At(1, 180, 0), w.At(1, 181, 0))
	}
	if z.doorPos != (blockPos{}) {
		t.Fatal("the door is forgotten once broken")
	}
	// Normal difficulty: no beating.
	w.SetBlock(1, 180, 0, lower)
	w.SetBlock(1, 181, 0, upper)
	h.rules.Difficulty = diffNormal
	if h.zombieBeatsDoor(players, z, pos) {
		t.Fatal("doors only break on hard")
	}
}
