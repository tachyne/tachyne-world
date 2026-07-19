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
// THIN SHEET, SOFT EDGES: every wet cell is rendered as a shallow FLOWING level
// (a uniform ~half-block body, thinned to a feathered edge where it borders dry
// ground) — never a full source cube, so the wave is a low consistent film, not
// tall chunks of water. The client slopes the flowing surface toward the thin/air
// sides and animates it, giving rounded moving edges. The water sits ON TOP of
// the beach surface only (no falling-water risers down step faces — those left
// overlay water stranded on the client). It stays a pure overlay — no world writes.
//
// FULL COAST: the scan spans the whole broadcast range, so the entire visible
// length of the coast waves, not just a patch around the player.
//
// RHYTHM: each cycle is one swell — a quick wash-in and a gentler roll-back —
// followed by a PAUSE where the beach sits bare, so waves arrive in distinct
// pulses rather than a continuous churn. The crest is UNIFORM across the coast
// (no per-column jitter), so the waterline is consistent and gap-free; its
// natural bend comes from following the beach's own height contour.
//
// CLEANUP: a receding/left-behind cell is restored with a broadcast radius one
// ring WIDER than the paint, so a cell leaving a moving player's scan is still
// cleared even though the player has stepped a chunk past it. And critically the
// RESTORE is sent RELIABLY (sendEvReliable), while paints are best-effort: the
// player out-queue drops under back-pressure, and the full-coast wave emits a
// big burst of block updates, so a dropped PAINT is harmless (a missing cell
// that self-heals next cycle) but a dropped RESTORE would strand water forever
// (the "water left behind" bug). Reliable restores can never be dropped.

const (
	waveReach    = 3.0             // blocks the crest climbs above sea level at the swell's peak
	wavePeriod   = 110             // ticks per full cycle (the swell plus the pause) ≈ 5.5 s
	waveActive   = 62              // ticks of actual in-out motion; the rest (48 ≈ 2.4 s) is the pause
	waveRiseFrac = 0.40            // fraction of the swell spent washing IN (the rest is a gentler roll-back)
	waveScanR    = viewRadius * 16 // scan the full broadcast range so the whole VISIBLE coast waves
	waveCadence  = 4               // step every 4 ticks (5 Hz) — smooth enough for water

	// Wave water is a THIN flowing film, never a source cube: a uniform low body
	// with a thinner feathered edge. Level 0 = source (full 8/8); 1..7 = flowing
	// (1 = 7/8 tall … 7 = 1/8). A uniform body level keeps the surface a
	// CONSISTENT height (no chunky tall/short patchwork).
	waveBodyLevel = 4 // the wave body: a ~half-block (4/8) film
	waveEdgeLevel = 6 // thinner (2/8) where it borders dry ground → a soft taper

	// A left/receding cell is restored with a broadcast radius this many chunks
	// WIDER than the paint (viewRadius), so a cell that leaves a moving player's
	// scan is still cleared once the player has stepped a chunk or two past it.
	waveRestoreRadius = viewRadius + 2

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

// crestAt is the wave's water height at tick t: sea level plus the swell. It is
// UNIFORM across the coast, so a beach cell is wet whenever its sheet is at or
// below it — a solid, connected sheet with a CONSISTENT waterline (no per-column
// holes or ragged edge). The waterline still bends naturally because it follows
// the beach's own height contour. The pause (swell 0) drops it below every sheet
// so the beach fully clears.
func crestAt(t uint64) float64 {
	return float64(worldgen.SeaLevel) - 1 + waveReach*waveBump(t)
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
// returns the cells that should show wave-water this tick, each mapped to the
// water STATE (source or a flowing level) that softens its edges. Water seeds at
// beach cells beside the ocean and climbs inland one block at a time (never up a
// 2-block step), bounded by the crest. Inland sand with no shoreline never
// seeds, so it never waves. Read-only — touches no world state.
func (h *hub) waveTargets(players map[int32]*tracked, t uint64) map[blockPos]uint32 {
	out := map[blockPos]uint32{}
	for _, pl := range players {
		if pl.dim != 0 || pl.y > float64(waveBandHigh+waveScanR) {
			continue // overworld coast only — skip other dims and mountain players
		}
		px, pz := int(math.Floor(pl.x)), int(math.Floor(pl.z))
		crest := crestAt(t) // uniform across the coast → solid sheet, consistent waterline

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
			if !ok || sheet > fromSheet+1 || float64(sheet) > crest {
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

		// Assign each wet cell a THIN flowing level: a uniform low body, thinned
		// to a feathered edge along any side bordering dry ground so the client
		// slopes it there instead of drawing a cube face. Bordering the ocean is
		// not an edge — the water is continuous. The water sits ON the surface
		// only (no source cubes, no risers down step faces), so the sheet is a
		// consistent low film rather than tall chunks.
		isOcean := func(x, z int) bool {
			return worldgen.IsWater(h.world.Block(x, worldgen.SeaLevel-1, z))
		}
		for k := range wet {
			sheet := beach[k]
			lvl := waveBodyLevel
			for _, d := range waveHoriz {
				n := [2]int{k[0] + d[0], k[1] + d[1]}
				if !wet[n] && !isOcean(n[0], n[1]) { // borders dry ground → feather the edge
					lvl = waveEdgeLevel
					break
				}
			}
			out[blockPos{k[0], sheet, k[1]}] = worldgen.WaterBase + uint32(lvl)
		}
	}
	return out
}

// broadcastWaveBlock sends a wave overlay block to overworld players within
// `radius` chunks. `reliable` routes through the guaranteed overflow (for
// restores, which must never be dropped); otherwise it is best-effort (paints,
// which self-heal). Restores also use a wider radius so a cell leaving a moving
// player's scan is still cleared (see waveRestoreRadius).
func (h *hub) broadcastWaveBlock(players map[int32]*tracked, x, y, z int, state uint32, radius int, reliable bool) {
	bcx, bcz := chunkFloor(float64(x)), chunkFloor(float64(z))
	body := blockSetEv(x, y, z, state)
	for _, t := range players {
		if t.dim != 0 {
			continue
		}
		if abs(chunkFloor(t.x)-bcx) <= radius && abs(chunkFloor(t.z)-bcz) <= radius {
			if reliable {
				t.p.sendEvReliable(body)
			} else {
				t.p.trySendEv(body)
			}
		}
	}
}

// updateWaves advances the overlay one step: paint newly-wet cells with water
// and restore cells the wave has left. The world is never modified — a restore
// re-sends whatever the world actually holds there (air, or a block a player
// placed on the beach meanwhile). Restores go out RELIABLY and one ring wider
// than paints, so nothing is stranded on recede or behind a moving player.
func (h *hub) updateWaves(players map[int32]*tracked, t uint64) {
	target := h.waveTargets(players, t)
	// Roll back: cells that were wet but no longer are → restore the real block,
	// RELIABLY (a dropped restore would strand water forever).
	for p := range h.waveWet {
		if _, ok := target[p]; !ok {
			h.broadcastWaveBlock(players, p.x, p.y, p.z, h.world.Block(p.x, p.y, p.z), waveRestoreRadius, true)
			delete(h.waveWet, p)
		}
	}
	// Wash up / re-level: send a cell whose water state is new or has changed
	// (the level shifts as the crest rises and falls). Best-effort — a dropped
	// paint is a harmless missing cell that self-heals on the next pass.
	for p, st := range target {
		if cur, ok := h.waveWet[p]; !ok || cur != st {
			h.broadcastWaveBlock(players, p.x, p.y, p.z, st, viewRadius, false)
			h.waveWet[p] = st
		}
	}
}
