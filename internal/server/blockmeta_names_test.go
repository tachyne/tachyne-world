package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Stone needs a pickaxe to drop anything, and the humblest one will do.
func TestStoneNeedsAPickaxe(t *testing.T) {
	stone := worldgen.BlockBase("stone")
	if !worldgen.HarvestableBy(stone, uint16(itemByName["wooden_pickaxe"])) {
		t.Error("stone should be harvestable with a wooden pickaxe")
	}
	if worldgen.HarvestableBy(stone, uint16(itemByName["oak_planks"])) {
		t.Error("stone should not drop for a plank")
	}
}
