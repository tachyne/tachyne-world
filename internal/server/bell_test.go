package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// isProperHit: a floor bell rings when struck along its facing axis, not
// across it; never from above/below or high on the block; a wall bell the
// other way round; a ceiling bell from any side.
func TestBellProperHit(t *testing.T) {
	floor := withProps(t, worldgen.BlockBase("bell"), map[string]string{"attachment": "floor", "facing": "north"})
	if !bellProperHit(floor, 2, 0.5) || !bellProperHit(floor, 3, 0.5) {
		t.Error("a north-facing floor bell rings when struck from north or south")
	}
	if bellProperHit(floor, 4, 0.5) || bellProperHit(floor, 1, 0.5) || bellProperHit(floor, 2, 0.9) {
		t.Error("not from the side, from above, or high on the block")
	}
	wall := withProps(t, worldgen.BlockBase("bell"), map[string]string{"attachment": "single_wall", "facing": "north"})
	if !bellProperHit(wall, 4, 0.5) || bellProperHit(wall, 2, 0.5) {
		t.Error("a wall bell rings across its facing axis")
	}
	ceiling := withProps(t, worldgen.BlockBase("bell"), map[string]string{"attachment": "ceiling", "facing": "north"})
	if !bellProperHit(ceiling, 2, 0.5) || !bellProperHit(ceiling, 4, 0.5) {
		t.Error("a ceiling bell rings from any side")
	}
	if bellRegistryID == 0 {
		t.Error("the bell's block registry id must resolve")
	}
}

// A rung bell makes every raider within 48 blocks glow; a villager or a
// stray zombie is left alone.
func TestBellRevealsRaiders(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.SetBlock(200, 70, 200, withProps(t, worldgen.BlockBase("bell"), map[string]string{"attachment": "floor", "facing": "north"}))
	raider := h.spawnSpecies(players, entityPillager, 0, 210.5, 70, 200.5)
	raider.raidCenter = blockPos{200, 70, 200}
	far := h.spawnSpecies(players, entityPillager, 0, 300.5, 70, 200.5)
	far.raidCenter = blockPos{200, 70, 200}
	zombie := h.spawnSpecies(players, entityZombie, 0, 205.5, 70, 200.5)
	if !h.ringBell(players, 0, blockPos{200, 70, 200}, -1) {
		t.Fatal("the bell should ring")
	}
	if raider.hasEffect(effGlowing) == 0 {
		t.Error("a raider within 48 blocks should glow")
	}
	if far.hasEffect(effGlowing) != 0 || zombie.hasEffect(effGlowing) != 0 {
		t.Error("a raider 100 blocks off and a plain zombie must not glow")
	}
}

// Villagers within 32 blocks of a rung bell go and hide at their beds for
// fifteen seconds.
func TestBellSendsVillagersToHide(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.SetBlock(300, 70, 300, withProps(t, worldgen.BlockBase("bell"), map[string]string{"attachment": "floor", "facing": "north"}))
	v := h.spawnSpecies(players, entityVillager, 0, 310.5, 70, 300.5)
	v.bed = blockPos{320, 70, 300}
	far := h.spawnSpecies(players, entityVillager, 0, 340.5, 70, 300.5)
	h.ringBell(players, 0, blockPos{300, 70, 300}, -1)
	if v.hideUntil != h.tick.Load()+villagerHideTicks {
		t.Errorf("villager hideUntil %d, want %d", v.hideUntil, h.tick.Load()+villagerHideTicks)
	}
	if far.hideUntil != 0 {
		t.Error("a villager 40 blocks away does not hear the bell")
	}
}
