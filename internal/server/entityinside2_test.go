package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestCauldronDousesAndLavaBurns: a burning player in a water cauldron is
// put out and the cauldron drops a level (emptying at one); a lava cauldron
// lights and hurts whoever stands in it.
func TestCauldronDousesAndLavaBurns(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	w.SetBlock(0, 179, 0, worldgen.Stone)
	w.SetBlock(0, 180, 0, waterCauldronBase+1) // level 2
	pl.x, pl.y, pl.z = 0.5, 180.25, 0.5
	pl.fireSecs = 5
	h.entityInsideTick(players)
	if pl.fireSecs != 0 || w.At(0, 180, 0) != waterCauldronBase {
		t.Fatalf("fire %d, cauldron %d (want out, level 1 = %d)", pl.fireSecs, w.At(0, 180, 0), waterCauldronBase)
	}
	h.entityInsideTick(players)
	if w.At(0, 180, 0) != waterCauldronBase {
		t.Fatal("a player who is not burning leaves the water alone")
	}
	pl.fireSecs = 5
	h.entityInsideTick(players)
	if pl.fireSecs != 0 || w.At(0, 180, 0) != cauldronState {
		t.Fatalf("the last level empties the cauldron: fire %d, block %d", pl.fireSecs, w.At(0, 180, 0))
	}
	// A burning mob does the same.
	w.SetBlock(0, 180, 0, waterCauldronBase+2)
	m := h.spawnMob(players, entityZombie, 0.5, 180.25, 0.5)
	m.fireSecs = 4
	h.entityInsideTick(players)
	if m.fireSecs != 0 || w.At(0, 180, 0) != waterCauldronBase+1 {
		t.Fatalf("mob: fire %d, cauldron %d", m.fireSecs, w.At(0, 180, 0))
	}
	m.dying = 1 // out of the way
	// Lava.
	w.SetBlock(0, 180, 0, lavaCauldronState)
	pl.health = 20
	h.entityInsideTick(players)
	if pl.fireSecs == 0 || pl.health >= 20 {
		t.Fatalf("lava cauldron: fire %d health %v", pl.fireSecs, pl.health)
	}
}

// TestRavagerTramplesCrops: a ravager walking through wheat flattens it.
func TestRavagerTramplesCrops(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	w.SetBlock(0, 179, 0, worldgen.BlockBase("farmland"))
	w.SetBlock(0, 180, 0, cropRanges[0][0]+3) // wheat, half grown
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	h.entityInsideTick(players)
	if !isCropState(w.At(0, 180, 0)) {
		t.Fatal("a cow does not trample wheat")
	}
	cow.dying = 1
	h.spawnMob(players, entityRavager, 0.5, 180, 0.5)
	h.entityInsideTick(players)
	if w.At(0, 180, 0) != worldgen.Air {
		t.Fatalf("the ravager should have flattened the wheat: %d", w.At(0, 180, 0))
	}
}

// TestFallingBlockSinksThroughWater: sand falls through water (and frogspawn,
// which it destroys) to the floor, as the falling-block entity does.
func TestFallingBlockSinksThroughWater(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	w.SetBlock(0, 181, 0, worldgen.Stone)
	w.SetBlock(0, 182, 0, worldgen.WaterBase)
	w.SetBlock(0, 183, 0, worldgen.WaterBase)
	w.SetBlock(0, 184, 0, frogspawnBlock)
	w.SetBlock(0, 185, 0, worldgen.Sand)
	for y := 185; y > 182; y-- {
		h.updateFalling(players, 0, blockPos{0, y, 0}, w.At(0, y, 0))
		if w.At(0, y-1, 0) != worldgen.Sand {
			t.Fatalf("sand should have dropped from %d: %d", y, w.At(0, y-1, 0))
		}
	}
	h.updateFalling(players, 0, blockPos{0, 182, 0}, worldgen.Sand)
	if w.At(0, 182, 0) != worldgen.Sand || w.At(0, 181, 0) != worldgen.Stone {
		t.Fatal("sand should rest on the stone floor")
	}
	if w.At(0, 184, 0) == frogspawnBlock {
		t.Fatal("the frogspawn should be gone")
	}
}

// TestOpenEyeblossomPoisonsBees: a bee inside an open eyeblossom is poisoned.
func TestOpenEyeblossomPoisonsBees(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.worldFor(0)
	w.SetBlock(0, 179, 0, worldgen.GrassBlock)
	w.SetBlock(0, 180, 0, openEyeblossom)
	bee := h.spawnMob(players, entityBee, 0.5, 180, 0.5)
	h.entityInsideTick(players)
	if bee.hasEffect(effPoison) == 0 {
		t.Fatal("the bee should be poisoned")
	}
	w.SetBlock(0, 180, 0, closedEyeblossom)
	bee2 := h.spawnMob(players, entityBee, 0.5, 180, 0.5)
	h.entityInsideTick(players)
	if bee2.hasEffect(effPoison) != 0 {
		t.Fatal("a closed eyeblossom does nothing")
	}
}
