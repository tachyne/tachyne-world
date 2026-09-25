package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The sun is overhead at noon and under the world at midnight, and the eased
// curve is monotone through the day.
func TestSunAngleCurve(t *testing.T) {
	if a := sunAngle(6000); math.Abs(a) > 1e-6 {
		t.Errorf("noon should be angle 0, got %v", a)
	}
	if a := sunAngle(18000); math.Abs(a-math.Pi) > 0.05 {
		t.Errorf("midnight should be about π, got %v", a)
	}
	last := -1.0
	for d := int64(6000); d < 30000; d += 500 {
		a := sunAngle(d % dayLengthTicks)
		if d > 6000 && a < last {
			t.Fatalf("the sun angle must rise through the day: %v then %v", last, a)
		}
		last = a
	}
}

// A daylight detector reads the sky at noon, reads nothing at midnight, and
// a roof over it takes it back to nothing.
func TestDaylightDetectorReadsTheSky(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	det := daylightWith(false, 0)
	w.SetBlock(x, y, z, det)

	h.dayTime.Store(6000) // noon
	h.updateDaylight(players, blockPos{x, y, z}, w.At(x, y, z))
	noon := daylightPower(w.At(x, y, z))
	if noon < 12 {
		t.Fatalf("a detector under an open sky at noon should read high, got %d", noon)
	}

	h.dayTime.Store(18000) // midnight
	h.updateDaylight(players, blockPos{x, y, z}, w.At(x, y, z))
	if p := daylightPower(w.At(x, y, z)); p != 0 {
		t.Fatalf("a detector reads nothing at midnight, got %d", p)
	}

	// Inverted, the same midnight reads high — that is what it is for.
	w.SetBlock(x, y, z, daylightWith(true, 0))
	h.updateDaylight(players, blockPos{x, y, z}, w.At(x, y, z))
	if p := daylightPower(w.At(x, y, z)); p < 10 {
		t.Fatalf("an inverted detector should read high at night, got %d", p)
	}

	// Wall it in: no sky light reaches it, so it reads nothing even at noon.
	w.SetBlock(x, y, z, daylightWith(false, 0))
	for dy := 1; dy <= 3; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		w.SetBlock(x+d[0], y, z+d[1], worldgen.Stone)
	}
	h.dayTime.Store(6000)
	h.updateDaylight(players, blockPos{x, y, z}, w.At(x, y, z))
	if p := daylightPower(w.At(x, y, z)); p != 0 {
		t.Fatalf("a roofed detector reads nothing, got %d", p)
	}
}

// A spider hunts by LIGHT, not by the clock: it stays neutral in a lit room
// at night and hunts in a dark one at noon.
func TestSpiderNeutralByLight(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(x)+1, float64(y), float64(z)
	players[pl.p.eid] = pl
	m := h.spawnMob(players, entitySpider, float64(x), float64(y), float64(z))
	m.hostile = true

	// Under an open sky at noon: too bright, no hunt.
	h.dayTime.Store(6000)
	h.acquireTarget(players, m)
	if m.hasTarget {
		t.Error("a spider in broad daylight must not pick a target")
	}
	// Roof it over (leaving head room, so the two can still see each other)
	// and it hunts, clock unchanged.
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			w.SetBlock(x+dx, y+3, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+2, z+dz, worldgen.Air)
		}
	}
	if got := h.lightMagic(m); got >= spiderLightNeutral {
		t.Fatalf("the fixture should be dark, brightness=%v", got)
	}
	h.acquireTarget(players, m)
	if !m.hasTarget {
		t.Error("a spider in the dark hunts whatever the clock says")
	}
}

// DaylightDetectorBlock.updateSignalStrength does nothing where the
// dimension has no sky light: an inverted detector in the Nether keeps its
// power rather than reading sky 0 as a full 15.
func TestDaylightDetectorIdleWithoutSkyLight(t *testing.T) {
	h, _, players, _, _, _ := redSetup(t)
	nw, err := world.NewNether(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.nether = nw
	nw.SetBlock(0, 60, 0, daylightWith(true, 0))
	h.inDim(dimNether, func() { h.updateDaylight(players, blockPos{0, 60, 0}, nw.At(0, 60, 0)) })
	if p := daylightPower(nw.At(0, 60, 0)); p != 0 {
		t.Fatalf("an inverted Nether detector should stay at 0, got %d", p)
	}
}
