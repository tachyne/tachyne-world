package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// countItems counts the dropped item entities of one kind.
func countItems(h *hub, item int32) int {
	n := 0
	for _, it := range h.items {
		if it.item == item {
			n += it.count
		}
	}
	return n
}

// A comparator reads a candle cake as a whole cake, lit or unlit.
func TestCandleCakeComparatorReadsWholeCake(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{0, 180, 0}
	base := worldgen.BlockBase("red_candle_cake")
	for _, st := range []uint32{base, base + 1} { // lit, unlit
		h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, st)
		if got := h.analogSignal(simPos{blockPos: pos}); got != 14 {
			t.Fatalf("candle cake state %d reads %d, want 14", st, got)
		}
	}
}

// A ravager tramples a grown pitcher crop, both halves, and it pays out the
// plant; without mob griefing it walks through.
func TestRavagerTramplesPitcherCrop(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	w.ForceLoad(0, 0, 2)
	w.SetBlock(0, 179, 0, worldgen.BlockBase("farmland"))
	w.SetBlock(0, 180, 0, pitcherLower(4))
	w.SetBlock(0, 181, 0, pitcherUpper(4))

	h.rules.MobGriefing = false
	h.spawnMob(players, entityRavager, 0.5, 180, 0.5)
	h.insideBoth(players)
	if w.At(0, 180, 0) != pitcherLower(4) {
		t.Fatal("no mob griefing: the pitcher should stand")
	}

	h.rules.MobGriefing = true
	h.insideBoth(players)
	if w.At(0, 180, 0) != worldgen.Air || w.At(0, 181, 0) != worldgen.Air {
		t.Fatalf("the ravager should flatten both halves: %d / %d", w.At(0, 180, 0), w.At(0, 181, 0))
	}
	if countItems(h, int32(itemByName["pitcher_plant"])) != 1 {
		t.Fatal("a trampled full-grown pitcher drops its plant")
	}
}

// A survival player mining a clutch of turtle eggs takes one and leaves the
// rest; the last egg goes, and creative takes the lot.
func TestPlayerBreakTakesOneTurtleEgg(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	s.modes.set(p.name, gmSurvival)
	x, y, z := 3, 180, 3
	dig := func() {
		s.handleDig(p, digBody(digStartBreak, x, y, z))
		p.digStartAt = h.tick.Load() - 1000 // long enough by any tool
		s.handleDig(p, digBody(digFinishBreak, x, y, z))
	}

	w.SetBlock(x, y, z, turtleEggState(3, 1))
	dig()
	if got := w.Block(x, y, z); got != turtleEggState(2, 1) {
		t.Fatalf("three eggs mined once: state %d, want two eggs (%d)", got, turtleEggState(2, 1))
	}
	w.SetBlock(x, y, z, turtleEggState(1, 0))
	dig()
	if got := w.Block(x, y, z); got != worldgen.Air {
		t.Fatalf("the last egg should go: state %d", got)
	}

	s.modes.set(p.name, gmCreative)
	w.SetBlock(x, y, z, turtleEggState(4, 0))
	s.handleDig(p, digBody(digStartBreak, x, y, z))
	if got := w.Block(x, y, z); got != worldgen.Air {
		t.Fatalf("creative breaks the whole clutch: state %d", got)
	}
}

// hiveFixture puts an occupied beehive at (0,180,0) on a stone floor.
func hiveFixture(t *testing.T) (*hub, map[int32]*tracked, blockPos) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	pos := blockPos{0, 180, 0}
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	return h, players, pos
}

func placeHive(h *hub, pos blockPos, bees int) {
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.BlockBase("beehive"))
	h.registerHive(pos)
	for i := 0; i < bees; i++ {
		h.hives[pos] = append(h.hives[pos], hiveOccupant{SecsLeft: 100})
	}
}

// TNT (and creepers, the wither, its skulls) blow a hive's bees out rather
// than killing them with it; any other blast takes them with the hive.
func TestExplosionReleasesHiveBees(t *testing.T) {
	h, players, pos := hiveFixture(t)
	placeHive(h, pos, 2)
	h.explodeIn(players, dimOverworld, 0.5, 180.5, 0.5, 4, 4, blastTNT, withHiveRelease())
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Fatal("the blast should take the hive")
	}
	if n := countMobs(h, entityBee); n != 2 {
		t.Fatalf("TNT should let both bees out, got %d", n)
	}
	if _, known := h.hives[pos]; known {
		t.Fatal("the destroyed hive is still on the books")
	}

	for _, m := range h.mobs {
		h.despawnMob(players, m)
	}
	placeHive(h, pos, 2)
	h.explodeIn(players, dimOverworld, 0.5, 180.5, 0.5, 4, 4, blastBlock)
	if n := countMobs(h, entityBee); n != 0 {
		t.Fatalf("a bed's blast takes the bees with the hive, %d came out", n)
	}
	if _, known := h.hives[pos]; known {
		t.Fatal("a hive lost to a bed blast would tip its bees out on the next sweep")
	}
}

// Fire appearing beside a hive empties it; soul fire does not.
func TestFireBesideHiveEmptiesIt(t *testing.T) {
	h, players, pos := hiveFixture(t)
	placeHive(h, pos, 3)
	h.setBlockAt(players, dimOverworld, blockPos{pos.x + 1, pos.y, pos.z}, soulFire)
	if countMobs(h, entityBee) != 0 {
		t.Fatal("soul fire is not FireBlock: the hive should keep its bees")
	}
	h.setBlockAt(players, dimOverworld, blockPos{pos.x - 1, pos.y, pos.z}, fireDefault)
	if n := countMobs(h, entityBee); n != 3 {
		t.Fatalf("fire beside the hive should turn all three out, got %d", n)
	}
	if len(h.hives[pos]) != 0 {
		t.Fatal("the hive should be empty")
	}
}

// An impact projectile breaks a chorus flower and it drops itself; a mob's
// shot needs mob griefing, and a bottle o' enchanting is no impact projectile.
func TestProjectileBreaksChorusFlower(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}
	flower := worldgen.BlockBase("chorus_flower")
	reset := func() {
		w.SetBlock(0, 179, 0, worldgen.BlockBase("end_stone"))
		w.SetBlock(pos.x, pos.y, pos.z, flower)
	}

	reset()
	h.projectileHitBlock(players, &arrowEntity{etype: entityXPBottle}, pos, flower)
	if w.At(pos.x, pos.y, pos.z) != flower {
		t.Fatal("a bottle o' enchanting is not an impact projectile")
	}

	skel := h.spawnMob(players, entitySkeleton, 5.5, 180, 5.5)
	h.rules.MobGriefing = false
	h.projectileHitBlock(players, &arrowEntity{etype: entityArrow, shooter: skel.eid}, pos, flower)
	if w.At(pos.x, pos.y, pos.z) != flower {
		t.Fatal("a skeleton's arrow needs mob griefing to break it")
	}

	h.rules.MobGriefing = true
	h.projectileHitBlock(players, &arrowEntity{etype: entityArrow, shooter: skel.eid}, pos, flower)
	if w.At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Fatal("the arrow should break the chorus flower")
	}
	if countItems(h, itemChorusFlower) != 1 {
		t.Fatal("a shot chorus flower drops itself")
	}

	reset()
	h.rules.ProjectilesBreak = false
	h.projectileHitBlock(players, &arrowEntity{etype: entitySnowball}, pos, flower)
	if w.At(pos.x, pos.y, pos.z) != flower {
		t.Fatal("projectiles_can_break_blocks off: the flower stays")
	}
}

// A boat breaks the lily pads its box reaches into, and only those.
func TestBoatBreaksLilyPads(t *testing.T) {
	h, players, _ := boatFixture(t)
	lily := worldgen.BlockBase("lily_pad")
	for x := -2; x <= 3; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 99, z, worldgen.WaterBase) // a pad needs water under it
		}
	}
	for x := 0; x <= 2; x++ {
		h.world.SetBlock(x, 100, 0, lily)
	}
	h.updateVehicles(players)
	if h.world.At(0, 100, 0) != worldgen.Air || h.world.At(1, 100, 0) != worldgen.Air {
		t.Fatal("the pads under the boat should break")
	}
	if h.world.At(2, 100, 0) != lily {
		t.Fatal("a pad clear of the boat's box should stay")
	}
	if countItems(h, int32(itemByName["lily_pad"])) != 2 {
		t.Fatal("each broken pad drops itself")
	}
}

// copperChest builds a copper chest state of the named block.
func copperChest(t *testing.T, name, facing, ctype string) uint32 {
	t.Helper()
	base := worldgen.BlockBase(name)
	info, ok := worldgen.InfoForState(base)
	if !ok {
		t.Fatalf("%s has no block info", name)
	}
	return worldgen.SetProperty(info, worldgen.SetProperty(info, base, "facing", facing), "type", ctype)
}

// Copper chests pair across oxidation and waxing, and the pair becomes the
// less oxidized of the two — unwaxed unless both were waxed.
func TestCopperChestsPairAcrossOxidation(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := h.world
	blockOf := func(st uint32) uint32 {
		info, _ := worldgen.InfoForState(st)
		return info.Min
	}
	onHub(t, h, func() {
		cases := []struct{ existing, placed, want string }{
			{"waxed_weathered_copper_chest", "exposed_copper_chest", "exposed_copper_chest"},
			{"waxed_oxidized_copper_chest", "waxed_exposed_copper_chest", "waxed_exposed_copper_chest"},
			{"copper_chest", "oxidized_copper_chest", "copper_chest"},
		}
		for i, c := range cases {
			z := i * 3
			w.SetBlock(1, 64, z, copperChest(t, c.existing, "north", "single"))
			placed := s.pairChestOnPlace(p, 0, 64, z, copperChest(t, c.placed, "north", "single"))
			partner := w.At(1, 64, z)
			if chestType(placed) != "left" || chestType(partner) != "right" {
				t.Errorf("%s beside %s did not pair: %q / %q", c.placed, c.existing, chestType(placed), chestType(partner))
				return
			}
			want := worldgen.BlockBase(c.want)
			if blockOf(placed) != want || blockOf(partner) != want {
				t.Errorf("%s + %s: halves %d / %d, want both %s (%d)", c.placed, c.existing,
					blockOf(placed), blockOf(partner), c.want, want)
				return
			}
		}
		// Wood still only pairs with wood.
		w.SetBlock(1, 64, 20, copperChest(t, "copper_chest", "north", "single"))
		if placed := s.pairChestOnPlace(p, 0, 64, 20, chestState(t, "north", "single")); chestType(placed) != "single" {
			t.Errorf("a wooden chest paired with a copper one")
		}
	})
}

// A head clicked onto a note block's top face goes on top instead of tuning
// it; on a side face, or with anything else in hand, the click tunes.
func TestNoteBlockLetsHeadOntoItsTop(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	x, y, z := 4, 180, 4
	s.world.SetBlock(x, y, z, noteBlockBase)
	head := itemByName["zombie_head"]
	p.setHotbarSlot(0, head)
	p.held = 0
	if s.tryUseBlock(p, x, y, z, 0, 1, 0.5, 1, 0.5) {
		t.Fatal("a head on the top face should pass through to placement")
	}
	if !s.tryUseBlock(p, x, y, z, 0, 2, 0.5, 0.5, 0) {
		t.Fatal("a head on a side face still tunes the block")
	}
	p.setHotbarSlot(0, itemByName["stick"])
	if !s.tryUseBlock(p, x, y, z, 0, 1, 0.5, 1, 0.5) {
		t.Fatal("anything but a head tunes it from the top")
	}
}

// A head's POWERED follows the redstone beside it, floor or wall.
func TestSkullPoweredFollowsSignal(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		for i, name := range []string{"dragon_head", "piglin_wall_head"} {
			pos := blockPos{5 + i*4, 70, 5}
			w.SetBlock(pos.x, pos.y, pos.z, setBoolProp(worldgen.BlockBase(name), "powered", false))
			w.SetBlock(pos.x+1, pos.y, pos.z, redstoneBlock)
			h.updateRedstone(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
			if !boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
				t.Errorf("%s did not power up beside a redstone block", name)
				return
			}
			w.SetBlock(pos.x+1, pos.y, pos.z, worldgen.Air)
			h.updateRedstone(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
			if boolProp(w.At(pos.x, pos.y, pos.z), "powered") {
				t.Errorf("%s stayed powered with the signal gone", name)
				return
			}
		}
	})
}

// CopperChestBlock.updateShape: when one half of a pair weathers (or is
// waxed or scraped), the other half becomes the same block.
func TestCopperChestHalvesWeatherTogether(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	left := copperChest(t, "copper_chest", "north", "left")
	w.SetBlock(0, 64, 0, left)
	w.SetBlock(1, 64, 0, copperChest(t, "exposed_copper_chest", "north", "right"))
	got, ok := shapeUpdated(w, blockPos{0, 64, 0}, left, [3]int{1, 0, 0})
	if !ok || got != copperChest(t, "exposed_copper_chest", "north", "left") {
		t.Errorf("the left half did not follow its partner: %d", got)
	}
	if got, _ := shapeUpdated(w, blockPos{0, 64, 0}, left, [3]int{-1, 0, 0}); got != left {
		t.Error("a change on the unpaired side changed the chest")
	}
}
