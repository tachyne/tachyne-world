package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Villager locomotion: goal-directed roaming with door use. Regular villagers
// used to amble on the pure-random wanderBehavior, which never purposefully
// threaded a one-wide doorway or climbed the step outside it — so a villager
// that wandered into a house effectively stayed trapped until a player dug it
// out. villagerBehavior instead paths (A*) toward a roam goal near its home,
// and the door pass below opens the wooden door in its way (and shuts it behind).

const (
	villagerRoam   = 10  // roam radius around home (blocks) for the wander goal
	doorCloseGrace = 100 // ticks a villager-opened door stays open after they clear it (~5s)
	doorReach      = 2.5 // a villager this close (per-axis) keeps "its" door open
)

// Villager day segments (dayTime % 24000): a coarse version of vanilla's
// schedule — work the job site by day, gather at the bell midday, sleep at
// night, roam otherwise. Times are in Minecraft ticks (0 = sunrise).
const (
	vsRoam   = iota // dawn + afternoon: amble near home
	vsWork          // morning: stand at the profession workstation
	vsGather        // midday: congregate at the village bell
	vsSleep         // night: return to the bed and lie down
)

func villagerSegment(dayTime uint64) int {
	switch t := dayTime % dayLengthTicks; {
	case t >= sleepStart && t <= sleepEnd: // the night sleep window (wakes at sunrise)
		return vsSleep
	case t >= 2000 && t < 9000:
		return vsWork
	case t >= 9000 && t < 11000:
		return vsGather
	default:
		return vsRoam
	}
}

// villagerBehavior steers a villager toward the destination its daily schedule
// dictates (workstation / bell / bed / roam), routing through — and opening —
// closed wooden doors via the door-aware pather.
type villagerBehavior struct{}

func (villagerBehavior) name() string { return "villager" }
func (villagerBehavior) steer(h *hub, m *mob) (float64, float64) {
	if m.sleeping {
		return 0, 0 // in bed — held still by villagerSleep, but be defensive
	}
	homeX, homeZ := float64(m.home.x), float64(m.home.z)
	switch villagerSegment(h.dayTime.Load()) {
	case vsWork:
		if m.work != (blockPos{}) {
			return h.pathSteer(m, float64(m.work.x)+0.5, float64(m.work.z)+0.5)
		}
	case vsGather:
		if m.meet != (blockPos{}) {
			return h.pathSteer(m, float64(m.meet.x)+0.5, float64(m.meet.z)+0.5)
		}
	case vsSleep:
		if m.bed != (blockPos{}) {
			return h.pathSteer(m, float64(m.bed.x)+0.5, float64(m.bed.z)+0.5)
		}
	}
	// Roam: re-pick a nearby stroll target on a timer, or head home if adrift.
	now := h.tick.Load()
	switch {
	case math.Hypot(homeX-m.x, homeZ-m.z) > villagerRoam+8:
		m.roamX, m.roamZ, m.roamAt = homeX, homeZ, now+60
	case now >= m.roamAt:
		ang := h.rng.Float64() * 2 * math.Pi
		r := 2 + h.rng.Float64()*villagerRoam
		m.roamX, m.roamZ = homeX+math.Cos(ang)*r, homeZ+math.Sin(ang)*r
		m.roamAt = now + uint64(80+h.rng.Intn(160)) // 4-12 s per leg
	}
	return h.pathSteer(m, m.roamX, m.roamZ)
}

// villagerSleep lies a villager down in its bed once it's night and the villager
// has reached the bed, and stands it back up at first light. Returns true while
// the villager is asleep so updateMobs holds it still. Best-effort on the visual
// pose: even if the client doesn't render the lying pose, the villager is parked
// on its bed and motionless, which is the schedule-correct outcome.
func (h *hub) villagerSleep(players map[int32]*tracked, m *mob) bool {
	night := villagerSegment(h.dayTime.Load()) == vsSleep
	if m.sleeping {
		if night {
			return true // still asleep
		}
		m.sleeping = false // dawn — wake up
		h.restockOffers(m) // a night's rest restocks the day's trades
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(wakeMetadata(m.eid)))
		return false
	}
	if !night {
		return false
	}
	// Near enough to the bed to lie down?
	bx, bz := float64(m.bed.x)+0.5, float64(m.bed.z)+0.5
	if math.Hypot(bx-m.x, bz-m.z) > 1.6 {
		return false // still walking home to bed
	}
	m.sleeping = true
	m.x, m.y, m.z = bx, float64(m.bed.y)+bedSurface, bz // snap onto the bed
	m.sx, m.sy, m.sz = m.x, m.y, m.z
	h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, true))
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sleepMetadata(m.eid, m.bed)))
	return true
}

// villagerDoors opens any closed wooden door in the cells around a villager and
// records it so updateOpenDoors shuts it once the villager has moved on. Called
// from updateMobs BEFORE the mob steps, so the door is already open when the
// walk collision test runs this tick and the villager passes through cleanly.
func (h *hub) villagerDoors(players map[int32]*tracked, m *mob) {
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for dy := -1; dy <= 1; dy++ {
				x, y, z := fx+dx, fy+dy, fz+dz
				s := h.world.Block(x, y, z)
				if !worldgen.IsClosedDoor(s) || !worldgen.IsWoodenDoor(s) {
					continue
				}
				// Anchor on the lower half so a door is one entry in openDoors.
				info, _ := worldgen.InfoForState(s)
				if worldgen.GetProperty(info, s, "half") != "lower" {
					continue
				}
				if h.setDoorOpen(players, blockPos{x, y, z}, s, true) {
					h.openDoors[blockPos{x, y, z}] = h.tick.Load()
				}
			}
		}
	}
}

// updateOpenDoors shuts villager-opened doors once no villager is near and the
// grace window has elapsed (vanilla villagers close doors behind them). Runs on
// the hub goroutine; deleting while ranging a map is safe in Go.
func (h *hub) updateOpenDoors(players map[int32]*tracked) {
	if len(h.openDoors) == 0 {
		return
	}
	now := h.tick.Load()
	for pos, opened := range h.openDoors {
		if now-opened < doorCloseGrace {
			continue
		}
		if h.villagerNear(pos, doorReach) {
			h.openDoors[pos] = now // still passing through — hold it open
			continue
		}
		s := h.world.Block(pos.x, pos.y, pos.z)
		if worldgen.IsWoodenDoor(s) && boolProp(s, "open") {
			h.setDoorOpen(players, pos, s, false)
		}
		delete(h.openDoors, pos)
	}
}

// villagerNear reports whether a live door-using mob stands within r (per-axis)
// of a door column — so a door isn't slammed on a villager mid-threshold.
func (h *hub) villagerNear(pos blockPos, r float64) bool {
	cx, cz := float64(pos.x)+0.5, float64(pos.z)+0.5
	for _, m := range h.mobs {
		if !m.usesDoors || m.dying > 0 {
			continue
		}
		if math.Abs(m.x-cx) <= r && math.Abs(m.z-cz) <= r && math.Abs(m.y-float64(pos.y)) <= 2 {
			return true
		}
	}
	return false
}

// setDoorOpen flips a door's open state (both halves) and broadcasts it, playing
// the wooden-door sound. Returns whether it actually changed (false = already in
// the requested state, so callers don't re-record a no-op). Overworld-only, like
// the rest of the hub's block simulation.
func (h *hub) setDoorOpen(players map[int32]*tracked, pos blockPos, state uint32, open bool) bool {
	info, ok := worldgen.InfoForState(state)
	if !ok || !info.HasProperty("open") || boolProp(state, "open") == open {
		return false
	}
	h.setBlock(players, pos, setBoolProp(state, "open", open))
	// The paired half shares the open state (a door is two blocks).
	oy := pos.y + 1
	if worldgen.GetProperty(info, state, "half") == "upper" {
		oy = pos.y - 1
	}
	other := h.world.Block(pos.x, oy, pos.z)
	if oi, ok := worldgen.InfoForState(other); ok && oi.HasProperty("open") {
		h.setBlock(players, blockPos{pos.x, oy, pos.z}, setBoolProp(other, "open", open))
	}
	snd := "minecraft:block.wooden_door.open"
	if !open {
		snd = "minecraft:block.wooden_door.close"
	}
	h.playSound(players, snd, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.9, 1)
	return true
}
