package server

import "math"

// Wolves beg (BegGoal): a wolf that sees a player within eight blocks
// holding a bone or any of its food tilts its head (the INTERESTED flag)
// and watches them for forty to eighty ticks, then loses interest until
// the goal is picked up again. Looking only — it never walks over.

const (
	begRange          = 8.0 // BegGoal lookDistance
	metaIndexWolfBeg  = 19  // DATA_INTERESTED_ID (1.21.5; the 26.2 shift is the gateway's)
	begLookMin        = 40  // start(): 40 + nextInt(40) ticks
	begLookRandom     = 40
	begGoalReconsider = 4 // a goal at priority 9 rechecks a few ticks apart
)

// wolfWantsToBeg: playerHoldingInteresting for either hand.
func wolfWantsToBeg(t *tracked) bool {
	for _, it := range []int32{heldStack(t).item, t.offhand.item} {
		if it != 0 && (it == itemBone || isLoveFood(entityWolf, it)) {
			return true
		}
	}
	return false
}

// begStep is the goal's tick: it never holds the wolf's legs (Flag.LOOK
// only), so it always returns false and just drives the head and flag.
func (h *hub) begStep(players map[int32]*tracked, m *mob) {
	if m.begTicks > 0 {
		m.begTicks -= mobMoveInterval
		t := players[m.begPlayer]
		if t == nil || t.dead || dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) > begRange*begRange || !wolfWantsToBeg(t) || m.begTicks <= 0 {
			h.setBegging(players, m, false)
			return
		}
		m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
		return
	}
	if m.begCalm > 0 {
		m.begCalm--
		return
	}
	m.begCalm = begGoalReconsider
	var best *tracked
	bestD2 := begRange * begRange
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmSpectator || !wolfWantsToBeg(t) {
			continue
		}
		if d2 := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); d2 < bestD2 {
			best, bestD2 = t, d2
		}
	}
	if best == nil {
		return
	}
	m.begPlayer = best.p.eid
	m.begTicks = begLookMin + h.rng.Intn(begLookRandom)
	h.setBegging(players, m, true)
}

// setBegging flips the INTERESTED flag (the head tilt) on every viewer.
func (h *hub) setBegging(players map[int32]*tracked, m *mob, on bool) {
	if !on {
		m.begTicks, m.begPlayer = 0, 0
	}
	if m.begging == on {
		return
	}
	m.begging = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexWolfBeg, on)))
}
