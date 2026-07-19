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
// The wave is a rising/falling CREST HEIGHT, uniform across the coast. As the
// crest rises, the waterline sweeps UP the beach slope — water rolling IN from
// the ocean toward the land, wetting progressively higher (further-inland)
// cells; as it falls, the wet band recedes back down to the waterline, rolling
// back out into the ocean. The motion is thus perpendicular to the shore (set
// by the beach's own slope), NOT a crest travelling along the coast. Cells
// higher up the beach only wet near the crest's peak; the shore edge wets and
// fully drains each cycle, so the water genuinely rolls in and back out.

const (
	waveReach   = 4.5  // blocks the crest climbs above sea level at its peak
	waveOmega   = 0.10 // temporal frequency (rad/tick): period ~63 ticks ≈ 3.1 s
	waveScanR   = 16   // horizontal scan radius (blocks) around each player
	waveCadence = 4    // step every 4 ticks (5 Hz) — smooth enough for water

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

// crestAt is the wave's water height at tick t: sea level plus a swell that
// rises and falls in time, uniform along the coast. A beach column's sheet cell
// is wet when its y is at or below this value, so a rising crest wets higher
// (further-inland) cells and a falling crest drains them back toward the ocean.
func crestAt(t uint64) float64 {
	return float64(worldgen.SeaLevel) - 1 + waveReach*(0.5+0.5*math.Sin(float64(t)*waveOmega))
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
				if float64(sheet) <= crestAt(t) {
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
