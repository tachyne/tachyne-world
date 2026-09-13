package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A fish on dry stone lasts its fifteen seconds of air, then takes two a
// second; an axolotl five minutes; a dolphin two minutes of moisture and
// then dies; none of them come to harm in water.
func TestWaterAnimalsDryOut(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	w.SetBlock(3, 180, 3, worldgen.Water)
	secondsToFirstHurt := func(etype int) int {
		m := h.spawnMob(players, etype, 0.5, 180, 0.5)
		if m == nil {
			t.Fatalf("spawn %d returned nil", etype)
		}
		m.spawnInvuln = 0
		hp := m.health
		for s := 1; s <= 400; s++ {
			h.waterAnimalDry(players, m)
			if m.health < hp {
				return s
			}
		}
		return -1
	}
	if got := secondsToFirstHurt(entityCod); got != 16 {
		t.Errorf("cod first hurt after %d s, want 16", got)
	}
	if got := secondsToFirstHurt(entityAxolotl); got != 301 {
		t.Errorf("axolotl first hurt after %d s, want 301", got)
	}
	if got := secondsToFirstHurt(entityDolphin); got != 121 {
		t.Errorf("dolphin first hurt after %d s, want 121", got)
	}
	// In water: nothing.
	fish := h.spawnMob(players, entityCod, 3.5, 180, 3.5)
	fish.spawnInvuln = 0
	for s := 0; s < 40; s++ {
		h.waterAnimalDry(players, fish)
	}
	if fish.health != speciesOf(entityCod).health {
		t.Errorf("a fish in water lost health: %v", fish.health)
	}
	// A dolphin has four minutes of air under water, a zombie fifteen seconds.
	if mobMaxAir(fish) != maxAir || mobMaxAir(h.spawnMob(players, entityDolphin, 3.5, 180, 3.5)) != dolphinMaxAir {
		t.Error("air caps")
	}
}

// A dolphin low on air makes for the surface.
func TestDolphinBreathesAtSurface(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 186; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	d := h.spawnMob(players, entityDolphin, 0.5, 180.5, 0.5)
	if d == nil {
		t.Fatal("dolphin spawn returned nil")
	}
	if h.dolphinBreathe(players, d) {
		t.Fatal("a dolphin with full air surfaced")
	}
	d.submerged = (dolphinMaxAir - 100) / envSecondTicks // 100 ticks of air left
	for i := 0; i < 60; i++ {
		if !h.dolphinBreathe(players, d) {
			t.Fatal("the goal gave up")
		}
		nx, nz := d.x+d.vx, d.z+d.vz
		h.swimMove(d, nx, nz, int(math.Floor(nx)), int(math.Floor(nz)))
	}
	if math.Floor(d.y) != 186 {
		t.Errorf("dolphin at y=%.2f, want in the top water cell (186)", d.y)
	}
}
