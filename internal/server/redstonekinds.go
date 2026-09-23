package server

import (
	"math"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Every kind of button, pressure plate, door, trapdoor and fence gate, by
// its BlockSetType: what it sounds like, how long a button stays pressed
// (stone 20 ticks, wood 30), what a plate feels (stone-like plates feel
// living things, wooden and weighted plates feel everything, weighted
// plates count it), and whether a door opens by hand (iron does not).

var buttonRanges = worldgen.BlockTag("buttons")

var plateRanges = worldgen.BlockTag("pressure_plates")

func isButton(s uint32) bool { return inRanges2(s, buttonRanges) }
func isPlate(s uint32) bool  { return inRanges2(s, plateRanges) }

// setTypeOf names a block's BlockSetType sound family: "stone", "metal",
// "copper", "iron", "cherry_wood", "bamboo_wood", "nether_wood" or "wooden".
func setTypeOf(name string) string {
	switch {
	case strings.HasPrefix(name, "stone_"), strings.HasPrefix(name, "polished_blackstone_"):
		return "stone"
	case strings.HasPrefix(name, "light_weighted_"), strings.HasPrefix(name, "heavy_weighted_"):
		return "metal"
	case strings.Contains(name, "copper"):
		return "copper"
	case strings.HasPrefix(name, "iron_"):
		return "iron"
	case strings.HasPrefix(name, "cherry_"):
		return "cherry_wood"
	case strings.HasPrefix(name, "bamboo_"):
		return "bamboo_wood"
	case strings.HasPrefix(name, "crimson_"), strings.HasPrefix(name, "warped_"):
		return "nether_wood"
	}
	return "wooden"
}

// buttonKind is a button's press length and click sounds.
func buttonKind(s uint32) (ticks int, on, off string, wooden bool) {
	name, _ := worldgen.StateName(s)
	st := setTypeOf(name)
	if st == "stone" {
		return 20, "minecraft:block.stone_button.click_on", "minecraft:block.stone_button.click_off", false
	}
	return 30, "minecraft:block." + st + "_button.click_on", "minecraft:block." + st + "_button.click_off", true
}

// plateKind is a plate's sensitivity and sounds: weighted plates count
// everything up to maxWeight (15 light, 150 heavy), wooden plates feel
// everything (items and arrows too), stone-like plates feel living things.
func plateKind(s uint32) (weighted bool, maxWeight int, everything bool, on, off string) {
	name, _ := worldgen.StateName(s)
	st := setTypeOf(name)
	switch st {
	case "metal":
		maxWeight = 15
		if strings.HasPrefix(name, "heavy") {
			maxWeight = 150
		}
		return true, maxWeight, true, "minecraft:block.metal_pressure_plate.click_on", "minecraft:block.metal_pressure_plate.click_off"
	case "stone":
		return false, 0, false, "minecraft:block.stone_pressure_plate.click_on", "minecraft:block.stone_pressure_plate.click_off"
	}
	return false, 0, true, "minecraft:block." + st + "_pressure_plate.click_on", "minecraft:block." + st + "_pressure_plate.click_off"
}

// platePower is a plate's signal: a weighted plate's power, else 15 when pressed.
func platePower(s uint32) int {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return 0
	}
	if info.HasProperty("power") {
		return atoi(worldgen.GetProperty(info, s, "power"))
	}
	if worldgen.GetProperty(info, s, "powered") == "true" {
		return 15
	}
	return 0
}

// plateWith is the plate state for a number of things on it: pressed or
// not, or for a weighted plate ceil(min(n, maxWeight) / maxWeight × 15).
func plateWith(s uint32, count int) uint32 {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return s
	}
	if info.HasProperty("power") {
		_, maxW, _, _, _ := plateKind(s)
		n := count
		if n > maxW {
			n = maxW
		}
		p := int(math.Ceil(float64(n) / float64(maxW) * 15))
		return worldgen.SetProperty(info, s, "power", strconv.Itoa(p))
	}
	if count > 0 {
		return worldgen.SetProperty(info, s, "powered", "true")
	}
	return worldgen.SetProperty(info, s, "powered", "false")
}

// opensByHand is BlockSetType.canOpenByHand: iron doors and trapdoors
// answer only to redstone.
func opensByHand(name string) bool { return !strings.HasPrefix(name, "iron_") }

// openCloseSound is the door, trapdoor or fence gate's open/close sound
// for its set type.
func openCloseSound(name string, open bool) string {
	oc := "close"
	if open {
		oc = "open"
	}
	st := setTypeOf(name)
	switch {
	case strings.HasSuffix(name, "_fence_gate"):
		if st == "wooden" {
			return "minecraft:block.fence_gate." + oc
		}
		return "minecraft:block." + st + "_fence_gate." + oc
	case strings.HasSuffix(name, "_trapdoor"):
		return "minecraft:block." + st + "_trapdoor." + oc
	}
	return "minecraft:block." + st + "_door." + oc
}

// evBlockSound plays a block's sound to everyone near it but the player
// who caused it (whose client already played it, as vanilla's does).
type evBlockSound struct {
	eid     int32
	dim     int
	x, y, z int
	name    string
	// volume and pitch as the vanilla call site passes them. A zero volume
	// asks for the door-and-button voice instead — full volume and the
	// 0.9-1.0 pitch jitter DoorBlock.playSound gives those.
	volume, pitch float32
}

func (evBlockSound) isHubEvent() {}
