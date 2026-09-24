package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// spitRig is a llama and a survival player twelve blocks apart on a stone
// floor at y=179, in open air.
func spitRig(t *testing.T) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	h.arrows = map[int32]*arrowEntity{}
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 16; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
			for y := 180; y < 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 12.5
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnMobIn(players, entityLlama, 0, 0.5, 180, 0.5)
	m.hostile = true
	return h, players, pl, m
}

// Llama.spit: the gob leaves from in front of the mouth and is shot with
// uncertainty 10, so no two spits fly quite the same line.
func TestLlamaSpitLeavesTheMouthWithScatter(t *testing.T) {
	h, players, _, m := spitRig(t)
	dirs := map[[3]float64]bool{}
	reach := (m.box().w + 1) * 0.5
	for i := 0; i < 20; i++ {
		h.arrows = map[int32]*arrowEntity{}
		m.attackCD = 0
		h.llamaSpit(players, m)
		a := onlyProjectile(t, h)
		if math.Abs(a.z-(m.z+reach)) > 1e-6 || math.Abs(a.x-m.x) > 1e-6 {
			t.Fatalf("spit left from (%.3f, %.3f), want the mouth at (%.3f, %.3f)", a.x, a.z, m.x, m.z+reach)
		}
		if want := m.y + mobEyeHeight(m) - 0.1; math.Abs(a.y-want) > 1e-6 {
			t.Fatalf("spit left at y %.3f, want the eyes less 0.1 (%.3f)", a.y, want)
		}
		sp := math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz)
		if sp < 1.5*(1-0.3) || sp > 1.5*(1+0.3) {
			t.Fatalf("spit speed %.3f, want about 1.5", sp)
		}
		dirs[[3]float64{a.vx, a.vy, a.vz}] = true
	}
	if len(dirs) < 10 {
		t.Errorf("20 spits flew only %d distinct lines: no aim scatter", len(dirs))
	}
}

// LlamaSpit.tick: a gob that touches anything that is not air — water, a
// tuft of grass — is gone, and never reaches the player behind it.
func TestLlamaSpitVanishesInWaterAndGrass(t *testing.T) {
	for _, block := range []string{"water", "short_grass"} {
		t.Run(block, func(t *testing.T) {
			h, players, pl, m := spitRig(t)
			for y := 180; y < 184; y++ {
				h.world.SetBlock(0, y, 6, worldgen.BlockBase(block))
			}
			a := h.launchProjectileIn(players, entityLlamaSpit, 0, 0.5, 181.3, 1.5, 0, 0.05, 1.5)
			a.shooter, a.dmg, a.breaks, a.mobShot = m.eid, llamaSpitDmg, true, true
			for i := 0; i < 20 && len(h.arrows) > 0; i++ {
				h.tick.Add(1)
				h.updateArrows(players)
				if len(h.arrows) > 0 && a.z > 7.5 {
					t.Fatalf("the spit flew on through the %s to z %.2f", block, a.z)
				}
			}
			if len(h.arrows) != 0 {
				t.Fatalf("the spit is still flying")
			}
			if pl.health < 20 {
				t.Errorf("the spit reached the player through the %s", block)
			}
		})
	}
}
