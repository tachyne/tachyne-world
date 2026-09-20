package server

import "math"

// PlayTagWithOtherKids — what baby villagers do all day. A child picks one of
// the other children it can see and runs after it; a child being chased runs
// away somewhere else in the village; and a child already being chased by one
// to five others is the one everybody else joins in on. Until now the babies
// ran the adults' schedule, which had them standing at workstations they
// cannot use.

const (
	playSeeRange = 16 // the babies it can see (VillagerBabiesSensor's reach)
	// CHASE_SPEED_MODIFIER and FLEE_SPEED_MODIFIER are both 0.6, against the
	// 0.5 every other villager activity walks at (Villager.registerBrainGoals
	// passes 0.5f to the play AND idle packages) — so tag is 1.2 times the
	// ordinary villager pace, not 0.6 of it. The engine's roam has no modifier
	// at all, which is what corresponds to vanilla's 0.5.
	playSpeed       = 0.6 / 0.5
	playFleeXZ      = 20.0 // MAX_FLEE_XZ_DIST
	playMaxChasers  = 5    // MAX_CHASERS_PER_TARGET
	playDecideOdds  = 10   // AVERAGE_WAIT_TIME_BETWEEN_RUNS: a 1-in-10 roll
	playReach       = 1.5  // close enough to count as caught — pick again
	playFleeArrived = 2.0
)

// villagerPlayStep runs a baby villager's game of tag. Reports whether it took
// the mob's movement this tick.
func (h *hub) villagerPlayStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityVillager || !m.baby || m.sleeping {
		return false
	}
	if villagerSegment(h.dayTime.Load()) == vsSleep {
		return false // Schedule.VILLAGER_BABY rests at night like everyone else
	}
	kids := h.visibleBabies(m)
	if len(kids) == 0 {
		m.playMate = 0
		return false // nobody to play with: the ordinary goals take over
	}
	// One roll in ten, and otherwise carry on with whatever it is doing.
	if h.rng.Intn(playDecideOdds) == 0 {
		h.pickPlay(m, kids)
	}
	switch {
	case m.playMate != 0:
		o := h.mobs[m.playMate]
		if o == nil || o.dying > 0 || math.Hypot(o.x-m.x, o.z-m.z) <= playReach {
			m.playMate = 0 // caught it, or lost it
			return false
		}
		m.vx, m.vz = h.pathSteer(m, o.x, o.z)
	case m.playFlee:
		if math.Hypot(m.playX-m.x, m.playZ-m.z) <= playFleeArrived {
			m.playFlee = false
			return false
		}
		m.vx, m.vz = h.pathSteer(m, m.playX, m.playZ)
	default:
		return false
	}
	m.vx, m.vz = m.vx*playSpeed, m.vz*playSpeed
	return true
}

// pickPlay is the body of PlayTagWithOtherKids: run away if somebody is after
// me, otherwise join the chase already in progress, otherwise chase anyone.
func (h *hub) pickPlay(m *mob, kids []*mob) {
	for _, k := range kids {
		if k.playMate == m.eid { // isFriendChasingMe
			m.playMate = 0
			m.playFlee = true
			ang := h.rng.Float64() * 2 * math.Pi
			r := 4 + h.rng.Float64()*(playFleeXZ-4)
			m.playX, m.playZ = m.x+math.Cos(ang)*r, m.z+math.Sin(ang)*r
			return
		}
	}
	// findSomeoneBeingChased: whoever already has between one and five after
	// them, fewest first.
	chasers := map[int32]int{}
	for _, k := range kids {
		if k.playMate != 0 {
			chasers[k.playMate]++
		}
	}
	best, bestN := int32(0), playMaxChasers+1
	for eid, n := range chasers {
		if eid == m.eid || n > playMaxChasers {
			continue
		}
		if n < bestN || (n == bestN && eid < best) { // stable pick
			best, bestN = eid, n
		}
	}
	if best != 0 {
		m.playMate, m.playFlee = best, false
		return
	}
	m.playMate, m.playFlee = kids[h.rng.Intn(len(kids))].eid, false
}

// visibleBabies is the VISIBLE_VILLAGER_BABIES memory: the other baby
// villagers within sight.
func (h *hub) visibleBabies(m *mob) []*mob {
	var out []*mob
	for _, o := range h.mobs {
		if o == m || o.etype != entityVillager || !o.baby || o.dim != m.dim || o.dying > 0 {
			continue
		}
		if math.Abs(o.x-m.x) <= playSeeRange && math.Abs(o.y-m.y) <= playSeeRange &&
			math.Abs(o.z-m.z) <= playSeeRange {
			out = append(out, o)
		}
	}
	return out
}
