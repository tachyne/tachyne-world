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
// The wave is an advancing/retreating FRONT measured as STRAIGHT-LINE distance
// from the ocean (a distance transform), NOT path distance. Each connected beach
// region's front `reach` grows from 0 up to that region's own furthest cell as
// the swell rises and shrinks back as it falls; a cell is wet while its distance
// ≤ reach. Straight-line distance is what keeps the waterline SOLID: a cell
// tucked behind a small rise still wets the instant the front reaches its spot,
// instead of waiting on a long go-around PATH and leaving a dry gap (which is
// what path distance did — visible as scattered dry sand once the wave reached
// far enough to have detours). It sweeps IN and RETREATS smoothly both ways,
// even across a flat.
//
// WHICH cells may be wet is simply the 2-tier beach band (beachSheet): every
// beach cell inside the front ring is wet, with NO reachability flood-fill — so a
// low pocket tucked behind a rise still fills instead of being left as a dry
// patch. The band caps the climb to the shore's first two steps, and a cell with
// no ocean within reach is never wet, so it stays tied to the coast.
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

// wave8 are the eight steps the straight-line distance transform spreads across.
var wave8 = [8][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

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

		// Straight-line distance to the nearest ocean for every column in range: an
		// 8-direction distance transform (≈ Chebyshev distance). Because it ignores
		// the sand's ups and downs, the front is a smooth ring — no cell is skipped
		// for being behind a little rise (the path-distance gap).
		size := 2*waveScanR + 1
		sd := make([]int16, size*size)
		for i := range sd {
			sd[i] = -1
		}
		var sq [][2]int
		for dx := -waveScanR; dx <= waveScanR; dx++ {
			for dz := -waveScanR; dz <= waveScanR; dz++ {
				if worldgen.IsWater(h.world.Block(px+dx, worldgen.SeaLevel-1, pz+dz)) {
					sd[(dx+waveScanR)*size+(dz+waveScanR)] = 0
					sq = append(sq, [2]int{dx, dz})
				}
			}
		}
		for head := 0; head < len(sq); head++ {
			c := sq[head]
			cd := sd[(c[0]+waveScanR)*size+(c[1]+waveScanR)]
			if int(cd) >= waveReachCap {
				continue
			}
			for _, d := range wave8 {
				nx, nz := c[0]+d[0], c[1]+d[1]
				if nx < -waveScanR || nx > waveScanR || nz < -waveScanR || nz > waveScanR {
					continue
				}
				i := (nx+waveScanR)*size + (nz + waveScanR)
				if sd[i] == -1 {
					sd[i] = cd + 1
					sq = append(sq, [2]int{nx, nz})
				}
			}
		}
		sdistOf := func(k [2]int) int {
			if v := sd[(k[0]-px+waveScanR)*size+(k[1]-pz+waveScanR)]; v >= 0 {
				return int(v)
			}
			return waveReachCap + 1 // no ocean within reach — never wet
		}

		// The extent = the furthest in-band beach cell from the ocean; the front
		// reach = extent × swell. Every beach cell within the straight-line ring is
		// wet — no reachability flood-fill, so a low pocket behind a rise fills too
		// (no dry patch). The 2-tier band still caps the climb, and cells with no
		// ocean within reach are never wet, so it stays tied to the coast.
		maxDist := 0
		for k := range beach {
			if s := sdistOf(k); s <= waveReachCap && s > maxDist {
				maxDist = s
			}
		}
		if maxDist == 0 {
			continue // no beach within reach of the ocean
		}
		thr := int(float64(maxDist)*bump + 0.5)

		// Wet every beach cell inside the front. Level grades by how far behind the
		// front the cell sits: thin at the leading/receding edge, fuller toward the
		// sea, capped to flowing (never source).
		for k, sheet := range beach {
			s := sdistOf(k)
			if s > thr {
				continue
			}
			out[blockPos{k[0], sheet, k[1]}] = worldgen.WaterBase + uint32(waterLevelForDepth(float64(thr-s)*0.5))
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
