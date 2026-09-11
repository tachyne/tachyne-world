package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Suspicious sand, the dragon egg and anvils fall like sand; an anvil
// dropping three cells onto a mob hurts it by vanilla's rule.
func TestNewFallingBlocksFall(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	for i, st := range []uint32{worldgen.SuspiciousSand, worldgen.DragonEgg, worldgen.BlockBase("anvil")} {
		px := x + i*2
		w.SetBlock(px, y-1, z, worldgen.Air) // a hole under the pad
		w.SetBlock(px, y-2, z, worldgen.Stone)
		w.SetBlock(px, y, z, st)
		h.schedule(blockPos{px, y, z}, 1)
		stepTicks(h, players, 4)
		if w.At(px, y, z) != worldgen.Air || w.At(px, y-1, z) != st {
			t.Errorf("state %d did not fall: %d / %d", st, w.At(px, y, z), w.At(px, y-1, z))
		}
	}
	if len(h.fallDist) != 0 {
		t.Errorf("fall records left: %v", h.fallDist)
	}
	// An anvil from three up lands on a cow.
	ax := x + 7
	w.SetBlock(ax, y-1, z, worldgen.Stone)
	for dy := 0; dy < 4; dy++ {
		w.SetBlock(ax, y+dy, z, worldgen.Air)
	}
	cow := h.spawnMob(players, entityCow, float64(ax)+0.5, float64(y), float64(z)+0.5)
	hp := cow.health
	w.SetBlock(ax, y+3, z, worldgen.BlockBase("anvil"))
	h.schedule(blockPos{ax, y + 3, z}, 1)
	stepTicks(h, players, 8)
	if !worldgen.IsAnvil(w.At(ax, y, z)) && w.At(ax, y, z) != worldgen.Air {
		t.Fatalf("anvil should land on the cow's cell, holds %d", w.At(ax, y, z))
	}
	if want := anvilFallDamage(3); float64(hp)-float64(cow.health) < want-0.01 {
		t.Errorf("cow lost %v, want at least %v", float64(hp)-float64(cow.health), want)
	}
}

func TestAnvilFallRules(t *testing.T) {
	if d := anvilFallDamage(1); d != 0 {
		t.Errorf("one cell hurts %v", d)
	}
	if d := anvilFallDamage(3); d != 4 {
		t.Errorf("three cells hurt %v, want 4", d)
	}
	if d := anvilFallDamage(60); d != 40 {
		t.Errorf("a long fall hurts %v, want the cap 40", d)
	}
	anvil := worldgen.BlockBase("anvil") + 2 // facing south
	if s := anvilAfterFall(anvil, 3, 0.5); s != anvil {
		t.Errorf("a roll above 15%% chipped it: %d", s)
	}
	chipped := anvilAfterFall(anvil, 3, 0.1)
	if chipped != worldgen.BlockBase("chipped_anvil")+2 {
		t.Errorf("chipped state %d (facing kept?)", chipped)
	}
	damaged := anvilAfterFall(chipped, 3, 0.1)
	if damaged != worldgen.BlockBase("damaged_anvil")+2 {
		t.Errorf("damaged state %d", damaged)
	}
	if s := anvilAfterFall(damaged, 3, 0.1); s != 0 {
		t.Errorf("a worn-out anvil should break, got %d", s)
	}
	if s := anvilAfterFall(anvil, 1, 0.0); s != anvil {
		t.Error("a one-cell fall never wears the anvil")
	}
}
