package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The damage pipeline used to leave armour to the ~20 places that deal damage,
// and the result was that lava, fire, cacti, magma, berry bushes and lightning
// all ignored armour completely while the dragon's blows and the guardian's
// bite never wore any. Nothing failed when that was true — hence these.

// armouredVsBare returns the health lost by a player in full diamond and by an
// identical player in nothing, for one damage type.
func armouredVsBare(t *testing.T, h *hub, dt dmgType, amount float32) (armoured, bare float32) {
	t.Helper()
	kitted := testTracked()
	kitted.health = maxHealth
	equipSet(t, kitted, [4]int{3, 8, 6, 3}, 2) // diamond
	h.damageOf(map[int32]*tracked{1: kitted}, kitted, amount, dt)

	naked := testTracked()
	naked.health = maxHealth
	h.damageOf(map[int32]*tracked{1: naked}, naked, amount, dt)
	return maxHealth - kitted.health, maxHealth - naked.health
}

// TestArmourAppliesExactlyWhereVanillaSaysItDoes is the invariant the per-call-
// site version could not hold: armour absorbs a hit if and only if the damage
// type is NOT tagged bypasses_armor. Every type below is one the engine really
// deals.
func TestArmourAppliesExactlyWhereVanillaSaysItDoes(t *testing.T) {
	h := newHub(world.New(1))
	for _, dt := range []dmgType{
		// Armour helps against all of these. Six of them silently did not.
		dtLava, dtInFire, dtCampfire, dtCactus, dtHotFloor, dtSweetBerryBush,
		dtLightningBolt, dtMobAttack, dtPlayerAttack, dtArrow, dtExplosion,
		dtThorns, dtDryOut, dtFallingAnvil,
		// And against none of these.
		dtFall, dtDrown, dtStarve, dtOnFire, dtOutOfWorld, dtInWall,
		dtMagic, dtIndirectMagic, dtWither, dtDragonBreath, dtSonicBoom,
		dtOutsideBorder, dtFreeze, dtGeneric,
	} {
		armoured, bare := armouredVsBare(t, h, dt, 8)
		bypasses := dt.has(tagBypassesArmor)
		switch {
		case bypasses && armoured != bare:
			t.Errorf("%s bypasses armour but armour reduced it (%v vs %v)", dt.name(), armoured, bare)
		case !bypasses && armoured >= bare:
			t.Errorf("%s should be reduced by armour but was not (%v vs %v)", dt.name(), armoured, bare)
		}
	}
}

// TestArmourWearsWithTheSameTag pins the other half: the hit that armour
// absorbs is the hit that wears it. The dragon's contact damage and the
// guardian's bite reduced but never wore, which is how a full set of diamond
// outlasted a whole End fight.
func TestArmourWearsWithTheSameTag(t *testing.T) {
	h := newHub(world.New(1))
	for _, c := range []struct {
		dt        dmgType
		wantsWear bool
	}{
		{dtMobAttack, true}, // the dragon's contact damage and the guardian's bite
		{dtLava, true},
		{dtCactus, true},
		{dtExplosion, true},
		{dtFall, false}, // armour has never softened a landing
		{dtDrown, false},
		{dtStarve, false},
		{dtIndirectMagic, false}, // evoker fangs
	} {
		pl := testTracked()
		pl.health = maxHealth
		players := map[int32]*tracked{1: pl}
		equipSet(t, pl, [4]int{3, 8, 6, 3}, 2)
		h.damageOf(players, pl, 8, c.dt)
		worn := pl.armor[1].dmg > 0 // chestplate
		if worn != c.wantsWear {
			t.Errorf("%s: armour worn = %v, want %v", c.dt.name(), worn, c.wantsWear)
		}
	}
}

// TestProtectionsStackWhenTagsOverlap — a ghast's fireball is both is_fire and
// is_projectile, so Fire Protection and Projectile Protection BOTH count. The
// old switch picked one family and stopped.
func TestProtectionsStackWhenTagsOverlap(t *testing.T) {
	if !dtFireball.has(tagIsFire) || !dtFireball.has(tagIsProjectile) {
		t.Fatal("fireball should be both is_fire and is_projectile")
	}
	pl := testTracked()
	pl.armor[0] = invStack{item: itemByName["diamond_helmet"], count: 1,
		ench: [2]enchApply{{id: enchFireProtection, lvl: 2}, {id: enchProjectileProtection, lvl: 2}}}

	both := protectionPoints(pl.armor[:], dtFireball)
	fireOnly := protectionPoints(pl.armor[:], dtInFire)
	arrowOnly := protectionPoints(pl.armor[:], dtArrow)
	if both != fireOnly+arrowOnly {
		t.Errorf("overlapping tags should stack: fireball=%d, fire=%d, projectile=%d",
			both, fireOnly, arrowOnly)
	}
}

// TestBypassTagsSkipTheirOwnMitigation — three tags each carve out exactly one
// step of the pipeline, and nothing else.
func TestBypassTagsSkipTheirOwnMitigation(t *testing.T) {
	h := newHub(world.New(1))

	// sonic_boom: bypasses_enchantments, so protection does nothing…
	warded := testTracked()
	warded.health = maxHealth
	equipSet(t, warded, [4]int{3, 8, 6, 3}, 2)
	for i := range warded.armor {
		warded.armor[i].ench = [2]enchApply{{id: enchProtection, lvl: 4}}
	}
	h.damageOf(map[int32]*tracked{1: warded}, warded, 10, dtSonicBoom)
	if maxHealth-warded.health != 10 {
		t.Errorf("sonic_boom should ignore protection: took %v of 10", maxHealth-warded.health)
	}

	// …starve: bypasses_effects, so Resistance does not help either.
	starving := testTracked()
	starving.health = maxHealth
	starving.effects[effResistance] = &activeEffect{amp: 4, left: 200}
	h.damageOf(map[int32]*tracked{1: starving}, starving, 4, dtStarve)
	if maxHealth-starving.health != 4 {
		t.Errorf("starve should ignore Resistance: took %v of 4", maxHealth-starving.health)
	}

	// …while Resistance does apply to an ordinary bite.
	tough := testTracked()
	tough.health = maxHealth
	tough.effects[effResistance] = &activeEffect{amp: 4, left: 200}
	h.damageOf(map[int32]*tracked{1: tough}, tough, 4, dtMobAttack)
	if maxHealth-tough.health != 0 {
		t.Errorf("Resistance V should null an ordinary bite: took %v", maxHealth-tough.health)
	}
}

// TestDamageTagTableMatchesVanilla pins the memberships the pipeline actually
// branches on, so a regenerated table cannot quietly move one. These were read
// off the canonical damage_type tags.
func TestDamageTagTableMatchesVanilla(t *testing.T) {
	for _, c := range []struct {
		dt   dmgType
		tag  dmgTag
		want bool
		note string
	}{
		{dtFall, tagBypassesArmor, true, "armour never softens a landing"},
		{dtOnFire, tagBypassesArmor, true, "the afterburn goes straight through"},
		{dtInFire, tagBypassesArmor, false, "but standing in the flames does not"},
		{dtLava, tagBypassesArmor, false, ""},
		{dtCactus, tagBypassesArmor, false, ""},
		{dtHotFloor, tagBypassesArmor, false, ""},
		{dtSweetBerryBush, tagBypassesArmor, false, ""},
		{dtLightningBolt, tagBypassesArmor, false, ""},
		{dtGeneric, tagBypassesArmor, true, "which is why it is the wrong label for a bite"},
		{dtIndirectMagic, tagBypassesArmor, true, "evoker fangs"},
		{dtDragonBreath, tagBypassesArmor, true, ""},
		{dtStarve, tagBypassesEffects, true, ""},
		{dtSonicBoom, tagBypassesEnchantments, true, ""},
		{dtOutOfWorld, tagBypassesResistance, true, ""},
		{dtGenericKill, tagBypassesResistance, true, ""},
		{dtFireball, tagIsFire, true, ""},
		{dtFireball, tagIsProjectile, true, ""},
		{dtEnderPearl, tagIsFall, true, "feather falling softens a pearl"},
	} {
		if got := c.dt.has(c.tag); got != c.want {
			t.Errorf("%s has tag = %v, want %v %s", c.dt.name(), got, c.want, c.note)
		}
	}
}
