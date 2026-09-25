package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Each animal bolts at its own goal's speed, not a flat double.
func TestPanicSpeedsPerSpecies(t *testing.T) {
	for _, c := range []struct {
		etype int
		want  float64
	}{
		{entityCow, 2.0}, {entitySheep, 1.25}, {entityPig, 1.25}, {entityChicken, 1.4},
		{entityRabbit, 2.2}, {entityLlama, 1.2}, {entityTurtle, 1.2}, {entityStrider, 1.65},
		{entityWanderingTrader, 0.5}, {entityCod, 1.25},
	} {
		if got := panicSpeed(c.etype); got != c.want {
			t.Errorf("panic speed of %s = %v, want %v", entityTypeName(c.etype), got, c.want)
		}
	}
	if got := panicSpeed(entityVillager); got != 2.0 {
		t.Errorf("the default is vanilla's usual 2.0, got %v", got)
	}
}

// Fire, lava and a cactus set an animal running; hunger and a fall do not.
// The species vanilla exempts from ordinary panic still run from the fire.
func TestPanicCauses(t *testing.T) {
	cow := &mob{etype: entityCow}
	wolf := &mob{etype: entityWolf}
	if !panicsAt(cow, dtLava) || !panicsAt(cow, dtInFire) || !panicsAt(cow, dtCactus) {
		t.Error("the environment panics an animal")
	}
	if !panicsAt(cow, dtMobAttack) {
		t.Error("a blow panics an ordinary animal")
	}
	if panicsAt(cow, dtFall) || panicsAt(cow, dtStarve) {
		t.Error("a fall or starvation is not a panic cause")
	}
	if panicsAt(wolf, dtMobAttack) {
		t.Error("a wolf fights back rather than running")
	}
	if !panicsAt(wolf, dtLava) {
		t.Error("…but it still runs out of the lava")
	}
	// A polar bear cub panics at everything; its mother only at the fire.
	if !panicsAt(&mob{etype: entityPolarBear, baby: true}, dtMobAttack) {
		t.Error("a cub bolts from a blow")
	}
	if panicsAt(&mob{etype: entityPolarBear}, dtMobAttack) {
		t.Error("an adult polar bear stands its ground")
	}
	// A skeleton horse plods: it has no panic goal. A goat does (GoatAi's
	// AnimalPanic), and an armadillo runs from the environment only
	// (ArmadilloPanic) — a blow rolls it up.
	if panicsAt(&mob{etype: entitySkeletonHorse}, dtMobAttack) {
		t.Error("a skeleton horse never panics in vanilla")
	}
	if panicsAt(&mob{etype: entityArmadillo}, dtMobAttack) || !panicsAt(&mob{etype: entityArmadillo}, dtLava) {
		t.Error("an armadillo panics at lava, not at a blow")
	}
	if !panicsAt(&mob{etype: entityGoat}, dtMobAttack) {
		t.Error("a struck goat runs")
	}
}

// A hurt animal runs; a burning one runs towards water.
func TestEnvironmentalDamageStartsPanic(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			h.world.SetBlock(x, 69, z, worldgen.Stone)
			h.world.SetBlock(x, 70, z, worldgen.Air)
		}
	}
	cow := h.spawnMob(players, entityCow, 0.5, 70, 0.5)
	h.hurtMobOf(players, cow, 1, dtCactus)
	if cow.panic == 0 {
		t.Fatal("a cactus should set a cow running")
	}
	// Burning, with water three blocks away: it heads for the water, so the
	// point it flees FROM is on the far side of it.
	cow.panic = 0
	cow.burning = true
	h.world.SetBlock(3, 70, 0, worldgen.WaterBase)
	h.hurtMobOf(players, cow, 1, dtOnFire)
	if cow.panic == 0 {
		t.Fatal("a burning cow panics")
	}
	if cow.fleeX > cow.x {
		t.Errorf("it should flee towards the water at +x, flee point %v", cow.fleeX)
	}
}

// Potion effects survive a relog, and they tick in creative too (the
// periodic damage and healing stay a survival concern).
func TestEffectsPersistAndTickOutsideSurvival(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "inventories.json")
	s := newInvStore(path)
	pl := testTracked()
	pl.effects = map[int32]*activeEffect{
		effSpeed:       {amp: 1, left: 600},
		effNightVision: {amp: 0, left: 1200, ambient: true},
	}
	s.save("Steve", pl)

	back := testTracked()
	newInvStore(path).loadInto(back, "Steve")
	if len(back.effects) != 2 {
		t.Fatalf("both effects should come back, got %d", len(back.effects))
	}
	if e := back.effects[effSpeed]; e == nil || e.amp != 1 || e.left != 600 {
		t.Errorf("speed II with 600 ticks left: %+v", e)
	}
	if e := back.effects[effNightVision]; e == nil || !e.ambient {
		t.Errorf("the ambient flag should survive: %+v", e)
	}

	// A creative player's effects still count down.
	h := newHub(world.New(1))
	back.gamemode = gmCreative
	players := map[int32]*tracked{back.p.eid: back}
	before := back.effects[effSpeed].left
	h.updateEffects(players)
	if back.effects[effSpeed].left >= before {
		t.Error("effects tick in creative as well")
	}
}

// A panicking cow runs to random spots within five blocks (PanicGoal), not
// in a straight line away from whoever hit it, and prefers grass.
func TestPanicRunsToRandomSpots(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 183; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	h.world.SetBlock(3, 179, 3, worldgen.GrassBlock) // the one patch of grass
	players := map[int32]*tracked{}
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	grass := 0
	for i := 0; i < 200; i++ {
		x, z, ok := h.panicTarget(cow)
		if !ok {
			t.Fatal("no spot found on an open floor")
		}
		if abs64(x-cow.x) > 5.5 || abs64(z-cow.z) > 5.5 {
			t.Fatalf("spot (%v, %v) is beyond five blocks", x, z)
		}
		if x == 3.5 && z == 3.5 {
			grass++
		}
	}
	if grass == 0 {
		t.Error("the grass patch was never chosen over bare stone")
	}
}

// ArmadilloPanic: the environment unrolls a scared armadillo and sets it
// running; a blow only rolls it up.
func TestArmadilloPanicsAtTheEnvironment(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	m := h.spawnMob(players, entityArmadillo, 0.5, 180, 0.5)
	m.health = 1000
	h.armadilloSetState(players, m, armScared)
	h.hurtMobOf(players, m, 1, dtLava)
	if m.panic == 0 || m.armState != armIdle {
		t.Errorf("after lava: panic %d, state %d; want running and unrolled", m.panic, m.armState)
	}
}

// PanicGoal: a burning animal makes for water within five blocks, and a fish
// in open water flees through the water. Since the random-spot rewrite the
// burning cow ran anywhere and a panicking fish froze where it was.
func TestPanicSeeksWaterAndSwims(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	h.world.SetBlock(4, 180, 0, worldgen.Water) // a pond cell four blocks off
	players := map[int32]*tracked{}
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	cow.burning = true
	if x, z, ok := h.panicTarget(cow); !ok || x != 4.5 || z != 0.5 {
		t.Errorf("a burning cow ran for (%v, %v), want the water at (4.5, 0.5)", x, z)
	}

	// Open water, no floor within reach: a cod still finds somewhere to go.
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			for y := 170; y <= 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	cod := h.spawnSpecies(players, entityCod, dimOverworld, 0.5, 178, 0.5)
	found := 0
	for i := 0; i < 20; i++ {
		if _, _, ok := h.panicTarget(cod); ok {
			found++
		}
	}
	if found == 0 {
		t.Error("a cod in open water found nowhere to flee to")
	}
}

// Animal.getWalkTargetValue off grass is the light's pathfinding cost, so a
// panicking animal on bare stone runs for the open, lit ground rather than
// under a roof (the spot value used to be a flat zero off grass).
func TestPanicPrefersTheLight(t *testing.T) {
	h := newHub(world.New(1))
	h.dayTime.Store(6000)
	h.world.ForceLoad(0, 0, 2)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			if x <= 0 {
				h.world.SetBlock(x, 185, z, worldgen.Stone) // a roof over the west half, out of the search's reach
			}
		}
	}
	players := map[int32]*tracked{}
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	dark := 0
	for i := 0; i < 200; i++ {
		x, _, ok := h.panicTarget(cow)
		if !ok {
			t.Fatal("no spot found")
		}
		if x < -2 {
			dark++
		}
	}
	if dark > 10 {
		t.Fatalf("a panicking cow picked the dark under the roof %d times in 200", dark)
	}
}
