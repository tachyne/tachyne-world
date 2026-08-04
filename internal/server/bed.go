package server

import (
	"fmt"

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

	bedSurface = 0.5625 // a bed's collision height — where a sleeper/waker stands
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

// setSleeping lies the player down: move them onto the bed (their own client
// gets a position sync; everyone's gets the sleeping pose + bed anchor).
func (h *hub) setSleeping(players map[int32]*tracked, t *tracked, pos blockPos) {
	t.sleeping, t.sleepPos, t.sleepingAt = true, pos, h.tick.Load()
	t.x, t.y, t.z = float64(pos.x)+0.5, float64(pos.y)+bedSurface, float64(pos.z)+0.5
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
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
	body := wakeMetadata(t.p.eid)
	t.p.trySendEv(metaEv(body))
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	for _, o := range players {
		if o != t {
			o.p.trySendEv(metaEv(body))
		}
	}
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
	if h.spawns != nil { // clicking a bed claims it as home (vanilla)
		h.spawns.set(t.p.name, pos, t.dim)
		t.p.trySendEv(chatEv("Respawn point set"))
	}
	if dt := h.dayTime.Load() % dayLengthTicks; dt < sleepStart || dt > sleepEnd {
		t.p.trySendEv(chatEv("You can only sleep at night"))
		return
	}
	for _, m := range h.mobs {
		if m.hostile && m.dying == 0 &&
			sq(m.x-float64(pos.x))+sq(m.y-float64(pos.y))+sq(m.z-float64(pos.z)) < monsterR2 {
			t.p.trySendEv(chatEv("You may not rest now; there are monsters nearby"))
			return
		}
	}
	if !t.sleeping {
		h.setSleeping(players, t, pos)
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
