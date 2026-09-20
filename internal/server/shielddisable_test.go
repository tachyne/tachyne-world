package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// An axe blow caught on the shield disables it for five seconds: the shield
// drops, the next blow lands, and it cannot be raised until the cooldown is
// out; a sword blow does nothing of the kind.
func TestAxeDisablesShield(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	sword, axe := int32(itemByName["iron_sword"]), int32(itemByName["iron_axe"])
	cause := deathCause{key: causePlayer, by: "foe"}

	if h.hurtFrom(players, pl, 4, dtPlayerAttack, cause, fromWeapon(5, 0, sword)) {
		t.Fatal("a sword blow on a raised shield landed")
	}
	if h.onCooldown(pl, itemShield) || pl.blockingSince == 0 {
		t.Fatal("a sword disabled the shield")
	}
	if h.hurtFrom(players, pl, 4, dtPlayerAttack, cause, fromWeapon(5, 0, axe)) {
		t.Fatal("the axe blow that disables the shield should itself be blocked")
	}
	if !h.onCooldown(pl, itemShield) || pl.blockingSince != 0 {
		t.Fatalf("an axe should disable the shield: cooldown %v raised %d", h.onCooldown(pl, itemShield), pl.blockingSince)
	}
	if got := pl.cooldowns[itemShield] - h.tick.Load(); got != 100 {
		t.Errorf("disabled for %d ticks, want 100", got)
	}
	// Down and on cooldown: the next blow lands, and it cannot be raised.
	hp := pl.health
	h.raiseShield(pl, 0)
	if pl.blockingSince != 0 {
		t.Error("the shield came up during its cooldown")
	}
	if !h.hurtFrom(players, pl, 4, dtPlayerAttack, cause, fromWeapon(5, 0, sword)) || pl.health >= hp {
		t.Error("a blow during the cooldown was blocked")
	}
	// Cooldown out: it comes up again.
	h.tick.Store(h.tick.Load() + 100)
	h.raiseShield(pl, 0)
	if pl.blockingSince == 0 {
		t.Error("the shield stayed down after its cooldown")
	}
}
