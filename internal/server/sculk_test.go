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
