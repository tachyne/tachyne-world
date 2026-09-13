package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Cats sit on things (CatSitOnBlockGoal, a MoveToBlockGoal): a tamed cat
// that has not been told to stay and is not trailing its owner looks, every
// ten to twenty seconds, for a chest nobody has open, a lit furnace or the
// foot of a bed within eight blocks with air above it, walks onto it at
// four fifths pace and settles into the sitting pose — for a minute to
// three, or until the block stops qualifying (somebody opens the chest,
// the furnace goes out) — then gets up and wanders off.

const (
	catSitSearch   = 8    // MoveToBlockGoal searchRange
	catSitSpeed    = 0.8  // speedModifier
	catSitReach    = 1.0  // acceptedDistance to the block above's centre
	catSitGiveUp   = 1200 // GIVE_UP_TICKS of trying to get there
	catSitInterval = 200  // nextStartTick: 200 + nextInt(200)
)

// catSitTarget is isValidTarget: air above; a chest with no opener, a lit
// furnace, or any bed part but the head.
func (h *hub) catSitTarget(dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	if w.At(pos.x, pos.y+1, pos.z) != worldgen.Air {
		return false
	}
	s := w.At(pos.x, pos.y, pos.z)
	switch {
	case s >= chestStateMin && s <= chestStateMax:
		return h.chestOpenCount(dim, pos) < 1
	case s >= furnaceStateMin && s <= furnaceStateMax:
		info, _ := worldgen.InfoForState(s)
		return worldgen.GetProperty(info, s, "lit") == "true"
	}
	if info, ok := worldgen.InfoForState(s); ok && isBed(info) {
		return worldgen.GetProperty(info, s, "part") != "head"
	}
	return false
}

// catSitFind is findNearestBlock: rings out to eight, the block a level
// below the feet first, then one up, then two down.
func (h *hub) catSitFind(m *mob) (blockPos, bool) {
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for _, dy := range []int{0, 1, -1} {
		for r := 0; r < catSitSearch; r++ {
			for dx := -r; dx <= r; dx++ {
				for dz := -r; dz <= r; dz++ {
					if dx != r && dx != -r && dz != r && dz != -r {
						continue // the ring's edge only
					}
					p := blockPos{bx + dx, by + dy - 1, bz + dz}
					if h.catSitTarget(m.dim, p) {
						return p, true
					}
				}
			}
		}
	}
	return blockPos{}, false
}

// catSitStep is the goal's tick. Returns whether it holds the cat.
func (h *hub) catSitStep(players map[int32]*tracked, m *mob) bool {
	if !m.tamed || m.sitting || m.hasTarget || m.loveTicks > 0 || m.tempted {
		h.catSitStop(players, m)
		return false
	}
	if m.sitBlock == (blockPos{}) {
		if m.sitNext > 0 {
			m.sitNext -= mobMoveInterval
			return false
		}
		m.sitNext = catSitInterval + h.rng.Intn(catSitInterval)
		pos, ok := h.catSitFind(m)
		if !ok {
			return false
		}
		m.sitBlock, m.sitTry = pos, 0
		m.sitStay = h.rng.Intn(h.rng.Intn(catSitGiveUp)+catSitGiveUp) + catSitGiveUp
	}
	if m.sitTry < -m.sitStay || m.sitTry > catSitGiveUp || !h.catSitTarget(m.dim, m.sitBlock) {
		h.catSitStop(players, m)
		return false
	}
	tx, ty, tz := float64(m.sitBlock.x)+0.5, float64(m.sitBlock.y)+1, float64(m.sitBlock.z)+0.5
	dx, dz := tx-m.x, tz-m.z
	hd := math.Hypot(dx, dz)
	if hd < catSitReach && math.Abs(m.y-ty) < 1 {
		m.sitTry -= mobMoveInterval
		m.vx, m.vz = 0, 0
		m.rest = 0
		h.setSitPose(players, m, true)
		return true
	}
	m.sitTry += mobMoveInterval
	h.setSitPose(players, m, false)
	sp := m.moveSpeed() * catSitSpeed
	m.vx, m.vz = dx/hd*sp, dz/hd*sp
	m.rest = 0
	return true
}

// catSitStop is stop(): up off the block, target forgotten.
func (h *hub) catSitStop(players map[int32]*tracked, m *mob) {
	if m.sitBlock != (blockPos{}) {
		m.sitBlock, m.sitTry = blockPos{}, 0
	}
	h.setSitPose(players, m, false)
}

// setSitPose is setInSittingPose: the same flag bit an ordered sit shows.
func (h *hub) setSitPose(players map[int32]*tracked, m *mob, on bool) {
	if m.sitPose == on {
		return
	}
	m.sitPose = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(petMeta(m)))
}
