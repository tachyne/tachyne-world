package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// placeLine is the success line for a structure, or "" when none came.
func placeLine(lines []string, id string) string {
	for _, l := range lines {
		if strings.HasPrefix(l, `Generated structure "`+id+`" at `) {
			return l
		}
	}
	return ""
}

// /place structure builds the code-built structures with what their
// generation leaves: a swamp hut's persistent witch and black cat, buried
// treasure's stocked chest; the temples need ground above sea level, and
// the nether fossil is not built on demand.
func TestCommandPlaceCodeStructures(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"place structure minecraft:swamp_hut 3 ~ 3",
		"place structure minecraft:buried_treasure 3 ~ 3",
		"place structure minecraft:shipwreck 20 ~ 20",
		"place structure minecraft:ruined_portal_desert -10 ~ -10",
		"place structure minecraft:jungle_pyramid 20 ~ -20",
		"place structure minecraft:desert_pyramid -20 ~ 20",
		"place structure minecraft:nether_fossil 3 ~ 3",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "CS1")
	a := linesBetween(logs["alice"], "", "CS1")
	for _, id := range []string{"minecraft:swamp_hut", "minecraft:buried_treasure",
		"minecraft:shipwreck", "minecraft:ruined_portal_desert"} {
		if placeLine(a, id) == "" {
			t.Errorf("no success line for %s in\n%s", id, strings.Join(a, "\n"))
		}
	}
	for _, id := range []string{"jungle_pyramid", "desert_pyramid"} {
		if placeLine(a, "minecraft:"+id) == "" && !hasLine(a, "Failed to place structure") {
			t.Errorf("%s neither placed nor was refused: %q", id, a)
		}
	}
	if !hasLine(a, "Failed to place structure") {
		t.Errorf("the nether fossil was placed: %q", a)
	}
	onHub(t, h, func() {
		witch, blackCat := false, false
		for _, m := range h.mobs {
			if m.x < -1 || m.x > 10 || m.z < -1 || m.z > 10 {
				continue
			}
			if m.etype == entityWitch && m.persistent {
				witch = true
			}
			if m.etype == entityCat && m.persistent && m.variantSet && m.variant == catVariantID[catAllBlack] {
				blackCat = true
			}
		}
		if !witch || !blackCat {
			t.Errorf("the swamp hut's mobs: witch %v, black cat %v", witch, blackCat)
		}
		y := h.world.Gen().Height(9, 9) - 3
		pos := simPos{dim: 0, blockPos: blockPos{9, y, 9}}
		c := h.chests[pos]
		if c == nil || !isChestLikeContainer(h.world.At(9, y, 9)) {
			t.Errorf("no buried treasure chest at 9,%d,9", y) // no Fatal on the hub goroutine
			return
		}
		stocked := false
		for _, st := range c.slots {
			stocked = stocked || st.item != 0
		}
		if !stocked {
			t.Error("the buried treasure chest was not stocked")
		}
	})
}

// The big structures: the monument with its elder guardians, and the rest
// either built or refused when they reach unloaded chunks.
func TestCommandPlaceBigStructures(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	for _, c := range []string{
		"place structure minecraft:monument 3 ~ 3",
		"place structure minecraft:ocean_ruin_warm 3 ~ 3",
		"place structure minecraft:mansion 3 ~ 3",
		"place structure minecraft:fortress 3 ~ 3",
		"place structure minecraft:stronghold 3 ~ 3",
		"place structure minecraft:mineshaft 3 ~ 3",
		"place structure minecraft:end_city 3 ~ 3",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "BS1")
	a := linesBetween(logs["alice"], "", "BS1")
	if placeLine(a, "minecraft:monument") == "" {
		t.Errorf("no monument in\n%s", strings.Join(a, "\n"))
	}
	for _, id := range []string{"ocean_ruin_warm", "mansion", "fortress", "stronghold", "mineshaft", "end_city"} {
		if placeLine(a, "minecraft:"+id) == "" && !hasLine(a, "That position is not loaded") && !hasLine(a, "Failed to place structure") {
			t.Errorf("%s neither placed nor was refused: %q", id, a)
		}
	}
	onHub(t, h, func() {
		if countMobs(h, entityElderGuardian) == 0 {
			t.Error("the monument placed no elder guardian")
		}
	})
}

// A placed village carries its template entities, as placeEntities does:
// villagers among them.
func TestPlacePieceMobsVillage(t *testing.T) {
	_, h, _, _ := feedbackServer(t)
	onHub(t, h, func() {
		pieces, ok := h.world.Gen().PlaceStructurePieces("village_plains", 0, 0)
		if !ok {
			t.Error("no village layout")
			return
		}
		before := countMobs(h, entityVillager)
		h.placePieceMobs(h.playersRef, dimOverworld, "village_plains", pieces)
		if countMobs(h, entityVillager) <= before {
			t.Error("the placed village has no villagers")
		}
	})
}

// /place feature grows the configured features decoration grows: an ore
// blob in stone, a sand disk in dirt, a water spring in a wall, a monster
// room with its spawner's mob; an unmodelled feature (a noise-driven
// flower provider) is refused by name.
func TestCommandPlaceFeatureStamps(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	stone, dirt := worldgen.BlockBase("stone"), worldgen.BlockBase("dirt")
	onHub(t, h, func() {
		w := h.world
		// PlaceCommand.placeFeature checks the chunks one around the
		// position are loaded; the geode at -20, 20 reaches chunk -3.
		w.ForceLoad(-20, 20, 1)
		for x := -1; x <= 11; x++ { // a stone block for the ore
			for y := 94; y <= 106; y++ {
				for z := -1; z <= 11; z++ {
					w.SetBlock(x, y, z, stone)
				}
			}
		}
		for x := 14; x <= 26; x++ { // a dirt bed for the disk
			for z := 14; z <= 26; z++ {
				w.SetBlock(x, 120, z, dirt)
				w.SetBlock(x, 119, z, dirt)
			}
		}
		for x := 29; x <= 31; x++ { // a stone wall with one opening for the spring
			for y := 129; y <= 131; y++ {
				for z := 29; z <= 31; z++ {
					w.SetBlock(x, y, z, stone)
				}
			}
		}
		w.SetBlock(30, 130, 30, worldgen.Air)
		w.SetBlock(31, 130, 30, worldgen.Air)
		for x := -15; x <= -5; x++ { // a stone mass for the room, one gap in its ring
			for y := 149; y <= 154; y++ {
				for z := -15; z <= -5; z++ {
					w.SetBlock(x, y, z, stone)
				}
			}
		}
		for _, x := range []int{-7, -6} {
			w.SetBlock(x, 150, -10, worldgen.Air)
			w.SetBlock(x, 151, -10, worldgen.Air)
		}
	})
	for _, c := range []string{
		"place feature minecraft:ore_diamond_large 5 100 5",
		"place feature minecraft:disk_sand 20 120 20",
		"place feature minecraft:spring_water 30 130 30",
		"place feature minecraft:monster_room -10 150 -10",
		"place feature minecraft:amethyst_geode -20 0 20",
		"place feature minecraft:flower_meadow 0 100 0",
	} {
		s.handleCommand(alice, c)
	}
	settle(t, h, logs, "PF1")
	a := linesBetween(logs["alice"], "", "PF1")
	for _, want := range []string{
		`Placed "minecraft:ore_diamond_large" at 5, 100, 5`,
		`Placed "minecraft:disk_sand" at 20, 120, 20`,
		`Placed "minecraft:spring_water" at 30, 130, 30`,
		`Placed "minecraft:monster_room" at -10, 150, -10`,
		"Can't find element 'minecraft:flower_meadow' in registry 'minecraft:worldgen/feature'",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(a, "\n"))
		}
	}
	if !hasLine(a, `Placed "minecraft:amethyst_geode" at -20, 0, 20`) && !hasLine(a, "Failed to place feature") {
		t.Errorf("the geode neither placed nor failed: %q", a)
	}
	onHub(t, h, func() {
		w := h.world
		diamonds := 0
		for x := -1; x <= 11; x++ {
			for y := 94; y <= 106; y++ {
				for z := -1; z <= 11; z++ {
					if w.At(x, y, z) == worldgen.DiamondOre {
						diamonds++
					}
				}
			}
		}
		if diamonds == 0 {
			t.Error("the ore blob placed no diamond ore")
		}
		if got := w.At(20, 120, 20); got != worldgen.Sand {
			t.Errorf("the disk's centre is %d, want sand", got)
		}
		if got := w.At(30, 130, 30); got != worldgen.Water {
			t.Errorf("the spring cell is %d, want a water source", got)
		}
		if got := w.At(-10, 150, -10); got != worldgen.BlockBase("spawner") {
			t.Errorf("no spawner at the room's centre: %d", got)
			return
		}
		switch h.rules.SpawnerMobs[spawnerKey(0, -10, 150, -10)] {
		case "minecraft:zombie", "minecraft:skeleton", "minecraft:spider":
		default:
			t.Errorf("the room's spawner spawns %q", h.rules.SpawnerMobs[spawnerKey(0, -10, 150, -10)])
		}
	})
}
