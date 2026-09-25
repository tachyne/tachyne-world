package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestCactusHurtsMobsAndEatsItems: CactusBlock.entityInside hurts every
// entity touching it — a cow pressed against one takes damage, and an item
// dropped on top is destroyed within five ticks. A lit campfire burns a
// mob standing in it.
func TestCactusHurtsMobsAndEatsItems(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 4, 180, 4
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.BlockBase("sand"))
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	w.SetBlock(x, y, z, worldgen.BlockBase("cactus"))
	// A cow (0.9 wide) standing with its side against the cactus's east face.
	cow := h.spawnMob(players, entityCow, float64(x)+1+0.45, float64(y), float64(z)+0.5)
	cow.spawnInvuln = 0
	before := cow.health
	h.mobContactTick(players)
	if cow.health >= before {
		t.Errorf("a cow against a cactus should be hurt: %v → %v", before, cow.health)
	}
	// An item lying on the cactus.
	it := h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+0.5, float64(y)+1, float64(z)+0.5, 0, 0, 0)
	it.x, it.y, it.z = float64(x)+0.5, float64(y)+1, float64(z)+0.5
	for i := 0; i < 5; i++ {
		h.tickItems(players)
	}
	if h.items[it.eid] != nil {
		t.Error("an item on a cactus should be destroyed within five ticks")
	}
	// A lit campfire under a pig.
	fire := worldgen.BlockBase("campfire")
	fire = setBoolProp(fire, "lit", true)
	w.SetBlock(x+3, y, z+3, fire)
	pig := h.spawnMob(players, entityPig, float64(x)+3.5, float64(y), float64(z)+3.5)
	pig.spawnInvuln = 0
	before = pig.health
	h.mobContactTick(players)
	if pig.health >= before {
		t.Errorf("a pig in a lit campfire should burn: %v → %v", before, pig.health)
	}
}
