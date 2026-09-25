package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Sculk family — the game-event VIBRATION system and its listeners, ported from
// VibrationSystem / SculkSensorBlock / SculkShriekerBlock / SculkCatalystBlock.
//
// A game event (a step, a block place, a death, …) is broadcast at a position
// with a vanilla FREQUENCY (1–15). Sculk sensors and shriekers within their
// listener radius schedule the vibration to arrive after `distance` ticks; on
// arrival a sensor goes ACTIVE (emitting redstone whose strength falls with
// distance, and a comparator signal equal to the frequency) and a shrieker
// shrieks (building toward a Warden summon in the deep dark). A sculk catalyst
// converts nearby blocks to sculk when a mob dies in range, consuming its XP.
//
// These blocks are not in the generated orientable-property table, so the state
// layouts are hand-encoded (verified against the 1.21.11 datagen report).

// Vanilla VibrationSystem frequencies (VIBRATION_FREQUENCY_FOR_EVENT) for
// the game events the engine emits.
const (
	freqStep            = 1
	freqProjectileLand  = 2
	freqSplash          = 2
	freqHitGround       = 2
	freqProjectileShoot = 3
	freqInstrumentPlay  = 3
	freqEntityAction    = 4
	freqElytraGlide     = 4
	freqDismount        = 5
	freqShear           = 6
	freqMount           = 6
	freqEntityDamage    = 7
	freqDrink           = 8
	freqEat             = 8
	freqContainerClose  = 9
	freqBlockClose      = 9
	freqBlockDeactivate = 9
	freqContainerOpen   = 10
	freqBlockOpen       = 10
	freqBlockActivate   = 10
	freqPrimeFuse       = 10
	freqNoteBlockPlay   = 10
	freqBlockChange     = 11
	freqBlockDestroy    = 12
	freqFluidPickup     = 12
	freqBlockPlace      = 13
	freqFluidPlace      = 13
	freqEntityPlace     = 14
	freqLightning       = 14
	freqTeleport        = 14
	freqEntityDie       = 15
	freqExplode         = 15
)

// vib is a game event in a dimension: the listeners there hear it, and a
// sensor built in the Nether or the End works as it does at home.
func (h *hub) vib(dim int, freq int, x, y, z int, src int32) {
	// A Warden moves without a sound anything hears (Warden.dampensVibrations).
	if m := h.mobs[src]; m != nil && m.etype == entityWarden {
		return
	}
	// A Warden is a listener in its own right: what it hears within sixteen
	// blocks it takes personally (Warden's VibrationUser).
	h.wardenHeard(dim, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, src)
	h.gameEvent(dim, freq, x, y, z, src)
}

// vibOn is vib for an event about a block (placing, breaking, stepping on
// it): one in #dampens_vibrations — wool, and wool carpets, slabs and
// stairs — makes none (VibrationSystem.User.isValidVibration).
func (h *hub) vibOn(dim, freq, x, y, z int, src int32, affected uint32) {
	if inRanges2(affected, vibDampers) {
		return
	}
	h.vib(dim, freq, x, y, z, src)
}

var (
	vibOccluders = worldgen.BlockTag("occludes_vibration_signals")
	vibDampers   = worldgen.BlockTag("dampens_vibrations")
)

// vibOccluded is VibrationSystem.Listener.isOccluded: from the centre of
// the source's cell to the centre of the listener's, a wool block on the
// line stops the vibration — unless one of the six paths nudged off the
// source centre gets past (so a line grazing an edge still counts as open).
func vibOccluded(w *world.World, sx, sy, sz, lx, ly, lz int) bool {
	to := [3]float64{float64(lx) + 0.5, float64(ly) + 0.5, float64(lz) + 0.5}
	const nudge = 1e-5
	for _, d := range supportNeighbours {
		from := [3]float64{float64(sx) + 0.5 + float64(d[0])*nudge, float64(sy) + 0.5 + float64(d[1])*nudge, float64(sz) + 0.5 + float64(d[2])*nudge}
		if !lineHits(w, from, to, vibOccluders) {
			return false
		}
	}
	return true
}

// lineHits walks the cells a segment passes through (BlockGetter.traverseBlocks,
// both ends included) and reports whether any is in the ranges.
func lineHits(w *world.World, from, to [3]float64, rs [][2]uint32) bool {
	cell := [3]int{floorInt(from[0]), floorInt(from[1]), floorInt(from[2])}
	end := [3]int{floorInt(to[0]), floorInt(to[1]), floorInt(to[2])}
	var step [3]int
	var tMax, tDelta [3]float64
	for i := 0; i < 3; i++ {
		d := to[i] - from[i]
		switch {
		case d > 0:
			step[i], tDelta[i] = 1, 1/d
			tMax[i] = (float64(cell[i]+1) - from[i]) / d
		case d < 0:
			step[i], tDelta[i] = -1, -1/d
			tMax[i] = (float64(cell[i]) - from[i]) / d
		default:
			tMax[i], tDelta[i] = math.Inf(1), math.Inf(1)
		}
	}
	for n := 0; n < 256; n++ {
		if inRanges2(w.At(cell[0], cell[1], cell[2]), rs) {
			return true
		}
		if cell == end {
			return false
		}
		i := 0
		if tMax[1] < tMax[i] {
			i = 1
		}
		if tMax[2] < tMax[i] {
			i = 2
		}
		if tMax[i] > 1 {
			return false
		}
		cell[i] += step[i]
		tMax[i] += tDelta[i]
	}
	return false
}

// vibAt is vib at an entity's feet.
func (h *hub) vibAt(dim int, freq int, x, y, z float64, src int32) {
	h.vib(dim, freq, floorInt(x), floorInt(y), floorInt(z), src)
}

// evVibration is a vibration the server side raises for a block it
// toggled (a door swung, a gate opened): quiet marks the block-change that
// follows as a toggle, not a placement.
type evVibration struct {
	eid     int32
	x, y, z int
	freq    int
	quiet   bool
}

func (evVibration) isHubEvent() {}

const (
	sculkPhaseInactive = 0
	sculkPhaseActive   = 1
	sculkPhaseCooldown = 2

	sensorActiveTicks   = 30 // ACTIVE_TICKS
	calibActiveTicks    = 10 // a calibrated sensor's active phase
	sensorCooldownTicks = 10 // COOLDOWN_TICKS
	shriekingTicks      = 90 // SHRIEKING_TICKS
	// (the warning level that summons a Warden now lives on the player —
	// see wardentracker.go's wardenWarnMax)
)

var (
	sculkSensorBase uint32
	calibSensorBase uint32
	shriekerBase    uint32
	catalystBase    uint32
	sculkBlockState uint32
	sculkVeinBase   uint32
)

func init() {
	sculkSensorBase, _ = worldgen.BlockRange("sculk_sensor")
	calibSensorBase, _ = worldgen.BlockRange("calibrated_sculk_sensor")
	shriekerBase, _ = worldgen.BlockRange("sculk_shrieker")
	catalystBase, _ = worldgen.BlockRange("sculk_catalyst")
	sculkBlockState, _ = worldgen.BlockRange("sculk")
	sculkVeinBase, _ = worldgen.BlockRange("sculk_vein")
}

// ---- state helpers (raw math; layouts from the datagen report) --------------
//
// sculk_sensor      base + power*6 + phase*2 + wl        (phase: inact0 act1 cool2; wl true0 false1)
// calibrated_sensor base + facing*96 + power*6 + phase*2 + wl   (facing n0 s1 w2 e3)
// sculk_shrieker    base + canSummon*4 + shrieking*2 + wl        (all bool true0 false1)
// sculk_catalyst    base + bloom                                 (bloom true0 false1)

func isSculkSensor(s uint32) bool { return s >= sculkSensorBase && s < sculkSensorBase+96 }
func isCalibSensor(s uint32) bool { return s >= calibSensorBase && s < calibSensorBase+384 }
func isAnySensor(s uint32) bool   { return isSculkSensor(s) || isCalibSensor(s) }
func isShrieker(s uint32) bool    { return s >= shriekerBase && s < shriekerBase+8 }
func isCatalyst(s uint32) bool    { return s == catalystBase || s == catalystBase+1 }

// sensorInner is the offset within the power*6+phase*2+wl block (identical for
// plain and calibrated sensors — calibrated just prefixes facing*96).
func sensorInner(s uint32) uint32 {
	if isCalibSensor(s) {
		return (s - calibSensorBase) % 96
	}
	return s - sculkSensorBase
}
func sensorPower(s uint32) int { return int(sensorInner(s) / 6) }
func sensorPhase(s uint32) int { return int(sensorInner(s) % 6 / 2) }

// sensorWith rebuilds a sensor state with a new power+phase, preserving facing
// (calibrated) and waterlogged.
func sensorWith(s uint32, power, phase int) uint32 {
	wl := sensorInner(s) % 2
	inner := uint32(power*6+phase*2) + wl
	if isCalibSensor(s) {
		return calibSensorBase + (s-calibSensorBase)/96*96 + inner
	}
	return sculkSensorBase + inner
}
func sensorRadius(s uint32) int {
	if isCalibSensor(s) {
		return 16
	}
	return 8
}

// calibBackDelta is the block-delta of a calibrated sensor's BACK (opposite its
// facing), where it reads the redstone signal that filters frequencies.
func calibBackDelta(s uint32) (int, int) {
	switch []string{"north", "south", "west", "east"}[(s-calibSensorBase)/96] {
	case "north":
		return 0, 1 // facing north → back south
	case "south":
		return 0, -1
	case "west":
		return 1, 0
	default: // east
		return -1, 0
	}
}

func shriekerCanSummon(s uint32) bool { return (s-shriekerBase)/4 == 0 }
func shriekerShrieking(s uint32) bool { return (s-shriekerBase)/2%2 == 0 }
func shriekerWith(s uint32, shrieking bool) uint32 {
	cs := (s - shriekerBase) / 4 // 0 = can_summon true
	wl := (s - shriekerBase) % 2 // 0 = waterlogged true
	sh := uint32(1)
	if shrieking {
		sh = 0
	}
	return shriekerBase + cs*4 + sh*2 + wl
}
func catalystWith(bloom bool) uint32 {
	if bloom {
		return catalystBase
	}
	return catalystBase + 1
}

// isSculkListener reports whether a state is a vibration listener the index
// tracks (sensors + shriekers; the catalyst is handled at the death site).
func isSculkListener(s uint32) bool { return isAnySensor(s) || isShrieker(s) }

// ---- listener index (maintained like the lightning-rod POI set) -------------

// sculkIndexOnBlockChange keeps the listener/catalyst sets current as blocks
// change, in whichever dimension the block is. Also clears per-block sculk
// state when a listener is removed so a rebuilt block starts fresh.
func (h *hub) sculkIndexOnBlockChange(dim, x, y, z int, state uint32) {
	pos := simPos{dim: dim, blockPos: blockPos{x, y, z}}
	if isSculkListener(state) {
		h.sculkList[pos] = true
	} else {
		delete(h.sculkList, pos)
		delete(h.sculkVib, pos)
		delete(h.sculkDue, pos)
		delete(h.sculkFreq, pos)
		delete(h.sculkWarn, pos)
	}
	if isCatalyst(state) {
		h.catalysts[pos] = true
	} else {
		delete(h.catalysts, pos)
		delete(h.sculkSpread, pos) // the block entity, and the charge it held, go with it
	}
}

// registerSculkChunks discovers WORLDGEN-placed sculk (deep_dark) near players
// and registers it in the listener/catalyst POI sets. Generated terrain is not
// in the edit overlay, so the block-change index never sees it; this scan is the
// only way a naturally-generated sensor or shrieker starts working. Each chunk is
// scanned once; a bounded few per call keep the hub responsive. Player-placed
// sculk still registers via sculkIndexOnBlockChange (block-change events).
func (h *hub) registerSculkChunks(players map[int32]*tracked) {
	scanned := 0
	for _, t := range players {
		if t.dim != dimOverworld {
			continue // the deep dark is the overworld's; elsewhere sculk is built, and indexed as it is placed
		}
		cx, cz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for dx := int32(-2); dx <= 2 && scanned < 4; dx++ {
			for dz := int32(-2); dz <= 2 && scanned < 4; dz++ {
				key := [2]int32{cx + dx, cz + dz}
				if h.sculkScanned[key] {
					continue
				}
				h.sculkScanned[key] = true
				scanned++
				h.scanChunkSculk(key[0], key[1])
			}
		}
	}
}

// scanChunkSculk reads a chunk's deep-dark Y band, registering any sculk listener
// or catalyst it finds (cheap: the sections are already cached for a nearby
// player, and the band is the only depth where deep_dark sculk generates).
func (h *hub) scanChunkSculk(cx, cz int32) {
	bx, bz := int(cx)*16, int(cz)*16
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for wy := -64; wy <= -16; wy++ {
				x, z := bx+lx, bz+lz
				s := h.world.At(x, wy, z)
				if isSculkListener(s) {
					h.sculkList[simPos{blockPos: blockPos{x, wy, z}}] = true
				} else if isCatalyst(s) {
					h.catalysts[simPos{blockPos: blockPos{x, wy, z}}] = true
				}
			}
		}
	}
}

// sculkPending is a vibration scheduled to reach a listener at tick `due`.
type sculkPending struct {
	due  uint64
	freq int
	dist float64
	src  int32
}

// ---- game-event emission + dispatch -----------------------------------------

// gameEvent broadcasts a vibration of the given frequency at (x,y,z) in dim.
// Every sculk listener of that dimension within its radius that can currently receive it schedules the
// signal to arrive after `distance` ticks (vanilla 1 tick/block).
func (h *hub) gameEvent(dim, freq, x, y, z int, src int32) {
	if len(h.sculkList) == 0 {
		return // cheap fast path: no listeners anywhere
	}
	now := h.tick.Load()
	w := h.worldFor(dim)
	for pos := range h.sculkList {
		if pos.dim != dim {
			continue
		}
		s := w.At(pos.x, pos.y, pos.z)
		r := 8
		if isAnySensor(s) {
			r = sensorRadius(s)
		}
		dx, dy, dz := float64(x-pos.x), float64(y-pos.y), float64(z-pos.z)
		d2 := dx*dx + dy*dy + dz*dz
		if d2 > float64(r*r) {
			continue
		}
		if _, pending := h.sculkVib[pos]; pending {
			continue // a listener tracks one vibration at a time
		}
		if !h.sculkCanReceive(pos, s, freq) || vibOccluded(w, x, y, z, pos.x, pos.y, pos.z) {
			continue
		}
		dist := math.Sqrt(d2)
		h.sculkVib[pos] = sculkPending{due: now + uint64(int(dist)), freq: freq, dist: dist, src: src}
	}
}

// sculkCanReceive is the listener's filter at dispatch time.
func (h *hub) sculkCanReceive(pos simPos, s uint32, freq int) bool {
	switch {
	case isCalibSensor(s):
		if sensorPhase(s) != sculkPhaseInactive {
			return false
		}
		// Calibrated: if the back is redstone-powered, only its exact frequency.
		bdx, bdz := calibBackDelta(s)
		bx, bz := pos.x+bdx, pos.z+bdz
		back := 0
		h.inDim(pos.dim, func() { back = h.emitPower(bx, pos.y, bz, pos.x, pos.y, pos.z) })
		return back == 0 || back == freq
	case isSculkSensor(s):
		return sensorPhase(s) == sculkPhaseInactive
	case isShrieker(s):
		return !shriekerShrieking(s)
	}
	return false
}

// tickSculk delivers due vibrations, advances phase timers, and emits STEP
// events for moving players. Runs once per hub tick.
func (h *hub) tickSculk(players map[int32]*tracked) {
	now := h.tick.Load()

	// 1. Deliver vibrations whose travel delay has elapsed.
	for pos, v := range h.sculkVib {
		if now < v.due {
			continue
		}
		delete(h.sculkVib, pos)
		s := h.worldFor(pos.dim).At(pos.x, pos.y, pos.z)
		switch {
		case isAnySensor(s) && sensorPhase(s) == sculkPhaseInactive:
			h.activateSensor(players, pos, s, v)
		case isShrieker(s) && !shriekerShrieking(s):
			h.shriek(players, pos, s, v.src)
		}
	}

	// 2. Advance phase timers (sensor active→cooldown→inactive, shrieker respond).
	for pos, due := range h.sculkDue {
		if now < due {
			continue
		}
		s := h.worldFor(pos.dim).At(pos.x, pos.y, pos.z)
		switch {
		case isAnySensor(s) && sensorPhase(s) == sculkPhaseActive:
			h.setBlockAt(players, pos.dim, pos.blockPos, sensorWith(s, 0, sculkPhaseCooldown))
			h.sculkDue[pos] = now + sculkCooldownTicks()
			h.inDim(pos.dim, func() { h.scheduleSignalAround(players, pos.blockPos) }) // redstone drops
		case isAnySensor(s) && sensorPhase(s) == sculkPhaseCooldown:
			h.setBlockAt(players, pos.dim, pos.blockPos, sensorWith(s, 0, sculkPhaseInactive))
			delete(h.sculkDue, pos)
			if !sensorWaterlogged(s) { // SculkSensorBlock.tick: the clicking stops
				h.playSoundDim(players, pos.dim, "minecraft:block.sculk_sensor.clicking_stop", sndBlock,
					float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 0.8+h.rng.Float32()*0.2)
			}
		case isShrieker(s) && shriekerShrieking(s):
			h.shriekerRespond(players, pos, s)
			h.setBlockAt(players, pos.dim, pos.blockPos, shriekerWith(s, false))
			delete(h.sculkDue, pos)
		case isCatalyst(s):
			// SculkCatalystBlock.tick: eight ticks after a bloom, BLOOM
			// (vanilla's PULSE) is set back — it used to stay on for good.
			h.setBlockAt(players, pos.dim, pos.blockPos, catalystWith(false))
			delete(h.sculkDue, pos)
		default:
			delete(h.sculkDue, pos)
		}
	}

	// 3. Catalysts spread the charge they hold (SculkCatalystBlockEntity.serverTick).
	if len(h.sculkSpread) > 0 {
		h.tickSculkSpread(players)
	}

	// 4. STEP events: players moving on the ground, throttled per player.
	for _, t := range players {
		if !t.p.onGround {
			continue
		}
		if nxt, ok := h.sculkStep[t.p.eid]; ok && now < nxt {
			continue
		}
		if !h.playerMovedHoriz(t) {
			continue
		}
		h.sculkStep[t.p.eid] = now + 3
		if t.p.sneaking {
			// VibrationSystem: a sneaking entity makes no step vibration
			// (#ignore_vibrations_sneaking), and a sensor that would have
			// heard it awards "Sneak 100" instead.
			if h.sensorWithinEarshot(t.dim, floorInt(t.x), floorInt(t.y), floorInt(t.z)) {
				h.advance(players, t, "avoid_vibration", advMatch{})
			}
			continue
		}
		if t.gliding() {
			h.vibAt(t.dim, freqElytraGlide, t.x, t.y, t.z, t.p.eid)
			continue
		}
		h.vibStep(t.dim, t.x, t.y, t.z, t.p.eid)
	}
}

// vibStep is the STEP event, which carries the block walked on: wool or a
// wool carpet underfoot keeps it silent.
func (h *hub) vibStep(dim int, x, y, z float64, src int32) {
	fx, fy, fz := floorInt(x), floorInt(y), floorInt(z)
	w := h.worldFor(dim)
	on := w.At(fx, fy, fz) // a carpet sits in the feet cell
	if !inRanges2(on, vibDampers) {
		on = w.At(fx, floorInt(y-0.2), fz) // getOnPos: the block under the feet
	}
	h.vibOn(dim, freqStep, fx, fy, fz, src, on)
}

func sculkCooldownTicks() uint64 { return sensorCooldownTicks }

// playerMovedHoriz reports whether a tracked player moved horizontally since the
// last check, updating the remembered position.
func (h *hub) playerMovedHoriz(t *tracked) bool {
	lx, lz, seen := h.sculkLastX[t.p.eid], h.sculkLastZ[t.p.eid], h.sculkStep[t.p.eid] != 0
	h.sculkLastX[t.p.eid], h.sculkLastZ[t.p.eid] = t.x, t.z
	if !seen {
		return false // first sighting: seed the position, no phantom step
	}
	dx, dz := t.x-lx, t.z-lz
	return dx*dx+dz*dz > 0.0025 // ~0.05 block
}

// activateSensor makes a sensor ACTIVE: redstone power falls with distance, the
// comparator signal is the event frequency (VibrationSystem numbers).
func (h *hub) activateSensor(players map[int32]*tracked, pos simPos, s uint32, v sculkPending) {
	power := redstoneForDistance(v.dist, sensorRadius(s))
	h.sculkFreq[pos] = v.freq
	h.setBlockAt(players, pos.dim, pos.blockPos, sensorWith(s, power, sculkPhaseActive))
	active := uint64(sensorActiveTicks)
	if isCalibSensor(s) {
		active = calibActiveTicks // CalibratedSculkSensorBlock.getActiveTicks
	}
	h.sculkDue[pos] = h.tick.Load() + active
	h.inDim(pos.dim, func() { h.scheduleSignalAround(players, pos.blockPos) }) // neighbours read the new redstone
	if snd := "minecraft:block.sculk_sensor.clicking"; !sensorWaterlogged(s) {
		h.playSoundDim(players, pos.dim, snd, sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, h.hurtPitch())
	}
}

func sensorWaterlogged(s uint32) bool { return sensorInner(s)%2 == 0 }

// redstoneForDistance is VibrationSystem.getRedstoneStrengthForDistance.
func redstoneForDistance(distance float64, radius int) int {
	p := 15 - int(15.0/float64(radius)*distance)
	if p < 1 {
		p = 1
	}
	return p
}

// shriek sets a shrieker SHRIEKING for 90 ticks and, if it can summon, builds
// its warning level toward a Warden.
func (h *hub) shriek(players map[int32]*tracked, pos simPos, s uint32, by int32) {
	// SculkShriekerBlockEntity.tryShriek: a shrieker that can summon asks the
	// players around it to take a warning first, and stays silent if the
	// warning cannot land (a Warden already about, or someone warned in the
	// last ten seconds). Only a player sets one off.
	h.sculkWarn[pos] = 0
	if shriekerCanSummon(s) && h.rules.Difficulty != diffPeaceful && h.rules.SpawnWardens {
		lvl, warned := h.tryWarnWarden(players, pos.blockPos, pos.dim, by)
		if !warned {
			return
		}
		h.sculkWarn[pos] = lvl
	}
	h.setBlockAt(players, pos.dim, pos.blockPos, shriekerWith(s, true))
	h.sculkDue[pos] = h.tick.Load() + shriekingTicks
	h.playSoundDim(players, pos.dim, "minecraft:block.sculk_shrieker.shriek", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 2, 1)
}

// shriekerRespond fires when a shriek ends: at max warning a Warden emerges.
func (h *hub) shriekerRespond(players map[int32]*tracked, pos simPos, s uint32) {
	lvl := h.sculkWarn[pos]
	delete(h.sculkWarn, pos)
	if !shriekerCanSummon(s) || lvl <= 0 || h.rules.Difficulty == diffPeaceful || !h.rules.SpawnWardens {
		return
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	summoned := false
	if lvl >= wardenWarnMax {
		if sp := h.wardenSpawnSpot(pos); sp != nil {
			// EntitySpawnReason.TRIGGERED: a warden a shrieker calls rises out
			// of the ground before it does anything (Warden.finalizeSpawn).
			if w := h.spawnHostileYIn(players, entityWarden, pos.dim, float64(sp.x)+0.5, float64(sp.y), float64(sp.z)+0.5); w != nil {
				h.wardenEmerge(players, w)
			}
			summoned = true
		}
	}
	if !summoned {
		// playWardenReplySound: the growl from somewhere below that tells you
		// how close you are — the warning that is the whole point of the
		// first three shrieks.
		h.playSoundDim(players, pos.dim, "minecraft:entity.warden.nearby_closer", sndHostile, cx, cy, cz, 5, 1)
	}
	// Every response darkens the room, summon or not.
	h.darknessAround(players, pos.dim, cx, cy, cz, wardenDarknessRing)
}

// wardenSpawnSpot finds a 2-tall air gap on solid ground within a few blocks of
// the shrieker, in the shrieker's own dimension, for the Warden to emerge, or
// nil if none fits.
func (h *hub) wardenSpawnSpot(at simPos) *blockPos {
	// SculkShriekerBlockEntity.trySummonWarden → SpawnUtil.trySpawnMob(20
	// tries, 5 sideways, 6 up and down, ON_TOP_OF_COLLIDER, no body check):
	// a random column, searched down from six above for a full-topped block
	// with an open cell over it.
	w, pos := h.worldFor(at.dim), at.blockPos
	for i := 0; i < 20; i++ {
		x, z := pos.x+h.rng.Intn(11)-5, pos.z+h.rng.Intn(11)-5
		if !h.withinBorder(at.dim, float64(x)+0.5, float64(z)+0.5) {
			continue
		}
		for y := pos.y + 5; y >= pos.y-7; y-- {
			if worldgen.IsSturdyTop(w.At(x, y, z)) && !worldgen.Collides(w.At(x, y+1, z)) {
				return &blockPos{x, y + 1, z}
			}
		}
	}
	return nil
}

// ---- catalyst: mob death → sculk spread -------------------------------------
//
// The catalyst's listener and its spreader live in sculkspread.go.

var sculkReplaceable = worldgen.BlockTag("sculk_replaceable")

// flyerSpecies reports a species that moves on the wing (no footsteps).
func flyerSpecies(etype int) bool {
	d := speciesTable[etype]
	return d != nil && (d.arch == archFlyer || d.arch == archFlyerHostile)
}
