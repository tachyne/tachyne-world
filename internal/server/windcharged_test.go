package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A Wind Charged zombie that dies bursts as a wind charge does: the gust
// swings the oak door beside it open (TRIGGER block interaction), which the
// hand-rolled shove it used to be never did.
func TestWindChargedDeathTriggersBlocks(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	door, ok := parseBlockState("oak_door[facing=north,half=lower,hinge=left,open=false,powered=false]")
	top, ok2 := parseBlockState("oak_door[facing=north,half=upper,hinge=left,open=false,powered=false]")
	if !ok || !ok2 {
		t.Fatal("no oak door state")
	}
	h.world.SetBlock(2, 180, 0, door)
	h.world.SetBlock(2, 181, 0, top)
	m := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	h.addMobEffect(players, m, effWindCharged, activeEffect{amp: 0, left: 600})
	h.killMob(players, m)
	if !boolProp(h.world.Block(2, 180, 0), "open") {
		t.Error("the wind-charged burst did not swing the door open")
	}
}
