package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestGoatRams: a goat off its cooldown walks out from a player, lowers its
// head, charges, and the hit lands with damage and a shove; a charge into
// stone snaps a horn off instead.
func TestGoatRams(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 180, z, worldgen.Air)
			w.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	g := h.spawnAnimal(players, entityGoat, 0, 0)
	g.x, g.y, g.z = 0.5, 180, 0.5
	g.ramCD = 0
	pl.x, pl.y, pl.z = 3.5, 180, 0.5
	if !h.goatStep(players, g) || g.ramPhase != goatWalking {
		t.Fatalf("the goat should pick the player and walk to its start: phase %d", g.ramPhase)
	}
	// Teleport to the start and let it prepare.
	g.x, g.z = float64(g.ramStart.x)+0.5, float64(g.ramStart.z)+0.5
	h.goatStep(players, g)
	if g.ramPhase != goatPreparing {
		t.Fatalf("at its start it prepares: phase %d", g.ramPhase)
	}
	for i := 0; i < 12 && g.ramPhase == goatPreparing; i++ {
		h.goatStep(players, g)
	}
	if g.ramPhase != goatCharging {
		t.Fatalf("after twenty ticks it charges: phase %d", g.ramPhase)
	}
	// Put the player in its path and let the charge land.
	pl.x, pl.z = g.x+g.ramDX*0.5, g.z+g.ramDZ*0.5
	pl.health = 20
	h.goatStep(players, g)
	if pl.health >= 20 || g.ramPhase != goatIdle || g.ramCD < 600 {
		t.Fatalf("the ram should land and start the cooldown: health %v phase %d cd %d", pl.health, g.ramPhase, g.ramCD)
	}
	// A charge into stone snaps a horn.
	g.ramCD, g.ramPhase, g.hornsGone = 0, goatCharging, 0
	g.ramDX, g.ramDZ = 1, 0
	g.x, g.z = 5.5, 5.5
	w.SetBlock(6, 180, 5, worldgen.Stone)
	before := len(h.items)
	h.goatStep(players, g)
	if g.hornsGone != 1 || len(h.items) != before+1 {
		t.Fatalf("stone should snap a horn: gone %d items %d→%d", g.hornsGone, before, len(h.items))
	}
	for _, it := range h.items {
		if it.item == itemGoatHorn && (it.instrument < 0 || it.instrument > 3) {
			t.Fatalf("a plain goat's horn is a plain instrument: %d", it.instrument)
		}
	}
	// A screaming goat drops the screaming instruments and rams more often.
	g.screaming, g.hornsGone, g.ramPhase, g.ramCD = true, 1, goatCharging, 0
	g.x, g.z = 5.5, 5.5
	h.goatStep(players, g)
	if g.hornsGone != 2 || g.ramCD > 300 {
		t.Fatalf("screaming goat: gone %d cd %d", g.hornsGone, g.ramCD)
	}
	if h.goatDropHorn(players, g) {
		t.Fatal("no horns left to drop")
	}
}
