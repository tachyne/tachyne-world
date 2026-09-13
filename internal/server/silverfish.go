package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Silverfish (SilverfishWakeUpFriendsGoal + SilverfishMergeWithStoneGoal):
// a silverfish hurt by something looks, twenty ticks later, through the
// infested blocks within five up or down and ten across and breaks them
// open one after another — each freeing its silverfish — stopping after
// any with a coin toss; and an idle one, one tick in ten, picks a random
// side and, if the block there is stone, cobblestone, stone bricks (or
// their mossy, cracked or chiselled kinds) or deepslate, burrows into it,
// turning it infested and vanishing in a puff.

const (
	silverWakeDelay = 20 // notifyHurt: adjustedTickDelay(20)
	silverWakeY     = 5
	silverWakeXZ    = 10
	silverMergeOdds = 10 // reducedTickDelay(10)
)

// infestedHosts pairs each InfestedBlock host with its infested state.
var infestedHosts = [][2]string{
	{"stone", "infested_stone"}, {"cobblestone", "infested_cobblestone"}, {"stone_bricks", "infested_stone_bricks"},
	{"mossy_stone_bricks", "infested_mossy_stone_bricks"}, {"cracked_stone_bricks", "infested_cracked_stone_bricks"},
	{"chiseled_stone_bricks", "infested_chiseled_stone_bricks"}, {"deepslate", "infested_deepslate"},
}

// infestedStateByHost is InfestedBlock.infestedStateByHost (deepslate keeps
// its axis); hostStateByInfested the reverse.
func infestedStateByHost(s uint32) (uint32, bool) {
	for _, p := range infestedHosts {
		lo, hi := worldgen.BlockRange(p[0])
		if s >= lo && s <= hi {
			return worldgen.BlockBase(p[1]) + (s - lo), true
		}
	}
	return 0, false
}

func hostStateByInfested(s uint32) (uint32, bool) {
	for _, p := range infestedHosts {
		lo, hi := worldgen.BlockRange(p[1])
		if s >= lo && s <= hi {
			return worldgen.BlockBase(p[0]) + (s - lo), true
		}
	}
	return 0, false
}

// silverfishStep runs each mob update. Returns whether it holds the mob.
func (h *hub) silverfishStep(players map[int32]*tracked, m *mob) bool {
	if m.silverHurt { // notifyHurt
		m.silverHurt = false
		if m.silverWake == 0 {
			m.silverWake = silverWakeDelay
		}
	}
	if m.silverWake > 0 {
		m.silverWake -= mobMoveInterval
		if m.silverWake <= 0 {
			m.silverWake = 0
			h.silverfishWakeFriends(players, m)
		}
		return false
	}
	// MergeWithStone: idle, mob griefing, one tick in ten.
	if m.hasTarget || !h.rules.MobGriefing || h.rng.Intn(silverMergeOdds/mobMoveInterval) != 0 {
		return false
	}
	dirs := [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	d := dirs[h.rng.Intn(6)]
	bx, by, bz := int(math.Floor(m.x))+d[0], int(math.Floor(m.y+0.5))+d[1], int(math.Floor(m.z))+d[2]
	w := h.worldFor(m.dim)
	inf, ok := infestedStateByHost(w.At(bx, by, bz))
	if !ok {
		return false
	}
	h.setBlockAt(players, m.dim, blockPos{bx, by, bz}, inf)
	h.spawnParticles(players, particlePoof, m.x, m.y+0.5, m.z, 0.3, 0.05, 10) // spawnAnim
	h.removeMob(players, m)
	return true
}

// silverfishWakeFriends is the goal's tick past the delay: vanilla's
// alternating walk out from the mob, breaking infested blocks open.
func (h *hub) silverfishWakeFriends(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for n := 0; n <= silverWakeY && n >= -silverWakeY; n = alternateOut(n) {
		for n2 := 0; n2 <= silverWakeXZ && n2 >= -silverWakeXZ; n2 = alternateOut(n2) {
			for n3 := 0; n3 <= silverWakeXZ && n3 >= -silverWakeXZ; n3 = alternateOut(n3) {
				pos := blockPos{bx + n2, by + n, bz + n3}
				s := w.At(pos.x, pos.y, pos.z)
				if !isInfested(s) {
					continue
				}
				if h.rules.MobGriefing {
					h.setBlockAt(players, m.dim, pos, worldgen.Air)
					h.toNearbyEv(players, m.dim, float64(pos.x), float64(pos.z), blockBreakEvent(pos.x, pos.y, pos.z, s))
					h.spawnMobIn(players, entitySilverfish, m.dim, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5)
				} else if host, ok := hostStateByInfested(s); ok {
					h.setBlockAt(players, m.dim, pos, host)
				}
				if h.rng.Intn(2) == 0 {
					return
				}
			}
		}
	}
}

// alternateOut is vanilla's (n <= 0 ? 1 : 0) - n: 0, 1, -1, 2, -2, …
func alternateOut(n int) int {
	if n <= 0 {
		return 1 - n
	}
	return -n
}
