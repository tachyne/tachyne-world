package server

import (
	"math"
	"testing"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Ravager.aiStep: the speed eases toward 0.35 while the ravager has a
// target and back toward 0.3 without one, and is nothing while it bites.
func TestRavagerSpeedSwap(t *testing.T) {
	h, players, pl := skeletonRig(t)
	m := h.spawnHostileYIn(players, entityRavager, dimOverworld, 6.5, 200, 0.5)
	base := func() float64 { return m.mobAttrs().Get(attr.MovementSpeed).Base() / attrToStep }
	if math.Abs(base()-ravagerBaseSpeed) > 1e-9 {
		t.Fatalf("a ravager starts at 0.3, got %v", base())
	}
	pl.x = 30.5 // far enough that it never bites
	for i := 0; i < 30; i++ {
		h.updateMobs(players)
	}
	if !m.hasTarget {
		t.Fatal("the ravager should be after the player")
	}
	if got := base(); math.Abs(got-ravagerAttackSpeed) > 0.001 {
		t.Errorf("with a target the speed eases to 0.35, got %.4f", got)
	}
	m.ravAttackTick = ravagerAttackTicks
	h.updateMobs(players)
	if base() != 0 {
		t.Errorf("a biting ravager has no speed, got %v", base())
	}
	m.ravAttackTick = 0
	pl.gamemode = gmCreative // nothing to chase
	for i := 0; i < 40; i++ {
		h.updateMobs(players)
	}
	if got := base(); math.Abs(got-ravagerBaseSpeed) > 0.001 {
		t.Errorf("without a target it settles at 0.3, got %.4f", got)
	}
}
