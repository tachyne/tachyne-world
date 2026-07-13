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
	if h.spawns != nil { // clicking a bed claims it as home (vanilla)
		h.spawns.set(t.p.name, pos)
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

// respawnPoint resolves where a player comes back after death: their claimed
// bed if it still stands, else world spawn.
func (h *hub) respawnPoint(t *tracked) (float64, float64, float64) {
	if h.spawns != nil {
		if pos, ok := h.spawns.get(t.p.name); ok {
			if info, ok2 := worldgen.InfoForState(h.world.Block(pos.x, pos.y, pos.z)); ok2 && isBed(info) {
				return float64(pos.x) + 0.5, float64(pos.y) + 0.6, float64(pos.z) + 0.5
			}
			t.p.trySendEv(chatEv("You have no home bed, or it was obstructed"))
		}
	}
	return h.worldSpawn()
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
