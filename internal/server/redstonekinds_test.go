package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Every button and plate kind is one; wooden buttons stay down 30 ticks,
// stone ones 20; weighted plates count everything up to their weight;
// iron doors do not open by hand and each family has its own sound.
func TestRedstoneKinds(t *testing.T) {
	for _, n := range []string{"crimson_button", "cherry_button", "polished_blackstone_button", "pale_oak_button"} {
		if !isButton(worldgen.BlockID(n)) {
			t.Errorf("%s is a button", n)
		}
	}
	for _, n := range []string{"birch_pressure_plate", "warped_pressure_plate", "polished_blackstone_pressure_plate", "heavy_weighted_pressure_plate"} {
		if !isPlate(worldgen.BlockID(n)) {
			t.Errorf("%s is a plate", n)
		}
	}
	if ticks, on, _, wooden := buttonKind(worldgen.BlockID("stone_button")); ticks != 20 || wooden || on != "minecraft:block.stone_button.click_on" {
		t.Errorf("stone button: %d %v %s", ticks, wooden, on)
	}
	if ticks, on, _, wooden := buttonKind(worldgen.BlockID("crimson_button")); ticks != 30 || !wooden || on != "minecraft:block.nether_wood_button.click_on" {
		t.Errorf("crimson button: %d %v %s", ticks, wooden, on)
	}
	heavy := worldgen.BlockID("heavy_weighted_pressure_plate")
	if p := platePower(plateWith(heavy, 20)); p != 2 {
		t.Errorf("20 things on a heavy plate: ceil(20/150×15) = 2, got %d", p)
	}
	if p := platePower(plateWith(heavy, 150)); p != 15 {
		t.Errorf("150 things on a heavy plate: 15, got %d", p)
	}
	light := worldgen.BlockID("light_weighted_pressure_plate")
	if p := platePower(plateWith(light, 3)); p != 3 {
		t.Errorf("3 things on a light plate: 3, got %d", p)
	}
	if p := platePower(plateWith(worldgen.BlockID("bamboo_pressure_plate"), 1)); p != 15 {
		t.Errorf("a pressed bamboo plate powers 15, got %d", p)
	}
	if _, _, everything, on, _ := plateKind(worldgen.BlockID("stone_pressure_plate")); everything || on != "minecraft:block.stone_pressure_plate.click_on" {
		t.Error("stone plates feel living things only")
	}
	if _, _, everything, on, _ := plateKind(worldgen.BlockID("oak_pressure_plate")); !everything || on != "minecraft:block.wooden_pressure_plate.click_on" {
		t.Error("wooden plates feel everything")
	}
	if opensByHand("iron_door") || opensByHand("iron_trapdoor") || !opensByHand("copper_door") || !opensByHand("oak_trapdoor") {
		t.Error("only iron doors and trapdoors refuse the hand")
	}
	cases := map[string]string{
		"oak_door": "minecraft:block.wooden_door.open", "cherry_trapdoor": "minecraft:block.cherry_wood_trapdoor.open",
		"exposed_copper_door": "minecraft:block.copper_door.open", "warped_fence_gate": "minecraft:block.nether_wood_fence_gate.open",
		"spruce_fence_gate": "minecraft:block.fence_gate.open", "bamboo_door": "minecraft:block.bamboo_wood_door.open",
	}
	for n, want := range cases {
		if got := openCloseSound(n, true); got != want {
			t.Errorf("%s: %s, want %s", n, got, want)
		}
	}
	if got := openCloseSound("oak_door", false); got != "minecraft:block.wooden_door.close" {
		t.Errorf("close: %s", got)
	}
}

// A dropped item presses a wooden plate but not a stone one; an iron door
// clicked by hand stays shut while an oak door opens.
func TestPlatesFeelItemsAndIronDoorsRefuseTheHand(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, worldgen.BlockID("oak_pressure_plate"))
	w.SetBlock(x+2, y, z, worldgen.BlockID("stone_pressure_plate"))
	h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+0.5, float64(y), float64(z)+0.5, 0, 0, 0)
	h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x+2)+0.5, float64(y), float64(z)+0.5, 0, 0, 0)
	h.inDim(0, func() { h.updatePlatesIn(players, 0) })
	if platePower(w.At(x, y, z)) != 15 {
		t.Fatal("an item presses a wooden plate")
	}
	if platePower(w.At(x+2, y, z)) != 0 {
		t.Fatal("an item does not press a stone plate")
	}

	s, _, p := breakPlaceServer(t)
	ww := s.world
	dx, dy, dz := 1500, 180, 1500
	clearAirBox(ww, dx, dy, dz, 3)
	ww.SetBlock(dx, dy-1, dz, worldgen.Stone)
	iron := worldgen.BlockID("iron_door")
	ii, _ := worldgen.InfoForState(iron)
	ww.SetBlock(dx, dy, dz, worldgen.SetProperty(ii, iron, "half", "lower"))
	ww.SetBlock(dx, dy+1, dz, worldgen.SetProperty(ii, iron, "half", "upper"))
	s.handlePlace(p, placeBodyAt(dx, dy, dz, 3, 0.5))
	if propOf(t, ww.Block(dx, dy, dz), "open") != "false" {
		t.Fatal("an iron door must not open by hand")
	}
	oak := worldgen.BlockID("oak_door")
	oi, _ := worldgen.InfoForState(oak)
	ww.SetBlock(dx, dy, dz, worldgen.SetProperty(oi, oak, "half", "lower"))
	ww.SetBlock(dx, dy+1, dz, worldgen.SetProperty(oi, oak, "half", "upper"))
	s.handlePlace(p, placeBodyAt(dx, dy, dz, 3, 0.5))
	if propOf(t, ww.Block(dx, dy, dz), "open") != "true" || propOf(t, ww.Block(dx, dy+1, dz), "open") != "true" {
		t.Fatal("an oak door opens by hand, both halves")
	}
}

// A hopper under a chest still takes an item that lands in its own cell.
func TestHopperUnderChestTakesItemInItsCell(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1520, 180, 1520
	clearAirBox(h.world, x, y, z, 2)
	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	hopper := worldgen.BlockID("hopper")
	h.world.SetBlock(x, y, z, hopper)
	h.world.SetBlock(x, y+1, z, worldgen.BlockID("chest"))
	c := h.binAt(simPos{dim: 0, blockPos: blockPos{x, y, z}}, hopper)
	if c == nil {
		t.Fatal("no hopper bin")
	}
	h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+0.5, float64(y)+0.7, float64(z)+0.5, 0, 0, 0) // resting in the bowl (11/16)
	if !h.hopperPull(players, simPos{dim: 0, blockPos: blockPos{x, y, z}}, c) {
		t.Fatal("the hopper should take the item lying in its cell despite the chest above")
	}
	if len(h.items) != 0 || c.slots[0].item != itemByName["stick"] {
		t.Fatalf("item should be in the hopper: items=%d slot0=%+v", len(h.items), c.slots[0])
	}
}
