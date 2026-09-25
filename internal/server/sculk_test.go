package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// stepSculk advances the vibration sweep + the block-update queue together (the
// real loop runs tickSculk beside runUpdates every tick).
func stepSculk(h *hub, players map[int32]*tracked, n int) {
	for i := 0; i < n; i++ {
		age := h.tick.Add(1)
		h.tickSculk(players)
		h.runUpdates(players, age)
	}
}

func TestSculkSensorActivatesAndDecays(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1 // default: inactive, power 0
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(0, x, y, z, sensor)
	w.SetBlock(x+1, y, z, worldgen.BlockBase("redstone_wire")+1160)

	// A frequency-10 event 3 blocks away (delay 3 ticks; power falls with range).
	h.gameEvent(0, 10, x+3, y, z, 0)
	stepSculk(h, players, 8)

	s := w.At(x, y, z)
	if sensorPhase(s) != sculkPhaseActive {
		t.Fatalf("sensor should be ACTIVE after the vibration arrives: state=%d phase=%d", s, sensorPhase(s))
	}
	wantPower := redstoneForDistance(3, 8) // 15 - floor(15/8*3) = 10
	if sensorPower(s) != wantPower {
		t.Fatalf("sensor power = %d, want %d (distance-scaled)", sensorPower(s), wantPower)
	}
	if p := wirePower(w.At(x+1, y, z)); p != wantPower {
		t.Fatalf("adjacent wire should carry the sensor power %d, got %d", wantPower, p)
	}
	if h.sculkFreq[simPos{blockPos: blockPos{x, y, z}}] != 10 {
		t.Fatalf("sensor comparator frequency = %d, want 10", h.sculkFreq[simPos{blockPos: blockPos{x, y, z}}])
	}

	// ACTIVE 30 + COOLDOWN 10 → back to inactive, wire dead.
	stepSculk(h, players, 45)
	if s := w.At(x, y, z); sensorPhase(s) != sculkPhaseInactive || sensorPower(s) != 0 {
		t.Fatalf("sensor should return to inactive: phase=%d power=%d", sensorPhase(s), sensorPower(s))
	}
	if p := wirePower(w.At(x+1, y, z)); p != 0 {
		t.Fatalf("wire should drop after the sensor deactivates, got %d", p)
	}
}

func TestCalibratedSensorFrequencyFilter(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	calib := worldgen.BlockBase("calibrated_sculk_sensor") + 1 // default: facing north
	w.SetBlock(x, y, z, calib)
	h.sculkIndexOnBlockChange(0, x, y, z, calib)
	// facing north → back is south (z+1); a redstone block there sets back signal 15.
	bdx, bdz := calibBackDelta(calib)
	w.SetBlock(x+bdx, y, z+bdz, redstoneBlock)

	// Wrong frequency (1) is filtered out.
	h.gameEvent(0, 1, x+2, y, z, 0)
	stepSculk(h, players, 6)
	if sensorPhase(w.At(x, y, z)) != sculkPhaseInactive {
		t.Fatal("calibrated sensor must ignore a frequency != its back signal")
	}
	// Matching frequency (15) activates it.
	h.gameEvent(0, 15, x+2, y, z, 0)
	stepSculk(h, players, 6)
	if sensorPhase(w.At(x, y, z)) != sculkPhaseActive {
		t.Fatal("calibrated sensor should activate on its tuned frequency")
	}
}

func TestSculkCatalystSpreadsOnDeath(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	cat := worldgen.BlockBase("sculk_catalyst") + 1
	w.SetBlock(x, y, z, cat)
	h.sculkIndexOnBlockChange(0, x, y, z, cat)
	// A patch of stone around the death spot for the bloom to convert.
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
		}
	}
	// The mob dies a block from the catalyst: a cursor starting in the
	// catalyst's own cell would find nothing to spread over.
	if !h.catalystHears(players, 0, float64(x)+1.5, float64(y), float64(z)+0.5, 5) {
		t.Fatal("catalyst within 8 blocks should consume the death XP")
	}
	stepSculk(h, players, 200) // the charge spreads from the catalyst's ticks
	sculk := worldgen.BlockBase("sculk")
	converted := 0
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if w.At(x+dx, y-1, z+dz) == sculk {
				converted++
			}
		}
	}
	if converted == 0 {
		t.Fatal("a catalyst bloom should convert nearby solid blocks to sculk")
	}
}

// The engine's mechanics raise vanilla's vibrations: a lever thrown is
// BLOCK_ACTIVATE (10) then BLOCK_DEACTIVATE (9), a chest opened
// CONTAINER_OPEN (10), a Nether event reaches no overworld listener, and
// a door swung is BLOCK_OPEN, not a placement.
func TestMechanicsRaiseVibrations(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(0, x, y, z, sensor)
	pos := simPos{blockPos: blockPos{x, y, z}}
	heard := func() int {
		v, ok := h.sculkVib[pos]
		if !ok {
			return 0
		}
		delete(h.sculkVib, pos)
		return v.freq
	}
	lever := setBoolProp(worldgen.BlockBase("lever"), "powered", false) // the base state is the powered one
	w.SetBlock(x+3, y, z, lever)
	h.toggleLever(players, blockPos{x + 3, y, z}, w.At(x+3, y, z))
	if f := heard(); f != freqBlockActivate {
		t.Fatalf("a lever thrown on should be heard at 10, got %d", f)
	}
	h.toggleLever(players, blockPos{x + 3, y, z}, w.At(x+3, y, z))
	if f := heard(); f != freqBlockDeactivate {
		t.Fatalf("a lever thrown off should be heard at 9, got %d", f)
	}
	h.vib(dimNether, freqExplode, x+2, y, z, 0)
	if f := heard(); f != 0 {
		t.Fatalf("a Nether blast reached an overworld sensor: %d", f)
	}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	h.playersRef = players
	pl.x, pl.y, pl.z = float64(x)+2.5, float64(y), float64(z)+1.5
	// A chest has an opener counter (a dispenser does not, and sends
	// nothing): its first opener is CONTAINER_OPEN, its last closer CLOSE.
	w.SetBlock(x+2, y, z, worldgen.BlockBase("chest"))
	w.SetBlock(x+2, y+1, z, worldgen.Air)
	h.openChest(pl, x+2, y, z)
	if f := heard(); f != freqContainerOpen {
		t.Fatalf("a container opened should be heard at 10, got %d", f)
	}
	h.closeWindow(players, pl)
	if f := heard(); f != freqContainerClose {
		t.Fatalf("a container closed should be heard at 9, got %d", f)
	}
	// A toggle marked quiet suppresses the placement vibration of the block
	// change that follows it in the same tick.
	h.vibQuiet[simPos{blockPos: blockPos{x + 4, y, z}}] = h.tick.Load()
	h.onBlock(players, evBlock{x: x + 4, y: y, z: z, state: lever, by: pl.p.eid})
	if f := heard(); f != 0 {
		t.Fatalf("a toggled block should not count as placed: heard %d", f)
	}
	h.onBlock(players, evBlock{x: x + 5, y: y, z: z, state: lever, by: pl.p.eid})
	if f := heard(); f != freqBlockPlace {
		t.Fatalf("a placed block should be heard at 13, got %d", f)
	}
}

// A mob walking past a sensor is heard as STEP (throttled like a player's
// footsteps); a flier makes no footstep.
func TestMobFootstepsAreVibrations(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(0, x, y, z, sensor)
	pos := simPos{blockPos: blockPos{x, y, z}}
	walk := func(m *mob) (steps int) {
		m.vx, m.kb = 0.25, 3 // a shove carries it: no steering in the way
		for i := 0; i < 8; i++ {
			h.tick.Add(1)
			h.updateMobs(players)
			if v, ok := h.sculkVib[pos]; ok && v.src == m.eid {
				if v.freq != freqStep {
					t.Fatalf("heard at %d, want STEP (1)", v.freq)
				}
				steps++
				delete(h.sculkVib, pos)
			}
			m.vx, m.kb = 0.25, 3
		}
		return steps
	}
	cow := h.spawnMob(players, entityCow, float64(x)+3.5, float64(y), float64(z)+0.5)
	if n := walk(cow); n < 1 || n > 3 {
		t.Fatalf("a cow shoved past the sensor should be heard once every three ticks: %d", n)
	}
	h.removeMob(players, cow)
	bat := h.spawnMob(players, entityBat, float64(x)+3.5, float64(y)+2, float64(z)+0.5)
	if n := walk(bat); n != 0 {
		t.Fatalf("a bat in flight makes no footstep: %d", n)
	}
}

// The Warden warning chain: the tally lives on the player, one shriek raises
// it at most every ten seconds, and the fourth calls a Warden — every
// response dropping Darkness on whoever is near.
func TestShriekerWarnsThenSummons(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(x)+1, float64(y), float64(z)
	players[pl.p.eid] = pl
	pos := simPos{blockPos: blockPos{x, y, z}}
	// can_summon true (property index 0), not shrieking, not waterlogged.
	shrieker := shriekerWith(worldgen.BlockBase("sculk_shrieker"), false)
	w.SetBlock(x, y, z, shrieker)
	if !shriekerCanSummon(shrieker) {
		t.Fatal("test fixture: the shrieker must be able to summon")
	}

	// One shriek: warning level 1, no Warden, but the room goes dark.
	h.shriek(players, pos, w.At(x, y, z), pl.p.eid)
	if pl.wardenWarn != 1 {
		t.Fatalf("the first shriek warns the player once, level=%d", pl.wardenWarn)
	}
	stepSculk(h, players, shriekingTicks+1)
	if _, dark := pl.effects[effDarkness]; !dark {
		t.Error("a shrieker's response darkens everyone near it")
	}
	if wardens := countMobs(h, entityWarden); wardens != 0 {
		t.Fatalf("one warning must not summon a Warden, got %d", wardens)
	}
	// A second shriek inside the cooldown does nothing at all.
	h.shriek(players, pos, w.At(x, y, z), pl.p.eid)
	if pl.wardenWarn != 1 {
		t.Fatalf("a warning inside the cooldown must not count, level=%d", pl.wardenWarn)
	}
	// Three more warnings, each after the cooldown, summon one.
	for i := 0; i < 3; i++ {
		pl.wardenCool = 0
		h.shriek(players, pos, w.At(x, y, z), pl.p.eid)
		stepSculk(h, players, shriekingTicks+1)
	}
	if pl.wardenWarn != wardenWarnMax {
		t.Fatalf("four warnings max the tally, got %d", pl.wardenWarn)
	}
	if wardens := countMobs(h, entityWarden); wardens != 1 {
		t.Fatalf("the fourth shriek should summon exactly one Warden, got %d", wardens)
	}
	for _, m := range h.mobs {
		if m.etype == entityWarden && !m.hostile {
			t.Error("a summoned Warden must be hostile: a wanderer never runs its fight")
		}
	}
}

func countMobs(h *hub, etype int) int {
	n := 0
	for _, m := range h.mobs {
		if m.etype == etype {
			n++
		}
	}
	return n
}

// A sculk sensor hears a note block played by redstone, not just one punched
// by a player: NoteBlock.playNote emits the event whatever set it off.
func TestSculkSensorHearsARedstoneNoteBlock(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(0, x, y, z, sensor)

	nb := worldgen.BlockBase("note_block")
	w.SetBlock(x+2, y, z, nb)
	w.SetBlock(x+2, y+1, z, worldgen.Air) // not muffled
	h.playNoteBlock(players, 0, x+2, y, z, nb, 0)
	stepSculk(h, players, 8)

	if s := w.At(x, y, z); sensorPhase(s) != sculkPhaseActive {
		t.Fatalf("the sensor should have heard the note: phase=%d", sensorPhase(s))
	}
	if f := h.sculkFreq[simPos{blockPos: blockPos{x, y, z}}]; f != freqNoteBlockPlay {
		t.Fatalf("frequency = %d, want note_block_play %d", f, freqNoteBlockPlay)
	}
}

// A muffled note block makes no sound and so emits no vibration either.
func TestMuffledNoteBlockIsSilentToSculk(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(0, x, y, z, sensor)

	nb := worldgen.BlockBase("note_block")
	w.SetBlock(x+2, y, z, nb)
	w.SetBlock(x+2, y+1, z, worldgen.BlockBase("stone")) // muffled
	h.playNoteBlock(players, 0, x+2, y, z, nb, 0)
	stepSculk(h, players, 8)

	if s := w.At(x, y, z); sensorPhase(s) == sculkPhaseActive {
		t.Fatal("a muffled note block should not be heard")
	}
}

// TestCatalystBloomClearsAndCalibratedIsShort: SculkCatalystBlock.tick sets
// BLOOM back eight ticks after a bloom (it used to stay on for good), and a
// calibrated sensor stays ACTIVE 10 ticks, not a plain sensor's 30.
func TestCatalystBloomClearsAndCalibratedIsShort(t *testing.T) {
	if calibActiveTicks != 10 || sensorActiveTicks != 30 {
		t.Fatalf("active ticks: calibrated %d plain %d", calibActiveTicks, sensorActiveTicks)
	}
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := simPos{blockPos: blockPos{4, 180, 4}}
	h.world.SetBlock(pos.x, pos.y, pos.z, catalystWith(true))
	h.sculkDue[pos] = h.tick.Load()
	stepSculk(h, players, 2)
	if got := h.world.At(pos.x, pos.y, pos.z); got != catalystWith(false) {
		t.Fatalf("the catalyst should stop blooming when its tick comes: state %d", got)
	}
}

// Wool between a vibration and a sensor hides it (isOccluded), and a block
// event about wool — placing it, or stepping on a wool carpet — makes none
// (#dampens_vibrations). A line that only grazes the wool still gets by.
func TestWoolOccludesAndDampensVibrations(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(0, x, y, z, sensor)
	pos := simPos{blockPos: blockPos{x, y, z}}
	wool := worldgen.BlockBase("white_wool")
	heard := func() bool {
		_, ok := h.sculkVib[pos]
		delete(h.sculkVib, pos)
		return ok
	}

	h.vib(0, 10, x+4, y, z, 0)
	if !heard() {
		t.Fatal("an open line was not heard")
	}
	w.SetBlock(x+2, y, z, wool)
	h.vib(0, 10, x+4, y, z, 0)
	if heard() {
		t.Fatal("a vibration through wool was heard")
	}
	h.vib(0, 10, x+4, y+2, z, 0) // the line from (x+4,y+2) passes over the wool
	if !heard() {
		t.Fatal("a line past the wool, not through it, was occluded")
	}
	w.SetBlock(x+2, y, z, worldgen.Air)

	h.vibOn(0, freqBlockPlace, x+3, y, z, 1, wool)
	if heard() {
		t.Fatal("placing wool was heard")
	}
	carpet := worldgen.BlockBase("white_carpet")
	w.SetBlock(x+3, y-1, z, worldgen.Stone)
	w.SetBlock(x+3, y, z, carpet)
	h.vibStep(0, float64(x)+3.5, float64(y)+0.0625, float64(z)+0.5, 1)
	if heard() {
		t.Fatal("a step on a wool carpet was heard")
	}
	w.SetBlock(x+3, y, z, worldgen.Air)
	h.vibStep(0, float64(x)+3.5, float64(y), float64(z)+0.5, 1)
	if !heard() {
		t.Fatal("a step on stone was not heard")
	}
	_ = players
}

// A catalyst's bloom turns only #sculk_replaceable ground to sculk: a floor
// of planks beside it is left as built.
func TestCatalystSpareBuiltBlocks(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	cat := worldgen.BlockBase("sculk_catalyst") + 1
	w.SetBlock(x, y, z, cat)
	h.sculkIndexOnBlockChange(0, x, y, z, cat)
	planks := worldgen.BlockBase("oak_planks")
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, planks)
		}
	}
	h.catalystHears(players, 0, float64(x)+1.5, float64(y), float64(z)+0.5, 5)
	stepSculk(h, players, 400)
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if w.At(x+dx, y-1, z+dz) != planks {
				t.Fatalf("the bloom changed the planks at %d,%d", x+dx, z+dz)
			}
		}
	}
}

// A sensor built in the Nether hears a mob walking there, as one in the
// overworld does.
func TestNetherSensorHearsFootsteps(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	nw.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	const x, y, z = 4, 100, 4
	for dx := -6; dx <= 6; dx++ {
		for dz := -2; dz <= 2; dz++ {
			nw.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			nw.SetBlock(x+dx, y, z+dz, worldgen.Air)
			nw.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	nw.SetBlock(x, y, z, sensor)
	h.sculkIndexOnBlockChange(dimNether, x, y, z, sensor)
	m := h.spawnMobIn(players, entityPiglin, dimNether, float64(x)+3.5, y, float64(z)+0.5)
	pos := simPos{dim: dimNether, blockPos: blockPos{x, y, z}}
	heard := false
	for i := 0; i < 8 && !heard; i++ {
		m.vx, m.kb = 0.25, 3
		h.tick.Add(1)
		h.updateMobs(players)
		if v, ok := h.sculkVib[pos]; ok && v.src == m.eid {
			heard = true
		}
	}
	if !heard {
		t.Fatal("a Nether sensor did not hear a piglin walking beside it")
	}
}

// A summoned Warden finds its spot as SpawnUtil.trySpawnMob does: within five
// blocks sideways, on a full-topped block with an open cell over it; with no
// ground in range, nowhere.
func TestWardenSpawnSpot(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	w := h.world
	const y = 200
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			for dy := -8; dy <= 8; dy++ {
				w.SetBlock(x, y+dy, z, worldgen.Air)
			}
			w.SetBlock(x, y-3, z, worldgen.Stone)
		}
	}
	at := simPos{blockPos: blockPos{0, y, 0}}
	for i := 0; i < 50; i++ {
		p := h.wardenSpawnSpot(at)
		if p == nil {
			t.Fatal("no spot on a flat floor")
		}
		if abs(p.x) > 5 || abs(p.z) > 5 || p.y != y-2 {
			t.Fatalf("spot %v: want within 5 sideways, standing at y=%d", *p, y-2)
		}
	}
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, y-3, z, worldgen.Air)
		}
	}
	if p := h.wardenSpawnSpot(at); p != nil {
		t.Fatalf("found %v with no ground in range", *p)
	}
}
