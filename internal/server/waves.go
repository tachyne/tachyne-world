package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Cosmetic ocean waves — OPT-IN via -waves, and deliberately NON-VANILLA.
//
// A thin sheet of water washes up the beach from the shoreline and then rolls
// back into the ocean. It is a pure client OVERLAY: wave water is broadcast to
// nearby viewers but NEVER written to the world — no persistence to the .gob,
// no fluid simulation, no collision, no interaction with the vanilla water
// model. That client-only nature is exactly why it "breaks vanilla logic" and
// must be enabled separately; it also makes it impossible to corrupt the save,
// since the server world is never touched.
//
// The wave is a rising/falling CREST HEIGHT, near-uniform across the coast. As
// the crest rises, the waterline sweeps UP the beach slope — water rolling IN
// from the ocean toward the land, wetting progressively higher (further-inland)
// cells; as it falls, the wet band recedes back to the waterline, rolling out
// into the ocean. The motion is perpendicular to the shore (set by the beach's
// own slope), NOT a crest travelling along the coast.
//
// Two refinements give it life. (1) RHYTHM: each cycle is one swell — a quick
// wash-in and a gentler roll-back — followed by a PAUSE where the beach sits
// bare, so waves arrive in distinct pulses rather than a continuous churn.
// (2) UNEVENNESS: a small STATIC (time-independent, so non-travelling) offset
// per column scallops the waterline, so the wet edge isn't a dead-straight line.

const (
	waveReach     = 4.8  // blocks the crest climbs above sea level at the swell's peak
	wavePeriod    = 110  // ticks per full cycle (the swell plus the pause) ≈ 5.5 s
	waveActive    = 62   // ticks of actual in-out motion; the rest (48 ≈ 2.4 s) is the pause
	waveRiseFrac  = 0.40 // fraction of the swell spent washing IN (the rest is a gentler roll-back)
	waveJitterAmp = 0.6  // blocks of static, non-travelling unevenness in the waterline
	waveScanR     = 16   // horizontal scan radius (blocks) around each player
	waveCadence   = 4    // step every 4 ticks (5 Hz) — smooth enough for water

	// The sheet cell (the air over the beach block) lives in this vertical band
	// around sea level. Sand at y ∈ [low-1, high-1] → sheet at y+1 ∈ [low, high].
	waveBandLow  = worldgen.SeaLevel     // 63: lowest sheet cell (shore edge)
	waveBandHigh = worldgen.SeaLevel + 3 // 66: highest the wash climbs
)

// beachFloor is the set of block states a wave washes over. Built once from
// names (BlockBase is a pure lookup) so red-sand desert beaches count too.
var beachFloor = map[uint32]bool{
	worldgen.Sand:                  true,
	worldgen.Gravel:                true,
	worldgen.BlockBase("red_sand"): true,
}

// waveBump is the swell shape over one cycle, in [0,1]: a quick wash-IN (a
// half-cosine rise over the first waveRiseFrac of the active window), a gentler
// roll-BACK (a half-cosine fall over the remainder), then 0 for the rest of the
// period — the PAUSE where the beach sits bare before the next wave.
func waveBump(t uint64) float64 {
	p := int(t % wavePeriod)
	if p >= waveActive {
		return 0 // the pause: fully receded
	}
	u := float64(p) / float64(waveActive) // 0..1 across the active swell
	if u < waveRiseFrac {
		return 0.5 - 0.5*math.Cos(math.Pi*u/waveRiseFrac) // wash IN: 0 → 1
	}
	return 0.5 + 0.5*math.Cos(math.Pi*(u-waveRiseFrac)/(1-waveRiseFrac)) // roll BACK: 1 → 0
}

// waveJitter is a small STATIC (no t → non-travelling) per-column offset in
// [-1,1] built from two incommensurate sines, so the waterline is scalloped
// rather than a straight line but never drifts along the shore.
func waveJitter(x, z int) float64 {
	fx, fz := float64(x), float64(z)
	return 0.5 * (math.Sin(fx*0.7+fz*0.31) + math.Sin(fx*0.23-fz*0.53))
}

// crestAt is the wave's water height over column (x,z) at tick t: sea level
// plus the swell (waveBump) plus the static waterline unevenness. A beach
// column's sheet cell is wet when its y is at or below this value, so a rising
// crest wets higher (further-inland) cells and a falling crest drains them.
func crestAt(x, z int, t uint64) float64 {
	return float64(worldgen.SeaLevel) - 1 + waveReach*waveBump(t) + waveJitterAmp*waveJitter(x, z)
}

// beachSheet finds the air cell just above beach sand/gravel in the sea-level
// band of a column, if there is one. It scans DOWN from the top of the band, so
// a cliff or overhang above the beach correctly disqualifies the column, and it
// ignores deep terrain entirely (cheaper than a full surface probe). Read-only.
func (h *hub) beachSheet(x, z int) (int, bool) {
	for y := waveBandHigh - 1; y >= waveBandLow-1; y-- {
		b := h.world.Block(x, y, z)
		if b == worldgen.Air {
			continue // keep descending to the topmost solid
		}
		if beachFloor[b] {
			return y + 1, true // beach block, with air above it → sheet cell
		}
		return 0, false // topmost solid in the band isn't beach material
	}
	return 0, false // all air through the band → no beach here
}

// waveTargets scans the shoreline near every player and returns the cells that
// should show wave-water this tick. A column only counts when real ocean water
// is somewhere in view, so inland sand flats never sprout waves. Read-only —
// touches no world state.
func (h *hub) waveTargets(players map[int32]*tracked, t uint64) map[blockPos]struct{} {
	out := map[blockPos]struct{}{}
	for _, pl := range players {
		if pl.dim != 0 || pl.y > float64(waveBandHigh+waveScanR) {
			continue // overworld coast only — skip other dims and mountain players
		}
		px, pz := int(math.Floor(pl.x)), int(math.Floor(pl.z))
		var cand []blockPos
		ocean := false
		for dx := -waveScanR; dx <= waveScanR; dx++ {
			for dz := -waveScanR; dz <= waveScanR; dz++ {
				x, z := px+dx, pz+dz
				if !ocean && worldgen.IsWater(h.world.Block(x, worldgen.SeaLevel-1, z)) {
					ocean = true
				}
				sheet, ok := h.beachSheet(x, z)
				if !ok {
					continue
				}
				if float64(sheet) <= crestAt(x, z, t) {
					cand = append(cand, blockPos{x, sheet, z})
				}
			}
		}
		if !ocean {
			continue // no shoreline in view — don't wet inland sand
		}
		for _, p := range cand {
			out[p] = struct{}{}
		}
	}
	return out
}

// updateWaves advances the overlay one step: paint newly-wet cells with water
// and restore cells the wave has left, broadcasting to nearby viewers only. The
// world is never modified — a restore re-sends whatever the world actually
// holds there (air, or a block a player placed on the beach meanwhile).
func (h *hub) updateWaves(players map[int32]*tracked, t uint64) {
	target := h.waveTargets(players, t)
	// Roll back: cells that were wet but no longer are → restore the real block.
	for p := range h.waveWet {
		if _, ok := target[p]; !ok {
			h.broadcastBlock(players, p.x, p.y, p.z, h.world.Block(p.x, p.y, p.z))
			delete(h.waveWet, p)
		}
	}
	// Wash up: newly-wet cells → water (broadcast overlay, not a world edit).
	for p := range target {
		if _, ok := h.waveWet[p]; !ok {
			h.broadcastBlock(players, p.x, p.y, p.z, worldgen.Water)
			h.waveWet[p] = struct{}{}
		}
	}
}
