package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The cosmetic wave overlay (-waves). These tests build a shoreline directly in
// the edit overlay at sea level and assert the wave washes up, rolls back, ties
// itself to real ocean, and NEVER writes to the world (it is a client overlay).

// buildBeach lays a deterministic shoreline: an ocean strip DIRECTLY beside a
// beach that slopes up one block per column (so the flood-fill can climb it a
// step at a time), spanning the whole wash band. The shore cell (sheet
// SeaLevel) sits right next to ocean at x=cx-1. Returns the sheet cells by
// height.
func buildBeach(w *world.World, cx, cz int) map[int]blockPos {
	sl := worldgen.SeaLevel // 63
	// Clear the band to air across the area first.
	for x := cx - 4; x <= cx+4; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= waveBandHigh+1; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	// Ocean directly left of the beach: source water up to y=sl-1 (62).
	for x := cx - 3; x <= cx-1; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= sl-1; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	// Beach: a smooth one-block-per-column slope. Column cx has its sand top at
	// sl-1 (sheet 63, beside the ocean); each column climbs one more block.
	sheets := map[int]blockPos{}
	for i, sheet := 0, sl; sheet <= waveBandHigh; i, sheet = i+1, sheet+1 {
		x := cx + i
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= sheet-1; y++ { // solid sand up to the floor
				w.SetBlock(x, y, z, worldgen.Sand)
			}
		}
		sheets[sheet] = blockPos{x, sheet, cz}
	}
	return sheets
}

func TestWaveWashesUpAndRollsBack(t *testing.T) {
	h := newHub(world.New(1))
	h.waves = true
	cx, cz := 200, 200
	sheets := buildBeach(h.world, cx, cz)
	pl := riderAt(1, float64(cx)+0.5, float64(worldgen.SeaLevel)+1, float64(cz)+0.5)
	players := map[int32]*tracked{1: pl}

	shore := sheets[worldgen.SeaLevel] // the lowest, wettest sheet cell (y=63)
	top := sheets[waveBandHigh]        // the highest wash-up cell (y=66)

	wetShore, dryShore, wetTop := false, false, false
	// Sweep a couple of wave periods; the travelling crest should at some point
	// cover the shore edge, later leave it dry (roll back), and at its peak reach
	// the top of the beach.
	for tk := uint64(0); tk <= 200; tk++ {
		tg := h.waveTargets(players, tk)
		if _, ok := tg[shore]; ok {
			wetShore = true
		} else {
			dryShore = true
		}
		if _, ok := tg[top]; ok {
			wetTop = true
		}
	}
	if !wetShore {
		t.Error("the shore edge never got a wave — water should wash up")
	}
	if !dryShore {
		t.Error("the shore edge never dried — water should roll back into the ocean")
	}
	if !wetTop {
		t.Error("the crest never reached the top of the beach")
	}
}

func TestWaveNeverWritesWorld(t *testing.T) {
	h := newHub(world.New(1))
	h.waves = true
	cx, cz := 400, 400
	sheets := buildBeach(h.world, cx, cz)
	pl := riderAt(1, float64(cx)+0.5, float64(worldgen.SeaLevel)+1, float64(cz)+0.5)
	players := map[int32]*tracked{1: pl}

	// Run the overlay across a full cycle: it must paint then restore, and the
	// world's sheet cells must stay air the whole time (overlay only).
	for tk := uint64(0); tk <= 200; tk++ {
		h.updateWaves(players, tk)
		for _, p := range sheets {
			if got := h.world.Block(p.x, p.y, p.z); got != worldgen.Air {
				t.Fatalf("wave wrote block %d into the world at %v (must be a client overlay only)", got, p)
			}
		}
	}
}

// TestWaveCannotClimbTwoBlockStep — a 2-block riser at the coast must stay dry
// on top: the wave climbs one block at a time and can't scale it, even when the
// crest peaks above the cliff top.
func TestWaveCannotClimbTwoBlockStep(t *testing.T) {
	h := newHub(world.New(1))
	h.waves = true
	sl := worldgen.SeaLevel
	cx, cz := 800, 800
	for x := cx - 4; x <= cx+4; x++ { // clear
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= waveBandHigh+1; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	for x := cx - 3; x <= cx-1; x++ { // ocean directly left
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= sl-1; y++ {
				h.world.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	// A 2-block ledge at the coast: sand top at sl (sheet sl+1), which is two
	// blocks above the ocean surface (sl-1), with NO one-block shore tier beside
	// it. The wave can only step up one block, so it can never seed or reach it.
	for x := cx; x <= cx+2; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= sl; y++ {
				h.world.SetBlock(x, y, z, worldgen.Sand)
			}
		}
	}
	cliff := blockPos{cx, sl + 1, cz}
	pl := riderAt(1, float64(cx)+0.5, float64(sl)+1, float64(cz)+0.5)
	players := map[int32]*tracked{1: pl}

	for tk := uint64(0); tk <= wavePeriod; tk++ {
		if _, ok := h.waveTargets(players, tk)[cliff]; ok {
			t.Fatalf("wave climbed a 2-block cliff at tick %d — it may only step up one block", tk)
		}
	}
}

// TestWaveEdgesAreFlowing — wave cells carry real water states and at least
// some are FLOWING (non-source) levels, so the edges slope/soften on the client
// rather than every cell being a full source cube.
func TestWaveEdgesAreFlowing(t *testing.T) {
	h := newHub(world.New(1))
	h.waves = true
	cx, cz := 1000, 1000
	buildBeach(h.world, cx, cz)
	pl := riderAt(1, float64(cx)+0.5, float64(worldgen.SeaLevel)+1, float64(cz)+0.5)
	players := map[int32]*tracked{1: pl}

	sawWater, sawFlowing := false, false
	for tk := uint64(0); tk <= wavePeriod; tk++ {
		for _, st := range h.waveTargets(players, tk) {
			if !worldgen.IsWater(st) {
				t.Fatalf("wave cell state %d is not water", st)
			}
			sawWater = true
			if st != worldgen.WaterBase { // a flowing level, not source
				sawFlowing = true
			}
		}
	}
	if !sawWater {
		t.Fatal("no wave water produced")
	}
	if !sawFlowing {
		t.Error("wave never used a flowing level — edges stay hard cubes")
	}
}

func TestWaveNeedsOcean(t *testing.T) {
	h := newHub(world.New(1))
	h.waves = true
	sl := worldgen.SeaLevel
	cx, cz := 600, 600
	// An inland sand flat at sea level with NO ocean anywhere in scan range: the
	// patch must cover the full radius so no generated water leaks into the
	// shoreline check (which reads y=sea-1 across the whole window).
	for x := cx - waveScanR - 1; x <= cx+waveScanR+1; x++ {
		for z := cz - waveScanR - 1; z <= cz+waveScanR+1; z++ {
			for y := sl; y <= waveBandHigh+1; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			h.world.SetBlock(x, sl-1, z, worldgen.Sand)
		}
	}
	pl := riderAt(1, float64(cx)+0.5, float64(sl)+1, float64(cz)+0.5)
	players := map[int32]*tracked{1: pl}

	for tk := uint64(0); tk <= 100; tk++ {
		if tg := h.waveTargets(players, tk); len(tg) != 0 {
			t.Fatalf("inland sand with no shoreline sprouted %d wave cells", len(tg))
		}
	}
}

// crestAt along the reference column (no jitter) must stay within
// [sea-1, sea-1+reach] and both fully drain and fully climb across a cycle.
func TestCrestBounds(t *testing.T) {
	lo := float64(worldgen.SeaLevel) - 1
	hi := lo + waveReach
	var min, max float64 = 1e9, -1e9
	for tk := uint64(0); tk <= 2*wavePeriod; tk++ {
		c := crestAt(0, 0, tk) // waveJitter(0,0)=0 → clean bounds
		if c < lo-1e-6 || c > hi+1e-6 {
			t.Fatalf("crest %f out of band [%f,%f]", c, lo, hi)
		}
		if c < min {
			min = c
		}
		if c > max {
			max = c
		}
	}
	if min > lo+0.5 {
		t.Errorf("crest never dropped near the trough (min %f)", min)
	}
	if max < hi-0.5 {
		t.Errorf("crest never rose near the peak (max %f)", max)
	}
}

// TestWaveBumpPausesAndSwells — each cycle fully washes in (bump→1) and then
// sits fully receded (bump==0) for a contiguous pause of the expected length.
func TestWaveBumpPausesAndSwells(t *testing.T) {
	maxB := 0.0
	run, maxRun := 0, 0
	for p := uint64(0); p < wavePeriod; p++ {
		b := waveBump(p)
		if b < 0 || b > 1.0001 {
			t.Fatalf("bump %f out of [0,1] at p=%d", b, p)
		}
		if b > maxB {
			maxB = b
		}
		if b == 0 {
			run++
			if run > maxRun {
				maxRun = run
			}
		} else {
			run = 0
		}
	}
	if maxB < 0.99 {
		t.Errorf("wave never fully washes in (max bump %.3f)", maxB)
	}
	wantPause := wavePeriod - waveActive // the bare stretch after the swell
	if maxRun < wantPause-2 {
		t.Errorf("pause too short: longest fully-receded run %d ticks, want ~%d", maxRun, wantPause)
	}
}

// TestWaterlineUneven — the static per-column level jitter varies along the
// shore but stays bounded in [-1,1] and never travels in time.
func TestWaterlineUneven(t *testing.T) {
	seen := map[float64]bool{}
	for x := 0; x < 40; x++ {
		j := waveJitter(x, 7)
		if j < -1.0001 || j > 1.0001 {
			t.Fatalf("jitter %.3f out of [-1,1]", j)
		}
		seen[math.Round(j*100)/100] = true
	}
	if len(seen) < 5 {
		t.Errorf("surface jitter barely varies across columns (%d distinct offsets)", len(seen))
	}
	// Static and deterministic: same column, same offset — no travel in time.
	if waveJitter(3, 4) != waveJitter(3, 4) {
		t.Fatal("jitter must be deterministic per column")
	}
}

// TestWaveRecedesFully — a receding wave leaves NO cells behind: once the crest
// drops below the shore tier (the pause), every wave cell is gone.
func TestWaveRecedesFully(t *testing.T) {
	h := newHub(world.New(1))
	h.waves = true
	cx, cz := 1400, 1400
	buildBeach(h.world, cx, cz)
	pl := riderAt(1, float64(cx)+0.5, float64(worldgen.SeaLevel)+1, float64(cz)+0.5)
	players := map[int32]*tracked{1: pl}
	// Find a pause tick (bump 0 → crest below every sheet) and assert emptiness.
	pausedEmpty := false
	for tk := uint64(0); tk <= wavePeriod; tk++ {
		if waveBump(tk) == 0 {
			if len(h.waveTargets(players, tk)) == 0 {
				pausedEmpty = true
			} else {
				t.Fatalf("wave cells remain during the pause at tick %d — water left behind", tk)
			}
		}
	}
	if !pausedEmpty {
		t.Fatal("never observed a pause tick")
	}
}
