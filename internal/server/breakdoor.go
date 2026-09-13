package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Zombies break doors (BreakDoorGoal, hard difficulty only): a zombie that
// spawned with the knack (f×10% of them) and is stopped by a closed wooden
// door on its way to a player beats on it — a thud and a swing about once
// a second, the crack overlay stepping up — and after two hundred and
// forty ticks the door comes off its hinges. Mob griefing gates it; moving
// two blocks away or the door opening gives it up.

const (
	doorBreakTicks = 240 // DEFAULT_DOOR_BREAK_TIME
	doorBreakReach = 2.0 // canContinueToUse: closerToCenterThan(doorPos, 2)
)

// doorAhead finds a closed wooden door in the cell the mob is walking into,
// at its feet or head.
func (h *hub) doorAhead(m *mob, nx, nz float64) (blockPos, bool) {
	w := h.worldFor(m.dim)
	fx, fy, fz := int(math.Floor(nx)), int(math.Floor(m.y)), int(math.Floor(nz))
	for _, y := range []int{fy, fy + 1} {
		if s := w.At(fx, y, fz); worldgen.IsClosedDoor(s) && worldgen.IsWoodenDoor(s) {
			return blockPos{fx, y, fz}, true
		}
	}
	return blockPos{}, false
}

// doorLowerHalf anchors a door position on its lower half.
func (h *hub) doorLowerHalf(dim int, pos blockPos) blockPos {
	s := h.worldFor(dim).At(pos.x, pos.y, pos.z)
	if info, ok := worldgen.InfoForState(s); ok && worldgen.GetProperty(info, s, "half") == "upper" {
		return blockPos{pos.x, pos.y - 1, pos.z}
	}
	return pos
}

// zombieBeatsDoor is the goal's tick: called when a door-breaking zombie
// hunting a player is stopped by a closed wooden door. Returns whether it
// is holding the zombie at the door.
func (h *hub) zombieBeatsDoor(players map[int32]*tracked, m *mob, door blockPos) bool {
	if !m.breaksDoors || h.rules.Difficulty != diffHard || !h.rules.MobGriefing {
		return false
	}
	door = h.doorLowerHalf(m.dim, door)
	if m.doorPos != door {
		h.zombieStopDoor(players, m)
		m.doorPos, m.doorTicks, m.doorStage = door, 0, -1
	}
	if dist3(m.x, m.y, m.z, float64(door.x)+0.5, float64(door.y), float64(door.z)+0.5) > doorBreakReach {
		h.zombieStopDoor(players, m)
		return false
	}
	m.vx, m.vz = 0, 0
	m.doorTicks += mobMoveInterval
	if h.rng.Intn(20/mobMoveInterval) == 0 { // levelEvent 1019 + the swing, 1/20 a tick
		h.playSoundDim(players, m.dim, "minecraft:entity.zombie.attack_wooden_door", sndHostile, float64(door.x)+0.5, float64(door.y)+0.5, float64(door.z)+0.5, 2, 0.8+h.rng.Float32()*0.4)
		h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	}
	if stage := int8(m.doorTicks * 10 / doorBreakTicks); stage != m.doorStage {
		m.doorStage = stage
		h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.BlockBreakProgress{EID: m.eid, X: int32(door.x), Y: int32(door.y), Z: int32(door.z), Progress: stage})
	}
	if m.doorTicks < doorBreakTicks {
		return true
	}
	// Off its hinges: both halves go, the door drops as an item.
	w := h.worldFor(m.dim)
	lower := w.At(door.x, door.y, door.z)
	h.setBlockLive(players, m.dim, door.x, door.y+1, door.z, worldgen.Air)
	h.breakBlockDrop(players, m.dim, door, lower)
	h.playSoundDim(players, m.dim, "minecraft:entity.zombie.break_wooden_door", sndHostile, float64(door.x)+0.5, float64(door.y)+0.5, float64(door.z)+0.5, 2, 0.8+h.rng.Float32()*0.4)
	h.zombieStopDoor(players, m)
	return false
}

// zombieStopDoor clears the crack overlay and forgets the door.
func (h *hub) zombieStopDoor(players map[int32]*tracked, m *mob) {
	if m.doorPos == (blockPos{}) {
		return
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, attachproto.BlockBreakProgress{EID: m.eid, X: int32(m.doorPos.x), Y: int32(m.doorPos.y), Z: int32(m.doorPos.z), Progress: -1})
	m.doorPos, m.doorTicks, m.doorStage = blockPos{}, 0, -1
}
