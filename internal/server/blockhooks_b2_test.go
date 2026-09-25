package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BeehiveBlock.onExplosionHit: a blast that reaches a hive sets the bees
// near it on a player near it.
func TestBlastAtAHiveAngersTheBees(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	const x, y, z = 80, 180, 80
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.BlockID("beehive"))
	near := &tracked{p: newPlayer(11, "keeper", [16]byte{}), gamemode: gmSurvival, x: 83.5, y: 180, z: 80.5}
	players := map[int32]*tracked{11: near}
	bee := h.spawnMob(players, entityBee, 82.5, 182, 82.5)
	h.explodeTyped(players, 0, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1, blastTNT, dtExplosion, deathCause{})
	if bee.targetEID != near.p.eid {
		t.Fatalf("the bee's target is %d, want the player beside the hive (%d)", bee.targetEID, near.p.eid)
	}
}

// BlockBehaviour.onExplosionHit: ore blown up by a player's TNT drops its
// experience; by a creeper, none.
func TestPlayerBlastDropsOreExperience(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	const x, y, z = 90, 180, 90
	by := &tracked{p: newPlayer(12, "miner", [16]byte{}), gamemode: gmSurvival, x: 100, y: 180, z: 100}
	players := map[int32]*tracked{12: by}
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.BlockID("diamond_ore"))
	h.explodeTyped(players, 0, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 4, 4, blastTNT, dtExplosion, deathCause{},
		withBlastCause(12, false))
	if len(h.orbs) == 0 {
		t.Fatal("diamond ore blown up by a player's TNT left no experience")
	}
	for id := range h.orbs {
		delete(h.orbs, id)
	}
	w.SetBlock(x, y, z, worldgen.BlockID("diamond_ore"))
	h.explodeTyped(players, 0, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 4, 4, blastTNT, dtExplosion, deathCause{})
	if len(h.orbs) != 0 {
		t.Fatal("ore blown up with no player behind it dropped experience")
	}
}

// TurtleEggBlock.fallOn: a mob landing on a clutch may break an egg; a
// zombie never does.
func TestMobLandingOnTurtleEggs(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 104, 180, 104
	eggs := worldgen.BlockID("turtle_egg")
	ei, _ := worldgen.InfoForState(eggs)
	four := worldgen.SetProperty(ei, eggs, "eggs", "4")
	w.SetBlock(x, y-1, z, worldgen.BlockID("sand"))
	w.SetBlock(x, y, z, four)
	z1 := h.spawnMob(players, entityZombie, float64(x)+0.5, float64(y)+0.4375, float64(z)+0.5)
	for i := 0; i < 60; i++ {
		h.mobFallOnEgg(players, z1)
	}
	if w.At(x, y, z) != four {
		t.Fatal("a zombie landing broke an egg")
	}
	cow := h.spawnMob(players, entityCow, float64(x)+0.5, float64(y)+0.4375, float64(z)+0.5)
	for i := 0; i < 60 && w.At(x, y, z) == four; i++ {
		h.mobFallOnEgg(players, cow)
	}
	if w.At(x, y, z) == four {
		t.Fatal("sixty cow landings never broke an egg")
	}
}
