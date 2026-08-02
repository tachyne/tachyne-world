package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The mob damage path used to carry the player path's bug INVERTED: rather
// than call sites forgetting to apply armour, they were instructed to skip the
// funnel and write to health directly whenever the damage "bypasses armor in
// vanilla". The premise was half right — falling and drowning do, standing in
// lava does not — so an armoured mob burned as fast as a naked one.

// armouredMobVsBare is the mob twin of the player invariant: two zombies, one
// in a full set, and how much health each loses to one hit of a damage type.
func armouredMobVsBare(t *testing.T, h *hub, dt dmgType, dmg float64) (armoured, bare int) {
	t.Helper()
	players := map[int32]*tracked{}

	kitted := h.spawnHostile(players, entityZombie, 0, 0)
	if kitted == nil {
		t.Fatal("spawn returned nil")
	}
	for i, n := range []string{"diamond_helmet", "diamond_chestplate", "diamond_leggings", "diamond_boots"} {
		kitted.gear[i] = invStack{item: int32(itemByName[n]), count: 1}
	}
	kitted.refreshGearArmor()
	before := kitted.health
	h.hurtMobOf(players, kitted, dmg, dt)

	naked := h.spawnHostile(players, entityZombie, 0, 0)
	if naked == nil {
		t.Fatal("spawn returned nil")
	}
	nakedBefore := naked.health
	h.hurtMobOf(players, naked, dmg, dt)
	return before - kitted.health, nakedBefore - naked.health
}

// TestMobArmourAppliesExactlyWhereVanillaSaysItDoes — the same invariant the
// player side holds, on the other half of the engine.
func TestMobArmourAppliesExactlyWhereVanillaSaysItDoes(t *testing.T) {
	h := newHub(world.New(1))
	for _, dt := range []dmgType{
		// Armour helps. Lava, fire, magma and berry bushes are the four the
		// old "environmental damage bypasses armour" premise got wrong.
		dtLava, dtInFire, dtHotFloor, dtSweetBerryBush, dtLightningBolt, dtExplosion,
		// Armour does not.
		dtFall, dtDrown, dtOnFire, dtMagic, dtIndirectMagic, dtWither,
	} {
		armoured, bare := armouredMobVsBare(t, h, dt, 12)
		bypasses := dt.has(tagBypassesArmor)
		switch {
		case bypasses && armoured != bare:
			t.Errorf("%s bypasses armour but a mob's armour reduced it (%d vs %d)",
				dt.name(), armoured, bare)
		case !bypasses && armoured >= bare:
			t.Errorf("%s should be reduced by a mob's armour but was not (%d vs %d)",
				dt.name(), armoured, bare)
		}
	}
}

// Evoker fangs bypass armour for a player (the tag says indirect_magic), and
// must do the same to a mob. The player side was fixed and the mob side was
// still running the fangs through plate.
func TestFangsIgnoreMobArmour(t *testing.T) {
	h := newHub(world.New(1))
	armoured, bare := armouredMobVsBare(t, h, dtIndirectMagic, 6)
	if armoured != bare {
		t.Errorf("evoker fangs should ignore a mob's armour: %d vs %d", armoured, bare)
	}
}

// Netherite carries vanilla's damage_resistant component against is_fire,
// which is what lets a set survive a lava bath that would eat diamond.
func TestNetheriteArmourDoesNotWearInFire(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	wear := func(set string, dt dmgType) int {
		pl := testTracked()
		pl.health = maxHealth
		for i, slot := range []string{"_helmet", "_chestplate", "_leggings", "_boots"} {
			pl.armor[i] = invStack{item: int32(itemByName[set+slot]), count: 1}
		}
		if pl.armor[1].item == 0 {
			t.Fatalf("no %s chestplate — the fixture is wrong", set)
		}
		h.wearArmor(players, pl, 8, dt)
		return pl.armor[1].dmg
	}

	if n := wear("netherite", dtLava); n != 0 {
		t.Errorf("netherite should not wear in lava, took %d", n)
	}
	if n := wear("diamond", dtLava); n == 0 {
		t.Error("diamond should wear in lava — otherwise this test proves nothing")
	}
	// The resistance is to FIRE specifically, not to everything.
	if n := wear("netherite", dtMobAttack); n == 0 {
		t.Error("netherite should still wear from an ordinary blow")
	}
}

// A mob's Resistance and its gear's protection enchantments are mitigations
// like any other, so the bypass tags carve them out on the mob side too.
func TestMobBypassTagsSkipTheirOwnMitigation(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	resisted := func(dt dmgType) int {
		m := h.spawnHostile(players, entityZombie, 0, 0)
		if m == nil {
			t.Fatal("spawn returned nil")
		}
		m.effects = map[int32]*activeEffect{effResistance: {amp: 4, left: 200}}
		before := m.health
		h.hurtMobOf(players, m, 8, dt)
		return before - m.health
	}
	if got := resisted(dtMobAttack); got != 0 {
		t.Errorf("Resistance V should null an ordinary blow to a mob: took %d", got)
	}
	if got := resisted(dtGenericKill); got == 0 {
		t.Error("generic_kill bypasses resistance — /kill must still work on a resistant mob")
	}
}
