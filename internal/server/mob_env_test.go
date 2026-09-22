package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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

// SAFE_FALL_DISTANCE and FALL_DAMAGE_MULTIPLIER were fixed constants, so every
// mob fell like a zombie. Vanilla moves two families off the defaults: a fox
// lands from five blocks unhurt, and an equine from six and then takes half.
func TestFallToleranceByFamily(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	fall := func(etype int, from float64) int {
		m := h.spawnMob(players, etype, 0, 80, 0)
		before := m.health
		h.mobFall(players, m, from)
		return before - m.health
	}

	// Seven blocks: a zombie loses four, a fox two, a horse half of one (so
	// nothing at all).
	if got := fall(entityZombie, 7); got != 4 {
		t.Errorf("a zombie falling 7 lost %d, want 4", got)
	}
	if got := fall(entityFox, 7); got != 2 {
		t.Errorf("a fox falling 7 lost %d, want 2", got)
	}
	if got := fall(entityHorse, 7); got != 0 {
		t.Errorf("a horse falling 7 lost %d, want 0", got)
	}
	// Sixteen: the horse's six-block grace then half of the rest.
	if got := fall(entityHorse, 16); got != 5 {
		t.Errorf("a horse falling 16 lost %d, want 5", got)
	}
}
