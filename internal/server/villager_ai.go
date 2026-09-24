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
	if vx, vz, fleeing := h.villagerFlee(m); fleeing {
		return vx, vz // a zombie within eight blocks: run
	}
	if vx, vz, gifting := h.villagerGiftSteer(h.playersRef, m); gifting {
		return vx, vz // a Hero of the Village nearby: bring it a gift
	}
	if h.tick.Load() < m.hideUntil && m.bed != (blockPos{}) { // a rung bell: hide at the bed
		return h.pathSteerTo(m, m.bed, poiValidRange[poiHome])
	}
	switch villagerSegment(h.dayTime.Load()) {
	case vsWork:
		if m.work != (blockPos{}) {
			// Vanilla WorkAtPoi: standing at the job-site POI restocks used
			// offers (gated by allowedToRestock to ≤2/day, ≥2400 ticks apart) —
			// this is the day's second restock a heavily-traded villager gets.
			if dist3(m.x, m.y, m.z,
				float64(m.work.x)+0.5, float64(m.work.y), float64(m.work.z)+0.5) < 2 &&
				h.shouldRestock(m) {
				h.restockOffers(m)
			}
			vx, vz := h.pathSteerTo(m, m.work, poiValidRange[poiJob])
			h.noteWalkToPoi(m, poiJob, m.work)
			return vx, vz
		}
	case vsGather:
		if m.meet != (blockPos{}) {
			vx, vz := h.pathSteerTo(m, m.meet, poiValidRange[poiMeet])
			h.noteWalkToPoi(m, poiMeet, m.meet)
			return vx, vz
		}
	case vsSleep:
		if m.bed != (blockPos{}) {
			vx, vz := h.pathSteerTo(m, m.bed, poiValidRange[poiHome])
			h.noteWalkToPoi(m, poiHome, m.bed)
			return vx, vz
		}
	}
	// Roam: vanilla's VillageBoundRandomStroll (and GoToClosestVillage for a
	// villager with no bed at rest), re-picked on a timer. A villager with
	// no home used to head for its zero home — the world origin — and leave.
	now := h.tick.Load()
	if now >= m.roamAt {
		m.roamX, m.roamZ = h.villageStroll(m)
		m.roamAt = now + uint64(80+h.rng.Intn(160)) // 4-12 s per leg
	}
	return h.pathSteer(m, m.roamX, m.roamZ)
}

// Villages are where villagers' claimed POIs are (PoiManager.sectionsToVillage):
// a 16-block section holding an OCCUPIED village POI — a claimed bed, job
// site or meeting bell — is a village centre; every other section is its
// distance in sections (a 3x3x3 step) from the nearest centre, up to 7.
// ServerLevel.isVillage is a distance of at most one.
const maxVillageDistance = 7

// villageCentres returns the sections holding a claimed village POI.
func (h *hub) villageCentres(dim int) map[[3]int]bool {
	out := map[[3]int]bool{}
	for _, o := range h.mobs {
		if o.etype != entityVillager || o.dim != dim || o.dying > 0 {
			continue
		}
		for _, p := range [3]blockPos{o.bed, o.work, o.meet} {
			if p != (blockPos{}) {
				out[[3]int{p.x >> 4, p.y >> 4, p.z >> 4}] = true
			}
		}
	}
	return out
}

// sectionsToVillage is the section's distance to the nearest centre.
func sectionsToVillage(centres map[[3]int]bool, s [3]int) int {
	best := maxVillageDistance
	for c := range centres {
		d := max(abs(c[0]-s[0]), abs(c[1]-s[1]), abs(c[2]-s[2]))
		if d < best {
			best = d
		}
	}
	return best
}

// villageStroll is VillageBoundRandomStroll's walk target: inside a village a
// random spot within ten blocks; outside one, a spot up to ten blocks toward
// the section within two that is closest to a village
// (BehaviorUtils.findSectionClosestToVillage: only a section strictly closer
// than this one); with no village about, a
// random spot where it stands.
func (h *hub) villageStroll(m *mob) (float64, float64) {
	here := [3]int{floorInt(m.x) >> 4, floorInt(m.y) >> 4, floorInt(m.z) >> 4}
	centres := h.villageCentres(m.dim)
	d := sectionsToVillage(centres, here)
	random := func() (float64, float64) {
		return m.x + float64(h.rng.Intn(2*villagerRoam+1)-villagerRoam), m.z + float64(h.rng.Intn(2*villagerRoam+1)-villagerRoam)
	}
	if d <= 1 {
		return random()
	}
	best, bestD := here, d
	// SectionPos.cube's order (x fastest, then y, then z): the first of
	// several equally close sections wins, as Stream.min keeps it.
	for dz := -2; dz <= 2; dz++ {
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				s := [3]int{here[0] + dx, here[1] + dy, here[2] + dz}
				if sd := sectionsToVillage(centres, s); sd < bestD {
					best, bestD = s, sd
				}
			}
		}
	}
	if best == here {
		return random()
	}
	// DefaultRandomPos.getPosTowards (RandomPos.generateRandomDirectionWithin
	// Radians): a heading within a quarter turn either side of the one to the
	// section's centre, a distance of sqrt(u)·√2 times ten, kept only inside
	// the ten-block box; ten tries.
	tx, tz := float64(best[0]*16+8)+0.5, float64(best[2]*16+8)+0.5 // Vec3.atBottomCenterOf
	for try := 0; try < 10; try++ {
		head := math.Atan2(tz-m.z, tx-m.x) + (2*h.rng.Float64()-1)*math.Pi/2
		r := math.Sqrt(h.rng.Float64()) * villagerRoam * math.Sqrt2
		dx, dz := math.Cos(head)*r, math.Sin(head)*r
		if math.Abs(dx) <= villagerRoam && math.Abs(dz) <= villagerRoam {
			return m.x + math.Floor(dx), m.z + math.Floor(dz)
		}
	}
	return m.x, m.z
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
		// A night's rest restocks the day's trades. The daily counter is NOT
		// zeroed here any more: vanilla rolls it over in shouldRestock, off the
		// day count, so a villager with no bed to wake from still gets its day.
		h.restockOffers(m)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(wakeMetadata(m.eid)))
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
	m.lastSlept = h.tick.Load() + 1 // LAST_SLEPT: what the iron-golem quorum asks for
	// Lie down on the HEAD half, wherever worldgen recorded the bed — the same
	// anchor rule players follow, and for the same rendering reason.
	head, ok := h.bedHead(m.dim, m.bed)
	if !ok {
		head = m.bed
	}
	m.x, m.y, m.z = float64(head.x)+0.5, float64(head.y)+bedSleepY, float64(head.z)+0.5
	m.sx, m.sy, m.sz = m.x, m.y, m.z
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(sleepMetadata(m.eid, head)))
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
	h.setBlockAt(players, dimOverworld, pos, setBoolProp(state, "open", open))
	// The paired half shares the open state (a door is two blocks).
	oy := pos.y + 1
	if worldgen.GetProperty(info, state, "half") == "upper" {
		oy = pos.y - 1
	}
	other := h.world.Block(pos.x, oy, pos.z)
	if oi, ok := worldgen.InfoForState(other); ok && oi.HasProperty("open") {
		h.setBlockAt(players, dimOverworld, blockPos{pos.x, oy, pos.z}, setBoolProp(other, "open", open))
	}
	snd := "minecraft:block.wooden_door.open"
	if !open {
		snd = "minecraft:block.wooden_door.close"
	}
	h.playSoundDim(players, dimOverworld, snd, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.9, 1)
	return true
}
