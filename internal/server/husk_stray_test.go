package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestHuskBiteHunger(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}

	husk := h.spawnHostile(players, entityHusk, 0, 0)
	husk.x, husk.y, husk.z = 1.0, 64, 0.5 // within melee reach of the player
	husk.attackCD = 0
	h.mobMelee(players, husk)

	if pl.hasEffect(effHunger) == 0 {
		t.Error("a husk bite did not inflict Hunger")
	}

	// Peaceful husks never reach melee, so no hunger even if forced.
	pl2 := testTracked()
	pl2.gamemode, pl2.x, pl2.y, pl2.z = gmSurvival, 0.5, 64, 0.5
	h.rules.Difficulty = diffPeaceful
	if secs := 7 * int(h.rules.Difficulty); secs != 0 {
		t.Errorf("peaceful hunger seconds %d, want 0", secs)
	}
}

func TestStrayArrowSlowness(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}

	stray := h.spawnHostile(players, entityStray, 0, 0)
	stray.x, stray.y, stray.z = 6.0, 64, 0.5
	before := len(h.arrows)
	h.spawnArrow(players, stray, pl)
	if len(h.arrows) != before+1 {
		t.Fatal("stray fired no arrow")
	}
	for _, a := range h.arrows {
		if a.slow != 30 {
			t.Errorf("stray arrow slow=%d, want 30", a.slow)
		}
	}

	// A plain skeleton's arrow carries no slowness.
	skel := h.spawnHostile(players, entitySkeleton, 0, 0)
	skel.x, skel.y, skel.z = 6.0, 64, 0.5
	h.arrows = map[int32]*arrowEntity{}
	h.spawnArrow(players, skel, pl)
	for _, a := range h.arrows {
		if a.slow != 0 {
			t.Errorf("skeleton arrow slow=%d, want 0", a.slow)
		}
	}
}

func TestDrownedTridentThrow(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.gamemode = gmSurvival
	pl.x, pl.y, pl.z = 0.5, 64, 0.5
	players := map[int32]*tracked{pl.p.eid: pl}

	d := h.spawnHostile(players, entityDrowned, 0, 0)
	d.x, d.y, d.z = 8.0, 64, 0.5
	d.trident = true
	d.attackCD = 0
	before := len(h.arrows)
	h.drownedThrow(players, d)
	if len(h.arrows) != before+1 {
		t.Fatal("armed drowned threw no trident")
	}
	for _, a := range h.arrows {
		if a.dmg != 9 {
			t.Errorf("trident damage %d, want 9", a.dmg)
		}
	}
}
