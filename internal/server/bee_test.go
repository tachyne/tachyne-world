package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The real hive cycle (#138 M2): a bee pollinates a flower, carries the
// nectar home, waits out its stay inside, and the hive gains a honey level
// as it leaves. Robbing an occupied hive without smoke throws the bees out
// angry, and a sting is the last thing a bee does.

func beeWorld(t *testing.T) (*hub, map[int32]*tracked, blockPos) {
	t.Helper()
	h := newHub(world.New(1))
	h.hivestore = newHiveStore("") // in-memory
	h.hivesLoad()
	players := map[int32]*tracked{}
	nest := blockPos{2000, 200, 2000}
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			h.world.SetBlock(nest.x+dx, nest.y-2, nest.z+dz, worldgen.Dirt)
		}
	}
	h.world.SetBlock(nest.x, nest.y, nest.z, worldgen.BlockBase("bee_nest")+6)
	h.registerHive(nest)
	return h, players, nest
}

func TestBeeCycleFillsTheHive(t *testing.T) {
	h, players, nest := beeWorld(t)
	flower := blockPos{nest.x + 4, nest.y - 1, nest.z}
	h.world.SetBlock(flower.x, flower.y, flower.z, worldgen.BlockBase("poppy"))
	m := h.spawnAnimal(players, entityBee, flower.x, flower.z)
	if m == nil {
		t.Fatal("no bee spawned")
	}
	m.x, m.y, m.z = float64(flower.x)+0.5, float64(flower.y)+0.5, float64(flower.z)+0.5
	// Forage: within a few seconds the bee picks the flower and hovers it out.
	for i := 0; i < 60 && !m.beeNectar; i++ {
		h.updateBees(players)
	}
	if !m.beeNectar {
		t.Fatal("the bee never gathered nectar beside a poppy")
	}
	// Home: put it beside the nest and let it enter.
	m.x, m.y, m.z = float64(nest.x)+0.5, float64(nest.y)+0.5, float64(nest.z)+1.5
	m.beeNoEnter = 0
	for i := 0; i < 30 && h.mobs[m.eid] != nil; i++ {
		h.updateBees(players)
	}
	if h.mobs[m.eid] != nil {
		t.Fatal("the bee never entered its hive")
	}
	if len(h.hives[nest]) != 1 || !h.hives[nest][0].Nectar {
		t.Fatalf("hive occupants %v, want one carrying nectar", h.hives[nest])
	}
	// The stay: it leaves with the honey delivered.
	for i := 0; i < beeOccupySecs+5 && len(h.hives[nest]) > 0; i++ {
		h.updateBees(players)
	}
	if len(h.hives[nest]) != 0 {
		t.Fatal("the occupant never finished its stay")
	}
	if lvl := honeyLevel(h.world.At(nest.x, nest.y, nest.z)); lvl != 1 {
		t.Fatalf("hive honey level %d after a nectar delivery, want 1", lvl)
	}
	bees := 0
	for _, mm := range h.mobs {
		if mm.etype == entityBee {
			bees++
		}
	}
	if bees != 1 {
		t.Fatalf("%d bees in the world after the release, want the one", bees)
	}
}

func TestRobbedHiveThrowsAngryBeesOut(t *testing.T) {
	h, players, nest := beeWorld(t)
	h.world.SetBlock(nest.x, nest.y, nest.z, withHoney(worldgen.BlockBase("bee_nest")+6, beeMaxHoney))
	h.hives[nest] = []hiveOccupant{{SecsLeft: 100}, {SecsLeft: 100, Nectar: true}}
	t2 := survPlayer(h)
	players[t2.p.eid] = t2
	t2.x, t2.y, t2.z = float64(nest.x), float64(nest.y), float64(nest.z)+2
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["shears"]), count: 1}
	if !h.harvestBeeHome(players, t2, nest) {
		t.Fatal("harvest did nothing on a full hive")
	}
	if len(h.hives[nest]) != 0 {
		t.Fatalf("robbed hive still holds %v", h.hives[nest])
	}
	angry := 0
	for _, m := range h.mobs {
		if m.etype == entityBee && m.anger > 0 {
			angry++
		}
	}
	if angry != 2 {
		t.Fatalf("%d angry bees out of a robbed hive, want 2", angry)
	}
}

func TestStingIsABeesLastAct(t *testing.T) {
	h, players, _ := beeWorld(t)
	m := h.spawnAnimal(players, entityBee, 2010, 2010)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeStingDie = 2
	h.updateBees(players)
	h.updateBees(players)
	if mm := h.mobs[m.eid]; mm != nil && mm.dying == 0 && mm.health > 0 {
		t.Fatalf("a stung-out bee should be dead or dying (health %d)", mm.health)
	}
}

// The client-visible bee (#138 M3): the flags byte (index 17: 0x08 pollen
// coat, 0x04 lost stinger) and the anger time (index 18: red eyes) are synced
// metadata, diffed once a second — whatever code path changed the state.
func TestBeeLookMetadataFollowsState(t *testing.T) {
	// The wire shape first: eid, index, type, value, terminator.
	if got := beeFlagsMeta(5, beeFlagNectar); string(got) != string([]byte{5, 17, 0, 8, 0xff}) {
		t.Fatalf("beeFlagsMeta bytes = %v", got)
	}
	if got := beeAngerMeta(5, 30); string(got) != string([]byte{5, 18, 1, 30, 0xff}) {
		t.Fatalf("beeAngerMeta bytes = %v", got)
	}
	h, players, _ := beeWorld(t)
	m := h.spawnAnimal(players, entityBee, 2030, 2030)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeNoEnter = 1 << 20
	h.updateBees(players)
	if m.beeSentFlags != 0 || m.beeSentAngry {
		t.Fatalf("a fresh bee synced flags %#x angry %v", m.beeSentFlags, m.beeSentAngry)
	}
	m.beeNectar = true
	h.updateBees(players)
	if m.beeSentFlags != beeFlagNectar {
		t.Fatalf("pollen-laden bee synced flags %#x, want %#x", m.beeSentFlags, beeFlagNectar)
	}
	m.beeStingDie = 30
	m.anger = 5
	h.updateBees(players)
	if m.beeSentFlags != beeFlagNectar|beeFlagStung || !m.beeSentAngry {
		t.Fatalf("stung+angry bee synced flags %#x angry %v", m.beeSentFlags, m.beeSentAngry)
	}
	m.anger = 0
	h.updateBees(players)
	if m.beeSentAngry {
		t.Fatal("a calmed bee still syncs angry")
	}
}

// BeeGrowCropGoal's growth table, one state at a time.
func TestBeeGrowTargetTable(t *testing.T) {
	wheat := worldgen.BlockBase("wheat")
	check := func(name string, in uint32, want uint32, ok bool) {
		t.Helper()
		got, gotOK := beeGrowTarget(in)
		if gotOK != ok || (ok && got != want) {
			t.Errorf("%s: beeGrowTarget(%d) = %d,%v want %d,%v", name, in, got, gotOK, want, ok)
		}
	}
	check("wheat age0", wheat, wheat+1, true)
	check("wheat max", wheat+7, 0, false)
	check("melon stem age6", melonStemBase+6, melonStemBase+7, true)
	check("melon stem max", melonStemBase+7, 0, false)
	check("berry age2", berryBase+2, berryBase+3, true)
	check("berry ripe", berryBase+3, 0, false)
	// Torchflower: the step past the last crop state is the flower itself.
	check("torchflower crop", torchflowerCropMin, torchflowerCropMin+1, true)
	check("torchflower final", torchflowerCropMax, torchflowerBlock, true)
	// Pitcher crops are tagged #bee_growables but are not CropBlocks: skipped.
	check("pitcher upper", pitcherUpper(0), 0, false)
	check("pitcher lower", pitcherLower(0), 0, false)
	// Cave vines: berries=true is the first of each state pair.
	check("cave vine bare", caveVinesLo+1, caveVinesLo, true)
	check("cave vine berried", caveVinesLo, 0, false)
	check("cave vine plant bare", caveVinesPlantLo+1, caveVinesPlantLo, true)
	check("cave vine plant berried", caveVinesPlantLo, 0, false)
}

// A pollen-laden bee with a valid hive boosts the crops it flies over.
func TestBeeBoostsCropsBelow(t *testing.T) {
	h, players, nest := beeWorld(t)
	wheat := worldgen.BlockBase("wheat")
	cx, cz := nest.x+10, nest.z
	// Vanilla checks BOTH cells below the bee — cover each.
	h.world.SetBlock(cx, nest.y-1, cz, wheat)
	h.world.SetBlock(cx, nest.y-2, cz, wheat)
	m := h.spawnAnimal(players, entityBee, cx, cz)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeNectar, m.beeHome, m.beeHasHome = true, nest, true
	m.beeNoEnter = 1 << 20 // stay out working
	grown := func() bool {
		return h.world.At(cx, nest.y-1, cz) != wheat && h.world.At(cx, nest.y-2, cz) != wheat
	}
	for i := 0; i < 200 && !grown(); i++ {
		m.x, m.y, m.z = float64(cx)+0.5, float64(nest.y)+0.5, float64(cz)+0.5
		h.updateBees(players)
	}
	if !grown() {
		t.Fatalf("minutes over wheat and the bee left (%d,%d) ungrown",
			h.world.At(cx, nest.y-1, cz), h.world.At(cx, nest.y-2, cz))
	}
	if m.beeCropsGrown < 2 {
		t.Fatalf("boost budget counted %d, want both crops", m.beeCropsGrown)
	}
}

// The dispenser's hive interactions: shears cut honeycomb and a bottle draws
// honey from a full hive ahead — both release the bees CALM (nobody to blame).
func TestDispenserWorksAFullHive(t *testing.T) {
	h, players, _ := beeWorld(t)
	state := eastDispenser(t)
	pos := blockPos{2100, 200, 2100}
	front := blockPos{2101, 200, 2100}
	full := withHoney(worldgen.BlockBase("beehive"), beeMaxHoney)

	fire := func(item int32) *bin {
		h.world.SetBlock(pos.x, pos.y, pos.z, state)
		h.world.SetBlock(front.x, front.y, front.z, full)
		h.hives[front] = []hiveOccupant{{SecsLeft: 100}, {SecsLeft: 100, Nectar: true}}
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: item, count: 1}
		h.bins[simPos{blockPos: pos}] = b
		h.ejectFromBin(players, simPos{blockPos: pos}, state)
		return b
	}

	b := fire(int32(itemByName["shears"]))
	if lvl := honeyLevel(h.world.At(front.x, front.y, front.z)); lvl != 0 {
		t.Fatalf("sheared hive honey %d, want 0", lvl)
	}
	combs := 0
	for _, it := range h.items {
		if it.item == int32(itemHoneycomb) {
			combs += it.count
		}
	}
	if combs != beeHoneycombYield {
		t.Fatalf("%d honeycomb dispensed, want %d", combs, beeHoneycombYield)
	}
	if b.slots[0].dmg != 1 {
		t.Fatalf("shears wear %d, want 1", b.slots[0].dmg)
	}
	calm, angry := 0, 0
	for _, m := range h.mobs {
		if m.etype != entityBee {
			continue
		}
		if m.anger > 0 {
			angry++
		} else {
			calm++
		}
	}
	if calm != 2 || angry != 0 {
		t.Fatalf("dispenser release: %d calm / %d angry bees, want 2/0", calm, angry)
	}

	b = fire(int32(itemGlassBottle))
	if b.slots[0].item != int32(itemHoneyBottle) || b.slots[0].count != 1 {
		t.Fatalf("bottled slot = %+v, want one honey bottle", b.slots[0])
	}
	if lvl := honeyLevel(h.world.At(front.x, front.y, front.z)); lvl != 0 {
		t.Fatalf("bottled hive honey %d, want 0", lvl)
	}
}

// Breaking a hive: Silk Touch carries the bees and honey on the dropped item
// and a later placement restores them; without it the occupants spill out
// angry at the breaker — and a bee nest then drops nothing at all.
func TestSilkTouchCarriesTheHive(t *testing.T) {
	h, players, nest := beeWorld(t)
	state := withHoney(worldgen.BlockBase("bee_nest"), 4)
	h.world.SetBlock(nest.x, nest.y, nest.z, state)
	h.hives[nest] = []hiveOccupant{{SecsLeft: 100}, {SecsLeft: 50, Nectar: true}}
	t2 := survPlayer(h)
	players[t2.p.eid] = t2
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["diamond_pickaxe"]),
		count: 1, ench: [2]enchApply{{enchSilkTouch, 1}}}

	h.world.SetBlock(nest.x, nest.y, nest.z, worldgen.Air)
	h.dropBeeHome(players, t2.p.eid, state, nest)
	if len(h.hives[nest]) != 0 {
		t.Fatalf("stowed hive still has occupants at the old position: %v", h.hives[nest])
	}
	var carried *itemEntity
	for _, it := range h.items {
		if it.item == int32(itemByName["bee_nest"]) {
			carried = it
		}
	}
	if carried == nil || carried.hiveID == 0 {
		t.Fatalf("Silk Touch dropped %+v, want a bee_nest stamped with a hiveID", carried)
	}
	for _, m := range h.mobs {
		if m.etype == entityBee {
			t.Fatal("Silk Touch spilled a bee")
		}
	}

	// Place it back somewhere else: the bees and honey come with it.
	dst := blockPos{nest.x + 20, nest.y, nest.z}
	h.world.SetBlock(dst.x, dst.y, dst.z, worldgen.BlockBase("bee_nest"))
	h.restoreBeeHome(players, dst, carried.hiveID)
	if len(h.hives[dst]) != 2 {
		t.Fatalf("restored hive holds %v, want the two carried bees", h.hives[dst])
	}
	if lvl := honeyLevel(h.world.At(dst.x, dst.y, dst.z)); lvl != 4 {
		t.Fatalf("restored hive honey %d, want 4", lvl)
	}
	if len(h.hiveItems) != 0 {
		t.Fatalf("stow store still holds %v after the restore", h.hiveItems)
	}

	// Without Silk Touch: a nest drops NOTHING and its bees come out angry.
	nest2 := blockPos{nest.x + 40, nest.y, nest.z}
	state2 := withHoney(worldgen.BlockBase("bee_nest"), 2)
	h.registerHive(nest2)
	h.hives[nest2] = []hiveOccupant{{SecsLeft: 100}}
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["diamond_pickaxe"]), count: 1}
	before := len(h.items)
	h.world.SetBlock(nest2.x, nest2.y, nest2.z, worldgen.Air)
	h.dropBeeHome(players, t2.p.eid, state2, nest2)
	if len(h.items) != before {
		t.Fatal("a bee nest without Silk Touch dropped an item")
	}
	angry := 0
	for _, m := range h.mobs {
		if m.etype == entityBee && m.anger > 0 {
			angry++
		}
	}
	if angry != 1 {
		t.Fatalf("%d angry bees out of the broken nest, want 1", angry)
	}
}

// The carried-hive stow and the widened container rows survive persistence.
func TestHiveStowPersists(t *testing.T) {
	cs := &containerStore{}
	stows := map[int32]hiveStow{7: {Honey: 3, Occ: []hiveOccupant{{SecsLeft: 90, Nectar: true}}}}
	cs.recordHiveItems(stows, 7)
	got, next := cs.loadHiveItems()
	if next != 7 || len(got) != 1 || got[7].Honey != 3 ||
		len(got[7].Occ) != 1 || !got[7].Occ[0].Nectar {
		t.Fatalf("stow round trip = %v next %d", got, next)
	}
	// A hive item in a CHEST keeps its hiveID: the container row is wide
	// enough for the whole stack pack (this is the [14]int32 truncation trap).
	st := invStack{item: int32(itemByName["beehive"]), count: 1, boxID: 9, hiveID: 7}
	if i, back := rowStack(slotRow(3, st)); i != 3 || back.hiveID != 7 || back.boxID != 9 {
		t.Fatalf("container row round trip = %d %+v", i, back)
	}
}

// Bees court over any flower — the #bee_food tag — and nothing else.
func TestBeesCourtOverFlowers(t *testing.T) {
	h, players, _ := beeWorld(t)
	m := h.spawnAnimal(players, entityBee, 2020, 2020)
	t2 := survPlayer(h)
	players[t2.p.eid] = t2
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["wheat"]), count: 1}
	if h.feedAnimal(players, t2, m) {
		t.Fatal("a bee courted over wheat")
	}
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["cornflower"]), count: 1}
	if !h.feedAnimal(players, t2, m) {
		t.Fatal("a bee refused a cornflower")
	}
	if m.loveTicks == 0 {
		t.Fatal("the courted bee is not in love")
	}
}
