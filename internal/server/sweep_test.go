package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Player.doSweepAttack through the attack event: a Sweeping Edge III,
// Sharpness V, Fire Aspect sword swept past the target hits the zombie
// beside it for 1 + 0.75 × 7 + 3 through its armour, shoves it along the
// swing, and sets it alight; a plain sword's sweep reads the ratio from the
// attribute, so a raised SWEEPING_DAMAGE_RATIO raises it.
func TestSweepingBlow(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z, pl.yaw = 0.5, 70, 0.5, -90 // facing +x
	pl.onGround = true
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	h.tick.Store(100)
	h.allocEID() // the player holds eid 1
	target := h.spawnMob(players, entityZombie, 2.0, 70, 0.5)
	side := h.spawnMob(players, entityZombie, 2.5, 70, 1.4)
	target.health, side.health = 100, 100
	pl.p.setHotbarSlot(0, tDiamondSword)
	pl.inv.slots[0] = invStack{item: tDiamondSword, count: 1, ench: enchList{
		{id: enchSweepingEdge, lvl: 3}, {id: enchSharpness, lvl: 5}, {id: enchFireAspect, lvl: 1}}}
	pl.refreshGearIfChanged()
	h.onAttack(players, evAttack{attacker: 1, target: target.eid})
	// 1 + 0.75×7 = 6.25, +3 Sharpness = 9.25, through the zombie's 2 armour:
	// 9.25 × (1 − max(0.4, 2 − 9.25/2)/25) = 9.102.
	if lost := 100 - side.health; lost != 9 {
		t.Errorf("the swept zombie lost %d, want 9", lost)
	}
	if side.kb == 0 || side.vx <= 0 {
		t.Errorf("the swept zombie was not shoved along the swing: kb %d vx %v", side.kb, side.vx)
	}
	if side.fireSecs <= 0 {
		t.Error("Fire Aspect did not reach the swept zombie")
	}

	// A plain sword: the sweep is 1 unless SWEEPING_DAMAGE_RATIO says more.
	side.health, side.fireSecs = 100, 0
	side.invulnTicks, target.invulnTicks = 0, 0 // no ticks ran to wind the hurt cooldown down
	pl.inv.slots[0] = invStack{item: tDiamondSword, count: 1}
	pl.refreshGearIfChanged()
	pl.playerAttrs().SetBase(attr.SweepingDamageRatio, 1)
	h.tick.Store(200)
	h.onAttack(players, evAttack{attacker: 1, target: target.eid})
	if lost := 100 - side.health; lost < 7 {
		t.Errorf("with sweeping_damage_ratio 1 the sweep took %d, want 8 less armour", lost)
	}
}
