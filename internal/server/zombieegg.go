package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Zombies and turtle eggs. Vanilla's Zombie.addBehaviourGoals registers
// ZombieAttackTurtleEggGoal (a RemoveBlockGoal) ABOVE the attack goal: a
// zombie, husk or drowned that finds a turtle egg within twenty-four blocks
// (three up or down) walks to it, stands on it stamping — the destroy sound
// every six ticks, a hop each tick — and after sixty ticks the clutch is
// gone, with the egg-break sound and a puff. Mob griefing gates it; the
// search runs every two to four hundred ticks; a clutch it cannot reach
// in twelve hundred ticks is given up. tachyne's zombies walked past eggs.

const (
	eggSearchRange   = 24   // MoveToBlockGoal searchRange
	eggSearchVert    = 3    // verticalSearchRange
	eggAccepted      = 1.14 // acceptedDistance to the block above the egg
	eggStampTicks    = 60   // ticksSinceReachedGoal > 60 → removeBlock
	eggGiveUpTicks   = 1200 // GIVE_UP_TICKS
	eggIntervalTicks = 200  // nextStartTick: 200 + nextInt(200)
	eggStampSoundMod = 6    // playDestroyProgressSound every 6 ticks on the egg
)

// eggValidTarget is RemoveBlockGoal.isValidTarget: a turtle egg with two
// clear blocks above.
func eggValidTarget(w blockReader, p blockPos) bool {
	return isTurtleEgg(w.At(p.x, p.y, p.z)) && w.At(p.x, p.y+1, p.z) == worldgen.Air && w.At(p.x, p.y+2, p.z) == worldgen.Air
}

// blockReader is the little of a world the egg search needs.
type blockReader interface{ At(x, y, z int) uint32 }

// findNearestEgg is MoveToBlockGoal.findNearestBlock: rings outward from
// the mob, nearest first, the mob's own level before those above and below.
func (h *hub) findNearestEgg(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	mx, my, mz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for y := 0; y <= eggSearchVert; y = nextSpiral(y) {
		for r := 0; r < eggSearchRange; r++ {
			for x := 0; x <= r; x = nextSpiral(x) {
				z0 := 0
				if x < r && x > -r {
					z0 = r // inside the ring's width only its two edges are new
				}
				for z := z0; z <= r; z = nextSpiral(z) {
					if p := (blockPos{mx + x, my + y - 1, mz + z}); eggValidTarget(w, p) {
						return p, true
					}
				}
			}
		}
	}
	return blockPos{}, false
}

// nextSpiral is vanilla's alternating walk: 0, 1, -1, 2, -2, …
func nextSpiral(v int) int {
	if v > 0 {
		return -v
	}
	return 1 - v
}

// zombieEggStep runs the goal for a zombie each mob update; returns whether
// it holds the zombie (walking to, or stamping on, a clutch).
func (h *hub) zombieEggStep(players map[int32]*tracked, m *mob) bool {
	if !zombieKind(m.etype) || m.dying > 0 {
		return false
	}
	if !h.rules.MobGriefing {
		m.eggPos = blockPos{}
		return false
	}
	w := h.worldFor(m.dim)
	if m.eggPos == (blockPos{}) { // canUse: the throttled search
		if m.eggNext > 0 {
			m.eggNext -= mobMoveInterval
			return false
		}
		m.eggNext = eggIntervalTicks + h.rng.Intn(eggIntervalTicks)
		p, ok := h.findNearestEgg(m)
		if !ok {
			return false
		}
		m.eggPos, m.eggTry, m.eggStamp = p, 0, 0
	}
	if !eggValidTarget(w, m.eggPos) || m.eggTry > eggGiveUpTicks { // canContinueToUse
		m.eggPos = blockPos{}
		return false
	}
	tx, ty, tz := float64(m.eggPos.x)+0.5, float64(m.eggPos.y+1), float64(m.eggPos.z)+0.5
	if dist3sq(tx, ty, tz, m.x, m.y, m.z) > eggAccepted*eggAccepted { // not there yet
		m.eggTry += mobMoveInterval
		m.eggStamp = 0
		h.steerTo(m, tx, tz, 1.0)
		return true
	}
	m.vx, m.vz = 0, 0
	for i := 0; i < mobMoveInterval; i++ { // RemoveBlockGoal.tick on the clutch
		if m.eggStamp%eggStampSoundMod == 0 {
			h.playSoundDim(players, m.dim, "minecraft:entity.zombie.destroy_egg", sndHostile, tx, ty, tz, 0.5, 0.9+h.rng.Float32()*0.2)
		}
		if m.eggStamp > eggStampTicks {
			h.setBlockAt(players, m.dim, m.eggPos, worldgen.Air) // removeBlock(eatPos, false): the whole clutch, no drop
			h.spawnParticles(players, particlePoof, tx, float64(m.eggPos.y), tz, 0.1, 0.15, 20)
			h.playSoundDim(players, m.dim, "minecraft:block.turtle_egg.break", sndBlock, tx, float64(m.eggPos.y), tz, 0.7, 0.9+h.rng.Float32()*0.2)
			m.eggPos = blockPos{}
			return true
		}
		m.eggStamp++
	}
	return true
}
