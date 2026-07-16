package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// maceSetup arms a survival player with a mace, high in the air and falling, and
// returns a full-health test mob just in reach.
func maceSetup(t *testing.T, ench [2]enchApply) (*hub, *tracked, *mob, map[int32]*tracked) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 82, 4.5
	pl.peakY, pl.airborne, pl.sprinting = 92, true, true // fell 10 blocks; sprinting → no jump-crit
	pl.p.setHotbarSlot(0, itemMace)
	pl.inv.slots[0] = invStack{item: itemMace, count: 1, ench: ench}
	m := &mob{eid: 9, etype: entityCow, health: 100, maxHealth: 100, x: 0.5, y: 80, z: 5.5}
	h.mobs[9] = m
	return h, pl, m, map[int32]*tracked{1: pl}
}

func TestMaceFallBonusTiers(t *testing.T) {
	cases := []struct {
		fall, want float64
	}{
		{1, 4}, {3, 12}, {5, 16}, {8, 22}, {10, 24}, {20, 34},
	}
	for _, c := range cases {
		if got := maceFallBonus(c.fall); got != c.want {
			t.Errorf("maceFallBonus(%v) = %v, want %v", c.fall, got, c.want)
		}
	}
}

func TestMaceSmashDamage(t *testing.T) {
	h, pl, m, players := maceSetup(t, [2]enchApply{})
	h.attackMob(players, pl.p.eid, m.eid)
	// base 6 + fall bonus 24 (fell 10), no crit (sprinting), no armour.
	if want := 100 - 30; m.health != want {
		t.Fatalf("smash dealt %d, want %d", 100-m.health, 30)
	}
	// The smash negates the attacker's own fall damage (fall distance reset).
	if pl.peakY != pl.y {
		t.Fatalf("smash must reset fall distance: peakY=%v y=%v", pl.peakY, pl.y)
	}
}

func TestMaceDensityScales(t *testing.T) {
	h, pl, m, players := maceSetup(t, [2]enchApply{{id: enchDensity, lvl: 2}})
	h.attackMob(players, pl.p.eid, m.eid)
	// base 6 + fall 24 + density(0.5·2·10=10) = 40.
	if want := 100 - 40; m.health != want {
		t.Fatalf("density smash dealt %d, want 40", 100-m.health)
	}
}

func TestMaceNotFallingIsPlainHit(t *testing.T) {
	h, pl, m, players := maceSetup(t, [2]enchApply{})
	pl.airborne, pl.peakY = false, pl.y // standing on the ground — no smash
	h.attackMob(players, pl.p.eid, m.eid)
	if 100-m.health != 6 { // just the mace's base damage
		t.Fatalf("a grounded mace hit should deal 6, dealt %d", 100-m.health)
	}
}

func TestMaceBreachPiercesArmor(t *testing.T) {
	plain := &mob{armor: 8, health: 100, maxHealth: 100}
	breached := &mob{armor: 8, health: 100, maxHealth: 100}
	plain.hurtBreach(20, 0)
	breached.hurtBreach(20, 0.30) // Breach II: −0.30 armour effectiveness
	if breached.health >= plain.health {
		t.Fatalf("breach should deal more through armour: plain=%d breached=%d", plain.health, breached.health)
	}
}

func TestMaceShockwaveKnocksNearby(t *testing.T) {
	h, pl, m, players := maceSetup(t, [2]enchApply{})
	bystander := &mob{eid: 10, etype: entityPig, health: 20, x: 1.5, y: 80, z: 6.0} // within 3.5 of the attacker
	h.mobs[10] = bystander
	h.attackMob(players, pl.p.eid, m.eid)
	if bystander.vx == 0 && bystander.vz == 0 {
		t.Fatal("the smash shockwave should knock back a nearby mob")
	}
}

func TestWindBurstMult(t *testing.T) {
	for lvl, want := range map[int]float64{1: 1.2, 2: 1.75, 3: 2.2} {
		if got := windBurstMult(lvl); got != want {
			t.Errorf("windBurstMult(%d) = %v, want %v", lvl, got, want)
		}
	}
}
