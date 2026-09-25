package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// MovingPistonBlock.useWithoutItem: a moving_piston cell with no block
// entity behind it (left over from before a restart) goes when clicked.
func TestClickClearsOrphanMovingPiston(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 3, 70, 3
	w.SetBlock(x, y, z, movingPistonState([3]int{0, 1, 0}, false))
	p.setHotbarSlot(0, 0)
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if !pollUntil(3*time.Second, func() bool { return w.At(x, y, z) == worldgen.Air }) {
		t.Fatal("the orphaned moving piston should be removed by the click")
	}
}

// DoorBlock/TrapDoorBlock.useWithoutItem PASS for iron: the click goes on
// to the held block, which is placed against it.
func TestIronTrapdoorClickPlacesAgainstIt(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	trap := worldgen.BlockBase("iron_trapdoor")
	w.SetBlock(x, y, z, trap)
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if w.Block(x, y, z) != trap {
		t.Error("a hand opened the iron trapdoor")
	}
	if w.Block(x, y+1, z) != worldgen.BlockBase("stone") {
		t.Error("the stone was not placed against the iron trapdoor")
	}
}

// ChestBlock.useWithoutItem for a pair: the open_chest statistic, as a single
// chest gives.
func TestDoubleChestOpenAwardsStat(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := testTracked()
	players[1] = pl
	h.world.SetBlock(10, 70, 10, withProps(t, worldgen.BlockBase("chest"), map[string]string{"facing": "north", "type": "left"}))
	h.world.SetBlock(11, 70, 10, withProps(t, worldgen.BlockBase("chest"), map[string]string{"facing": "north", "type": "right"}))
	h.openChest(pl, 10, 70, 10)
	if pl.winKind != winDoubleChest {
		t.Fatalf("the pair should open as a large chest, kind %d", pl.winKind)
	}
	if got := customStat(pl, "open_chest"); got != 1 {
		t.Errorf("open_chest = %d after opening a large chest, want 1", got)
	}
}

// ChiseledBookShelfBlock: whatever is in hand, a click on an occupied slot
// takes the book (TRY_WITH_EMPTY_HAND → useWithoutItem).
func TestShelfGivesBookWhateverIsHeld(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := testTracked()
	players[1] = pl
	x, y, z := 10, 70, 10
	h.world.SetBlock(x, y, z, withProps(t, bookshelfMin, map[string]string{"facing": "north", "slot_0_occupied": "true"}))
	pos := simPos{blockPos: blockPos{x, y, z}}
	shelf := &[6]invStack{}
	shelf[0] = invStack{item: itemByName["book"], count: 1}
	h.bookshelves[pos] = shelf
	stone := itemByName["stone"]
	pl.inv.slots[0] = invStack{item: stone, count: 5}
	// North face, left third from the front (cx 0.9 → face x 0.1), top row.
	h.onUseShelf(players, evUseShelf{eid: 1, x: x, y: y, z: z, face: 2, cx: 0.9, cy: 0.8, cz: 0})
	if shelf[0].item != 0 {
		t.Fatal("the book should come out with stone in hand")
	}
	found := false
	for _, s := range pl.inv.slots {
		if s.item == itemByName["book"] && s.count == 1 {
			found = true
		}
	}
	if !found || pl.inv.slots[0].item != stone || pl.inv.slots[0].count != 5 {
		t.Errorf("the book should be in the inventory and the stone untouched: %+v", pl.inv.slots[:3])
	}
}

// A click on a chiseled bookshelf's side (no slot there) PASSes: the held
// block is placed against it.
func TestShelfSideClickPlaces(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	shelfState := withProps(t, bookshelfMin, map[string]string{"facing": "north"})
	w.SetBlock(x, y, z, shelfState)
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1)) // the top face
	if w.Block(x, y+1, z) != worldgen.BlockBase("stone") {
		t.Error("a top-face click on a bookshelf should place the held block")
	}
}

// ComparatorBlock.getInputSignal: through a conductor, an item frame hung on
// its far face is read (rotation % 8 + 1).
func TestComparatorReadsItemFrameThroughBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	comp := withProps(t, comparatorMin, map[string]string{"facing": "north", "mode": "compare"})
	pos := blockPos{0, 180, 0}
	h.world.SetBlock(pos.x, pos.y, pos.z, comp)
	h.world.SetBlock(0, 180, -1, worldgen.Stone)
	h.itemFrames[9001] = &itemFrame{eid: 9001, x: 0, y: 180, z: -2, dir: 2, held: invStack{item: itemByName["stone"], count: 1}, rot: 3}
	var got int
	h.inDim(0, func() { got = h.comparatorOutput(pos, comp) })
	if got != 4 {
		t.Errorf("comparator reads %d from a framed item turned 3 times, want 4", got)
	}
	h.itemFrames[9001].held = invStack{}
	h.inDim(0, func() { got = h.comparatorOutput(pos, comp) })
	if got != 0 {
		t.Errorf("an empty frame reads %d, want 0", got)
	}
}

// DetectorRailBlock.getAnalogOutputSignal: a powered detector rail gives the
// fullness of a container cart on it; a cart on a plain rail is not a block
// and gives a comparator nothing.
func TestDetectorRailReadsContainerCart(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, railMin, false)
	v := specialCart(t, h, players, entityChestMinecart, 5)
	v.chest.slots[0] = invStack{item: itemByName["stone"], count: 64}
	pos := simPos{blockPos: blockPos{5, cartY, 10}}
	if got := h.analogSignal(pos); got >= 0 {
		t.Errorf("a cart on a plain rail reads %d, want no analog output", got)
	}
	h.world.SetBlock(5, cartY, 10, railWith(detectorRailMin, shapeEW, false))
	h.updateVehicles(players)
	if !railPowered(h.world.At(5, cartY, 10)) {
		t.Fatal("the detector rail should press under the cart")
	}
	if got, want := h.analogSignal(pos), 1+14/27; got != want {
		t.Errorf("detector rail under a chest cart with one full slot reads %d, want %d", got, want)
	}
}

// CreakingHeartBlockEntity.serverTick: when the heart's reading changes as
// its creaking moves, the comparator beside it hears of it that tick.
func TestHeartOutputTellsComparator(t *testing.T) {
	h, pos, link := paleTrunk(t)
	players := map[int32]*tracked{}
	h.playersRef = players
	nightHub(h)
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.CreakingHeartAwake)
	comp := withProps(t, comparatorMin, map[string]string{"facing": "north", "mode": "compare"})
	cpos := blockPos{pos.x, pos.y, pos.z + 1}
	h.world.SetBlock(cpos.x, cpos.y, cpos.z, comp)
	m := h.spawnMob(players, entityCreaking, float64(pos.x)+10.5, float64(pos.y), float64(pos.z)+0.5)
	link.creaking = m.eid
	link.nextAt = h.tick.Load() + 1000 // the heart's own self-check stays out of it
	h.updateHearts(players)
	if link.outSig == 0 || !h.hasScheduledTick(cpos) {
		t.Errorf("the reading (%d) should reach the comparator, scheduled %v", link.outSig, h.hasScheduledTick(cpos))
	}
}

// Level.destroyBlock: a block knocked out by the world (a ravager's
// trample, a breached door) shows its break to those near and sends sculk a
// BLOCK_DESTROY.
func TestBreakBlockDropShowsTheBreak(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 180, 3.5
	players[1] = pl
	w := h.worldFor(0)
	w.SetBlock(0, 179, 0, worldgen.BlockBase("farmland"))
	wheat := cropRanges[0][0] + 3
	w.SetBlock(0, 180, 0, wheat)
	h.breakBlockDrop(players, 0, blockPos{0, 180, 0}, wheat)
	saw := false
	for _, fx := range drainFX(pl) {
		if fx.Event == worldEventBlockBreak && fx.Data == int32(wheat) {
			saw = true
		}
	}
	if !saw {
		t.Error("the trampled wheat should show its break (levelEvent 2001)")
	}
}

// FireBlock.tick: #infiniburn_overworld is netherrack AND magma blocks —
// a fire on magma with nothing to burn never goes out.
func TestFireOnMagmaBurnsForever(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 3.5, 180, 3.5
	players := map[int32]*tracked{1: pl}
	h.world.SetBlock(3, 179, 3, magmaBlockState)
	h.world.SetBlock(3, 180, 3, fireDefault)
	h.fireAge[simPos{blockPos: blockPos{3, 180, 3}}] = 15
	for i := 0; i < 64; i++ {
		h.updateFire(players, blockPos{3, 180, 3})
	}
	if !isFire(h.world.At(3, 180, 3)) {
		t.Error("a fire on a magma block is eternal")
	}
}

// FireBlock.tick eats the blocks around it whatever mob_griefing says (the
// rule is about mobs); only fire_spread_radius_around_player governs fire.
func TestFireBurnsBlocksWithoutMobGriefing(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 100.5, 180, 100.5
	players := map[int32]*tracked{1: pl}
	h.rules.MobGriefing = false
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			h.world.SetBlock(100+dx, 179, 100+dz, worldgen.OakPlanks)
		}
	}
	fire := blockPos{100, 180, 100}
	burnt := false
	for i := 0; i < 400 && !burnt; i++ {
		h.world.SetBlock(fire.x, fire.y, fire.z, fireDefault) // keep one lit above the planks
		h.updateFire(players, fire)
		burnt = h.world.At(100, 179, 100) != worldgen.OakPlanks
	}
	if !burnt {
		t.Error("the fire should burn the planks under it with mob griefing off")
	}
}

// BaseFireBlock.onPlace: any fire that appears inside an empty obsidian
// frame in the overworld lights the portal — not only flint and steel.
func TestFireInFrameLightsPortal(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.world
	for x := 0; x <= 3; x++ {
		for y := 180; y <= 184; y++ {
			edge := x == 0 || x == 3 || y == 180 || y == 184
			if edge {
				w.SetBlock(x, y, 0, worldgen.Obsidian)
			} else {
				w.SetBlock(x, y, 0, worldgen.Air)
			}
		}
	}
	h.setBlockAt(players, dimOverworld, blockPos{1, 181, 0}, fireDefault) // a fire charge's fire, say
	for x := 1; x <= 2; x++ {
		for y := 181; y <= 183; y++ {
			if !isPortalBlock(w.At(x, y, 0)) {
				t.Fatalf("the frame should be lit: (%d,%d) holds %d", x, y, w.At(x, y, 0))
			}
		}
	}
}

// sculkHears builds a pad at y=180 with a sculk sensor at (4,180,4), lets
// it settle, runs act, and returns the frequency the sensor heard (0 for
// nothing).
func sculkHears(t *testing.T, act func(h *hub, players map[int32]*tracked, pl *tracked)) int {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 7.5, 180, 7.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 4, 180, 4
	w.ForceLoad(x, z, 2)
	for dx := -3; dx <= 7; dx++ {
		for dz := -3; dz <= 7; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimOverworld, x: x, y: y, z: z, state: sensor})
	stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2)
	act(h, players, pl)
	stepSculk(h, players, 5)
	return h.sculkFreq[simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}]
}

// A candle snuffed by hand is a BLOCK_CHANGE (AbstractCandleBlock.extinguish).
func TestSculkHearsACandleSnuffed(t *testing.T) {
	f := sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		lit := withProps(t, worldgen.BlockBase("candle"), map[string]string{"lit": "true"})
		h.world.SetBlock(7, 180, 4, lit)
		pl.inv.slots[pl.p.heldSlot()] = invStack{}
		h.useCandle(players, evUseCandle{eid: pl.p.eid, x: 7, y: 180, z: 4, cy: 0.5})
	})
	if f != freqBlockChange {
		t.Errorf("the sensor heard %d, want BLOCK_CHANGE %d", f, freqBlockChange)
	}
}

// 26.3 #maintains_farmland holds the fence gates too: dry farmland under a
// gate stays tilled.
func TestFenceGateKeepsFarmland(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	for dx := -5; dx <= 5; dx++ {
		for dz := -5; dz <= 5; dz++ {
			w.SetBlock(dx, 179, dz, worldgen.Stone)
			w.SetBlock(dx, 180, dz, worldgen.Air)
			w.SetBlock(dx, 181, dz, worldgen.Air)
		}
	}
	w.SetBlock(0, 180, 0, farmlandMin) // moisture 0
	gate := worldgen.BlockBase("oak_fence_gate")
	w.SetBlock(0, 181, 0, gate)
	h.farmlandRandomTick(players, 0, 0, 180, 0, farmlandMin)
	if w.At(0, 180, 0) != farmlandMin || w.At(0, 181, 0) != gate {
		t.Errorf("dry farmland under a fence gate should stay: soil %d gate %d", w.At(0, 180, 0), w.At(0, 181, 0))
	}
}

// PathBlock.turnToBaseBlock: a path covered by a solid block becomes dirt
// with a BLOCK_CHANGE, and a mob standing on it is lifted onto the dirt.
func TestPathRevertIsHeardAndLifts(t *testing.T) {
	var lifted bool
	f := sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		h.world.SetBlock(7, 179, 4, dirtPathState)
		h.world.SetBlock(7, 179, 6, dirtPathState)
		m := h.spawnMob(players, entityCow, 7.5, 179+15.0/16, 6.5)
		h.setBlockAt(players, dimOverworld, blockPos{7, 180, 4}, worldgen.Stone)
		h.setBlockAt(players, dimOverworld, blockPos{7, 180, 6}, worldgen.BlockBase("oak_slab")) // a slab is solid enough
		lifted = h.world.At(7, 179, 6) == worldgen.Dirt && m.y == 180
	})
	if f != freqBlockChange {
		t.Errorf("the sensor heard %d, want BLOCK_CHANGE %d", f, freqBlockChange)
	}
	if !lifted {
		t.Error("the cow on the path should stand on the dirt")
	}
}

// EnderChestBlock.useWithoutItem awards open_enderchest only when the chest
// opens: a conductor on its lid refuses the open and the statistic.
func TestEnderChestStatOnlyWhenItOpens(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.world.SetBlock(0, 180, 0, worldgen.BlockBase("ender_chest"))
	h.world.SetBlock(0, 181, 0, worldgen.Stone)
	h.openEnderChest(players, pl, 0, 180, 0)
	if got := customStat(pl, "open_enderchest"); got != 0 {
		t.Errorf("a blocked ender chest awarded open_enderchest %d", got)
	}
	h.world.SetBlock(0, 181, 0, worldgen.Air)
	h.openEnderChest(players, pl, 0, 180, 0)
	if got := customStat(pl, "open_enderchest"); got != 1 {
		t.Errorf("open_enderchest = %d after an open, want 1", got)
	}
}

// JukeboxBlock.useItemOn: an empty jukebox clicked with anything but a disc
// PASSes, so the held block is placed against it.
func TestEmptyJukeboxPassesABlock(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	w.SetBlock(x, y, z, jukeboxState(false))
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if w.Block(x, y+1, z) != worldgen.BlockBase("stone") {
		t.Error("stone clicked on an empty jukebox should be placed on it")
	}
}

// Putting a disc in a jukebox is a BLOCK_CHANGE (tryInsertIntoJukebox).
func TestSculkHearsADiscGoIn(t *testing.T) {
	f := sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		h.world.SetBlock(7, 180, 4, jukeboxState(false))
		pl.inv.slots[0] = invStack{item: itemByName["music_disc_cat"], count: 1}
		h.onUseJukebox(players, evUseJukebox{eid: pl.p.eid, x: 7, y: 180, z: 4, slot: 0})
	})
	if f != freqBlockChange {
		t.Errorf("the sensor heard %d, want BLOCK_CHANGE %d", f, freqBlockChange)
	}
}

// LayeredCauldronBlock.handleEntityOnFireInside: a burning player in a full
// powder-snow cauldron is put out, the snow melts to a water cauldron one
// level down, and it is a BLOCK_CHANGE.
func TestBurningPlayerMeltsSnowCauldron(t *testing.T) {
	var got uint32
	var fire int
	f := sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		snow := worldgen.BlockBase("powder_snow_cauldron") + 2 // level 3
		h.world.SetBlock(7, 180, 4, snow)
		pl.x, pl.y, pl.z = 7.5, 180.3, 4.5
		pl.fireSecs = 5
		h.playerInsideTick(players)
		got, fire = h.world.At(7, 180, 4), pl.fireSecs
	})
	if got != waterCauldronBase+1 {
		t.Errorf("the cauldron holds %d, want a level-2 water cauldron %d", got, waterCauldronBase+1)
	}
	if fire != 0 {
		t.Error("the player should be put out")
	}
	if f != freqBlockChange {
		t.Errorf("the sensor heard %d, want BLOCK_CHANGE %d", f, freqBlockChange)
	}
}

// LecternBlock.placeBook → resetBookState: a book set on a lectern is a
// BLOCK_CHANGE, and placing it is not an interact_with_lectern.
func TestLecternBookPlacedIsHeard(t *testing.T) {
	var stat int32
	f := sculkHears(t, func(h *hub, players map[int32]*tracked, pl *tracked) {
		h.world.SetBlock(7, 180, 4, worldgen.BlockBase("lectern"))
		pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["writable_book"], count: 1}
		h.onUseLectern(players, evUseLectern{eid: pl.p.eid, x: 7, y: 180, z: 4})
		if !boolProp(h.world.At(7, 180, 4), "has_book") {
			t.Fatal("the book should be on the lectern")
		}
		stat = customStat(pl, "interact_with_lectern")
	})
	if f != freqBlockChange {
		t.Errorf("the sensor heard %d, want BLOCK_CHANGE %d", f, freqBlockChange)
	}
	if stat != 0 {
		t.Errorf("placing a book awarded interact_with_lectern %d", stat)
	}
}

// SculkSensorBlock.tick: a sensor going from cooldown to inactive plays the
// clicking-stop sound (not when waterlogged).
func TestSensorClickingStops(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 7.5, 180, 7.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pos := simPos{blockPos: blockPos{4, 180, 4}}
	sensor := sensorWith(worldgen.BlockBase("sculk_sensor")+1, 0, sculkPhaseCooldown)
	h.world.SetBlock(pos.x, pos.y, pos.z, sensor)
	h.sculkDue[pos] = h.tick.Load() + 1
	drainOut(pl.p)
	stepSculk(h, players, 2)
	if sensorPhase(h.world.At(pos.x, pos.y, pos.z)) != sculkPhaseInactive {
		t.Fatal("the sensor should be inactive after its cooldown")
	}
	if !heardSound(pl.p, "minecraft:block.sculk_sensor.clicking_stop") {
		t.Error("the sensor's clicking should stop audibly")
	}
}
