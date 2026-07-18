package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestGuardianBeamAndElderAura(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffNormal
	pl := testTracked()
	pl.x, pl.y, pl.z = 5, 63, 5
	players := map[int32]*tracked{1: pl}

	// Elder guardian 7 blocks away: in beam range (> 3 blocks).
	g := h.spawnMob(players, entityElderGuardian, 12, 63, 5)
	if g == nil {
		t.Fatal("elder guardian spawn returned nil")
	}
	h.guardianTick(players, g)
	if pl.health >= maxHealth {
		t.Fatalf("elder guardian beam should damage the player, health=%v", pl.health)
	}
	if g.sonicCD == 0 {
		t.Fatal("firing the beam should start a cooldown")
	}

	// Elder aura fires on the interval and lays Mining Fatigue.
	g.digClock = elderAuraUpd - 1
	h.guardianTick(players, g)
	if pl.hasEffect(effMiningFatigue) == 0 {
		t.Fatal("elder guardian should curse a nearby player with Mining Fatigue")
	}
}
