package server

import (
	"math"

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

// Vanilla VibrationSystem frequencies for the events the engine emits.
const (
	freqStep         = 1
	freqBlockDestroy = 12
	freqBlockPlace   = 13
	freqEntityDie    = 15
)

const (
	sculkPhaseInactive = 0
	sculkPhaseActive   = 1
	sculkPhaseCooldown = 2

	sensorActiveTicks   = 30 // ACTIVE_TICKS
	sensorCooldownTicks = 10 // COOLDOWN_TICKS
	shriekingTicks      = 90 // SHRIEKING_TICKS
	shriekWarnMax       = 4  // warning level that summons a Warden
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
// change (overworld only). Also clears per-block sculk state when a listener is
// removed so a rebuilt block starts fresh.
func (h *hub) sculkIndexOnBlockChange(x, y, z int, state uint32) {
	pos := blockPos{x, y, z}
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
		if t.dim != 0 {
			continue
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
					h.sculkList[blockPos{x, wy, z}] = true
				} else if isCatalyst(s) {
					h.catalysts[blockPos{x, wy, z}] = true
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

// gameEvent broadcasts a vibration of the given frequency at (x,y,z). Every
// sculk listener within its radius that can currently receive it schedules the
// signal to arrive after `distance` ticks (vanilla 1 tick/block).
func (h *hub) gameEvent(freq, x, y, z int, src int32) {
	if len(h.sculkList) == 0 {
		return // cheap fast path: no listeners anywhere
	}
	now := h.tick.Load()
	for pos := range h.sculkList {
		s := h.world.At(pos.x, pos.y, pos.z)
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
		if !h.sculkCanReceive(pos, s, freq) {
			continue
		}
		dist := math.Sqrt(d2)
		h.sculkVib[pos] = sculkPending{due: now + uint64(int(dist)), freq: freq, dist: dist, src: src}
	}
}

// sculkCanReceive is the listener's filter at dispatch time.
func (h *hub) sculkCanReceive(pos blockPos, s uint32, freq int) bool {
	switch {
	case isCalibSensor(s):
		if sensorPhase(s) != sculkPhaseInactive {
			return false
		}
		// Calibrated: if the back is redstone-powered, only its exact frequency.
		bdx, bdz := calibBackDelta(s)
		bx, bz := pos.x+bdx, pos.z+bdz
		back := h.emitPower(bx, pos.y, bz, pos.x, pos.y, pos.z)
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
		s := h.world.At(pos.x, pos.y, pos.z)
		switch {
		case isAnySensor(s) && sensorPhase(s) == sculkPhaseInactive:
			h.activateSensor(players, pos, s, v)
		case isShrieker(s) && !shriekerShrieking(s):
			h.shriek(players, pos, s)
		}
	}

	// 2. Advance phase timers (sensor active→cooldown→inactive, shrieker respond).
	for pos, due := range h.sculkDue {
		if now < due {
			continue
		}
		s := h.world.At(pos.x, pos.y, pos.z)
		switch {
		case isAnySensor(s) && sensorPhase(s) == sculkPhaseActive:
			h.setBlock(players, pos, sensorWith(s, 0, sculkPhaseCooldown))
			h.sculkDue[pos] = now + sculkCooldownTicks()
			h.scheduleAround(pos, 1) // redstone drops
		case isAnySensor(s) && sensorPhase(s) == sculkPhaseCooldown:
			h.setBlock(players, pos, sensorWith(s, 0, sculkPhaseInactive))
			delete(h.sculkDue, pos)
		case isShrieker(s) && shriekerShrieking(s):
			h.shriekerRespond(players, pos, s)
			h.setBlock(players, pos, shriekerWith(s, false))
			delete(h.sculkDue, pos)
		default:
			delete(h.sculkDue, pos)
		}
	}

	// 3. STEP events: players moving on the ground, throttled per player.
	for _, t := range players {
		if t.dim != 0 || !t.p.onGround {
			continue
		}
		if nxt, ok := h.sculkStep[t.p.eid]; ok && now < nxt {
			continue
		}
		if !h.playerMovedHoriz(t) {
			continue
		}
		h.sculkStep[t.p.eid] = now + 3
		h.gameEvent(freqStep, floorInt(t.x), floorInt(t.y), floorInt(t.z), t.p.eid)
	}
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
func (h *hub) activateSensor(players map[int32]*tracked, pos blockPos, s uint32, v sculkPending) {
	power := redstoneForDistance(v.dist, sensorRadius(s))
	h.sculkFreq[pos] = v.freq
	h.setBlock(players, pos, sensorWith(s, power, sculkPhaseActive))
	h.sculkDue[pos] = h.tick.Load() + sensorActiveTicks
	h.scheduleAround(pos, 1) // neighbours read the new redstone
	if snd := "minecraft:block.sculk_sensor.clicking"; !sensorWaterlogged(s) {
		h.playSound(players, snd, sndBlock,
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
func (h *hub) shriek(players map[int32]*tracked, pos blockPos, s uint32) {
	h.setBlock(players, pos, shriekerWith(s, true))
	h.sculkDue[pos] = h.tick.Load() + shriekingTicks
	if shriekerCanSummon(s) {
		if h.sculkWarn[pos] < shriekWarnMax {
			h.sculkWarn[pos]++
		}
	}
	h.playSound(players, "minecraft:block.sculk_shrieker.shriek", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 2, 1)
}

// shriekerRespond fires when a shriek ends: at max warning a Warden emerges.
func (h *hub) shriekerRespond(players map[int32]*tracked, pos blockPos, s uint32) {
	if !shriekerCanSummon(s) || h.sculkWarn[pos] < shriekWarnMax {
		return
	}
	h.sculkWarn[pos] = 0
	if sp := h.wardenSpawnSpot(pos); sp != nil {
		h.spawnMobIn(players, entityWarden, 0, float64(sp.x)+0.5, float64(sp.y), float64(sp.z)+0.5)
	}
}

// wardenSpawnSpot finds a 2-tall air gap on solid ground within a few blocks of
// the shrieker for the Warden to emerge, or nil if none fits.
func (h *hub) wardenSpawnSpot(pos blockPos) *blockPos {
	for _, d := range [][2]int{{0, 0}, {2, 0}, {-2, 0}, {0, 2}, {0, -2}, {2, 2}, {-2, -2}} {
		x, z := pos.x+d[0], pos.z+d[1]
		for dy := -2; dy <= 2; dy++ {
			y := pos.y + dy
			if worldgen.IsSolidFull(h.world.At(x, y-1, z)) &&
				h.world.At(x, y, z) == worldgen.Air && h.world.At(x, y+1, z) == worldgen.Air {
				return &blockPos{x, y, z}
			}
		}
	}
	return nil
}

// ---- catalyst: mob death → sculk spread -------------------------------------

// catalystConsume looks for a sculk catalyst within 8 blocks of a dying mob. If
// one is found it consumes the mob's XP into a sculk bloom and returns true (the
// caller then skips the normal XP-orb drop).
func (h *hub) catalystConsume(players map[int32]*tracked, m *mob, xp int) bool {
	if m.dim != 0 || xp <= 0 || len(h.catalysts) == 0 {
		return false
	}
	mx, my, mz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	var best *blockPos
	bestD := 8*8 + 1
	for pos := range h.catalysts {
		dx, dy, dz := pos.x-mx, pos.y-my, pos.z-mz
		if d := dx*dx + dy*dy + dz*dz; d <= 64 && d < bestD {
			bestD, best = d, &blockPos{pos.x, pos.y, pos.z}
		}
	}
	if best == nil {
		return false
	}
	if s := h.world.At(best.x, best.y, best.z); isCatalyst(s) {
		h.setBlock(players, *best, catalystWith(true)) // bloom particle
		h.sculkDue[*best] = h.tick.Load() + 8
	}
	h.spreadSculk(players, blockPos{mx, my, mz}, xp)
	return true
}

// spreadSculk is a bounded stand-in for SculkSpreader: it converts solid,
// air-exposed blocks near `origin` to sculk (up to the charge), then studs the
// exposed faces of the new sculk with sculk_vein. Faithful in appearance, not in
// the per-tick cursor charge model.
func (h *hub) spreadSculk(players map[int32]*tracked, origin blockPos, charge int) {
	cap := charge
	if cap > 12 {
		cap = 12 // avoid runaway conversion from a high-XP death
	}
	placed := 0
	// Grow outward in shells until the charge is spent.
	for r := 0; r <= 3 && placed < cap; r++ {
		for dx := -r; dx <= r && placed < cap; dx++ {
			for dz := -r; dz <= r && placed < cap; dz++ {
				for dy := -1; dy <= 1 && placed < cap; dy++ {
					if abs(dx)+abs(dy)+abs(dz) != r {
						continue // one Manhattan shell per radius
					}
					p := blockPos{origin.x + dx, origin.y + dy, origin.z + dz}
					s := h.world.At(p.x, p.y, p.z)
					if !worldgen.IsSolidFull(s) || isSculkFamily(s) {
						continue
					}
					if !h.airExposed(p) {
						continue
					}
					h.setBlock(players, p, sculkBlockState)
					placed++
					h.veinExposedFaces(players, p)
				}
			}
		}
	}
}

// isSculkFamily reports whether a state is any sculk block (so spread never
// re-converts what it already grew, or a sensor/shrieker/catalyst).
func isSculkFamily(s uint32) bool {
	return s == sculkBlockState || (s >= sculkVeinBase && s < sculkVeinBase+128) ||
		isAnySensor(s) || isShrieker(s) || isCatalyst(s)
}

// airExposed reports whether any of a block's six faces touches air.
func (h *hub) airExposed(p blockPos) bool {
	for _, d := range sixDirs {
		if h.world.At(p.x+d.x, p.y+d.y, p.z+d.z) == worldgen.Air {
			return true
		}
	}
	return false
}

// veinExposedFaces places sculk_vein in the air cells directly touching a newly
// grown sculk block, with the face toward the sculk set.
func (h *hub) veinExposedFaces(players map[int32]*tracked, p blockPos) {
	for _, d := range sixDirs {
		np := blockPos{p.x + d.x, p.y + d.y, p.z + d.z}
		if h.world.At(np.x, np.y, np.z) != worldgen.Air {
			continue
		}
		// The vein clings to the face pointing back at the sculk block.
		if face := veinFaceBit(-d.x, -d.y, -d.z); face != 0 {
			h.setBlock(players, np, sculkVeinBase+veinStateForFaces(face))
		}
	}
}

// veinFaceBit maps a unit direction to sculk_vein's face bit. Face properties in
// order down,east,north,south,up,west; the state offset packs them (see below).
func veinFaceBit(dx, dy, dz int) int {
	switch {
	case dy < 0:
		return 1 << 0 // down
	case dx > 0:
		return 1 << 1 // east
	case dz < 0:
		return 1 << 2 // north
	case dz > 0:
		return 1 << 3 // south
	case dy > 0:
		return 1 << 4 // up
	case dx < 0:
		return 1 << 5 // west
	}
	return 0
}

// veinStateForFaces builds a sculk_vein offset with exactly the given faces set
// and waterlogged=false. Property order down,east,north,south,up,waterlogged,west
// (7 bools, last fastest, true-first) → each false-bit adds its place value.
func veinStateForFaces(faces int) uint32 {
	// places (true=0,false=1): down=64,east=32,north=16,south=8,up=4,waterlogged=2,west=1
	off := uint32(0)
	if faces&(1<<0) == 0 {
		off += 64 // down false
	}
	if faces&(1<<1) == 0 {
		off += 32 // east false
	}
	if faces&(1<<2) == 0 {
		off += 16 // north false
	}
	if faces&(1<<3) == 0 {
		off += 8 // south false
	}
	if faces&(1<<4) == 0 {
		off += 4 // up false
	}
	off += 2 // waterlogged false
	if faces&(1<<5) == 0 {
		off += 1 // west false
	}
	return off
}
