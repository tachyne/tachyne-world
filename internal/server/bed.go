package server

import (
	"fmt"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Beds: right-clicking a bed always sets the player's respawn point; at night
// it also starts sleeping, and when every eligible player (non-spectator) is
// asleep the clock jumps to sunrise. Sleepers LIE DOWN: set_entity_data with
// pose SLEEPING + the bed position makes every client render the lying pose,
// and the sleeper's own client shows the sleep screen with its "Leave Bed"
// button (serverbound player_command STOP_SLEEPING). Walking away or taking
// damage also wakes you.

const (
	sleepStart = 12542 // first tick of the sleepable window (vanilla, clear sky)
	sleepEnd   = 23459 // last tick of the window
	bedRange   = 3.0   // how far a sleeper may drift from the bed before waking
	monsterR2  = 8 * 8 // vanilla: monsters within 8 blocks prevent sleep

	// Entity-data fields for the sleeping pose (1.21.5 layouts; the indexes are
	// append-only in vanilla and the chain remaps the pose TYPE id at 773+).
	metaIndexPose        = 6  // Entity: current pose
	metaIndexSleepingPos = 14 // LivingEntity: optional bed position
	metaTypePose         = 21 // pose serializer id (canonical 770; 20 from 1.21.9)
	metaTypeOptBlockPos  = 11 // optional_block_pos serializer id (stable)
	poseStanding         = 0
	poseSleeping         = 2

	// LivingEntity.setPosToBed: a sleeper sits at the bed block's centre, this
	// far up. Not the mattress height (0.5625) — vanilla lifts the body clear
	// of it, and the difference reads as a player sunk into the bed.
	bedSleepY = 0.6875

	// ServerPlayer.startSleepInBed's monster box: ±8 horizontally, ±5 up and
	// down from the bed. A sphere is close but lets a mob two floors up stop
	// you sleeping when vanilla would not.
	monsterRangeH = 8.0
	monsterRangeV = 5.0
)

// sleepMetadata builds set_entity_data putting an entity into the sleeping
// pose, anchored to its bed; wakeMetadata stands it back up.
func sleepMetadata(eid int32, pos blockPos) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexPose)
	b = protocol.AppendVarInt(b, metaTypePose)
	b = protocol.AppendVarInt(b, poseSleeping)
	b = protocol.AppendU8(b, metaIndexSleepingPos)
	b = protocol.AppendVarInt(b, metaTypeOptBlockPos)
	b = protocol.AppendBool(b, true)
	b = protocol.AppendPosition(b, pos.x, pos.y, pos.z)
	return protocol.AppendU8(b, itemMetaEnd)
}

func wakeMetadata(eid int32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexPose)
	b = protocol.AppendVarInt(b, metaTypePose)
	b = protocol.AppendVarInt(b, poseStanding)
	b = protocol.AppendU8(b, metaIndexSleepingPos)
	b = protocol.AppendVarInt(b, metaTypeOptBlockPos)
	b = protocol.AppendBool(b, false)
	return protocol.AppendU8(b, itemMetaEnd)
}

type evUseBed struct {
	eid     int32
	x, y, z int
}

// evStopSleep: the sleeper clicked "Leave Bed" (player_command STOP_SLEEPING).
type evStopSleep struct{ eid int32 }

func (evUseBed) isHubEvent()    {}
func (evStopSleep) isHubEvent() {}

// bedHead resolves a clicked bed cell to the HEAD half, which is the block
// every other part of sleeping is anchored to. Vanilla does this first thing in
// BedBlock.useWithoutItem, and skipping it is visible: the client draws a
// sleeper lying from the anchor block DOWN the bed, so anchoring at the foot
// leaves the body hanging off the end with nothing under its legs.
func (h *hub) bedHead(dim int, pos blockPos) (blockPos, bool) {
	w := h.worldFor(dim)
	if w == nil {
		return pos, false
	}
	state := w.Block(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(state)
	if !ok || !isBed(info) {
		return pos, false
	}
	if worldgen.GetProperty(info, state, "part") == "head" {
		return pos, true
	}
	dx, dz := facingStep(worldgen.GetProperty(info, state, "facing")) // foot → head
	head := blockPos{pos.x + dx, pos.y, pos.z + dz}
	if hi, ok := worldgen.InfoForState(w.Block(head.x, head.y, head.z)); ok && isBed(hi) {
		return head, true
	}
	return pos, false // a half-bed: vanilla returns CONSUME rather than sleeping
}

// setBedOccupied writes the bed's `occupied` property, which is what makes the
// blanket render rumpled while someone is in it.
func (h *hub) setBedOccupied(players map[int32]*tracked, dim int, pos blockPos, occupied bool) {
	w := h.worldFor(dim)
	if w == nil {
		return
	}
	state := w.Block(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("occupied") {
		return
	}
	v := "false"
	if occupied {
		v = "true"
	}
	if next := worldgen.SetProperty(info, state, "occupied", v); next != state {
		h.setBlockAt(players, dim, pos, next)
	}
}

// setSleeping lies the player down: move them onto the bed (their own client
// gets a position sync; everyone's gets the sleeping pose + bed anchor). pos is
// the HEAD half — see bedHead.
func (h *hub) setSleeping(players map[int32]*tracked, t *tracked, pos blockPos) {
	t.sleeping, t.sleepPos, t.sleepingAt = true, pos, h.tick.Load()
	t.x, t.y, t.z = float64(pos.x)+0.5, float64(pos.y)+bedSleepY, float64(pos.z)+0.5
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	h.setBedOccupied(players, t.dim, pos, true)
	body := sleepMetadata(t.p.eid, pos)
	for _, o := range players {
		o.p.trySendEv(metaEv(body))
	}
	h.advance(players, t, "slept_in_bed", advMatch{})
	h.incCustom(t, "sleep_in_bed", 1)
	// Vanilla resets the insomnia clock on GETTING IN, not on waking — lying
	// down is what buys off the phantoms, even if something wakes you early.
	h.resetCustom(t, "time_since_rest")
}

// wakePlayer stands a sleeper back up (no-op for the awake). Safe with a nil
// players map (headless damage paths) — the sleeper itself is always synced.
func (h *hub) wakePlayer(players map[int32]*tracked, t *tracked) {
	if !t.sleeping {
		return
	}
	t.sleeping = false
	bed := t.sleepPos
	h.setBedOccupied(players, t.dim, bed, false)
	// LivingEntity.stopSleeping: you get OUT of the bed, beside it, turned to
	// face it. Leaving the player standing in the bed's own cell is what made
	// waking look like nothing had happened.
	if x, y, z, ok := h.bedStandUp(t, bed); ok {
		t.x, t.y, t.z = x, y, z
		t.yaw = bedFacingAwayFrom(bed, x, z)
		t.pitch = 0
	}
	body := wakeMetadata(t.p.eid)
	t.p.trySendEv(metaEv(body))
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	for _, o := range players {
		if o != t {
			o.p.trySendEv(metaEv(body))
		}
	}
}

// bedStandUp is BedBlock.findStandUpPosition: the ring of cells around the bed
// is tried in a fixed order that starts on whichever side the sleeper is
// already facing, then the bed's own two cells as a last resort.
func (h *hub) bedStandUp(t *tracked, bed blockPos) (float64, float64, float64, bool) {
	w := h.worldFor(t.dim)
	if w == nil {
		return 0, 0, 0, false
	}
	state := w.Block(bed.x, bed.y, bed.z)
	info, ok := worldgen.InfoForState(state)
	if !ok || !isBed(info) {
		return 0, 0, 0, false
	}
	dx, dz := facingStep(worldgen.GetProperty(info, state, "facing"))
	// The clockwise neighbour, flipped to the far side when the sleeper is
	// already looking that way (Direction.isFacingAngle).
	sx, sz := -dz, dx // facing.getClockWise()
	rad := float64(t.yaw) * math.Pi / 180
	if float64(sx)*-math.Sin(rad)+float64(sz)*math.Cos(rad) > 0 {
		sx, sz = -sx, -sz
	}
	offsets := [][2]int{
		{sx, sz}, {sx - dx, sz - dz}, {sx - dx*2, sz - dz*2}, {-dx * 2, -dz * 2},
		{-sx - dx*2, -sz - dz*2}, {-sx - dx, -sz - dz}, {-sx, -sz}, {-sx + dx, -sz + dz},
		{dx, dz}, {sx + dx, sz + dz},
		{0, 0}, {-dx, -dz}, // bedAboveStandUpOffsets: the bed's own cells
	}
	for _, o := range offsets {
		x, z := bed.x+o[0], bed.z+o[1]
		if h.bedStandable(t.dim, x, bed.y, z) {
			return float64(x) + 0.5, float64(bed.y), float64(z) + 0.5, true
		}
	}
	// Vanilla's fallback: on top of the bed, just clear of the mattress.
	return float64(bed.x) + 0.5, float64(bed.y) + 1.1, float64(bed.z) + 0.5, true
}

// bedStandable reports whether a player fits stood up in this cell. Looser
// than the creaking's air-only test: vanilla's dismount check is about
// COLLISION, so grass, snow and carpet are all fine to stand up into.
func (h *hub) bedStandable(dim, x, y, z int) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	return !worldgen.IsSolidFull(w.At(x, y, z)) && !worldgen.IsSolidFull(w.At(x, y+1, z)) &&
		worldgen.IsSolidFull(w.At(x, y-1, z))
}

// bedFacingAwayFrom turns a woken sleeper back toward the bed they left.
func bedFacingAwayFrom(bed blockPos, x, z float64) float32 {
	vx, vz := float64(bed.x)+0.5-x, float64(bed.z)+0.5-z
	if vx == 0 && vz == 0 {
		return 0
	}
	return float32(wrapDegrees(math.Atan2(vz, vx)*180/math.Pi - 90))
}

// wrapDegrees folds an angle into (-180, 180], like Mth.wrapDegrees.
func wrapDegrees(d float64) float64 {
	d = math.Mod(d, 360)
	if d >= 180 {
		d -= 360
	}
	if d < -180 {
		d += 360
	}
	return d
}

// handleUseBed processes a right-click on a bed block.
func (h *hub) handleUseBed(players map[int32]*tracked, t *tracked, pos blockPos) {
	if t.dead {
		return
	}
	if !bedWorks(t.dim) {
		// BedBlock.useWithoutItem: outside the overworld a bed is a bomb. Both
		// halves go first, then the blast — so the bed cannot be relit by the
		// explosion's own block updates.
		h.blowUpRespawnBlock(players, t, pos, true)
		return
	}
	// Everything below is anchored to the HEAD half, whichever end was clicked.
	head, ok := h.bedHead(t.dim, pos)
	if !ok {
		return // half a bed: nothing to lie in
	}
	if h.spawns != nil { // clicking a bed claims it as home (vanilla)
		h.spawns.set(t.p.name, head, t.dim)
		t.p.trySendEv(chatEv("Respawn point set"))
	}
	if dt := h.dayTime.Load() % dayLengthTicks; dt < sleepStart || dt > sleepEnd {
		t.p.trySendEv(chatEv("You can only sleep at night"))
		return
	}
	bx, by, bz := float64(head.x)+0.5, float64(head.y), float64(head.z)+0.5
	for _, m := range h.mobs {
		if m.hostile && m.dying == 0 && m.dim == t.dim &&
			math.Abs(m.x-bx) <= monsterRangeH && math.Abs(m.z-bz) <= monsterRangeH &&
			math.Abs(m.y-by) <= monsterRangeV {
			t.p.trySendEv(chatEv("You may not rest now; there are monsters nearby"))
			return
		}
	}
	if !t.sleeping {
		h.setSleeping(players, t, head)
		n, m := sleepCount(players)
		body := chatEv(fmt.Sprintf("%s is sleeping (%d/%d)", t.p.name, n, m))
		for _, o := range players {
			o.p.trySendEv(body)
		}
	}
	// The night-skip itself is timed: updateSleep (tick loop) turns the clock
	// only after everyone has been in bed sleepSkipTicks — the window where the
	// client plays the lying pose + screen fade (vanilla behaviour).
}

// sleepCount returns (sleeping, eligible) — spectators don't count toward the
// everyone-in-bed requirement.
func sleepCount(players map[int32]*tracked) (int, int) {
	slept, eligible := 0, 0
	for _, t := range players {
		if t.gamemode == gmSpectator {
			continue
		}
		eligible++
		if t.sleeping {
			slept++
		}
	}
	return slept, eligible
}

// sleepSkipTicks is how long everyone must be in bed before the night turns —
// vanilla's ~5s sleep timer, and the window where the client shows the lying
// pose and fades the screen (an instant skip made sleep look like nothing
// happened).
const sleepSkipTicks = 100

// updateSleep runs every tick: once every eligible player has been asleep for
// sleepSkipTicks, the clock jumps to sunrise and everyone stands up.
func (h *hub) updateSleep(players map[int32]*tracked) {
	slept, eligible := sleepCount(players)
	need := (eligible*h.rules.SleepPercent + 99) / 100 // gamerule playersSleepingPercentage
	if h.rules.SleepPercent > 100 {
		need = eligible + 1 // vanilla: >100 makes sleeping never skip
	}
	if eligible == 0 || slept < need || slept == 0 {
		return
	}
	now := h.tick.Load()
	for _, t := range players {
		if t.sleeping && now-t.sleepingAt < sleepSkipTicks {
			return // still settling in — let the fade play out
		}
	}
	dt := h.dayTime.Load()
	h.dayTime.Store((dt/dayLengthTicks + 1) * dayLengthTicks) // next sunrise
	if h.raining {                                            // vanilla: sleeping through the night resets the weather cycle
		h.resetWeatherCycle()
	}
	body := timeEv(h.tick.Load(), h.dayTime.Load())
	morning := chatEv("Good morning — the night was slept away")
	for _, t := range players {
		h.wakePlayer(players, t)
		t.p.trySendEv(body)
		t.p.trySendEv(morning)
	}
}

// wakeIfAway ends sleep when the player wanders from their bed — a fallback
// for clients that move instead of sending STOP_SLEEPING.
func (h *hub) wakeIfAway(players map[int32]*tracked, t *tracked) {
	if t.sleeping &&
		sq(t.x-float64(t.sleepPos.x)-0.5)+sq(t.z-float64(t.sleepPos.z)-0.5) > bedRange*bedRange {
		h.wakePlayer(players, t)
	}
}

// blowUpRespawnBlock is the shared "you cannot respawn here" detonation behind
// a bed in the Nether and a respawn anchor anywhere but. The block is removed
// first (both halves, for a bed), then a power-5 blast goes off at its centre
// carrying the bad_respawn_point damage type — the one whose death message
// vanilla writes as [Intentional Game Design].
func (h *hub) blowUpRespawnBlock(players map[int32]*tracked, t *tracked, pos blockPos, bed bool) {
	dim := t.dim
	if bed {
		for _, p := range h.bedHalves(dim, pos) {
			h.setBlockAt(players, dim, p, worldgen.Air)
		}
	} else {
		h.setBlockAt(players, dim, pos, worldgen.Air)
	}
	h.explodeTyped(players, dim, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5,
		badRespawnPower, badRespawnDamage, dtBadRespawnPoint, deathCause{key: causeBadRespawn})
}

const (
	badRespawnPower  = 5  // BedBlock/RespawnAnchorBlock explode(): radius 5.0F
	badRespawnDamage = 50 // point-blank, before armour — scaled from TNT's 4/40
)

// bedHalves returns the clicked bed cell plus its other half, found through the
// facing/part pair the two blocks share.
func (h *hub) bedHalves(dim int, pos blockPos) []blockPos {
	out := []blockPos{pos}
	w := h.worldFor(dim)
	if w == nil {
		return out
	}
	state := w.Block(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(state)
	if !ok || !isBed(info) {
		return out
	}
	dx, dz := facingStep(worldgen.GetProperty(info, state, "facing"))
	if worldgen.GetProperty(info, state, "part") == "head" {
		dx, dz = -dx, -dz // the head points back down the bed to the foot
	}
	other := blockPos{pos.x + dx, pos.y, pos.z + dz}
	if oi, ok := worldgen.InfoForState(w.Block(other.x, other.y, other.z)); ok && isBed(oi) {
		out = append(out, other)
	}
	return out
}

// facingStep is the horizontal step a "facing" property names.
func facingStep(facing string) (int, int) {
	switch facing {
	case "north":
		return 0, -1
	case "south":
		return 0, 1
	case "west":
		return -1, 0
	case "east":
		return 1, 0
	}
	return 0, 0
}

// respawnPoint resolves where a player comes back after death, and in which
// dimension: their claimed bed or charged respawn anchor if it still stands and
// still works where it stands, else world spawn in the overworld.
func (h *hub) respawnPoint(players map[int32]*tracked, t *tracked) (float64, float64, float64, int) {
	if h.spawns != nil {
		if pos, dim, ok := h.spawns.get(t.p.name); ok {
			w := h.worldFor(dim)
			if w != nil {
				state := w.Block(pos.x, pos.y, pos.z)
				if info, ok2 := worldgen.InfoForState(state); ok2 && isBed(info) && bedWorks(dim) {
					return float64(pos.x) + 0.5, float64(pos.y) + 0.6, float64(pos.z) + 0.5, dim
				}
				if charge := anchorCharge(state); charge > 0 && anchorWorks(dim) {
					// The anchor spends one charge per respawn.
					h.setBlockAt(players, dim, pos, anchorWithCharge(state, charge-1))
					return float64(pos.x) + 0.5, float64(pos.y) + 1, float64(pos.z) + 0.5, dim
				}
			}
			t.p.trySendEv(chatEv("You have no home bed or charged respawn anchor, or it was obstructed"))
		}
	}
	x, y, z := h.worldSpawn()
	return x, y, z, dimOverworld
}

// worldSpawn is the death-respawn fallback (no bed): the configured spawn when
// THIS shard owns it, else the surface at the centre of this shard's own region.
// Never returns another shard's turf or void — a death keeps you on your island.
func (h *hub) worldSpawn() (x, y, z float64) {
	if h.hasWorldSpawn {
		return h.worldSpawnX, h.worldSpawnY, h.worldSpawnZ
	}
	if h.shardOf != nil {
		bx, bz := h.regionCenter()
		return float64(bx) + 0.5, h.world.SurfaceY(bx, bz), float64(bz) + 0.5
	}
	return 0.5, h.world.SurfaceY(0, 0), 0.5
}
