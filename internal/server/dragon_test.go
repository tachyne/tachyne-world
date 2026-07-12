package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func endHub(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(7))
	ew, _ := world.NewEnd(7, nil)
	h.end = ew
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.dim = 1 // will switch to 2
	players := map[int32]*tracked{1: pl}
	return h, pl, players
}

func TestDragonStagesOnFirstEndArrival(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	if h.dragon == nil {
		t.Fatal("the dragon should await the first End arrival")
	}
	if h.dragon.dim != 2 || h.dragon.health != dragonHealth {
		t.Fatalf("dragon misconfigured: dim=%d hp=%d", h.dragon.dim, h.dragon.health)
	}
	if len(h.crystals) != worldgen.EndPillars {
		t.Fatalf("want %d crystals, got %d", worldgen.EndPillars, len(h.crystals))
	}
	// Second arrival must not double-stage.
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	if len(h.crystals) != worldgen.EndPillars {
		t.Fatal("re-arrival duplicated the crystals")
	}
}

func TestCrystalsHealAndDie(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	h.dragon.health = 100
	h.tick.Store(20) // now%20==0 heal beat
	h.updateDragon(players)
	if h.dragon.health <= 100 {
		t.Fatal("living crystals should heal the dragon")
	}
	// Pop every crystal; healing stops.
	for eid := range h.crystals {
		if !h.hitCrystal(players, eid) {
			t.Fatal("crystal hit should register")
		}
	}
	if len(h.crystals) != 0 {
		t.Fatal("all crystals should be gone")
	}
	hp := h.dragon.health
	h.tick.Store(40)
	h.updateDragon(players)
	if h.dragon.health != hp {
		t.Fatal("no crystals — no healing")
	}
}

func TestDragonDeathOpensExitAndDropsElytra(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	m := h.dragon
	m.hitByPlayer = true
	h.killMob(players, m)
	m.dying = 1
	h.despawnMob(players, m)
	if h.dragon != nil || !h.rules.DragonDefeated {
		t.Fatal("defeat not recorded")
	}
	// Exit portal blocks exist near the origin.
	found := false
	for y := worldgen.EndSurfaceY - 2; y < worldgen.EndSurfaceY+10 && !found; y++ {
		if h.end.At(1, y, 0) == worldgen.EndPortalBlock {
			found = true
		}
	}
	if !found {
		t.Fatal("exit portal missing")
	}
	elytra := false
	for _, it := range h.items {
		if it.item == itemElytra && it.dim == 2 {
			elytra = true
		}
	}
	if !elytra {
		t.Fatal("the elytra should drop at the exit portal")
	}
	// A rejoin must not respawn the dragon.
	h.enterEnd(players, nil)
	if h.dragon != nil {
		t.Fatal("a defeated dragon stays dead")
	}
}

func TestDragonBossbarLifecycle(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	h.updateDragonBar(players)
	if !pl.bossBarOn {
		t.Fatal("End players should see the dragon bar")
	}
	// Leaving the End removes it.
	pl.dim = 0
	h.updateDragonBar(players)
	if pl.bossBarOn {
		t.Fatal("the bar must go when the player leaves the End")
	}
	// Dragon dead: no bar even in the End.
	pl.dim = 2
	h.dragon = nil
	h.updateDragonBar(players)
	if pl.bossBarOn {
		t.Fatal("no dragon — no bar")
	}
}
