package server

import (
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
	// A goat rams and an armadillo rolls up: neither has a panic goal.
	if panicsAt(&mob{etype: entityGoat}, dtMobAttack) || panicsAt(&mob{etype: entityArmadillo}, dtLava) {
		t.Error("these species never panic in vanilla")
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
