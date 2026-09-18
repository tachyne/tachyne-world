package server

import (
	"testing"

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
	h.sculkIndexOnBlockChange(x, y, z, sensor)
	w.SetBlock(x+1, y, z, worldgen.BlockBase("redstone_wire")+1160)

	// A frequency-10 event 3 blocks away (delay 3 ticks; power falls with range).
	h.gameEvent(10, x+3, y, z, 0)
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
	if h.sculkFreq[blockPos{x, y, z}] != 10 {
		t.Fatalf("sensor comparator frequency = %d, want 10", h.sculkFreq[blockPos{x, y, z}])
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
	h.sculkIndexOnBlockChange(x, y, z, calib)
	// facing north → back is south (z+1); a redstone block there sets back signal 15.
	bdx, bdz := calibBackDelta(calib)
	w.SetBlock(x+bdx, y, z+bdz, redstoneBlock)

	// Wrong frequency (1) is filtered out.
	h.gameEvent(1, x+2, y, z, 0)
	stepSculk(h, players, 6)
	if sensorPhase(w.At(x, y, z)) != sculkPhaseInactive {
		t.Fatal("calibrated sensor must ignore a frequency != its back signal")
	}
	// Matching frequency (15) activates it.
	h.gameEvent(15, x+2, y, z, 0)
	stepSculk(h, players, 6)
	if sensorPhase(w.At(x, y, z)) != sculkPhaseActive {
		t.Fatal("calibrated sensor should activate on its tuned frequency")
	}
}

func TestSculkCatalystSpreadsOnDeath(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	cat := worldgen.BlockBase("sculk_catalyst") + 1
	w.SetBlock(x, y, z, cat)
	h.sculkIndexOnBlockChange(x, y, z, cat)
	// A patch of stone around the death spot for the bloom to convert.
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
		}
	}
	m := &mob{eid: 9001, dim: 0, x: float64(x) + 0.5, y: float64(y), z: float64(z) + 0.5}
	if !h.catalystConsume(players, m, 5) {
		t.Fatal("catalyst within 8 blocks should consume the death XP")
	}
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
	h.sculkIndexOnBlockChange(x, y, z, sensor)
	pos := blockPos{x, y, z}
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
	disp := worldgen.BlockID("dispenser")
	w.SetBlock(x+2, y, z, disp)
	h.openBin(pl, x+2, y, z)
	if f := heard(); f != freqContainerOpen {
		t.Fatalf("a container opened should be heard at 10, got %d", f)
	}
	h.closeWindow(players, pl)
	if f := heard(); f != freqContainerClose {
		t.Fatalf("a container closed should be heard at 9, got %d", f)
	}
	// A toggle marked quiet suppresses the placement vibration of the block
	// change that follows it in the same tick.
	h.vibQuiet[blockPos{x + 4, y, z}] = h.tick.Load()
	h.onBlock(players, evBlock{x: x + 4, y: y, z: z, state: lever, by: pl.p.eid})
	if f := heard(); f != 0 {
		t.Fatalf("a toggled block should not count as placed: heard %d", f)
	}
	h.onBlock(players, evBlock{x: x + 5, y: y, z: z, state: lever, by: pl.p.eid})
	if f := heard(); f != freqBlockPlace {
		t.Fatalf("a placed block should be heard at 13, got %d", f)
	}
}
