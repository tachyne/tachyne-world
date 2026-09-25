package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BaseFireBlock.entityInside is reached through the entity's whole box: a
// player or a mob whose box leans into a fire's cell burns, not only one
// whose feet or head cell holds it.
func TestFireTouchesTheWholeBox(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	for x := -2; x <= 3; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			h.world.SetBlock(x, 180, z, worldgen.Air)
			h.world.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	h.world.SetBlock(1, 180, 0, fireDefault)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.85, 180, 0.5 // box 0.55..1.15 reaches into x=1
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	hp := pl.health
	h.playerContactTick(players)
	if pl.health >= hp || pl.fireSecs == 0 {
		t.Fatalf("a player leaning into the fire's cell should burn: health %v→%v fire %d", hp, pl.health, pl.fireSecs)
	}
	cow := h.spawnMob(players, entityCow, 0.3, 180, 0.5) // feet cell x=0, box ±0.45 reaches x=-1
	h.world.SetBlock(1, 180, 0, worldgen.Air)
	h.world.SetBlock(-1, 180, 0, fireDefault)
	mhp := cow.health
	h.mobContactTick(players)
	if cow.health >= mhp {
		t.Fatal("a cow leaning into the fire's cell should burn")
	}
}
