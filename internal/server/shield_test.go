package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// blocking sets a player up with a shield that has been raised long enough.
func blocking(h *hub) *tracked {
	h.tick.Store(100)
	pl := testTracked()
	pl.health = maxHealth
	pl.x, pl.z, pl.yaw = 0, 0, -90 // facing +x
	pl.blockingSince = 90          // raised 10 ticks ago (past the delay)
	pl.p.setHotbarSlot(0, itemShield)
	return pl
}

func TestShieldBlocksFrontArc(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	if h.shieldBlocked(pl, 4, dtPlayerAttack, from(5, 0)) != 4 {
		t.Fatal("should block an attacker in front (+x)")
	}
	if h.shieldBlocked(pl, 4, dtPlayerAttack, from(-5, 0)) != 0 {
		t.Fatal("should NOT block an attacker behind (-x)")
	}
}

func TestShieldBlockDelay(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	pl.blockingSince = 98 // only 2 ticks ago — under the 5-tick raise delay
	if h.shieldBlocked(pl, 4, dtPlayerAttack, from(5, 0)) != 0 {
		t.Fatal("a shield under the raise delay should not block yet")
	}
}

// A shield stops what vanilla says it stops. bypasses_shield is NOT the same
// set as bypasses_armor — it is that set plus the environmental hazards — so
// grading a shield off the armour tag would let it catch lava.
func TestShieldHonoursTheBypassTag(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	for _, c := range []struct {
		dt    dmgType
		stops bool
	}{
		// The shield was checked at three call sites, so these three worked…
		{dtPlayerAttack, true}, {dtMobAttack, true}, {dtArrow, true},
		// …and none of these did, though vanilla stops every one.
		{dtExplosion, true}, {dtFireball, true}, {dtWitherSkull, true},
		{dtThrown, true}, {dtWindCharge, true}, {dtSpit, true},
		{dtSting, true}, {dtThorns, true}, {dtMaceSmash, true},
		{dtFireworks, true}, {dtTrident, true},
		// Environmental hazards: in bypasses_shield though NOT in bypasses_armor,
		// which is exactly why the two tags cannot be conflated.
		{dtLava, false}, {dtInFire, false}, {dtCactus, false},
		{dtHotFloor, false}, {dtSweetBerryBush, false}, {dtCampfire, false},
		{dtLightningBolt, false}, {dtFallingAnvil, false},
		// And everything armour misses, a shield misses too.
		{dtFall, false}, {dtDrown, false}, {dtStarve, false},
		{dtMagic, false}, {dtIndirectMagic, false}, {dtSonicBoom, false},
		{dtOutOfWorld, false},
	} {
		got := h.shieldBlocked(pl, 4, c.dt, from(5, 0)) > 0
		if got != c.stops {
			t.Errorf("%s: shield stops = %v, want %v", c.dt.name(), got, c.stops)
		}
	}
}

// An unsourced hit cannot be blocked — vanilla resolves the arc against the
// source position and a missing one lands outside every arc. This is what lets
// starving and drowning through a raised shield without a tag for each.
func TestUnsourcedDamageIsNeverBlocked(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	if h.shieldBlocked(pl, 4, dtPlayerAttack, dmgFrom{}) != 0 {
		t.Fatal("a hit with no source position should not be blockable")
	}
}

// The shield wears by what it stopped rather than a flat point per hit, so it
// shrugs off a swarm of weak blows for free and pays for a heavy one.
func TestShieldWearScalesWithTheHit(t *testing.T) {
	for _, c := range []struct {
		blocked float32
		want    int
	}{
		{0, 0}, {1, 0}, {2.9, 0}, // below item_damage threshold 3
		{3, 4}, {5, 6}, {10, 11},
	} {
		if got := shieldWear(c.blocked); got != c.want {
			t.Errorf("shieldWear(%v) = %d, want %d", c.blocked, got, c.want)
		}
	}
}

// Blocking cancels what the blow would have delivered — vanilla gates a hit's
// follow-on effects on its return value, and a caught bite carries no venom.
func TestBlockedHitReportsNotLanded(t *testing.T) {
	h := newHub(world.New(1))
	pl := blocking(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	if h.hurtFrom(players, pl, 4, dtMobAttack, deathCause{}, from(5, 0)) {
		t.Error("a blow caught on the shield should report as not landed")
	}
	if pl.health != maxHealth {
		t.Errorf("a caught blow should cost no health: %v", pl.health)
	}
	// Whereas armour merely soaking a hit still counts as landing, so a bite
	// that gets through plate still poisons.
	pl.blockingSince = 0
	if !h.hurtFrom(players, pl, 4, dtMobAttack, deathCause{}, from(5, 0)) {
		t.Error("an unblocked blow should report as landed")
	}
}

// A falling anvil batters the helmet in particular and loses a quarter of its
// force doing it.
func TestDamagesHelmetWearsTheHelmetAndSoftensTheBlow(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.health = maxHealth
	players := map[int32]*tracked{pl.p.eid: pl}
	equipSet(t, pl, [4]int{3, 8, 6, 3}, 2)
	h.hurtFrom(players, pl, 8, dtFallingAnvil, deathCause{}, dmgFrom{})
	if pl.armor[0].dmg == 0 {
		t.Error("a falling anvil should batter the helmet")
	}

	// The same blow that is not tagged damages_helmet takes the ordinary
	// armour wear, and more health with it.
	bare := testTracked()
	bare.health = maxHealth
	equipSet(t, bare, [4]int{3, 8, 6, 3}, 2)
	h.hurtFrom(map[int32]*tracked{bare.p.eid: bare}, bare, 8, dtMobAttack, deathCause{}, dmgFrom{})
	if maxHealth-pl.health >= maxHealth-bare.health {
		t.Errorf("damages_helmet should soften the blow: anvil %v vs bite %v",
			maxHealth-pl.health, maxHealth-bare.health)
	}
}

func TestRaiseShieldRequiresShield(t *testing.T) {
	h := newHub(world.New(1))
	h.tick.Store(50)
	pl := testTracked()
	pl.p.setHotbarSlot(0, itemShield)
	h.raiseShield(pl)
	if pl.blockingSince != 50 {
		t.Fatalf("holding a shield should raise it, blockingSince=%d", pl.blockingSince)
	}
	pl2 := testTracked()
	pl2.p.setHotbarSlot(0, itemBow)
	h.raiseShield(pl2)
	if pl2.blockingSince != 0 {
		t.Fatal("a non-shield item should not raise a block")
	}
}
