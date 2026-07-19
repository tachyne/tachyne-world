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
// The wave is an advancing/retreating FRONT measured as SHORE DISTANCE from the
// ocean. A BFS gives every shore cell its distance (steps from the water); each
// connected beach region's front `reach` grows from 0 up to that region's OWN
// full extent as the swell rises and shrinks back to 0 as it falls, and a cell
// is wet while its distance ≤ reach. So the wave reaches exactly as far as that
// beach's sand extends (up to the second step) — not a fixed distance — and the
// waterline sweeps IN and RETREATS smoothly in BOTH directions, even across a
// FLAT (a vertical crest would instead pop the whole same-height area on/off).
//
// The BFS steps to a neighbour only if it is at most ONE block higher, so the
// wave climbs a gentle slope but CANNOT scale a 2-block riser — a cliff at the
// coast stays dry on top. The ocean seeds only the shore tier (a 2-block ledge
// can't seed). The 2-tier band + beach-sand material bound the extent: the sand
// running out, or rising past the second step, is what stops each region.
//
// SMOOTH THIN SHEET: every wet cell is rendered as a FLOWING level graded by how
// far BEHIND the front it sits — thin at the leading/receding edge, ramping
// fuller toward the sea. So the wave FADES in and out gradually and its surface
// RAMPS up the shore (the fuller lower tier meets the thinner upper tier without
// a hard full-block step). Capped to flowing levels (never a full source cube),
// so it stays a low sheet and the client slopes every cell at its air edges +
// animates the flow. Water sits ON TOP of the beach surface only (no falling-
// water risers down step faces — those left overlay water stranded on the
// client). Pure overlay — no world writes.
//
// FULL COAST: the scan spans the whole broadcast range, so the entire visible
// length of the coast waves, not just a patch around the player.
//
// RHYTHM: each cycle is one swell — a quick wash-in and a gentler roll-back —
// followed by a PAUSE where the beach sits bare, so waves arrive in distinct
// pulses rather than a continuous churn. The front is the same across the coast
// (waveMaxReach × the swell), so the waterline is consistent and gap-free; its
// bend comes naturally from the shore's own shape (the distance field).
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
	waveReachCap = 24              // SAFETY cap on shore-distance BFS: the wave never washes further inland than this (rarely binds — the 2-tier sand runs out first)
	wavePeriod   = 110             // ticks per full cycle (the swell plus the pause) ≈ 5.5 s
	waveActive   = 62              // ticks of actual in-out motion; the rest (48 ≈ 2.4 s) is the pause
	waveRiseFrac = 0.40            // fraction of the swell spent washing IN (the rest is a gentler roll-back)
	waveScanR    = viewRadius * 16 // scan the full broadcast range so the whole VISIBLE coast waves
	waveCadence  = 4               // step every 4 ticks (5 Hz) — smooth enough for water

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

// isWaveFloor reports whether a wave washes over this block: any full solid
// ground surface (sand, gravel, dirt, grass, stone, clay, …), NOT just sand — so
// the wash is CONTINUOUS along a real shore instead of covering only sand columns
// and leaving the dirt/grass/rock ones between them as dry gaps (which also block
// the flood-fill past them). Fluids and leaves are excluded. The 2-tier band + the
// per-region reach still bound how far/high it goes.
func isWaveFloor(state uint32) bool {
	return worldgen.IsSolidFull(state) && !worldgen.IsFluid(state) && !worldgen.IsLeaves(state)
}

// isWavePassable reports whether the wave can occupy this cell above the ground:
// air or ANY non-solid decoration (grass, ferns, every flower, dead bushes, …) —
// things water floods over. Only solid blocks and fluids block it. Using this
// (not a short whitelist of plants) is what keeps a flowery/grassy shore from
// leaving a dry gap on every decorated column.
func isWavePassable(state uint32) bool {
	return !worldgen.IsSolidFull(state) && !worldgen.IsFluid(state)
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

// beachSheet finds the air cell just above solid shore ground in the sea-level
// band of a column, if there is one. It scans DOWN from the top of the band, so
// a cliff or overhang above the shore correctly disqualifies the column, and it
// ignores deep terrain entirely (cheaper than a full surface probe). Read-only.
func (h *hub) beachSheet(x, z int) (int, bool) {
	for y := waveBandHigh - 1; y >= waveBandLow-1; y-- {
		b := h.world.Block(x, y, z)
		if isWavePassable(b) {
			continue // air or a plant/decoration — keep descending to the ground
		}
		if isWaveFloor(b) && isWavePassable(h.world.Block(x, y+1, z)) {
			return y + 1, true // solid ground with air/plants above → the sheet cell
		}
		return 0, false // topmost solid in the band isn't washable ground
	}
	return 0, false // nothing but air/plants through the band → no ground here
}

// waveHoriz are the four horizontal steps the flood-fill spreads across.
var waveHoriz = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// waterLevelForDepth maps a cell's "depth" behind the wave front (d, in blocks)
// to a flowing water level: a thin film at the leading/receding edge, ramping to
// nearly full toward the sea. Water level 0 = source (full 8/8), 1..7 = flowing
// (1 = 7/8 … 7 = 1/8). CAPPED to flowing levels (never source), so every cell
// slopes at its edges and the surface RAMPS smoothly rather than stepping.
func waterLevelForDepth(d float64) int {
	lvl := 7 - int(d*6+0.5) // d≈0 → 7 (1/8 film) … d≈1 → 1 (7/8)
	if lvl < 1 {
		lvl = 1
	}
	if lvl > 7 {
		lvl = 7
	}
	return lvl
}

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
		bump := waveBump(t)
		if bump == 0 {
			continue // the pause — the beach is bare
		}

		// Every beach cell (air/plants over sea-level sand) in range, by column.
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

		// BFS the shore-DISTANCE of every reachable beach cell (steps from the
		// ocean). Water may step to a neighbour only if it is at most ONE block
		// higher (so it can't scale a 2-block riser), and the ocean seeds only the
		// shore tier (SeaLevel-1 surface → sheet 63). Pruned at waveReachCap. The
		// 2-tier band (beachSheet) is what actually stops it — where the sand rises
		// past the second step, or the sand ends, the cells stop being beach.
		dist := map[[2]int]int{}
		var queue [][2]int
		visit := func(x, z, fromSheet, d int) {
			k := [2]int{x, z}
			if _, ok := dist[k]; ok || d > waveReachCap {
				return
			}
			sheet, ok := beach[k]
			if !ok || sheet > fromSheet+1 {
				return
			}
			dist[k] = d
			queue = append(queue, k)
		}
		for k := range beach { // seeds: shore cells beside the ocean, distance 1
			for _, d := range waveHoriz {
				if worldgen.IsWater(h.world.Block(k[0]+d[0], worldgen.SeaLevel-1, k[1]+d[1])) {
					visit(k[0], k[1], worldgen.SeaLevel-1, 1)
					break
				}
			}
		}
		for len(queue) > 0 { // grow inland, one-block steps only, counting distance
			cur := queue[0]
			queue = queue[1:]
			cs, cd := beach[cur], dist[cur]
			for _, d := range waveHoriz {
				visit(cur[0]+d[0], cur[1]+d[1], cs, cd+1)
			}
		}

		// Label connected beach regions and find how far EACH one extends (its max
		// shore-distance = where its 2-tier sand runs out). The front for a region
		// is its OWN extent × the swell, so the wave reaches as far as that beach's
		// second step allows — not a fixed distance — and still recedes gradually
		// across whatever length that is.
		comp := map[[2]int]int{}
		var compMax []int
		var stack [][2]int
		for start := range dist {
			if _, ok := comp[start]; ok {
				continue
			}
			cid, mx := len(compMax), 0
			stack = append(stack[:0], start)
			comp[start] = cid
			for len(stack) > 0 {
				c := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if dist[c] > mx {
					mx = dist[c]
				}
				for _, d := range waveHoriz {
					n := [2]int{c[0] + d[0], c[1] + d[1]}
					if _, in := dist[n]; in {
						if _, done := comp[n]; !done {
							comp[n] = cid
							stack = append(stack, n)
						}
					}
				}
			}
			compMax = append(compMax, mx)
		}

		// Wet every cell within its region's current front. Level grades by how far
		// BEHIND the front a cell sits: thin at the leading/receding edge, fuller
		// toward the sea, capped to flowing (never source). Surface only, no risers.
		for k, d := range dist {
			reach := float64(compMax[comp[k]]) * bump
			if d > int(reach+0.5) {
				continue
			}
			hd := reach - float64(d)
			if hd < 0 {
				hd = 0
			}
			out[blockPos{k[0], beach[k], k[1]}] = worldgen.WaterBase + uint32(waterLevelForDepth(hd*0.5))
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
