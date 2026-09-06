package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bells — BellBlock.onHit / attemptToRing and BellBlockEntity.onHit. A bell
// rings when struck on a proper side (a floor bell along its facing axis, a
// wall bell across it, a ceiling bell from any side; never from above or
// below, nor high on the block), when its redstone input rises, or when a
// projectile hits it: the bell sound at volume 2 and a block event (action
// 1, the strike direction) that swings it on every client. Raiders glowing
// and villagers running for their beds are not modelled.

var (
	bellRange       = blockRange("bell")
	bellRegistryID  = func() int32 { id, _ := worldgen.BlockRegistryID("bell"); return int32(id) }()
	bellSoundVolume = float32(2)
)

func isBell(s uint32) bool { return inRanges2(s, bellRange) }

type evRingBell struct {
	eid     int32
	x, y, z int
	dir     int32   // the face struck (0 down … 5 east)
	hitY    float32 // cursor height within the block (isProperHit's clickY)
}

func (evRingBell) isHubEvent() {}

// bellProperHit is BellBlock.isProperHit.
func bellProperHit(state uint32, dir int32, hitY float32) bool {
	if dir <= 1 || hitY > 0.8124 {
		return false
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return false
	}
	facing := worldgen.GetProperty(info, state, "facing")
	facingAxisZ := facing == "north" || facing == "south"
	clickAxisZ := dir == 2 || dir == 3
	switch worldgen.GetProperty(info, state, "attachment") {
	case "floor":
		return facingAxisZ == clickAxisZ
	case "single_wall", "double_wall":
		return facingAxisZ != clickAxisZ
	case "ceiling":
		return true
	}
	return false
}

// dirParam is Direction.get3DDataValue for the face ids the connection uses.
func dirParam(dir int32) uint8 {
	if dir < 0 || dir > 5 {
		return 2
	}
	return uint8(dir)
}

// onRingBell is a player striking the bell.
func (h *hub) onRingBell(players map[int32]*tracked, e evRingBell) {
	t := players[e.eid]
	if t == nil || t.dead {
		return
	}
	state := h.worldFor(t.dim).At(e.x, e.y, e.z)
	if !isBell(state) || !bellProperHit(state, e.dir, e.hitY) {
		return
	}
	h.ringBell(players, t.dim, blockPos{e.x, e.y, e.z}, e.dir)
	h.incCustom(t, "bell_ring", 1)
}

// ringBell is attemptToRing: the sound and the swing, from a direction (-1 =
// the bell's own facing, as redstone and explosions ring it).
func (h *hub) ringBell(players map[int32]*tracked, dim int, pos blockPos, dir int32) bool {
	state := h.worldFor(dim).At(pos.x, pos.y, pos.z)
	if !isBell(state) {
		return false
	}
	if dir < 0 {
		dir = 2
		if info, ok := worldgen.InfoForState(state); ok {
			switch worldgen.GetProperty(info, state, "facing") {
			case "south":
				dir = 3
			case "west":
				dir = 4
			case "east":
				dir = 5
			}
		}
	}
	h.toNearbyEv(players, dim, float64(pos.x), float64(pos.z), attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 1, Param: dirParam(dir), Block: bellRegistryID})
	h.playSoundDim(players, dim, "minecraft:block.bell.use", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, bellSoundVolume, 1)
	h.bellRevealRaiders(players, dim, pos)
	return true
}

// bellRevealRaiders is BellBlockEntity.updateEntities + makeRaidersGlow:
// every raider within 48 blocks of a rung bell glows for three seconds,
// and the bell resonates if it found any.
const (
	bellRaiderRange   = 48
	bellVillagerRange = 32  // BellBlockEntity: villagers within 32 blocks hear it
	villagerHideTicks = 300 // SetHiddenState.create(15 s, …)
)

func (h *hub) bellRevealRaiders(players map[int32]*tracked, dim int, pos blockPos) {
	found := false
	h.grid().nearby(dim, float64(pos.x)+0.5, float64(pos.z)+0.5, bellRaiderRange, func(m *mob) {
		if m.dying > 0 {
			return
		}
		if m.etype == entityVillager { // HEARD_BELL_TIME → the hide package: 15 s at a hiding place
			if dist3(m.x, m.y, m.z, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5) <= bellVillagerRange {
				m.hideUntil = h.tick.Load() + villagerHideTicks
			}
			return
		}
		if m.raidCenter == (blockPos{}) {
			return
		}
		if dist3(m.x, m.y, m.z, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5) > bellRaiderRange {
			return
		}
		h.applyMobEffect(players, m, effGlowing, 0, 3)
		found = true
	})
	if found {
		h.playSoundDim(players, dim, "minecraft:block.bell.resonate", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	}
}
