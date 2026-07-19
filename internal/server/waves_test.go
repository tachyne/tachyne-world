package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The cosmetic wave overlay (-waves). These tests build a shoreline directly in
// the edit overlay at sea level and assert the wave washes up, rolls back, ties
// itself to real ocean, and NEVER writes to the world (it is a client overlay).

// buildBeach lays a deterministic shoreline near a column: an ocean strip (real
// water at sea level) beside a beach that slopes up away from the water, so its
// sheet cells span the whole wash band. Returns the beach sheet cells by height.
func buildBeach(w *world.World, cx, cz int) map[int]blockPos {
	sl := worldgen.SeaLevel // 63
	// Clear the sea-level band to air across the whole area first.
	for x := cx - 6; x <= cx+6; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 1; y <= waveBandHigh+1; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	// Ocean: real source water filling the sea-level column on the low side.
	for x := cx - 6; x <= cx-2; x++ {
		for z := cz - 3; z <= cz+3; z++ {
			for y := sl - 4; y <= sl-1; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	// Beach: sand steps climbing from the waterline (sheet 63) up the shore
	// (sheet 66), one step per column, with air above each so beachSheet finds it.
	sheets := map[int]blockPos{}
	for i, sheet := 0, sl; sheet <= waveBandHigh; i, sheet = i+1, sheet+1 {
		x := cx + i // cx, cx+1, cx+2, cx+3 → sheet 63,64,65,66
		for z := cz - 3; z <= cz+3; z++ {
			w.SetBlock(x, sheet-1, z, worldgen.Sand) // sand block; sheet = the air above
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

// crestAt must stay within [sea-1, sea-1+reach] and actually oscillate, so the
// wash both climbs the beach and fully drains the shore edge.
func TestCrestBounds(t *testing.T) {
	lo := float64(worldgen.SeaLevel) - 1
	hi := lo + waveReach
	var min, max float64 = 1e9, -1e9
	for tk := uint64(0); tk <= 200; tk++ {
		c := crestAt(0, 0, tk)
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
