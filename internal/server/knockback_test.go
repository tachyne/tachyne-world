package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// knockbackRig: a player beside a zombie, both on a real floor high above the
// terrain. The floor matters — a mob that is not standing on anything has its
// every step refused, and the refusal path throws the impulse away and picks a
// new heading, which reads exactly like "knockback does nothing".
func knockbackRig(t *testing.T) (*hub, *tracked, map[int32]*tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	w := h.world
	const y, z = 180, 40
	for x := 30; x <= 60; x++ {
		for dz := -6; dz <= 6; dz++ {
			w.SetBlock(x, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 3; dy++ {
				w.SetBlock(x, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = 40.5, y, float64(z)+0.5
	players := map[int32]*tracked{1: pl}
	m := h.spawnMobIn(players, entityZombie, 0, 42.0, y, float64(z)+0.5)
	if m == nil {
		t.Fatal("no zombie spawned")
	}
	m.health = 100
	h.tick.Store(100)
	return h, pl, players, m
}

// Legion, report #7: "when i hit a mob with an item in my hand the creature
// doesnt knockback". The shove was HALF vanilla's: LivingEntity.knockback's
// 0.4 is per TICK, and the engine was storing it as the per-UPDATE step,
// which the mob loop runs every other tick.
func TestKnockbackIsVanillaStrength(t *testing.T) {
	h, _, players, m := knockbackRig(t)
	x0 := m.x
	h.attackMob(players, 1, m.eid)

	// Per tick, which is the unit vanilla's 0.4 is quoted in.
	if got := m.vx / mobMoveInterval; math.Abs(got-0.4) > 1e-9 {
		t.Errorf("the shove is %.3f blocks a tick, want vanilla's 0.4", got)
	}
	for i := 0; i < 6; i++ {
		h.updateMobs(players)
		h.tick.Add(1)
	}
	// Vanilla's 0.4 impulse under ground friction carries a mob most of a
	// block; anything under half a block is the "it barely moved" complaint.
	if d := m.x - x0; d < 0.5 {
		t.Errorf("the zombie was shoved %.3f blocks, want most of a block", d)
	}
}

// …and the mob's own momentum is halved rather than thrown away, so a zombie
// walking INTO the swing is turned rather than simply reset.
func TestKnockbackKeepsHalfTheMobsMomentum(t *testing.T) {
	h, _, players, m := knockbackRig(t)
	m.vx = -0.4 // charging at the player
	h.attackMob(players, 1, m.eid)
	want := -0.2 + 0.4*mobMoveInterval
	if math.Abs(m.vx-want) > 1e-9 {
		t.Errorf("velocity after the hit %.3f, want %.3f (half of its own, plus the shove)", m.vx, want)
	}
}

// The hop is vanilla's min(0.4, vy/2 + strength) on a grounded victim. Every
// real hit clears 0.4, so the ceiling is what a player sees — it was a flat
// 0.36, just under it. What must NOT happen is exceeding it: a sprinting
// Knockback II swing carries a strength of 1.9 and still lifts only 0.4.
func TestKnockbackHopIsCappedAtVanillasCeiling(t *testing.T) {
	h, pl, players, m := knockbackRig(t)
	drainEvents(pl)
	h.attackMob(players, 1, m.eid)
	if vy := lastKnockVY(t, pl); math.Abs(vy-0.4) > 1e-9 {
		t.Errorf("a plain hit lifts %.3f, want vanilla's 0.4", vy)
	}

	h2, pl2, players2, m2 := knockbackRig(t)
	pl2.sprinting = true
	pl2.p.setHotbarSlot(0, tDiamondSword)
	pl2.inv.slots[0] = invStack{item: tDiamondSword, count: 1,
		ench: enchList{{id: int8(enchKnockback), lvl: 2}}}
	drainEvents(pl2)
	h2.attackMob(players2, 1, m2.eid)
	hard := lastKnockVY(t, pl2)
	if hard > 0.4+1e-9 {
		t.Errorf("a sprinting Knockback II hit lifts %.3f, above vanilla's 0.4 ceiling", hard)
	}
	// …while its HORIZONTAL shove is much bigger: 0.4 + 0.5 sprint + 1.0 for
	// two levels of Knockback.
	if got := m2.vx / mobMoveInterval; math.Abs(got-1.9) > 1e-9 {
		t.Errorf("the shove is %.3f a tick, want 1.9", got)
	}
}

// lastKnockVY is the vertical component of the last Velocity frame a player
// was sent — the hop the client actually renders.
func lastKnockVY(t *testing.T, tr *tracked) float64 {
	t.Helper()
	vy, found := 0.0, false
	for _, ev := range takeEvents(tr) {
		if v, ok := ev.(attachproto.Velocity); ok {
			vy, found = v.VY, true
		}
	}
	if !found {
		t.Fatal("no velocity frame was sent for the knockback")
	}
	return vy
}
