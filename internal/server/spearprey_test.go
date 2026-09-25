package server

import "testing"

// SpearUseGoal runs on getTarget(), whatever it is: a spear zombie with a
// villager for its target and no player about lowers its spear and charges
// the villager, as it would a player, where it used to put the spear away
// and bite. A zombie standing in the way is left alone.
func TestSpearZombieChargesItsVillager(t *testing.T) {
	h, players := preyFixture(t)
	h.rules.Difficulty = diffNormal
	h.tick.Store(1000)
	z := h.spawnHostileY(players, entityZombie, -5.5, 180, 0.5)
	z.held, z.spearGoal = itemIronSpear, nil
	v := h.spawnMob(players, entityVillager, 6.5, 180, 0.5)
	v.rest = 1 << 20
	z.preyTarget, z.hasTarget = v.eid, true
	hp := v.health
	lowered := false
	for i := 0; i < 200 && v.health == hp; i++ {
		h.tick.Add(mobMoveInterval)
		z.preyTarget, z.hasTarget = v.eid, true // keep the fixture's target
		h.updateMobs(players)
		lowered = lowered || z.handActive
		if v.dying > 0 {
			break
		}
	}
	if !lowered {
		t.Fatal("the zombie never lowered its spear at the villager")
	}
	if v.health >= hp {
		t.Fatalf("the spear charge never struck the villager (health %v)", v.health)
	}
}

// The charge strikes the creatures its kind hunts, not its own kind.
func TestSpearChargeSparesItsOwnKind(t *testing.T) {
	h, players := preyFixture(t)
	h.tick.Store(1000)
	z := h.spawnHostileY(players, entityZombie, 0.5, 180, 0.5)
	z.held = itemIronSpear
	v := h.spawnMob(players, entityVillager, 0.5, 180, 2.3)
	other := h.spawnHostileY(players, entityZombie, 0.5, 180, 2.0)
	z.preyTarget, z.hasTarget = v.eid, true
	z.yaw = 0
	z.spearUseAt = h.tick.Load() - uint64(spearOf(itemIronSpear).delay) - 1
	z.spearHits = map[int32]uint64{}
	z.vx, z.vz = 0, 0.2
	vh, oh := v.health, other.health
	h.mobSpearTick(players, z)
	if v.health >= vh {
		t.Error("the charge should strike the villager in its line")
	}
	if other.health != oh {
		t.Error("a zombie in the line is not stabbed by its fellow")
	}
}
