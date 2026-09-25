package server

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An ender chest is a door onto the PLAYER's storage, so two of them anywhere
// in the world show the same 27 slots.
func TestEnderChestIsPerPlayerNotPerBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	h.openEnderChest(players, pl, 0, 70, 0)
	if pl.viewChest != pl.enderChest() {
		t.Fatal("the window is not looking at the player's own storage")
	}
	pl.enderChest().slots[0] = invStack{item: itemByName["diamond"], count: 3}

	// A different block, far away: same contents.
	h.openEnderChest(players, pl, 5000, 70, -5000)
	if got := pl.viewChest.slots[0]; got.item != itemByName["diamond"] || got.count != 3 {
		t.Errorf("a second ender chest showed %+v, want the same diamonds", got)
	}
	// …and it is not backed by any block storage.
	if len(h.chests) != 0 {
		t.Errorf("%d block chests created by opening ender chests, want 0", len(h.chests))
	}
}

// Two players never see each other's ender contents.
func TestEnderChestsAreSeparatePerPlayer(t *testing.T) {
	h := newHub(world.New(1))
	a, b := survPlayer(h), survPlayer(h)
	a.enderChest().slots[0] = invStack{item: itemByName["diamond"], count: 1}
	if b.enderChest().slots[0].item != 0 {
		t.Error("a second player can see the first's ender chest")
	}
}

// The ender chest survives a logout, since it rides the player's saved
// inventory rather than any block.
func TestEnderChestPersists(t *testing.T) {
	store := newInvStore(t.TempDir() + "/inv.json")
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.enderChest().slots[4] = invStack{item: itemByName["emerald"], count: 7, dmg: 0}
	store.save("wesley", pl)

	back := survPlayer(h)
	store.loadInto(back, "wesley")
	if got := back.enderChest().slots[4]; got.item != itemByName["emerald"] || got.count != 7 {
		t.Errorf("reloaded ender slot %+v, want 7 emeralds", got)
	}
}

// The point of a shulker box: breaking it keeps what is inside.
func TestShulkerBoxKeepsItsContents(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{3, 70, 3}

	c := &chest{}
	c.slots[0] = invStack{item: itemByName["diamond"], count: 5}
	c.slots[26] = invStack{item: itemByName["emerald"], count: 2}
	h.chests[simPos{blockPos: pos}] = c

	boxID := h.stowShulkerBox(simPos{blockPos: pos})
	if boxID == 0 {
		t.Fatal("a box with contents stowed as empty")
	}
	if _, still := h.chests[simPos{blockPos: pos}]; still {
		t.Error("the block still has storage after being stowed")
	}

	// Placed again somewhere else, the contents come back.
	dest := blockPos{-40, 12, 900}
	h.restoreShulkerBox(simPos{blockPos: dest}, boxID)
	back := h.chests[simPos{blockPos: dest}]
	if back == nil {
		t.Fatal("the box restored no storage")
	}
	if got := back.slots[0]; got.item != itemByName["diamond"] || got.count != 5 {
		t.Errorf("slot 0 came back as %+v, want 5 diamonds", got)
	}
	if got := back.slots[26]; got.item != itemByName["emerald"] || got.count != 2 {
		t.Errorf("slot 26 came back as %+v, want 2 emeralds", got)
	}
	if _, leaked := h.boxes.get(boxID); leaked {
		t.Error("the box id was not retired once its contents were placed")
	}
}

// An EMPTY box needs no id — it drops as a plain item.
func TestEmptyShulkerBoxNeedsNoIdentity(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{1, 70, 1}
	h.chests[simPos{blockPos: pos}] = &chest{}
	if id := h.stowShulkerBox(simPos{blockPos: pos}); id != 0 {
		t.Errorf("an empty box minted id %d, want 0", id)
	}
	if n := len(h.boxes.snapshot()); n != 0 {
		t.Errorf("%d box records for an empty box, want 0", n)
	}
}

// Contents in transit survive a restart.
func TestShulkerContentsPersist(t *testing.T) {
	dir := t.TempDir()
	store := newContainerStore(dir + "/containers.json")
	boxes := map[int32]*chest{}
	c := &chest{}
	c.slots[2] = invStack{item: itemByName["diamond"], count: 9}
	boxes[7] = c
	store.recordBoxes(boxes, 7)

	back, next := store.loadBoxes()
	if next != 7 {
		t.Errorf("next box id %d, want 7", next)
	}
	if back[7] == nil {
		t.Fatal("box 7 did not come back")
	}
	if got := back[7].slots[2]; got.item != itemByName["diamond"] || got.count != 9 {
		t.Errorf("reloaded contents %+v, want 9 diamonds", got)
	}
}

// The box identity has to ride the stack through the save format, or a box in
// a chest comes back empty.
func TestBoxIDSurvivesTheStackRow(t *testing.T) {
	st := invStack{item: itemByName["shulker_box"], count: 1, boxID: 42}
	if got := unpackStack(packStack(st)); got.boxID != 42 {
		t.Errorf("boxID %d after a round trip, want 42", got.boxID)
	}
	// A short row from an older file must still decode, zero-filling the new
	// column rather than failing.
	var old stackRow
	old[0], old[1] = itemByName["stone"], 3
	if got := unpackStack(old); got.item != itemByName["stone"] || got.count != 3 || got.boxID != 0 {
		t.Errorf("legacy row decoded as %+v", got)
	}
}

// isShulkerBox must cover every colour, not just the plain one.
func TestShulkerBoxStatesCoverEveryColour(t *testing.T) {
	for _, name := range []string{"shulker_box", "white_shulker_box", "red_shulker_box",
		"black_shulker_box", "cyan_shulker_box"} {
		lo, hi := worldgen.BlockRange(name)
		if lo == 0 {
			t.Fatalf("%s has no block states", name)
		}
		for _, s := range []uint32{lo, hi} {
			if !isShulkerBox(s) {
				t.Errorf("%s state %d not recognised as a shulker box", name, s)
			}
		}
	}
	if isShulkerBox(worldgen.BlockBase("chest")) {
		t.Error("a plain chest was taken for a shulker box")
	}
}

// Every storage block must resolve to the container it opens. This is the test
// that was missing when placed shulker boxes shipped with working storage and
// no way to open one: the storage side and the interaction side lived in
// different files and only one of them knew about the block.
func TestEveryContainerBlockOpensSomething(t *testing.T) {
	for _, c := range []struct {
		name string
		want containerOpen
	}{
		{"chest", openChestWindow},
		{"trapped_chest", openChestWindow},
		{"shulker_box", openChestWindow},
		{"red_shulker_box", openChestWindow},
		{"black_shulker_box", openChestWindow},
		{"ender_chest", openEnderWindow},
		{"decorated_pot", openPotSlot},
		{"stone", openNothing},
	} {
		lo, hi := worldgen.BlockRange(c.name)
		if lo == 0 && c.name != "stone" {
			t.Fatalf("%s has no block states", c.name)
		}
		for _, s := range []uint32{lo, hi} {
			if got := containerOpenFor(s); got != c.want {
				t.Errorf("%s state %d opens %v, want %v", c.name, s, got, c.want)
			}
		}
	}
}

// A decorated pot takes ONE item per click and never hands anything back:
// what goes in comes out only when the pot is broken (DecoratedPotBlock).
func TestDecoratedPotTakesOneAtATime(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pos := blockPos{2, 70, 2}
	key := simPos{dim: pl.dim, blockPos: pos}

	// An empty hand is a refusal, not a pass: the pot wobbles and says no.
	if !h.usePot(players, pl, pos) {
		t.Error("the pot should handle (and refuse) an empty hand")
	}
	if _, stored := h.pots[key]; stored {
		t.Error("an empty hand must not put anything in")
	}

	diamonds := invStack{item: itemByName["diamond"], count: 12}
	pl.inv.slots[pl.p.heldSlot()] = diamonds
	if !h.usePot(players, pl, pos) {
		t.Fatal("the pot refused a held stack")
	}
	if got := h.pots[key]; got.item != diamonds.item || got.count != 1 {
		t.Errorf("one diamond goes in, the pot holds %+v", got)
	}
	if left := pl.inv.slots[pl.p.heldSlot()].count; left != 11 {
		t.Errorf("one diamond leaves the hand, %d left", left)
	}
	h.usePot(players, pl, pos)
	if got := h.pots[key]; got.count != 2 {
		t.Errorf("a second click adds one more, pot holds %d", got.count)
	}

	// Something else is refused outright, and nothing comes back out.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["emerald"], count: 4}
	h.usePot(players, pl, pos)
	if got := h.pots[key]; got.item != diamonds.item || got.count != 2 {
		t.Errorf("a different item must not go into a full-of-diamonds pot: %+v", got)
	}
	if pl.inv.slots[pl.p.heldSlot()].count != 4 {
		t.Error("a refused insert must not consume the held item")
	}
	for _, st := range pl.inv.slots {
		if st.item == diamonds.item && st.count == 12 {
			t.Error("the pot handed its contents back — only breaking it should")
		}
	}
}

// A hopper fills a pot one item at a time and one underneath empties it.
func TestDecoratedPotHopperFlow(t *testing.T) {
	h := newHub(world.New(1))
	pos := simPos{dim: 0, blockPos: blockPos{3, 70, 3}}
	st := invStack{item: itemByName["diamond"], count: 5}
	if !h.potInsert(pos, st) || h.pots[pos].count != 1 {
		t.Fatalf("a hopper insert puts exactly one in: %+v", h.pots[pos])
	}
	h.potInsert(pos, st)
	if h.pots[pos].count != 2 {
		t.Fatalf("the second insert stacks: %+v", h.pots[pos])
	}
	if h.potInsert(pos, invStack{item: itemByName["emerald"], count: 1}) {
		t.Error("a pot holding diamonds must refuse emeralds")
	}
	one, ok := h.potExtract(pos)
	if !ok || one.count != 1 || h.pots[pos].count != 1 {
		t.Fatalf("extract takes one: %+v left %+v", one, h.pots[pos])
	}
	h.potExtract(pos)
	// The entry STAYS, empty. It is the record that this pot has been looked
	// in, which is what stops a structure pot restocking itself.
	if st, still := h.pots[pos]; !still || st.item != 0 {
		t.Errorf("an emptied pot should be remembered as empty, got %+v (present=%v)", st, still)
	}
	if _, ok := h.potExtract(pos); ok {
		t.Error("an empty pot has nothing to give")
	}
}

// Breaking a pot scatters what is inside.
func TestDecoratedPotSpillsOnBreak(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := blockPos{4, 70, 4}
	h.pots = map[simPos]invStack{{dim: 0, blockPos: pos}: {item: itemByName["emerald"], count: 3}}

	h.spillPot(players, 0, pos, worldgen.Air)
	if len(h.pots) != 0 {
		t.Error("the pot kept its contents after the block went")
	}
	got := 0
	for _, it := range h.items {
		if it.item == itemByName["emerald"] {
			got += it.count
		}
	}
	if got != 3 {
		t.Errorf("%d emeralds scattered, want 3", got)
	}
}

// The conduit frame: vanilla needs 16 blocks on the 5x5x5 rings and water all
// around the conduit itself.
func TestConduitNeedsWaterAndAFrame(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}
	w.SetBlock(pos.x, pos.y, pos.z, conduitState)

	// No water, no frame.
	if got := h.conduitActiveBlocks(0, pos); got != 0 {
		t.Errorf("a dry conduit counted %d frame blocks, want 0", got)
	}

	// Water all around it, still no frame.
	for ox := -1; ox <= 1; ox++ {
		for oy := -1; oy <= 1; oy++ {
			for oz := -1; oz <= 1; oz++ {
				if ox == 0 && oy == 0 && oz == 0 {
					continue
				}
				w.SetBlock(pos.x+ox, pos.y+oy, pos.z+oz, worldgen.WaterBase)
			}
		}
	}
	if got := h.conduitActiveBlocks(0, pos); got != 0 {
		t.Errorf("a frameless conduit counted %d, want 0", got)
	}

	// Build the full frame: every ring position gets prismarine.
	prismarine := worldgen.BlockBase("prismarine")
	placed := 0
	for ox := -2; ox <= 2; ox++ {
		for oy := -2; oy <= 2; oy++ {
			for oz := -2; oz <= 2; oz++ {
				ax, ay, az := abs(ox), abs(oy), abs(oz)
				if ax <= 1 && ay <= 1 && az <= 1 {
					continue
				}
				onRing := (ox == 0 && (ay == 2 || az == 2)) ||
					(oy == 0 && (ax == 2 || az == 2)) ||
					(oz == 0 && (ax == 2 || ay == 2))
				if !onRing {
					continue
				}
				w.SetBlock(pos.x+ox, pos.y+oy, pos.z+oz, prismarine)
				placed++
			}
		}
	}
	got := h.conduitActiveBlocks(0, pos)
	if got != placed {
		t.Errorf("counted %d frame blocks, want the %d placed", got, placed)
	}
	if got < conduitMinActive {
		t.Errorf("a full frame is %d blocks, below the %d needed to activate", got, conduitMinActive)
	}
	// Breaking the water seal switches it off, however good the frame is.
	w.SetBlock(pos.x+1, pos.y, pos.z, prismarine)
	if got := h.conduitActiveBlocks(0, pos); got != 0 {
		t.Errorf("a conduit walled in on one side counted %d, want 0", got)
	}
}

// The conduit registry is what makes them findable without scanning blocks.
func TestConduitRegistryTracksPlacement(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{7, 70, 7}
	h.noteConduitBlock(0, pos, conduitState)
	if !h.conduits[simPos{dim: 0, blockPos: pos}] {
		t.Fatal("a placed conduit was not remembered")
	}
	h.noteConduitBlock(0, pos, worldgen.Air)
	if h.conduits[simPos{dim: 0, blockPos: pos}] {
		t.Error("a broken conduit is still remembered")
	}
}

// Block-interaction events must be wired at BOTH ends — posted from the
// interaction path and handled in the hub loop.
//
// Three features shipped inert this way before this test existed: the shulker
// box could not be opened, the decorated pot ignored clicks, and the vault was
// handled but never posted. Note that last one: a check in only ONE direction
// misses half the cases, which is exactly what the first version of this test
// did. Each time the block-side code was thoroughly tested, because those tests
// called the handler directly and never went through the event.
func TestBlockInteractionEventsAreWiredBothWays(t *testing.T) {
	posted, err := os.ReadFile("interaction.go")
	if err != nil {
		t.Fatalf("read interaction.go: %v", err)
	}
	hub, err := os.ReadFile("hub.go")
	if err != nil {
		t.Fatalf("read hub.go: %v", err)
	}
	// The block-interaction events are the ones shaped {eid, x, y, z}. Find
	// them by their declaration so the list cannot go stale.
	decl := regexp.MustCompile(`(?s)type (ev[A-Z][A-Za-z]*) struct \{\s*eid\s+int32\s*
\s*x, y, z int\s*
\s*\}`)
	var names []string
	for _, f := range mustGoFiles(t) {
		for _, m := range decl.FindAllStringSubmatch(f, -1) {
			names = append(names, m[1])
		}
	}
	if len(names) < 4 {
		t.Fatalf("found only %d block-interaction events — the scan is not working", len(names))
	}
	for _, n := range names {
		if !strings.Contains(string(posted), n+"{eid:") {
			t.Errorf("%s is handled by the hub but the interaction path never posts it", n)
		}
		if !strings.Contains(string(hub), "case "+n+":") {
			t.Errorf("%s is posted by the interaction path but the hub never handles it", n)
		}
	}
}

// mustGoFiles reads every .go file in the package once.
func mustGoFiles(t *testing.T) []string {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err != nil {
			t.Fatalf("read %s: %v", n, err)
		}
		out = append(out, string(b))
	}
	return out
}

// The original one-directional check, kept because it also covers events that
// are not block-shaped.
func TestEveryInteractionEventIsHandled(t *testing.T) {
	posted, err := os.ReadFile("interaction.go")
	if err != nil {
		t.Fatalf("read interaction.go: %v", err)
	}
	handled, err := os.ReadFile("hub.go")
	if err != nil {
		t.Fatalf("read hub.go: %v", err)
	}
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`\bev[A-Z][A-Za-z]*\{`).FindAllString(string(posted), -1) {
		name := strings.TrimSuffix(m, "{")
		if seen[name] {
			continue
		}
		seen[name] = true
		if !strings.Contains(string(handled), "case "+name+":") {
			t.Errorf("%s is posted by the interaction path but the hub never handles it", name)
		}
	}
	if len(seen) < 5 {
		t.Errorf("only found %d posted events — the scan is not working", len(seen))
	}
}

// A full hive gives honeycomb to shears and honey to a bottle, and empties.
func TestBeehiveHarvest(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pos := blockPos{0, 180, 0}
	w := h.worldFor(0)

	// Not full: nothing happens.
	w.SetBlock(pos.x, pos.y, pos.z, withHoney(beehiveMin, 3))
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: int32(itemByName["shears"]), count: 1}
	if h.harvestBeeHome(players, pl, pos) {
		t.Error("a half-full hive was harvested")
	}

	// Full + shears: three honeycomb, hive emptied.
	w.SetBlock(pos.x, pos.y, pos.z, withHoney(beehiveMin, beeMaxHoney))
	if !h.harvestBeeHome(players, pl, pos) {
		t.Fatal("shears did not harvest a full hive")
	}
	if got := honeyLevel(w.At(pos.x, pos.y, pos.z)); got != 0 {
		t.Errorf("honey level %d after harvesting, want 0", got)
	}
	// dropHoneycomb: the comb pops out of the hive, not into the pocket.
	comb := 0
	for _, it := range h.items {
		if it.item == int32(itemByName["honeycomb"]) && math.Abs(it.x-float64(pos.x)-0.5) < 0.5 && math.Abs(it.z-float64(pos.z)-0.5) < 0.5 {
			comb += it.count
		}
	}
	for _, st := range pl.inv.slots {
		if st.item == int32(itemByName["honeycomb"]) {
			t.Error("the honeycomb went into the inventory")
		}
	}
	if comb != beeHoneycombYield {
		t.Errorf("%d honeycomb dropped at the hive, want %d", comb, beeHoneycombYield)
	}

	// Full + bottle: one honey bottle.
	w.SetBlock(pos.x, pos.y, pos.z, withHoney(beehiveMin, beeMaxHoney))
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: int32(itemByName["glass_bottle"]), count: 1}
	if !h.harvestBeeHome(players, pl, pos) {
		t.Fatal("a bottle did not draw honey")
	}
	if st := pl.inv.slots[pl.p.heldSlot()]; st.item != int32(itemByName["honey_bottle"]) || st.count != 1 {
		t.Errorf("the last bottle should become the honey in the same hand, holding %+v", st)
	}
}

// The honey level lives in the low digit of the state, and the facing must
// survive a harvest — a hive that empties must not turn round.
func TestBeehiveStateMath(t *testing.T) {
	for _, base := range []uint32{beehiveMin, beeNestMin} {
		for facing := uint32(0); facing < 4; facing++ {
			s := base + facing*(beeMaxHoney+1)
			for lvl := 0; lvl <= beeMaxHoney; lvl++ {
				got := withHoney(s, lvl)
				if honeyLevel(got) != lvl {
					t.Errorf("level %d round-tripped as %d", lvl, honeyLevel(got))
				}
				if (got-base)/(beeMaxHoney+1) != facing {
					t.Errorf("facing changed: %d -> %d", facing, (got-base)/(beeMaxHoney+1))
				}
			}
		}
	}
	if isBeeHome(worldgen.BlockBase("stone")) {
		t.Error("stone was taken for a hive")
	}
}

// ConduitBlockEntity: a full frame keeps hunting the enemy it picked
// (updateDestroyTarget) rather than whichever it meets first; the power
// needs the player in water or in rain at their own position, not merely
// a storm somewhere.
func TestConduitTargetMemoryAndRain(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	w.ForceLoad(0, 0, 2)
	pos := blockPos{0, 180, 0}
	for ox := -9; ox <= 9; ox++ {
		for oy := -9; oy <= 9; oy++ {
			for oz := -9; oz <= 9; oz++ {
				w.SetBlock(pos.x+ox, pos.y+oy, pos.z+oz, worldgen.WaterBase)
			}
		}
	}
	w.SetBlock(pos.x, pos.y, pos.z, conduitState)
	prismarine := worldgen.BlockBase("prismarine")
	for ox := -2; ox <= 2; ox++ {
		for oy := -2; oy <= 2; oy++ {
			for oz := -2; oz <= 2; oz++ {
				ax, ay, az := abs(ox), abs(oy), abs(oz)
				if ax <= 1 && ay <= 1 && az <= 1 {
					continue
				}
				if (ox == 0 && (ay == 2 || az == 2)) || (oy == 0 && (ax == 2 || az == 2)) || (oz == 0 && (ax == 2 || ay == 2)) {
					w.SetBlock(pos.x+ox, pos.y+oy, pos.z+oz, prismarine)
				}
			}
		}
	}
	players := map[int32]*tracked{}
	h.playersRef = players
	a := h.spawnMob(players, entityDrowned, 4.5, 180, 0.5)
	b := h.spawnMob(players, entityDrowned, -3.5, 180, 0.5)
	a.health, b.health = 1000, 1000
	var first int32
	for cycle := 0; cycle < 6; cycle++ {
		h.tick.Store(uint64(cycle) * 40)
		h.runConduit(players, 0, pos)
		cr := h.conduitRuns[simPos{dim: 0, blockPos: pos}]
		if cr == nil || cr.target == 0 {
			t.Fatal("a full conduit should be hunting")
		}
		if cycle == 0 {
			first = cr.target
		} else if cr.target != first {
			t.Fatalf("the conduit switched targets from %d to %d while the first still lived in range", first, cr.target)
		}
	}
	hurt := a
	if first == b.eid {
		hurt = b
	}
	if hurt.health >= 1000 {
		t.Fatal("the target took no damage")
	}

	// A dry player under a roof during a storm gets nothing.
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	for x := 12; x <= 14; x++ {
		for z := -1; z <= 1; z++ {
			for y := 179; y <= 185; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 186, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 13.5, 180, 0.5
	h.raining = true
	h.tick.Store(400)
	h.runConduit(players, 0, pos)
	if pl.effects[effConduitPower] != nil {
		t.Fatal("a dry, roofed player got Conduit Power from a storm elsewhere")
	}
}
