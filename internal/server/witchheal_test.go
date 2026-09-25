package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestRaidWitchHealsRaiders: a raid witch beside a hurt pillager throws it a
// regeneration (or healing) potion and holds off the players for ten
// seconds; a witch outside a raid heals nobody.
func TestRaidWitchHealsRaiders(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 40, 180, 40
	w := h.spawnMob(players, entityWitch, 0.5, 180, 0.5)
	p := h.spawnMob(players, entityPillager, 4.5, 180, 0.5)
	p.health = 3
	h.gridDirty()
	for i := 0; i < 5000; i++ {
		if h.witchHealTick(players, w) {
			t.Fatal("no raid, no healing")
		}
	}
	w.raidCenter, p.raidCenter = blockPos{0, 180, 0}, blockPos{0, 180, 0}
	for i := 0; i < 20000 && w.witchHealTarget == 0; i++ {
		h.witchHealTick(players, w)
	}
	if w.witchHealTarget != p.eid || w.witchHealCD != witchHealCooldown || len(h.arrows) != 0 {
		t.Fatalf("the goal makes the raider her target first: target %d cd %d potions %d", w.witchHealTarget, w.witchHealCD, len(h.arrows))
	}
	h.acquireTarget(players, w)
	if !w.hasTarget || w.tx != p.x {
		t.Fatal("the raider she means to heal is what she walks toward")
	}
	for i := 0; i < 100 && len(h.arrows) == 0; i++ {
		if !h.witchHealTick(players, w) {
			t.Fatal("the heal holds her attack until she throws")
		}
	}
	if len(h.arrows) != 1 || w.witchHealTarget != 0 || w.witchHealCD <= 0 {
		t.Fatalf("a raid witch heals her raider on her throw clock: potions %d target %d cd %d", len(h.arrows), w.witchHealTarget, w.witchHealCD)
	}
	for _, a := range h.arrows {
		if !a.splash || a.potion != potHealing || !a.mobShot {
			t.Fatalf("healing for a raider on three health: %+v", a)
		}
	}
}

// Witch.performRangedAttack throws at 0.75 blocks a tick from just under her
// eyes, and the potion lands on the player it was aimed at. It flew at
// twice that — the speed was doubled as if projectiles moved once per mob
// update — and sailed over a player eight blocks off.
func TestWitchPotionFliesAtVanillaSpeedAndLands(t *testing.T) {
	h, players := preyFixture(t)
	h.rules.Difficulty = diffHard
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 8.5, 180, 0.5
	players[pl.p.eid] = pl
	w := h.spawnHostileY(players, entityWitch, 0.5, 180, 0.5)
	before := len(h.arrows)
	for i := 0; i < 200 && len(h.arrows) == before; i++ {
		w.drinkTicks = 0
		h.witchTick(players, w)
	}
	var pot *arrowEntity
	for _, a := range h.arrows {
		if a.splash && a.shooter == w.eid {
			pot = a
		}
	}
	if pot == nil {
		t.Fatal("the witch never threw")
	}
	if sp := math.Sqrt(pot.vx*pot.vx + pot.vy*pot.vy + pot.vz*pot.vz); sp < 0.7 || sp > 0.8 {
		t.Errorf("thrown at %.3f blocks a tick, want 0.75", sp)
	}
	if got := pot.y - w.y; math.Abs(got-(mobEyeHeight(w)-0.1)) > 1e-9 {
		t.Errorf("thrown from %.2f above her feet, want her eyes less 0.1 (%.2f)", got, mobEyeHeight(w)-0.1)
	}
	for i := 0; i < 80 && h.arrows[pot.eid] != nil; i++ {
		h.tick.Add(1)
		h.updateArrows(players)
	}
	if pl.hasEffect(effSlowness) == 0 {
		t.Errorf("the slowness potion (thrown at eight blocks) missed; it broke at %.1f,%.1f,%.1f", pot.x, pot.y, pot.z)
	}
}

// NearestHealableRaiderTargetGoal is mustSee and asks nothing of the
// raider's health: an unhurt raider in view gets regeneration, one behind a
// wall is not picked.
func TestRaidWitchNeedsSightNotAWound(t *testing.T) {
	h, players := preyFixture(t)
	for z := -4; z <= 4; z++ {
		for y := 180; y < 184; y++ {
			h.world.SetBlock(3, y, z, worldgen.Stone)
		}
	}
	w := h.spawnMob(players, entityWitch, 0.5, 180, 0.5)
	p := h.spawnMob(players, entityPillager, 6.5, 180, 0.5)
	w.raidCenter, p.raidCenter = blockPos{0, 180, 0}, blockPos{0, 180, 0}
	h.gridDirty()
	for i := 0; i < 20000 && w.witchHealTarget == 0; i++ {
		h.witchHealTick(players, w)
	}
	if w.witchHealTarget != 0 {
		t.Fatal("a raider behind a wall must not be picked")
	}
	p.x = -4.5 // same side, unhurt
	h.gridDirty()
	for i := 0; i < 20000 && w.witchHealTarget == 0; i++ {
		h.witchHealTick(players, w)
	}
	if w.witchHealTarget != p.eid {
		t.Fatal("an unhurt raider in view is still picked (regeneration)")
	}
}
