package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestRaidWavesAndVictory(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	pl := testTracked()
	lx, lz := h.findLand(120, 120) // a spawnable spot so raiders can appear
	center := blockPos{lx, h.world.SurfaceFeet(lx, lz), lz}
	pl.x, pl.y, pl.z = float64(center.x), float64(center.y), float64(center.z)
	players[pl.p.eid] = pl
	h.rules.Difficulty = diffNormal

	h.startRaid(players, center)
	r := h.raids[center]
	if r == nil || r.numGroups != 5 {
		t.Fatalf("normal raid should have 5 waves, got %+v", r)
	}
	if len(r.alive) == 0 {
		t.Fatal("wave 1 should have spawned raiders")
	}
	// Clear every wave by killing all its raiders; the raid should end in victory.
	for i := 0; i < 20 && h.raids[center] != nil; i++ {
		for eid := range h.raids[center].alive {
			delete(h.mobs, eid) // simulate the raiders dying
		}
		h.updateRaids(players)
	}
	if h.raids[center] != nil {
		t.Fatalf("raid should have ended after clearing all %d waves", r.numGroups)
	}
}

func TestBadOmenTriggersRaidNearVillage(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	// Put the player ON a real generated village so villageNear resolves.
	gen := h.world.Gen()
	var v = gen.VillageIn(0, 0)
	if !v.Exists {
		t.Skip("no village near origin in this seed")
	}
	pl.x, pl.y, pl.z = float64(v.X), float64(v.Y), float64(v.Z)
	h.applyEffect(players, pl, effBadOmen, 0, 6000)
	h.checkRaidTrigger(players, pl)
	if pl.hasEffect(effBadOmen) != 0 {
		t.Fatal("reaching a village should consume Bad Omen")
	}
	if len(h.raids) == 0 {
		t.Fatal("Bad Omen at a village should start a raid")
	}
}
