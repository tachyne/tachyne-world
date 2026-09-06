package server

import (
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The six roster species vanilla spawns naturally that had no pool here:
// mooshroom, turtle, armadillo, camel, frog and axolotl. Pools and weights
// are the 1.21.11 biome reports; the placement rules are each species'
// SpawnPlacements predicate (checkMushroomSpawnRules and friends), which
// replace the generic Animal rule's "grass below" with the species' own
// spawnable-on block tag and, for turtles, a height cap.
//
// Axolotls are their own MobCategory (AXOLOTLS: cap 5, despawn 64) and their
// pool lives on the lush_caves CAVE biome, which is three-dimensional — hence
// the section-biome lookup rather than the surface one.

var (
	creaturePoolMushroom = []spawnerEntry{{entityMooshroom, 8, 4, 8}} // mushroom_fields: nothing else
	creaturePoolBeach    = []spawnerEntry{{entityTurtle, 5, 2, 5}}    // beach only (snowy_beach/stony_shore: empty)
	creaturePoolBadlands = append([]spawnerEntry{
		{entityArmadillo, 6, 1, 2},
	}, creaturePoolDefault...)
	creaturePoolWoodedBadlands = append([]spawnerEntry{
		{entityArmadillo, 6, 1, 2}, {entityWolf, 2, 4, 8},
	}, creaturePoolDefault...)
	creaturePoolSwamp = append([]spawnerEntry{
		{entityFrog, 10, 2, 5},
	}, creaturePoolDefault...)
	creaturePoolMangrove = []spawnerEntry{{entityFrog, 10, 2, 5}} // mangrove_swamp: frogs only

	axolotlPool = []spawnerEntry{{entityAxolotl, 10, 4, 6}} // lush_caves "axolotls" category
)

func isMushroomBiome(b string) bool { return b == "minecraft:mushroom_fields" }
func isBeachBiome(b string) bool    { return b == "minecraft:beach" }
func isBadlandsBiome(b string) bool { return strings.Contains(b, "badlands") }
func isMangroveBiome(b string) bool { return b == "minecraft:mangrove_swamp" }

// Spawnable-on block sets (block tags, expanded to state ranges so the snowy
// and waterlogged variants count).
var (
	mooshroomFloor = blockRange("mycelium")                                                     // #mooshrooms_spawnable_on
	sandFloor      = blockRange("sand", "red_sand", "suspicious_sand")                          // #sand (camels, turtles)
	frogFloor      = blockRange("grass_block", "mud", "mangrove_roots", "muddy_mangrove_roots") // #frogs_spawnable_on
	armadilloFloor = blockRange("grass_block", "red_sand", "coarse_dirt",                       // #armadillo_spawnable_on
		"terracotta", "white_terracotta", "yellow_terracotta", "orange_terracotta",
		"red_terracotta", "brown_terracotta", "light_gray_terracotta") // + #badlands_terracotta
	axolotlFloor = blockRange("clay") // #axolotls_spawnable_on
)

func inRanges2(state uint32, rs [][2]uint32) bool {
	for _, r := range rs {
		if inRange(state, r) {
			return true
		}
	}
	return false
}

// creatureFloorOK is the per-species "block below" half of the creature
// spawn rules; ok=false, handled=true means the species has its own rule and
// it failed; handled=false means the generic Animal rule applies.
func creatureFloorOK(etype int, below uint32, y int) (ok, handled bool) {
	switch etype {
	case entityMooshroom:
		return inRanges2(below, mooshroomFloor), true
	case entityTurtle: // checkTurtleSpawnRules: y < seaLevel+4 && TurtleEggBlock.onSand
		return y < worldgen.SeaLevel+4 && inRanges2(below, sandFloor), true
	case entityCamel:
		return inRanges2(below, sandFloor), true
	case entityArmadillo:
		return inRanges2(below, armadilloFloor), true
	case entityFrog:
		return inRanges2(below, frogFloor), true
	}
	return false, false
}
