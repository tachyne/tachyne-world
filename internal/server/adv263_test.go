package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The advancement table read from 26.3's data: its new advancement, the
// criteria whose contents changed, and the ones still waiting on content.

func TestAdv263Table(t *testing.T) {
	if len(advTable) != 126 {
		t.Errorf("advTable has %d advancements, 26.3 has 126", len(advTable))
	}
	// uh_oh needs a sulfur cube, which the engine has had since 2026-09-24:
	// both criteria are live and name an adult cube and TNT.
	n := advByID["minecraft:husbandry/uh_oh"]
	if n == nil || n.display == nil {
		t.Fatal("husbandry/uh_oh missing or undisplayed")
	}
	for _, c := range n.criteria {
		if c.unmatchable || c.entity != "sulfur_cube" || !c.hasBaby || c.baby != 0 {
			t.Errorf("uh_oh/%s: unmatchable=%v entity=%q baby=%v/%d", c.name, c.unmatchable, c.entity, c.hasBaby, c.baby)
		}
	}
	for _, ref := range advByTrigger["location"] {
		if ref.crit.biome == "sulfur_caves" {
			t.Error("sulfur_caves is indexed, but the generator never places it")
		}
	}
}

// The world reports registry names ("minecraft:dappled_forest"); criteria
// carry bare paths. Both forms match, on the surface, underground and in
// the Nether.
func TestAdvLocationBiomes(t *testing.T) {
	for _, tc := range []struct{ adv, crit, biome string }{
		{"minecraft:adventure/adventuring_time", "minecraft:dappled_forest", "minecraft:dappled_forest"},
		{"minecraft:adventure/adventuring_time", "minecraft:lush_caves", "minecraft:lush_caves"},
		{"minecraft:adventure/adventuring_time", "minecraft:badlands", "badlands"},
		{"minecraft:nether/explore_nether", "minecraft:crimson_forest", "minecraft:crimson_forest"},
	} {
		c := critOf(t, tc.adv, tc.crit)
		if !(advMatch{biome: tc.biome}).criterion(c) {
			t.Errorf("%s/%s: biome %q does not match", tc.adv, tc.crit, tc.biome)
		}
		if (advMatch{biome: "minecraft:plains"}).criterion(c) {
			t.Errorf("%s/%s: plains matches", tc.adv, tc.crit)
		}
	}
	meadow := critOf(t, "minecraft:adventure/play_jukebox_in_meadows", "play_jukebox_in_meadows")
	m := advMatch{blockState: worldgen.BlockBase("jukebox"), item: int32(itemByName["music_disc_cat"]), biome: "minecraft:meadow"}
	if !m.criterion(meadow) {
		t.Error("a disc played in minecraft:meadow does not match")
	}
}

// 26.3 grew three tags the criteria read: #boat (poplar), the sign blocks
// (poplar) and #piglin_loved (the golden dandelion).
func TestAdv263GrownTags(t *testing.T) {
	goat := critOf(t, "minecraft:husbandry/ride_a_boat_with_a_goat", "ride_a_boat_with_a_goat")
	if !(advMatch{vehicle: "poplar_boat", passenger: "goat"}).criterion(goat) {
		t.Error("a goat in a poplar boat does not match")
	}
	glow := critOf(t, "minecraft:husbandry/make_a_sign_glow", "make_a_sign_glow")
	if !(advMatch{blockState: worldgen.BlockBase("poplar_sign"), item: int32(itemByName["glow_ink_sac"])}).criterion(glow) {
		t.Error("glow ink on a poplar sign does not match")
	}
	dandelion := int32(itemByName["golden_dandelion"])
	if !piglinLoved[dandelion] {
		t.Error("piglins do not love the golden dandelion")
	}
	distract := critOf(t, "minecraft:nether/distract_piglin", "distract_piglin")
	if !(advMatch{entity: "piglin", item: dandelion}).criterion(distract) {
		t.Error("a piglin picking up a golden dandelion does not match")
	}
	if !piglinSafeArmor[int32(itemByName["golden_helmet"])] {
		t.Error("the golden helmet is not piglin-safe armour")
	}
}

// read_power_of_chiseled_bookshelf's shelf criterion is shelf AND a
// comparator reading it — placing a lone shelf earns nothing.
func TestAdvPlacedShelfNeedsComparator(t *testing.T) {
	crit := critOf(t, "minecraft:adventure/read_power_of_chiseled_bookshelf", "chiseled_bookshelf")
	shelf := worldgen.BlockBase("chiseled_bookshelf")
	world := map[[3]int]uint32{{0, 0, 0}: shelf}
	at := func(dx, dy, dz int) uint32 { return world[[3]int{dx, dy, dz}] }
	if (advMatch{blockState: shelf, blockAt: at}).criterion(crit) {
		t.Error("a lone chiseled bookshelf matches")
	}
	world[[3]int{0, 0, 1}] = withProps(t, worldgen.BlockBase("comparator"), map[string]string{"facing": "north"})
	if !(advMatch{blockState: shelf, blockAt: at}).criterion(crit) {
		t.Error("a shelf with a comparator reading it does not match")
	}
	stone := worldgen.Stone
	world[[3]int{0, 0, 0}] = stone
	if (advMatch{blockState: stone, blockAt: at}).criterion(crit) {
		t.Error("stone beside a comparator matches")
	}
}
