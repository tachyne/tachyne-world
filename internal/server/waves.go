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
// The wet set is a FLOOD-FILL from the shoreline, not a pure height test: water
// starts at beach cells one step above the ocean's surface and may step to a
// neighbour only if that cell is at most ONE block higher (and within the crest
// this tick). So it climbs a gentle slope one block at a time but CANNOT scale
// a 2-block riser — a cliff at the coast stays dry on top instead of water
// teleporting onto it. Combined with the 2-tier band, a wave reaches at most
// the shore tier plus one step inland.
//
// Two refinements give it life. (1) RHYTHM: each cycle is one swell — a quick
// wash-in and a gentler roll-back — followed by a PAUSE where the beach sits
// bare, so waves arrive in distinct pulses rather than a continuous churn.
// (2) UNEVENNESS: a small STATIC (time-independent, so non-travelling) offset
// per column scallops the waterline, so the wet edge isn't a dead-straight line.

const (
	waveReach     = 3.0  // blocks the crest climbs above sea level at the swell's peak
	wavePeriod    = 110  // ticks per full cycle (the swell plus the pause) ≈ 5.5 s
	waveActive    = 62   // ticks of actual in-out motion; the rest (48 ≈ 2.4 s) is the pause
	waveRiseFrac  = 0.40 // fraction of the swell spent washing IN (the rest is a gentler roll-back)
	waveJitterAmp = 0.6  // blocks of static, non-travelling unevenness in the waterline
	waveScanR     = 16   // horizontal scan radius (blocks) around each player
	waveCadence   = 4    // step every 4 ticks (5 Hz) — smooth enough for water

	// The sheet cell (the air over the beach block) lives in this vertical band
	// around sea level. Sand at y ∈ [low-1, high-1] → sheet at y+1 ∈ [low, high].
	// The band height caps how many water tiers a wave climbs: [63,64] = at most
	// TWO tiers (the shore edge 63 and one step up to 64), never higher.
	waveBandLow  = worldgen.SeaLevel     // 63: lowest sheet cell (shore edge)
	waveBandHigh = worldgen.SeaLevel + 1 // 64: highest tier the wash reaches
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
		if beachFloor[b] && h.world.Block(x, y+1, z) == worldgen.Air {
			return y + 1, true // beach block with air above → the sheet cell
		}
		return 0, false // topmost solid in the band isn't a beach surface
	}
	return 0, false // all air through the band → no beach here
}

// waveHoriz are the four horizontal steps the flood-fill spreads across.
var waveHoriz = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// waveTargets flood-fills the wave from the shoreline near every player and
// returns the cells that should show wave-water this tick. Water seeds at beach
// cells beside the ocean and climbs inland one block at a time (never up a
// 2-block step), bounded by the crest. Inland sand with no shoreline never
// seeds, so it never waves. Read-only — touches no world state.
func (h *hub) waveTargets(players map[int32]*tracked, t uint64) map[blockPos]struct{} {
	out := map[blockPos]struct{}{}
	for _, pl := range players {
		if pl.dim != 0 || pl.y > float64(waveBandHigh+waveScanR) {
			continue // overworld coast only — skip other dims and mountain players
		}
		px, pz := int(math.Floor(pl.x)), int(math.Floor(pl.z))

		// Every beach cell (air over sea-level sand/gravel) in range, by column.
		beach := map[[2]int]int{}
		for dx := -waveScanR; dx <= waveScanR; dx++ {
			for dz := -waveScanR; dz <= waveScanR; dz++ {
				x, z := px+dx, pz+dz
				if sheet, ok := h.beachSheet(x, z); ok {
					beach[[2]int{x, z}] = sheet
				}
			}
		}
		if len(beach) == 0 {
			continue
		}

		// Flood-fill: reach a beach cell only if it is at most ONE block higher
		// than where the water comes from and its sheet is at or below the crest
		// this tick. The ocean acts as a virtual wet cell at its own surface
		// (SeaLevel-1), so only the shore tier (63) seeds directly — a ledge two
		// blocks above the water can't seed, only be reached via a 63 neighbour.
		wet := map[[2]int]bool{}
		var queue [][2]int
		reach := func(x, z, fromSheet int) {
			k := [2]int{x, z}
			if wet[k] {
				return
			}
			sheet, ok := beach[k]
			if !ok || sheet > fromSheet+1 || float64(sheet) > crestAt(x, z, t) {
				return
			}
			wet[k] = true
			queue = append(queue, k)
		}
		// Seeds: beach cells with an ocean neighbour (ocean = virtual sheet at its
		// surface, SeaLevel-1, so only the 63 shore tier seeds).
		for k := range beach {
			for _, d := range waveHoriz {
				if worldgen.IsWater(h.world.Block(k[0]+d[0], worldgen.SeaLevel-1, k[1]+d[1])) {
					reach(k[0], k[1], worldgen.SeaLevel-1)
					break
				}
			}
		}
		// Grow inland, one-block steps only.
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			cs := beach[cur]
			for _, d := range waveHoriz {
				reach(cur[0]+d[0], cur[1]+d[1], cs)
			}
		}
		for k := range wet {
			out[blockPos{k[0], beach[k], k[1]}] = struct{}{}
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
