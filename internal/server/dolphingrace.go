package server

// Dolphins swim with you (DolphinSwimWithPlayerGoal): a dolphin that sees a
// swimming player within ten blocks keeps them company — racing to within
// two and a half blocks at four times its pace — and gives them Dolphin's
// Grace for five seconds, renewed one tick in six while they keep
// swimming, until they stop or draw off past sixteen.

const (
	dolphinSwimRange  = 10.0 // SWIM_WITH_PLAYER_TARGETING
	dolphinSwimGiveUp = 16.0 // canContinueToUse: within 256 sq
	dolphinSwimNearSq = 6.25
	dolphinSwimSpeed  = 4.0
	dolphinGraceSecs  = 5 // DOLPHINS_GRACE 100 ticks
	dolphinGraceRenew = 6 // nextInt(6) == 0 a tick
)

// playerSwimming is Player.isSwimming: sprinting through water.
func (h *hub) playerSwimming(t *tracked) bool {
	return t.sprinting && !t.dead && h.inWater(t.dim, t.x, t.y, t.z)
}

// dolphinSwimWithPlayer runs each mob update. Returns whether it holds
// the dolphin.
func (h *hub) dolphinSwimWithPlayer(players map[int32]*tracked, m *mob) bool {
	t := players[m.dolphinSwimmer]
	if t != nil && (t.dim != m.dim || !h.playerSwimming(t) || dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) >= dolphinSwimGiveUp*dolphinSwimGiveUp) {
		t, m.dolphinSwimmer = nil, 0 // stop()
	}
	if t == nil {
		bestD2 := dolphinSwimRange * dolphinSwimRange
		for _, o := range players {
			if o.dim != m.dim || o.gamemode == gmSpectator || !h.playerSwimming(o) {
				continue
			}
			if d2 := dist3sq(o.x, o.y, o.z, m.x, m.y, m.z); d2 < bestD2 {
				t, bestD2 = o, d2
			}
		}
		if t == nil {
			return false
		}
		m.dolphinSwimmer = t.p.eid
		h.applyEffect(players, t, effDolphinsGrace, 0, dolphinGraceSecs) // start()
	}
	if dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) < dolphinSwimNearSq {
		m.vx, m.vz = 0, 0
	} else {
		h.steerTo(m, t.x, t.z, dolphinSwimSpeed)
	}
	m.rest = 0
	for i := 0; i < mobMoveInterval; i++ {
		if h.rng.Intn(dolphinGraceRenew) == 0 {
			h.applyEffect(players, t, effDolphinsGrace, 0, dolphinGraceSecs)
		}
	}
	return true
}
