package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Rabbits raid gardens (Rabbit.RaidGardenGoal): a hungry rabbit looks for a
// grown carrot on farmland within sixteen blocks, hops to it and takes a
// bite — one growth stage off, or the whole plant when it is at its first —
// then is full for a while. moreCarrotTicks counts down by a random 0–2 a
// tick and eating sets it to 40; mobGriefing gates the whole thing.

const (
	rabbitRaidRange = 16  // MoveToBlockGoal search range
	rabbitRaidSpeed = 0.7 // …speed modifier
	rabbitFullTicks = 40  // moreCarrotTicks after a bite
	rabbitRaidRest  = 10  // nextStartTick after a bite
	rabbitRaidReach = 1.0 // isReachedTarget: within a block of the farmland
)

var carrotLo, carrotHi = worldgen.BlockRange("carrots") // age 0..7

func isCarrot(s uint32) bool { return s >= carrotLo && s <= carrotHi }

// rabbitStep runs the raid: returns whether the goal holds the rabbit.
func (h *hub) rabbitStep(players map[int32]*tracked, m *mob) bool {
	if m.carrotTicks > 0 {
		m.carrotTicks -= h.rng.Intn(3) + h.rng.Intn(3) // two ticks' worth of nextInt(3)
		if m.carrotTicks < 0 {
			m.carrotTicks = 0
		}
	}
	if m.raidRest > 0 {
		m.raidRest -= mobMoveInterval
		return false
	}
	if m.baby || !h.rules.MobGriefing || m.carrotTicks > 0 {
		m.raidTarget = blockPos{}
		return false
	}
	w := h.worldFor(m.dim)
	if m.raidTarget == (blockPos{}) {
		fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
		best := math.MaxFloat64
		for dy := -1; dy <= 1; dy++ {
			for dx := -rabbitRaidRange; dx <= rabbitRaidRange; dx++ {
				for dz := -rabbitRaidRange; dz <= rabbitRaidRange; dz++ {
					x, y, z := fx+dx, fy+dy, fz+dz
					if !isFarmland(w.At(x, y, z)) || w.At(x, y+1, z) != carrotHi {
						continue
					}
					if d := float64(dx*dx + dy*dy + dz*dz); d < best {
						best, m.raidTarget = d, blockPos{x, y, z}
					}
				}
			}
		}
		if m.raidTarget == (blockPos{}) {
			m.raidRest = rabbitRaidRest * 20 // nothing to raid: look again later
			return false
		}
	}
	tgt := m.raidTarget
	if !isFarmland(w.At(tgt.x, tgt.y, tgt.z)) || !isCarrot(w.At(tgt.x, tgt.y+1, tgt.z)) {
		m.raidTarget = blockPos{}
		return false
	}
	tx, tz := float64(tgt.x)+0.5, float64(tgt.z)+0.5
	dx, dz := tx-m.x, tz-m.z
	if d := math.Hypot(dx, dz); d > rabbitRaidReach {
		sp := m.moveSpeed() * rabbitRaidSpeed
		m.vx, m.vz = dx/d*sp, dz/d*sp
		m.rest = 0
		return true
	}
	// A bite: one stage off, or the plant when it is at its first.
	crop := w.At(tgt.x, tgt.y+1, tgt.z)
	if crop == carrotLo {
		h.breakBlockDrop(players, m.dim, blockPos{tgt.x, tgt.y + 1, tgt.z}, crop)
	} else {
		h.setBlockLive(players, m.dim, tgt.x, tgt.y+1, tgt.z, crop-1)
	}
	m.carrotTicks, m.raidRest, m.raidTarget = rabbitFullTicks, rabbitRaidRest, blockPos{}
	m.vx, m.vz = 0, 0
	return true
}
