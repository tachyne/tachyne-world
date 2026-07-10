package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func TestMobLavaDamageAndIgnite(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityCow, 200, 70, 200)
	m.health = 50
	h.world.SetBlock(200, 70, 200, worldgen.LavaBase) // stand it in lava
	before := m.health
	h.mobEnvironment(players)
	if m.health >= before {
		t.Fatalf("mob in lava took no damage (%d -> %d)", before, m.health)
	}
	if m.fireSecs == 0 {
		t.Fatal("lava should set the mob on fire (afterburn)")
	}
}

func TestMobFallDamageOnGroundRemoval(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityCow, 210, 90, 210)
	m.health = 50
	before := m.health
	// Ground under it is ~30 below (open air) → a big drop → fall damage.
	h.mobFall(players, m, 12)
	if m.health != before-int(12-mobSafeFall) {
		t.Fatalf("fall of 12 should deal %d, health %d->%d", int(12-mobSafeFall), before, m.health)
	}
	// Chickens are fall-immune.
	c := h.spawnMob(players, entityChicken, 211, 90, 211)
	c.health = 50
	h.mobFall(players, c, 12)
	if c.health != 50 {
		t.Fatal("chickens are fall-damage immune")
	}
}

func TestLandMobDrowns(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityCow, 220, 70, 220)
	m.health = 50
	// Submerge head + feet in water.
	h.world.SetBlock(220, 70, 220, worldgen.WaterBase)
	h.world.SetBlock(220, 71, 220, worldgen.WaterBase)
	before := m.health
	for i := 0; i < maxAir/20+3; i++ {
		h.mobEnvironment(players)
	}
	if m.health >= before {
		t.Fatal("a submerged land mob should drown")
	}
	// A squid (water-breather) never drowns.
	s := h.spawnMob(players, entitySquid, 221, 70, 221)
	s.health = 50
	h.world.SetBlock(221, 70, 221, worldgen.WaterBase)
	h.world.SetBlock(221, 71, 221, worldgen.WaterBase)
	for i := 0; i < maxAir/20+3; i++ {
		h.mobEnvironment(players)
	}
	if s.health != 50 {
		t.Fatal("water-breathers should not drown")
	}
}

func TestFireImmuneMobsIgnoreLava(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityStrider, 230, 70, 230)
	m.health = 20
	h.world.SetBlock(230, 70, 230, worldgen.LavaBase)
	for i := 0; i < 5; i++ {
		h.mobEnvironment(players)
	}
	if m.health != 20 || m.fireSecs != 0 {
		t.Fatalf("strider should be unharmed in lava: health=%d fireSecs=%d", m.health, m.fireSecs)
	}
}
