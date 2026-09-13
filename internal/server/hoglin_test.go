package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestHoglinZombifiesAndFleesFungus: a hoglin in the overworld turns into a
// nauseous zoglin after three hundred ticks; one near warped fungus is
// pacified and walks away from it; the bite rolls half-plus damage.
func TestHoglinZombifiesAndFleesFungus(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	hog := h.spawnMob(players, entityHoglin, 0.5, 180, 0.5)
	hog.baby = false
	for i := 0; i < 20; i++ {
		d := h.hoglinBiteDamage(hog)
		if d < 3 || d > 8 {
			t.Fatalf("an adult's bite is 3..8 for damage 6: %.1f", d)
		}
	}
	w.SetBlock(3, 180, 0, worldgen.BlockBase("warped_fungus"))
	h.tick.Store(20)
	hog.hasTarget = true
	if !h.hoglinStep(players, hog) || hog.hasTarget || hog.hogPacified != hoglinPacifyTicks || hog.vx >= 0 {
		t.Fatalf("warped fungus pacifies it and sends it off: target %v pacified %d vx %.2f", hog.hasTarget, hog.hogPacified, hog.vx)
	}
	eid := hog.eid
	for i := 0; i < 160 && h.mobs[eid] != nil; i++ {
		h.zombifyTick(players, hog)
	}
	if h.mobs[eid] != nil {
		t.Fatal("three hundred ticks outside the Nether should zombify it")
	}
	var zog *mob
	for _, o := range h.mobs {
		if o.etype == entityZoglin {
			zog = o
		}
	}
	if zog == nil || zog.hasEffect(effNausea) == 0 {
		t.Fatalf("a nauseous zoglin should stand in its place: %v", zog != nil)
	}
	// In the Nether nothing happens; an immune hoglin is safe anywhere.
	hog2 := h.spawnMobIn(players, entityHoglin, 1, 0.5, 80, 0.5)
	if hog2 != nil {
		for i := 0; i < 200; i++ {
			h.zombifyTick(players, hog2)
		}
		if hog2.overworldTicks != 0 {
			t.Fatal("the Nether never converts")
		}
	}
	hog3 := h.spawnMob(players, entityHoglin, 0.5, 180, 0.5)
	hog3.immuneZombify = true
	for i := 0; i < 200; i++ {
		h.zombifyTick(players, hog3)
	}
	if h.mobs[hog3.eid] == nil {
		t.Fatal("an immune hoglin stays a hoglin")
	}
}
