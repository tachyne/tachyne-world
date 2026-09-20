package server

import "math"

// FollowFlockLeaderGoal — why cod, salmon and tropical fish move in shoals
// rather than as a scatter of singletons. Every few seconds a fish with no
// school of its own looks for others of its kind within eight blocks: one of
// them becomes the leader and the rest follow it about, breaking off if they
// fall more than eleven blocks behind.

const (
	schoolSearch   = 8.0   // getEntitiesOfClass(inflate(8, 8, 8))
	schoolBreakSq  = 121.0 // inRangeOfLeader: 11 blocks
	schoolRecheck  = 200   // nextStartTick: 200 + rand(200) % 20
	schoolMaxSize  = 5     // getMaxSchoolSize (the spawn cluster size)
	schoolFollowSp = 1.0   // pathToLeader moves at 1.0
)

// schoolingFish is the AbstractSchoolingFish set.
var schoolingFish = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"cod", "salmon", "tropical_fish"} {
		if id, ok := entityByName[n]; ok {
			out[id] = true
		}
	}
	return out
}()

// schoolStep runs each mob update for a schooling fish. Reports whether it is
// steering the fish (following its leader).
func (h *hub) schoolStep(players map[int32]*tracked, m *mob) bool {
	if !schoolingFish[m.etype] || m.dying != 0 || m.panic > 0 {
		return false
	}
	// Following somebody: keep after them until they are out of range.
	if leader := h.mobs[m.schoolLeader]; leader != nil {
		if leader.dying != 0 || leader.dim != m.dim ||
			dist3sq(leader.x, leader.y, leader.z, m.x, m.y, m.z) > schoolBreakSq {
			m.schoolLeader = 0
		} else {
			dx, dz := leader.x-m.x, leader.z-m.z
			if d := math.Hypot(dx, dz); d > 1 {
				sp := m.moveSpeed() * schoolFollowSp
				m.vx, m.vz = dx/d*sp, dz/d*sp
				m.rest = 0
			}
			return true
		}
	}
	if m.schoolFollowers > 0 { // a leader does not follow anyone
		return false
	}
	if m.schoolNext -= mobMoveInterval; m.schoolNext > 0 {
		return false
	}
	m.schoolNext = schoolRecheck + h.rng.Intn(schoolRecheck)%20
	// Find a leader: another fish of the same kind that is either leading a
	// school with room in it, or not following anyone.
	var leader *mob
	h.grid().nearby(m.dim, m.x, m.z, schoolSearch, func(o *mob) {
		if leader != nil || o.eid == m.eid || o.etype != m.etype || o.dying != 0 {
			return
		}
		if math.Abs(o.y-m.y) > schoolSearch {
			return
		}
		if o.schoolFollowers > 0 && o.schoolFollowers < schoolMaxSize {
			leader = o // join an existing school
			return
		}
		if o.schoolLeader == 0 && o.schoolFollowers == 0 {
			leader = o // …or start one with a fish that is on its own
		}
	})
	if leader == nil {
		return false
	}
	m.schoolLeader = leader.eid
	leader.schoolFollowers++
	return true
}
