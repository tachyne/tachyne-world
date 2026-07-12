package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestNetherWorldIsDistinct(t *testing.T) {
	ow := world.New(7)
	nw, err := world.NewNether(7, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := newHub(ow)
	h.nether = nw
	if h.worldFor(0) != ow || h.worldFor(1) != nw {
		t.Fatal("worldFor must route by dimension")
	}
	// Nether terrain is netherrack country, not grass.
	found := false
	for y := -60; y < 120 && !found; y++ {
		if nw.At(8, y, 8) == worldgen.BlockBase("netherrack") {
			found = true
		}
	}
	if !found {
		t.Fatal("nether world should contain netherrack")
	}
}

func TestDimSwitchIsolation(t *testing.T) {
	ow := world.New(7)
	nw, _ := world.NewNether(7, nil)
	h := newHub(ow)
	h.nether = nw
	a, b := testTracked(), testTracked()
	b.p.eid = 2
	players := map[int32]*tracked{1: a, 2: b}
	// a goes to the nether.
	h.onDimSwitch(players, a, evDim{eid: 1, dim: 1, x: 10, y: 40, z: 10})
	if a.dim != 1 {
		t.Fatal("dim not applied")
	}
	// Block updates in the nether must not reach overworld players — and the
	// nether edit must land in the nether world.
	h.onBlock(players, evBlock{x: 10, y: 40, z: 10, dim: 1, state: 1, by: 1})
	// (isolation is by t.dim filter; b.dim==0 so b got nothing — verified by
	// the trySend going to a lossy test channel; here we assert the sim gate:)
	if len(h.pending) != 0 {
		t.Fatal("nether edits must not schedule overworld simulation")
	}
	// An overworld edit still schedules.
	h.onBlock(players, evBlock{x: 5, y: 64, z: 5, dim: 0, state: 1, by: 1})
	if len(h.pending) == 0 {
		t.Fatal("overworld edits should schedule simulation")
	}
}

func TestDeathInNetherRespawnsToOverworld(t *testing.T) {
	ow := world.New(7)
	nw, _ := world.NewNether(7, nil)
	h := newHub(ow)
	h.nether = nw
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.dim, pl.p.dim = 1, 1
	pl.dead = true
	h.respawn(pl)
	if pl.p.pendingDim.Load() != 0 || !pl.p.pendingDestOK {
		t.Fatalf("nether death must flag an overworld switch: dim=%d ok=%v",
			pl.p.pendingDim.Load(), pl.p.pendingDestOK)
	}
	// An overworld death doesn't need the machinery.
	pl2 := testTracked()
	pl2.p.eid = 2
	pl2.gamemode = gmSurvival
	pl2.dead = true
	h.respawn(pl2)
	if pl2.p.pendingDim.Load() != -1 {
		t.Fatal("overworld deaths must not trigger a dimension switch")
	}
}

func TestNetherMovementNotBlockedByOverworldTerrain(t *testing.T) {
	ow := world.New(7)
	nw, _ := world.NewNether(7, nil)
	h := newHub(ow)
	h.nether = nw
	// A spot solid in the overworld but open cavern in the nether.
	var x, z int
	found := false
	for x = 0; x < 400 && !found; x += 4 {
		for z = 0; z < 400 && !found; z += 4 {
			y, ok := nw.Gen().NetherFloorOK(x, z)
			if ok && fullCube(ow.At(x, y, z)) && fullCube(ow.At(x, y+1, z)) {
				found = true
				// Nether-dim checks must consult the NETHER world.
				if h.insideSolid(1, float64(x)+0.5, float64(y), float64(z)+0.5) {
					t.Fatalf("(%d,%d,%d): open nether cavern read as solid (overworld leak)", x, y, z)
				}
				if !h.insideSolid(0, float64(x)+0.5, float64(y), float64(z)+0.5) {
					t.Fatal("sanity: the overworld really is solid here")
				}
			}
		}
	}
	if !found {
		t.Skip("no overlapping solid/cavern column found")
	}
}
