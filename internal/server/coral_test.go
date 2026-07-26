package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Every live coral has a dead twin, and the mapping keeps the block's own
// state offset (a wall fan must not lose its facing on the way).
func TestCoralDeadMappingCoversTheFamilies(t *testing.T) {
	if len(coralDead) < 40 {
		t.Fatalf("only %d coral states mapped — the families are incomplete", len(coralDead))
	}
	live, _, _ := worldgen.BlockRangeOK("tube_coral_wall_fan")
	dead, _, _ := worldgen.BlockRangeOK("dead_tube_coral_wall_fan")
	if coralDead[live+2] != dead+2 {
		t.Error("the state offset is not carried across, so facing would be lost")
	}
}

// Coral out of water bleaches; coral with water beside it does not.
func TestCoralDiesOutOfWater(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	live, _, _ := worldgen.BlockRangeOK("tube_coral_block")
	dead, _, _ := worldgen.BlockRangeOK("dead_tube_coral_block")

	dry := blockPos{0, 180, 0}
	w.SetBlock(dry.x, dry.y, dry.z, live)
	h.tickCoral(players, 0, dry, live)
	if w.At(dry.x, dry.y, dry.z) != dead {
		t.Error("coral in open air should have bleached")
	}

	wet := blockPos{10, 180, 0}
	w.SetBlock(wet.x, wet.y, wet.z, live)
	w.SetBlock(wet.x+1, wet.y, wet.z, worldgen.WaterBase)
	h.tickCoral(players, 0, wet, live)
	if w.At(wet.x, wet.y, wet.z) != live {
		t.Error("coral with water beside it should live")
	}
}

// Taking the water away arms the die tick on the coral next to it.
func TestRemovingWaterSchedulesCoralDeath(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	// The BASE state of a coral plant is waterlogged=true — the first value of
	// the property — and waterlogged coral never dies. The one that can dry out
	// is the next state along.
	base, _, _ := worldgen.BlockRangeOK("tube_coral")
	live := base + 1
	pos := blockPos{0, 180, 0}
	w.SetBlock(pos.x, pos.y, pos.z, live)
	w.SetBlock(pos.x+1, pos.y, pos.z, worldgen.WaterBase)

	before := len(h.pending)
	h.scheduleCoralDeath(0, blockPos{pos.x + 1, pos.y, pos.z})
	if len(h.pending) != before {
		t.Error("coral still beside water should not be scheduled to die")
	}
	// A waterlogged plant survives even with no water around it.
	w.SetBlock(pos.x, pos.y, pos.z, base)
	w.SetBlock(pos.x+1, pos.y, pos.z, worldgen.Air)
	h.scheduleCoralDeath(0, blockPos{pos.x + 1, pos.y, pos.z})
	if len(h.pending) != before {
		t.Error("waterlogged coral was scheduled to die")
	}
	w.SetBlock(pos.x, pos.y, pos.z, live)
	w.SetBlock(pos.x+1, pos.y, pos.z, worldgen.WaterBase)

	w.SetBlock(pos.x+1, pos.y, pos.z, worldgen.Air)
	h.scheduleCoralDeath(0, blockPos{pos.x + 1, pos.y, pos.z})
	if len(h.pending) == before {
		t.Error("coral left dry should have been scheduled to die")
	}
}
