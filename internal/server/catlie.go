package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Cats lie on beds. CatRelaxOnOwnerGoal: a tamed cat whose owner is asleep
// within ten blocks walks to the foot of their bed, watches them for a
// moment (the relax pose), then curls up lying there until they wake — one
// cat per bed. CatLieOnBedGoal: a tamed cat with nothing better to do finds
// any bed within eight blocks and lies on it for a while. The morning gift
// the first goal leaves is the sunrise code's (catMorningGifts).

const (
	metaIndexCatLying = 20 // IS_LYING (1.21.5; the gateways shift 26.2)
	metaIndexCatRelax = 21 // RELAX_STATE_ONE
	catRelaxRangeSq   = 100.0
	catRelaxNearSq    = 2.5
	catRelaxSpeed     = 1.1
	catRelaxSettle    = 16 // onBedTicks before lying (adjustedTickDelay(16))
	catLieSearch      = 8
	catLieVertical    = 6
	catLieSpeed       = 1.1
	catLieInterval    = 40 // nextStartTick
)

func catLieMeta(m *mob) []byte {
	return boolsMeta(m.eid, boolEntry{metaIndexCatLying, m.lying}, boolEntry{metaIndexCatRelax, m.relaxOne})
}

// setCatLying is setLying + setRelaxStateOne, synced on change.
func (h *hub) setCatLying(players map[int32]*tracked, m *mob, lying, relax bool) {
	if m.lying == lying && m.relaxOne == relax {
		return
	}
	m.lying, m.relaxOne = lying, relax
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(catLieMeta(m)))
}

// catRelaxGoal is CatRelaxOnOwnerGoal.canUse: the owner asleep in a bed
// within ten blocks; the spot is the block behind the bed's head (its
// foot), and no other cat may already be lying or relaxing within two.
func (h *hub) catRelaxGoal(players map[int32]*tracked, m *mob) (*tracked, blockPos, bool) {
	owner := players[m.owner]
	if owner == nil || !owner.sleeping || owner.dim != m.dim || dist3sq(owner.x, owner.y, owner.z, m.x, m.y, m.z) > catRelaxRangeSq {
		return nil, blockPos{}, false
	}
	w := h.worldFor(m.dim)
	pos := blockPos{int(math.Floor(owner.x)), int(math.Floor(owner.y)), int(math.Floor(owner.z))}
	s := w.At(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(s)
	if !ok || !isBed(info) {
		return nil, blockPos{}, false
	}
	goal := pos
	switch worldgen.GetProperty(info, s, "facing") { // relative(facing.getOpposite())
	case "north":
		goal.z++
	case "south":
		goal.z--
	case "west":
		goal.x++
	case "east":
		goal.x--
	}
	occupied := false
	h.grid().nearby(m.dim, float64(goal.x)+0.5, float64(goal.z)+0.5, 3, func(o *mob) {
		if o != m && o.etype == entityCat && (o.lying || o.relaxOne) && math.Abs(o.x-float64(goal.x)-0.5) <= 2.5 && math.Abs(o.z-float64(goal.z)-0.5) <= 2.5 {
			occupied = true
		}
	})
	if occupied {
		return nil, blockPos{}, false
	}
	return owner, goal, true
}

// catRelaxStep is the goal's tick; returns whether it holds the cat.
func (h *hub) catRelaxStep(players map[int32]*tracked, m *mob) bool {
	if !m.tamed || m.sitting {
		return false
	}
	owner, goal, ok := h.catRelaxGoal(players, m)
	if !ok {
		if m.relaxTicks > 0 { // stop()
			m.relaxTicks = 0
			h.setCatLying(players, m, false, false)
		}
		return false
	}
	m.relaxTicks = max(m.relaxTicks, 1)
	h.setSitPose(players, m, false)
	m.rest = 0
	if dist3sq(owner.x, owner.y, owner.z, m.x, m.y, m.z) < catRelaxNearSq {
		m.vx, m.vz = 0, 0
		m.relaxTicks += mobMoveInterval
		if m.relaxTicks > catRelaxSettle {
			h.setCatLying(players, m, true, false)
		} else {
			m.yaw = float32(math.Atan2(-(owner.x-m.x), owner.z-m.z) * 180 / math.Pi)
			h.setCatLying(players, m, false, true)
		}
		return true
	}
	h.setCatLying(players, m, false, m.relaxOne)
	h.steerTo(m, float64(goal.x)+0.5, float64(goal.z)+0.5, catRelaxSpeed)
	return true
}

// catLieTarget is CatLieOnBedGoal.isValidTarget: any bed block with air above.
func (h *hub) catLieTarget(dim int, pos blockPos) bool {
	w := h.worldFor(dim)
	if w.At(pos.x, pos.y+1, pos.z) != worldgen.Air {
		return false
	}
	info, ok := worldgen.InfoForState(w.At(pos.x, pos.y, pos.z))
	return ok && isBed(info)
}

// catLieStep is CatLieOnBedGoal: a MoveToBlockGoal (1.1, range 8, six
// up, starting two down) that lies where the sit goal would sit.
func (h *hub) catLieStep(players map[int32]*tracked, m *mob) bool {
	if !m.tamed || m.sitting || m.relaxTicks > 0 || m.loveTicks > 0 || m.tempted {
		if m.lieBlock != (blockPos{}) {
			m.lieBlock, m.lieTry = blockPos{}, 0
			h.setCatLying(players, m, false, false)
		}
		return false
	}
	if m.lieBlock == (blockPos{}) {
		if m.lieNext > 0 {
			m.lieNext -= mobMoveInterval
			return false
		}
		m.lieNext = catLieInterval
		pos, ok := h.catFindBlock(m, catLieSearch, -2, catLieVertical, h.catLieTarget)
		if !ok {
			return false
		}
		m.lieBlock, m.lieTry = pos, 0
		m.lieStay = h.rng.Intn(h.rng.Intn(catSitGiveUp)+catSitGiveUp) + catSitGiveUp
	}
	if m.lieTry < -m.lieStay || m.lieTry > catSitGiveUp || !h.catLieTarget(m.dim, m.lieBlock) {
		m.lieBlock, m.lieTry = blockPos{}, 0
		h.setCatLying(players, m, false, false)
		return false
	}
	h.setSitPose(players, m, false)
	tx, ty, tz := float64(m.lieBlock.x)+0.5, float64(m.lieBlock.y)+1, float64(m.lieBlock.z)+0.5
	dx, dz := tx-m.x, tz-m.z
	hd := math.Hypot(dx, dz)
	m.rest = 0
	if hd < catSitReach && math.Abs(m.y-ty) < 1 {
		m.lieTry -= mobMoveInterval
		m.vx, m.vz = 0, 0
		h.setCatLying(players, m, true, false)
		return true
	}
	m.lieTry += mobMoveInterval
	h.setCatLying(players, m, false, false)
	sp := m.moveSpeed() * catLieSpeed
	m.vx, m.vz = dx/hd*sp, dz/hd*sp
	return true
}
