package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Potent sulfur (26.3): the block at the bottom of the sulfur caves' pools.
// Its state follows what is around it (PotentSulfurBlock.validBlockState):
//
//   - DRY: no water source above it.
//   - WET: water above — a vent of noxious gas that gives Nausea to whatever
//     breathes it at the surface.
//   - DORMANT / ERUPTING: water above and a magma block below — a geyser
//     that waits (10×(water−1) + 15..30 seconds), erupts (water−1 + 1..2
//     seconds), and waits again, gassing the surface while it waits.
//   - CONTINUOUS: water above and a lava source below — a geyser that never
//     stops.
//
// An erupting or continuous geyser lifts everything in the column above it
// (LAUNCH_ENTITY_TICKER): +0.2 upward per tick while the entity rises slower
// than 0.3 + 0.1 per block of water. The column reaches six blocks per block
// of water, stopped by the first block with a collision shape. The client
// draws the plume and the bubbles from the block state, runs the same launch
// on its own player (movement is the client's), and plays the eruption from
// the block event onPlace sends.
//
// The block entity's only server state is the countdown. Vanilla saves it;
// the engine keeps it in memory. A restart costs a geyser nothing it could
// notice: the countdown is drawn from a per-position stream
// (geyserPositional), so the fresh draw is the same number the geyser drew
// before, and the worst case is one period waited or erupted again.

const (
	psDry = iota
	psWet
	psDormant
	psErupting
	psContinuous
)

const (
	psWaterAllowed      = 4          // PotentSulfurBlock.ALLOWED_WATER_BLOCKS_ABOVE
	psEffectEvery       = 10         // PotentSulfurBlockEntity.EFFECT_APPLICATION_FREQUENCY_TICKS
	psNauseaTicks       = 80         // EFFECT_DURATION_IN_TICKS
	psEffectReachSq     = 9.0        // canBeReachedByNoxiousGas: EFFECT_RANGE 3, squared
	psEffectInflate     = 2.5        // getNearbyLivingEntities: the source cell inflated 2.5 sideways
	psCountdownEvery    = 20         // SERVER_WAITING_COUNTDOWN_TICKER runs once a second
	psLaunchBase        = 0.3        // GEYSER_BASE_LAUNCH_SPEED
	psLaunchPerWater    = 0.1        // …plus this per block of water
	psLaunchForce       = 0.2        // GEYSER_LAUNCH_FORCE
	psColumnPerWater    = 6          // getUnobstructedBlockCount: 6 × the water blocks
	psGeyserSalt        = -904011478 // PotentSulfurBlockEntity.GEYSER_SALT
	psFallCapSpeed      = -0.5       // Entity.checkFallDistanceAccumulation
	psPlayerWidth       = 0.6
	psPlayerHeight      = 1.8
	psPlayerSneakHeight = 1.5
	psItemSize          = 0.25
)

var (
	potentSulfurLo, potentSulfurHi, _ = worldgen.BlockRangeOK("potent_sulfur")
	potentSulfurInfo, _               = worldgen.InfoForState(potentSulfurLo)
	potentSulfurReg, _                = worldgen.BlockRegistryID("potent_sulfur")
	caveAirState                      = worldgen.BlockBase("cave_air")
	voidAirState                      = worldgen.BlockBase("void_air")
	psStateNames                      = [...]string{"dry", "wet", "dormant", "erupting", "continuous"}
)

func isPotentSulfur(st uint32) bool {
	return potentSulfurLo != 0 && st >= potentSulfurLo && st <= potentSulfurHi
}

// psKind is a potent sulfur state's POTENT_SULFUR_STATE.
func psKind(st uint32) int {
	switch worldgen.GetProperty(potentSulfurInfo, st, "potent_sulfur_state") {
	case "wet":
		return psWet
	case "dormant":
		return psDormant
	case "erupting":
		return psErupting
	case "continuous":
		return psContinuous
	}
	return psDry
}

// psState is the potent sulfur state of a kind.
func psState(kind int) uint32 {
	return worldgen.SetProperty(potentSulfurInfo, potentSulfurLo, "potent_sulfur_state", psStateNames[kind])
}

// psWaterSource is getFluidState().isSourceOfType(WATER): source water, a
// bubble column, a waterlogged block, or an underwater plant.
func psWaterSource(st uint32) bool {
	if worldgen.IsWater(st) {
		return worldgen.IsFluidSource(st, worldgen.WaterBase)
	}
	return worldgen.HoldsWater(st)
}

// psWaterBlock is state.is(Blocks.WATER): the fluid block itself, at any
// level — a bubble column is its own block.
func psWaterBlock(st uint32) bool {
	return worldgen.IsWater(st) && !worldgen.IsBubbleColumn(st)
}

func psAir(st uint32) bool { return st == worldgen.Air || st == caveAirState || st == voidAirState }

// psPassable is isGeyserPassableBlock: air and water always, anything else
// only without a collision shape.
func psPassable(st uint32) bool {
	return psAir(st) || psWaterBlock(st) || !worldgen.Collides(st)
}

// potentSulfurValid is PotentSulfurBlock.validBlockState for the block st at
// pos: the state it should hold given the water above and the block below.
// A periodic geyser keeps ERUPTING through a neighbour change; any other
// state becomes DORMANT (the countdown reset is the caller's, see
// potentSulfurChanged).
func potentSulfurValid(w *world.World, pos blockPos, st uint32) uint32 {
	if !psWaterSource(w.At(pos.x, pos.y+1, pos.z)) {
		return psState(psDry)
	}
	below := w.At(pos.x, pos.y-1, pos.z)
	switch {
	case worldgen.IsFluidSource(below, worldgen.LavaBase): // #causes_continuous_geyser_eruptions, a source
		return psState(psContinuous)
	case below == magmaBlockState: // #causes_periodic_geyser_eruptions
		if psKind(st) == psErupting {
			return st
		}
		return psState(psDormant)
	}
	return psState(psWet)
}

// psGasSource is findNoxiousGasSourceBlock: up through at most four cells of
// source water (or passable waterlogged blocks) to the first open cell, which
// is where the gas comes out. ok=false when the column is capped or deeper
// than four.
func psGasSource(w *world.World, pos blockPos) (blockPos, bool) {
	for y := pos.y + 1; y <= pos.y+psWaterAllowed+1; y++ {
		st := w.At(pos.x, y, pos.z)
		if !psWaterSource(st) || !psWaterBlock(st) && !psPassable(st) {
			if psAir(st) || psPassable(st) {
				return blockPos{pos.x, y, pos.z}, true
			}
			return blockPos{}, false
		}
	}
	return blockPos{}, false
}

// psColumnHeight is getUnobstructedBlockCount from the cell above the geyser:
// six cells per block of water, cut at the first that is not passable.
func psColumnHeight(w *world.World, pos blockPos, water int) int {
	n := psColumnPerWater * water
	for i := 0; i < n; i++ {
		if !psPassable(w.At(pos.x, pos.y+1+i, pos.z)) {
			return i
		}
	}
	return n
}

// sulfurVent is PotentSulfurBlockEntity's server state.
type sulfurVent struct {
	countdown int // waitingCountdown; -1 = draw a fresh one
}

// noteVent keeps the vent registry in step with a cell's state.
func (h *hub) noteVent(dim int, pos blockPos, st uint32) *sulfurVent {
	key := simPos{dim: dim, blockPos: pos}
	if !isPotentSulfur(st) {
		delete(h.vents, key)
		return nil
	}
	if h.vents == nil {
		h.vents = map[simPos]*sulfurVent{}
	}
	v := h.vents[key]
	if v == nil {
		v = &sulfurVent{countdown: -1}
		h.vents[key] = v
	}
	return v
}

// discoverVents registers the potent sulfur a chunk holds — generated or
// placed — the first time this pod loads it (primeFluids' pass).
func (h *hub) discoverVents(dim int, w *world.World, cx, cz int32, edits []world.EditedBlock) {
	if potentSulfurLo == 0 {
		return
	}
	for _, p := range w.GeneratedStatesIn(cx, cz, potentSulfurLo, potentSulfurHi) {
		if st := w.At(p[0], p[1], p[2]); isPotentSulfur(st) {
			h.noteVent(dim, blockPos{p[0], p[1], p[2]}, st)
		}
	}
	for _, e := range edits {
		if isPotentSulfur(e.State) {
			h.noteVent(dim, blockPos{int(cx)*16 + e.LX, e.Y, int(cz)*16 + e.LZ}, e.State)
		}
	}
}

// potentSulfurChanged is the block entity's side of a state change at pos:
// registration, validBlockState's countdown reset (a cell that becomes
// DORMANT from anything but a periodic geyser starts a fresh wait), and
// onPlace — an eruption starting, from placement, a neighbour or the
// countdown, sends the block event, the start sound and BLOCK_ACTIVATE.
func (h *hub) potentSulfurChanged(players map[int32]*tracked, dim int, pos blockPos, old, st uint32) {
	if !isPotentSulfur(old) && !isPotentSulfur(st) {
		return
	}
	v := h.noteVent(dim, pos, st)
	if v == nil || old == st {
		return
	}
	oldKind := -1
	if isPotentSulfur(old) {
		oldKind = psKind(old)
	}
	kind := psKind(st)
	if kind == psDormant && oldKind != psErupting && oldKind != psDormant {
		v.countdown = -1 // PotentSulfurBlockEntity.resetCountdown
	}
	if kind != psErupting && kind != psContinuous {
		return
	}
	h.toNearbyEv(players, dim, float64(pos.x)+0.5, float64(pos.z)+0.5, attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 0, Param: 0, Block: int32(potentSulfurReg)})
	sound := "minecraft:block.potent_sulfur.geyser_eruption" // GEYSER_ERUPTION_START
	if kind == psContinuous {
		sound = "minecraft:block.potent_sulfur.geyser_continuous_eruption" // GEYSER_CONTINUOUS_START
	}
	h.playSoundDim(players, dim, sound, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	h.vib(dim, freqBlockActivate, pos.x, pos.y, pos.z, 0)
}

// potentSulfurTick runs every known vent in a loaded chunk, once per tick —
// the block entity tickers PotentSulfurBlock.getTicker picks by state.
// Vents whose block has gone are forgotten as they are found; the loop is
// bounded by the registry, never by a scan of the world.
func (h *hub) potentSulfurTick(players map[int32]*tracked) {
	if len(h.vents) > 0 {
		now := h.tick.Load()
		loaded := map[[3]int]bool{}
		for key, v := range h.vents {
			w := h.worldFor(key.dim)
			if w == nil {
				delete(h.vents, key)
				continue
			}
			ck := [3]int{key.dim, key.x >> 4, key.z >> 4}
			ok, seen := loaded[ck]
			if !seen {
				ok = w.Loaded(int32(ck[1]), int32(ck[2]))
				loaded[ck] = ok
			}
			if !ok {
				continue // only in a loaded chunk; never generate one here
			}
			st := w.At(key.x, key.y, key.z)
			if !isPotentSulfur(st) {
				delete(h.vents, key)
				continue
			}
			switch psKind(st) {
			case psWet:
				h.ventNausea(players, key.dim, w, key.blockPos, now)
			case psDormant:
				h.ventCountdown(players, key.dim, w, key.blockPos, st, v, now)
				h.ventNausea(players, key.dim, w, key.blockPos, now)
			case psErupting:
				h.ventLaunch(players, key.dim, w, key.blockPos)
				h.ventCountdown(players, key.dim, w, key.blockPos, st, v, now)
			case psContinuous:
				h.ventLaunch(players, key.dim, w, key.blockPos)
			}
		}
	}
	h.geyserFlights(players)
}

// ventCountdown is SERVER_WAITING_COUNTDOWN_TICKER: once a second, a spent
// countdown is drawn afresh from the geyser's positional stream — a wait of
// 10×(water−1) + 15..30 seconds while DORMANT, an eruption of water−1 +
// 1..2 seconds while ERUPTING — and at zero the geyser flips.
func (h *hub) ventCountdown(players map[int32]*tracked, dim int, w *world.World, pos blockPos, st uint32, v *sulfurVent, now uint64) {
	if now%psCountdownEvery != 0 {
		return
	}
	src, ok := psGasSource(w, pos)
	if !ok {
		return
	}
	kind := psKind(st)
	if v.countdown <= 0 {
		water := src.y - pos.y - 1
		r := geyserPositional(w.Seed(), pos)
		if kind == psDormant {
			v.countdown = 10*(water-1) + int(r.nextIntBetween(15, 30))
		} else {
			r.nextInt()
			v.countdown = water - 1 + int(r.nextIntBetween(1, 2))
		}
	}
	if v.countdown > 0 {
		v.countdown--
	}
	if v.countdown != 0 {
		return
	}
	next := psErupting
	if kind != psDormant {
		next = psDormant
	}
	h.setBlockAt(players, dim, pos, psState(next))
	if next == psDormant {
		h.vib(dim, freqBlockDeactivate, pos.x, pos.y, pos.z, 0)
	}
}

// geyserPositional is PotentSulfurBlockEntity.geyserPositional: the world
// seed xor the geyser salt, forked positionally at the block.
func geyserPositional(seed int64, pos blockPos) *xoroshiro {
	return newXoroshiro(seed^psGeyserSalt).forkPositional().at(pos.x, pos.y, pos.z)
}

// ventNausea is SERVER_NAUSEA_EFFECT_TICKER: every ten ticks, whatever lives
// in the gas source's cell inflated 2.5 sideways and can breathe it gets
// Nausea for four seconds (ambient, visible).
func (h *hub) ventNausea(players map[int32]*tracked, dim int, w *world.World, pos blockPos, now uint64) {
	if now%psEffectEvery != 0 {
		return
	}
	src, ok := psGasSource(w, pos)
	if !ok {
		return
	}
	lo := [3]float64{float64(src.x) - psEffectInflate, float64(src.y), float64(src.z) - psEffectInflate}
	hi := [3]float64{float64(src.x) + 1 + psEffectInflate, float64(src.y) + 1, float64(src.z) + 1 + psEffectInflate}
	for _, t := range players {
		if t.dim != dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if !psBoxHits(lo, hi, t.x, t.y, t.z, psPlayerWidth, playerHeight(t)) {
			continue
		}
		if h.psGasReaches(dim, w, src, t.x, playerEyeY(t), t.z) {
			h.applyEffectFrom(players, t, effNausea, 0, psNauseaTicks, true, true)
		}
	}
	h.grid().nearby(dim, float64(src.x)+0.5, float64(src.z)+0.5, psEffectInflate+4, func(m *mob) {
		if m.dying > 0 || m.health <= 0 {
			return
		}
		b := m.box()
		if !psBoxHits(lo, hi, m.x, m.y, m.z, b.w, b.h) {
			return
		}
		if h.psGasReaches(dim, w, src, m.x, m.y+mobEyeHeight(m), m.z) {
			h.applyMobEffectTicks(players, m, effNausea, 0, psNauseaTicks)
		}
	})
}

func playerHeight(t *tracked) float64 {
	if t.p.sneaking {
		return psPlayerSneakHeight
	}
	return psPlayerHeight
}

// psBoxHits is AABB.intersects between [lo, hi] and an entity's box of the
// given width and height standing at (x, y, z).
func psBoxHits(lo, hi [3]float64, x, y, z, width, height float64) bool {
	r := width / 2
	return x+r > lo[0] && x-r < hi[0] && y+height > lo[1] && y < hi[1] && z+r > lo[2] && z-r < hi[2]
}

// psGasReaches is canBeReachedByNoxiousGas for eyes at (x, eyeY, z): the eye
// cell is open, within three blocks of the source's centre, over source
// water, and the water under the eyes sees the water under the source.
func (h *hub) psGasReaches(dim int, w *world.World, src blockPos, x, eyeY, z float64) bool {
	if !psPassable(w.At(floorInt(x), floorInt(eyeY), floorInt(z))) {
		return false
	}
	dx, dy, dz := x-(float64(src.x)+0.5), eyeY-(float64(src.y)+0.5), z-(float64(src.z)+0.5)
	if dx*dx+dy*dy+dz*dz > psEffectReachSq {
		return false
	}
	if !psWaterSource(w.At(floorInt(x), floorInt(eyeY-1), floorInt(z))) {
		return false
	}
	return h.sightClear(dim, float64(src.x)+0.5, float64(src.y)-0.5, float64(src.z)+0.5, x, eyeY-1, z)
}

// ventLaunch is LAUNCH_ENTITY_TICKER on the server: everything the server
// moves in the column over the geyser is lifted. Players are the client's
// to move (canSimulateMovement), so for them the server only does what it
// owns — the fall-distance cap — and keeps its floating check off them.
func (h *hub) ventLaunch(players map[int32]*tracked, dim int, w *world.World, pos blockPos) {
	src, ok := psGasSource(w, pos)
	if !ok {
		return
	}
	water := src.y - pos.y - 1
	height := psColumnHeight(w, pos, water)
	// AABB(pos.above()).expandTowards(0, height-1, 0)
	lo := [3]float64{float64(pos.x), float64(pos.y + 1), float64(pos.z)}
	hi := [3]float64{float64(pos.x + 1), float64(pos.y + 2), float64(pos.z + 1)}
	if height-1 >= 0 {
		hi[1] += float64(height - 1)
	} else {
		lo[1] += float64(height - 1)
	}
	limit := psLaunchBase + float64(water)*psLaunchPerWater
	cx, cz := float64(pos.x)+0.5, float64(pos.z)+0.5

	for _, t := range players {
		if t.dim != dim || t.dead || t.gamemode == gmSpectator {
			continue
		}
		if !psBoxHits(lo, hi, t.x, t.y, t.z, psPlayerWidth, playerHeight(t)) {
			continue
		}
		// checkFallDistanceAccumulation: rising (or falling slower than 0.5)
		// in the column, at most one block of fall is carried.
		if t.airborne && t.kvy > psFallCapSpeed && t.peakY-t.y > 1 {
			t.peakY = t.y + 1
		}
		// The geyser holds the player up; the client moves them. Without
		// this a ride on a continuous geyser's plume reads as hovering.
		t.floatTicks = 0
	}
	h.grid().nearby(dim, cx, cz, 3, func(m *mob) {
		if m.dying > 0 || m.health <= 0 || m.etype == entityEnderDragon || m.statik ||
			m.mount != 0 || m.cart != 0 || m.rider != 0 {
			return // #not_affected_by_geysers, a passenger, or a player-driven mount
		}
		b := m.box()
		if !psBoxHits(lo, hi, m.x, m.y, m.z, b.w, b.h) {
			return
		}
		if m.hasBody() { // a sulfur cube carrying a block is a physics ball
			if m.cube.vy < limit {
				m.cube.vy += psLaunchForce
			}
			return
		}
		vy := 0.0
		if m.geyserFly {
			vy = m.geyserVY
			if vy > psFallCapSpeed && m.geyserFall > 1 {
				m.geyserFall = 1
			}
		}
		if vy >= limit {
			return
		}
		if !m.geyserFly {
			m.geyserFly, m.geyserVY, m.geyserFall = true, 0, 0
			if h.geyserFliers == nil {
				h.geyserFliers = map[int32]*mob{}
			}
			h.geyserFliers[m.eid] = m
		}
		m.geyserVY += psLaunchForce
	})
	for _, it := range h.items {
		if it.dim != dim || !psBoxHits(lo, hi, it.x, it.y, it.z, psItemSize, psItemSize) {
			continue
		}
		if it.vy < limit {
			it.vy += psLaunchForce
		}
		it.geyser = true
	}
	for _, t := range h.tnt {
		if t.dim != dim || !psBoxHits(lo, hi, t.x, t.y, t.z, tntHeight, tntHeight) {
			continue
		}
		if t.vy < limit {
			t.vy += psLaunchForce
		}
	}
}

// geyserFlights moves every mob a geyser has lifted, one tick of vanilla's
// travel each: up against gravity and drag (in water ×0.8 and a sixteenth
// of gravity, in air −0.08 then ×0.98), stopped by a ceiling. The flight
// ends when it comes down — onto a floor, where the fall it built up out of
// the water hurts as any fall does, or back into the water, where the mob's
// own swimming and floating take over again. Walkers otherwise have no
// vertical motion at all: updateMobs seats them on their floor.
func (h *hub) geyserFlights(players map[int32]*tracked) {
	for eid, m := range h.geyserFliers {
		if h.mobs[eid] != m || m.dying > 0 || !m.geyserFly {
			m.geyserFly = false
			delete(h.geyserFliers, eid)
			continue
		}
		w := h.worldFor(m.dim)
		if w == nil {
			continue
		}
		fx, fz := floorInt(m.x), floorInt(m.z)
		ny := m.y + m.geyserVY
		switch {
		case m.geyserVY > 0:
			if top := ny + m.box().h; worldgen.Collides(w.At(fx, floorInt(top), fz)) {
				ny = math.Floor(top) - m.box().h
				if ny < m.y {
					ny = m.y
				}
				m.geyserVY = 0
			}
		case m.geyserVY < 0:
			if floor := float64(w.MobFeetFrom(fx, fz, floorInt(m.y))); ny <= floor {
				m.geyserFall += m.y - floor
				m.y = floor
				fell := m.geyserFall
				h.endGeyserFlight(m)
				if !worldgen.HoldsWater(w.At(fx, floorInt(m.y), fz)) {
					h.mobFall(players, m, fell)
				}
				continue
			}
		}
		if ny < m.y {
			m.geyserFall += m.y - ny
		}
		m.y = ny
		if worldgen.HoldsWater(w.At(fx, floorInt(m.y), fz)) {
			m.geyserFall = 0
			m.geyserVY = m.geyserVY*0.8 - m.effectiveGravity(m.geyserVY)/16
			if m.geyserVY <= 0 {
				h.endGeyserFlight(m) // back in the pool, rising no more
			}
			continue
		}
		m.geyserVY = (m.geyserVY - m.effectiveGravity(m.geyserVY)) * 0.98
	}
}

func (h *hub) endGeyserFlight(m *mob) {
	m.geyserFly, m.geyserVY, m.geyserFall = false, 0, 0
	delete(h.geyserFliers, m.eid)
}
