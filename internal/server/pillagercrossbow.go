package server

import "math"

// The pillager's crossbow (RangedCrossbowAttackGoal + Pillager's charging
// flag): with a target within eight blocks it stops, draws the crossbow
// for twenty-five ticks (the charging flag the client animates from),
// holds the loaded bolt for twenty to forty ticks, fires, and starts
// again; farther off it closes at walking pace, at half pace while
// loaded.

const (
	metaIndexPillagerCharging = 17 // Pillager IS_CHARGING_CROSSBOW (bool; Raider's celebrating is 16)
	crossbowChargeTicks       = 25 // CrossbowItem.getChargeDuration without Quick Charge
	crossbowAimMin            = 20 // attackDelay 20 + nextInt(20)
	crossbowAimRandom         = 20
	crossbowRadius            = 8.0 // RangedCrossbowAttackGoal(…, 8.0f)
)

// CrossbowState.
const (
	cbUncharged = iota
	cbCharging
	cbCharged
	cbReady
)

func pillagerChargingMeta(m *mob) []byte {
	return boolMeta(m.eid, metaIndexPillagerCharging, m.cbState == cbCharging)
}

func (h *hub) setCrossbowState(players map[int32]*tracked, m *mob, st int8) {
	was := m.cbState == cbCharging
	m.cbState = st
	if now := st == cbCharging; now != was {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(pillagerChargingMeta(m)))
		h.setHandActive(players, m, now) // startUsingItem / releaseUsingItem
	}
}

// pillagerTick runs each mob update from the hostile switch.
func (h *hub) pillagerTick(players map[int32]*tracked, m *mob) {
	t := h.nearestHuntable(players, m.dim, m.x, m.z, m.followRange())
	if t == nil {
		if m.cbState != cbUncharged {
			h.setCrossbowState(players, m, cbUncharged)
		}
		return
	}
	d2 := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z)
	closing := d2 > crossbowRadius*crossbowRadius && m.cbTicks == 0
	if !closing {
		m.vx, m.vz = 0, 0 // within range: it stands to shoot
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
	switch m.cbState {
	case cbUncharged:
		if !closing {
			h.setCrossbowState(players, m, cbCharging)
			m.cbTicks = 0
		}
	case cbCharging:
		m.cbTicks += mobMoveInterval
		if m.cbTicks >= crossbowChargeTicks {
			h.setCrossbowState(players, m, cbCharged)
			m.cbTicks = crossbowAimMin + h.rng.Intn(crossbowAimRandom)
		}
	case cbCharged:
		m.cbTicks -= mobMoveInterval
		if m.cbTicks <= 0 {
			m.cbTicks = 0
			h.setCrossbowState(players, m, cbReady)
		}
	case cbReady:
		if !h.seeTimeTick(m, t, true) {
			return // READY_TO_ATTACK waits for line of sight
		}
		h.spawnArrow(players, m, t) // performCrossbowAttack at 1.6
		h.playSoundDim(players, m.dim, "minecraft:item.crossbow.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
		h.setCrossbowState(players, m, cbUncharged)
	}
}
