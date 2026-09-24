package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// CombatTracker's fall family: a fall off a ladder reads "fell off a
// ladder", a fall after a mob's blow "was doomed to fall by", and a plain
// short fall keeps the plain fall message.
func TestFallDeathMessages(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	name := pl.p.name

	// Off a ladder: the ladder was the last climbable, then twenty blocks down.
	h.world.SetBlock(0, 200, 0, worldgen.BlockBase("ladder"))
	pl.x, pl.y, pl.z = 0.5, 200, 0.5
	h.trackClimbable(pl, 0.5, 200, 0.5, false)
	pl.lastCause = deathCause{dt: dtFall}
	h.recordCombat(pl, deathCause{dt: dtFall}, 20, 20)
	if got, want := h.combatDeathMessage(pl), name+" fell off a ladder"; got != want {
		t.Errorf("ladder: %q, want %q", got, want)
	}

	// A zombie's blow, then the fall.
	pl.combat = combatLog{}
	h.recordCombat(pl, deathCause{dt: dtMobAttack, by: "Zombie"}, 3, 0)
	h.recordCombat(pl, deathCause{dt: dtFall}, 20, 20)
	if got, want := h.combatDeathMessage(pl), name+" was doomed to fall by Zombie"; got != want {
		t.Errorf("knocked off: %q, want %q", got, want)
	}

	// A four-block drop is not significant: the plain fall message.
	pl.combat = combatLog{}
	h.recordCombat(pl, deathCause{dt: dtFall}, 1, 4)
	if got, want := h.combatDeathMessage(pl), deathMessage(name, deathCause{dt: dtFall}); got != want {
		t.Errorf("short fall: %q, want %q", got, want)
	}

	// The log clears after five quiet seconds.
	h.recordCombat(pl, deathCause{dt: dtFall}, 20, 20)
	h.tick.Add(combatResetTicks + 1)
	h.recheckCombat(pl, h.tick.Load())
	if len(pl.combat.entries) != 0 {
		t.Error("the combat log outlived five quiet seconds")
	}
}
