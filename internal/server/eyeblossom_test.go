package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The eyeblossom follows the sun: open through the night, shut by day.
func TestEyeblossomFollowsTheSun(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}

	h.dayTime.Store(15000) // night
	w.SetBlock(pos.x, pos.y, pos.z, closedEyeblossom)
	h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, closedEyeblossom)
	if w.At(pos.x, pos.y, pos.z) != openEyeblossom {
		t.Error("an eyeblossom should open at night")
	}

	h.dayTime.Store(6000) // noon
	h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, openEyeblossom)
	if w.At(pos.x, pos.y, pos.z) != closedEyeblossom {
		t.Error("an eyeblossom should close by day")
	}

	// Anything else is left alone.
	if h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, worldgen.BlockBase("stone")) {
		t.Error("the eyeblossom tick claimed a block that is not one")
	}
}

// A nether portal breeds zombified piglins — but only in the Nether, and never
// on peaceful.
func TestNetherPortalBreedsPiglins(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	players := map[int32]*tracked{}
	h.rules.DoMobSpawning = true
	h.rules.Difficulty = diffHard
	pos := blockPos{0, 80, 0}
	nw.SetBlock(pos.x, pos.y-1, pos.z, worldgen.BlockBase("netherrack"))
	nw.SetBlock(pos.x, pos.y, pos.z, netherPortalBase)

	spawned := false
	for i := 0; i < 40000 && !spawned; i++ {
		before := len(h.mobs)
		h.tickNetherPortal(players, 1, pos.x, pos.y, pos.z, netherPortalBase)
		spawned = len(h.mobs) > before
	}
	if !spawned {
		t.Error("a nether portal on hard never bred a piglin")
	}

	// Peaceful breeds nothing.
	h.rules.Difficulty = diffPeaceful
	before := len(h.mobs)
	for i := 0; i < 20000; i++ {
		h.tickNetherPortal(players, 1, pos.x, pos.y, pos.z, netherPortalBase)
	}
	if len(h.mobs) != before {
		t.Error("a portal bred piglins on peaceful")
	}
}
