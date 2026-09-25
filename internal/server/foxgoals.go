package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The fox's quieter goals (vanilla Fox): SeekShelterGoal sends it out of
// the open sky by day and out of a thunderstorm at once; by night, near a
// village, FoxStrollThroughVillageGoal walks it in toward the houses;
// FoxEatBerriesGoal takes it to a ripe sweet berry bush or a cave vine in
// fruit, where after two seconds it picks them, keeping one in its mouth;
// and with nothing better to do PerchAndSearchGoal sits it down to look
// about, now this way, now that.

const (
	foxShelterSpeed    = 1.25 // SeekShelterGoal(1.25)
	foxShelterEvery    = 100  // …and between later ones
	foxShelterReach    = 1.0  // navigation done
	foxShelterTries    = 10   // FleeSunGoal.getHidePos attempts
	foxBerrySpeed      = 1.2  // FoxEatBerriesGoal(1.2, 12, 1)
	foxBerryRange      = 12
	foxBerryVert       = 1
	foxBerryAccept     = 2.0  // acceptedDistance
	foxBerryWaitTicks  = 40   // WAIT_TICKS beside the bush before it picks
	foxBerryGiveUp     = 1200 // MoveToBlockGoal: tryTicks past this and it gives up
	foxBerryStayMin    = 1200 // maxStayTicks: 1200 + nextInt(nextInt(1200) + 1200)
	foxVillageOdds     = 100  // StrollThroughVillageGoal(200): reducedTickDelay(200) canUse calls
	foxVillageNear     = 6    // isCloseToVillage(pos, 6): sections
	foxVillageXZ       = 15   // LandRandomPos.getPos(mob, 15, 7, …)
	foxVillageY        = 7
	foxVillageTries    = 10
	foxVillageLeg      = 10.0 // tick: a leg of ten blocks toward it, once farther than ten
	foxVillageArrive   = 1.0
	foxPerchOdds       = 0.02 // PerchAndSearchGoal.canUse: nextFloat() < 0.02
	foxPerchLookMin    = 80   // resetLook: adjustedTickDelay(80 + nextInt(20))
	foxPerchLookSpread = 20
	foxStrollInterval  = 120 // WaterAvoidingRandomStrollGoal's default: it outranks the perch
)

// foxIdleStep is the fox's goals below the follow-parent: the village walk
// (priority 9), the berries (10), the item search (11) and the perch (13).
func (h *hub) foxIdleStep(players map[int32]*tracked, m *mob) bool {
	if m.foxFlags&foxFlagSleeping != 0 || m.foxPrey != 0 || m.dying > 0 {
		return false
	}
	if h.foxVillageStep(players, m) {
		m.foxPerchLooks = 0
		return true
	}
	if h.foxBerryStep(players, m) {
		m.foxPerchLooks = 0
		return true
	}
	if h.foxPickupStep(players, m) {
		m.foxPerchLooks = 0
		return true
	}
	return h.foxPerchStep(players, m)
}

// foxShelterStep is SeekShelterGoal: awake and after nothing, a fox under
// open sky in a thunderstorm makes for cover at once; otherwise every
// hundred looks, by day, out of a village, it does the same. Cover is a spot the sky cannot see that it would not walk to
// (FleeSunGoal.getHidePos with the fox's own walk value).
func (h *hub) foxShelterStep(players map[int32]*tracked, m *mob) bool {
	if m.foxFlags&foxFlagSleeping != 0 {
		m.hidePos = blockPos{}
		return false
	}
	if m.hidePos != (blockPos{}) { // canContinueToUse: the path is not done
		tx, tz := float64(m.hidePos.x)+0.5, float64(m.hidePos.z)+0.5
		if math.Hypot(tx-m.x, tz-m.z) <= foxShelterReach {
			m.hidePos = blockPos{}
			return false
		}
		h.steerTo(m, tx, tz, foxShelterSpeed)
		return true
	}
	open := h.canSeeSky(m.dim, floorInt(m.x), floorInt(m.y), floorInt(m.z))
	start := false
	switch {
	case h.thundering && open:
		start = true
	case m.foxShelterIn > 0:
		m.foxShelterIn--
		return false
	default:
		m.foxShelterIn = foxShelterEvery
		start = h.isDayTime() && open && !h.isVillageAt(floorInt(m.x), floorInt(m.y), floorInt(m.z))
	}
	if !start {
		return false
	}
	pos, ok := h.foxHidePos(m)
	if !ok {
		return false
	}
	h.foxClearStates(players, m) // SeekShelterGoal.start
	m.hidePos = pos
	h.steerTo(m, float64(pos.x)+0.5, float64(pos.z)+0.5, foxShelterSpeed)
	return true
}

// foxHidePos is FleeSunGoal.getHidePos for a fox: ten tries within ten
// blocks across and three up or down for a standable spot the sky cannot
// see and whose walk value is negative (no grass underfoot, dimmer than
// light twelve).
func (h *hub) foxHidePos(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for i := 0; i < foxShelterTries; i++ {
		x, y, z := bx+h.rng.Intn(20)-10, by+h.rng.Intn(6)-3, bz+h.rng.Intn(20)-10
		if h.canSeeSky(m.dim, x, y, z) || h.foxWalkValue(m.dim, x, y, z) >= 0 {
			continue
		}
		if w.MobFeetFrom(x, z, y) != y {
			continue // not a floor at that height
		}
		return blockPos{x, y, z}, true
	}
	return blockPos{}, false
}

// foxCanRoam is FoxStrollThroughVillageGoal.canFoxMove: awake, not sat,
// not defending anyone and after nothing.
func foxCanRoam(m *mob) bool {
	return m.foxFlags&(foxFlagSleeping|foxFlagSitting|foxFlagDefending) == 0 && m.foxPrey == 0
}

// foxVillageStep is FoxStrollThroughVillageGoal: at night, one look in a
// hundred, a fox within six sections of a village picks a spot within
// fifteen blocks nearest the village and walks a ten-block leg toward it.
func (h *hub) foxVillageStep(players map[int32]*tracked, m *mob) bool {
	if !foxCanRoam(m) || m.dim != dimOverworld {
		m.foxVillage = false
		return false
	}
	if m.foxVillage {
		if math.Hypot(m.foxVillageX-m.x, m.foxVillageZ-m.z) <= foxVillageArrive {
			m.foxVillage = false
			return false
		}
		h.steerTo(m, m.foxVillageX, m.foxVillageZ, 1.0)
		return true
	}
	if h.isDayTime() || h.rng.Intn(foxVillageOdds) != 0 {
		return false
	}
	centres := h.villageCentres(m.dim)
	sec := func(x, y, z int) [3]int { return [3]int{x >> 4, y >> 4, z >> 4} }
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	if sectionsToVillage(centres, sec(bx, by, bz)) > foxVillageNear {
		return false
	}
	// LandRandomPos.getPos scored by -sectionsToVillage: the best of the tries.
	w := h.worldFor(m.dim)
	best, bestScore, found := blockPos{}, math.MinInt, false
	for i := 0; i < foxVillageTries; i++ {
		x, z := bx+h.rng.Intn(2*foxVillageXZ+1)-foxVillageXZ, bz+h.rng.Intn(2*foxVillageXZ+1)-foxVillageXZ
		y := w.MobFeetFrom(x, z, by)
		if abs(y-by) > foxVillageY || !w.Walkable(x, z) {
			continue
		}
		if s := -sectionsToVillage(centres, sec(x, y, z)); s > bestScore {
			best, bestScore, found = blockPos{x, y, z}, s, true
		}
	}
	if !found {
		return false
	}
	wx, wz := float64(best.x)+0.5, float64(best.z)+0.5
	d := math.Hypot(wx-m.x, wz-m.z)
	if d <= foxVillageLeg {
		return false // already within ten: the goal's tick has nothing to do
	}
	// tick: aim at the point sixty percent of the way there, ten blocks out.
	ax, az := wx+(m.x-wx)*0.4, wz+(m.z-wz)*0.4
	dx, dz := ax-m.x, az-m.z
	n := math.Hypot(dx, dz)
	if n < 1e-6 {
		return false
	}
	h.foxClearStates(players, m) // FoxStrollThroughVillageGoal.start
	m.foxVillage = true
	m.foxVillageX, m.foxVillageZ = m.x+dx/n*foxVillageLeg, m.z+dz/n*foxVillageLeg
	h.steerTo(m, m.foxVillageX, m.foxVillageZ, 1.0)
	return true
}

// foxBerryTarget is FoxEatBerriesGoal.isValidTarget: a sweet berry bush of
// age two or more, or a cave vine bearing glow berries.
func foxBerryTarget(s uint32) bool {
	if isBerryBush(s) {
		return s-berryBase >= 2
	}
	return isCaveVine(s) && caveVineHasBerries(s)
}

// caveVineHasBerries is CaveVines.hasGlowBerries.
func caveVineHasBerries(s uint32) bool {
	info, ok := worldgen.InfoForState(s)
	return ok && worldgen.GetProperty(info, s, "berries") == "true"
}

// foxFindBerries is MoveToBlockGoal.findNearestBlock over twelve blocks and
// one up or down, nearest ring first.
func (h *hub) foxFindBerries(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	mx, my, mz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for y := 0; y <= foxBerryVert; y = nextSpiral(y) {
		for r := 0; r < foxBerryRange; r++ {
			for x := 0; x <= r; x = nextSpiral(x) {
				z0 := 0
				if x < r && x > -r {
					z0 = r
				}
				for z := z0; z <= r; z = nextSpiral(z) {
					if p := (blockPos{mx + x, my + y - 1, mz + z}); foxBerryTarget(w.At(p.x, p.y, p.z)) {
						return p, true
					}
				}
			}
		}
	}
	return blockPos{}, false
}

// foxBerryStep is FoxEatBerriesGoal: a search every two to four hundred
// ticks; then to within two blocks of the spot above the berries at 1.2,
// sniffing as it goes; forty ticks beside them and it picks.
func (h *hub) foxBerryStep(players map[int32]*tracked, m *mob) bool {
	w := h.worldFor(m.dim)
	if m.foxBerry == (blockPos{}) {
		if m.foxBerryNext > 0 {
			m.foxBerryNext--
			return false
		}
		m.foxBerryNext = (200 + h.rng.Intn(200)) / mobMoveInterval // reducedTickDelay: canUse runs every other tick
		p, ok := h.foxFindBerries(m)
		if !ok {
			return false
		}
		m.foxBerry, m.foxBerryTry, m.foxBerryWait = p, 0, 0
		m.foxBerryStay = foxBerryStayMin + h.rng.Intn(h.rng.Intn(1200)+1200)
		h.foxSetFlags(players, m, m.foxFlags&^foxFlagSitting) // start: setSitting(false)
	}
	p := m.foxBerry
	if !foxBerryTarget(w.At(p.x, p.y, p.z)) || m.foxBerryTry > foxBerryGiveUp || m.foxBerryTry < -m.foxBerryStay {
		m.foxBerry = blockPos{}
		return false
	}
	tx, ty, tz := float64(p.x)+0.5, float64(p.y+1)+0.5, float64(p.z)+0.5
	if dist3sq(tx, ty, tz, m.x, m.y, m.z) >= foxBerryAccept*foxBerryAccept {
		m.foxBerryTry += mobMoveInterval
		for i := 0; i < mobMoveInterval; i++ {
			if h.rng.Float32() < 0.05 {
				h.playSoundDim(players, m.dim, "minecraft:entity.fox.sniff", sndNeutral, m.x, m.y, m.z, 1, 1)
				break
			}
		}
		h.steerTo(m, tx, tz, foxBerrySpeed)
		return true
	}
	m.foxBerryTry -= mobMoveInterval
	m.vx, m.vz = 0, 0
	if m.foxBerryWait < foxBerryWaitTicks {
		m.foxBerryWait += mobMoveInterval
		return true
	}
	h.foxPickBerries(players, m, p)
	m.foxBerry = blockPos{}
	return true
}

// foxPickBerries is onReachedTarget: with mob griefing on, a bush gives one
// or two berries (one more when full-grown), the first into an empty mouth
// and the rest onto the ground, and drops back to age one; a vine gives its
// glow berries up as a player's pick would.
func (h *hub) foxPickBerries(players map[int32]*tracked, m *mob, p blockPos) {
	if !h.rules.MobGriefing {
		return
	}
	w := h.worldFor(m.dim)
	s := w.At(p.x, p.y, p.z)
	fx, fy, fz := float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5
	switch {
	case isBerryBush(s):
		age := int(s - berryBase)
		n := 1 + h.rng.Intn(2)
		if age == 3 {
			n++
		}
		if m.held == 0 {
			h.foxHold(players, m, itemSweetBerries)
			n--
		}
		if n > 0 {
			h.spawnItemIn(players, m.dim, itemSweetBerries, n, fx, fy, fz)
		}
		h.playSoundDim(players, m.dim, "minecraft:block.sweet_berry_bush.pick_berries", sndNeutral, m.x, m.y, m.z, 1, 1)
		h.setBlockAt(players, m.dim, p, berryBase+1)
		h.vib(m.dim, freqBlockChange, p.x, p.y, p.z, m.eid)
	case isCaveVine(s) && caveVineHasBerries(s):
		info, _ := worldgen.InfoForState(s)
		h.spawnItemIn(players, m.dim, itemGlowBerries, 1, fx, fy, fz) // harvest/cave_vine
		h.playSoundDim(players, m.dim, "minecraft:block.cave_vines.pick_berries", sndBlock, fx, fy, fz, 1, 0.8+h.rng.Float32()*0.4)
		h.setBlockAt(players, m.dim, p, worldgen.SetProperty(info, s, "berries", "false"))
		h.vib(m.dim, freqBlockChange, p.x, p.y, p.z, m.eid)
	}
}

// foxPerchStep is PerchAndSearchGoal: a fox standing idle, unhurt, with
// nothing about to alarm it, now and then sits down and looks around —
// two to four looks of four to five seconds each, a random way every time
// — until the looks run out or a stroll gets it up again.
func (h *hub) foxPerchStep(players map[int32]*tracked, m *mob) bool {
	if m.foxPerchLooks > 0 && m.foxFlags&foxFlagSitting != 0 {
		// The stroll (priority 11) outranks the perch and shares its MOVE flag.
		stroll := false
		for i := 0; i < mobMoveInterval; i++ {
			if h.rng.Intn(foxStrollInterval/mobMoveInterval) == 0 {
				stroll = true
			}
		}
		if !stroll {
			if m.foxPerchLook -= mobMoveInterval; m.foxPerchLook <= 0 {
				m.foxPerchLooks--
				h.foxPerchResetLook(m)
			}
			if m.foxPerchLooks > 0 {
				m.vx, m.vz = 0, 0
				m.headYaw = yawToward(0, 0, m.foxPerchDX, m.foxPerchDZ)
				m.foxHeld = true
				return true
			}
		}
		m.foxPerchLooks = 0
		h.foxSetFlags(players, m, m.foxFlags&^foxFlagSitting) // stop: up again
		return false
	}
	if m.foxFlags&foxFlagSitting != 0 {
		h.foxSetFlags(players, m, m.foxFlags&^foxFlagSitting)
	}
	// canUse: nothing hurt it lately, the roll, awake, no target, standing
	// still (the navigation is done), not alarmed, not pouncing or crouched.
	if m.panic > 0 || m.hurtByPlayerTil > h.tick.Load() || m.rest <= 0 || m.reroute > 0 ||
		m.foxFlags&(foxFlagSleeping|foxFlagPouncing|foxFlagCrouching) != 0 || m.foxPrey != 0 {
		return false
	}
	hit := false
	for i := 0; i < mobMoveInterval; i++ {
		if h.rng.Float64() < foxPerchOdds {
			hit = true
		}
	}
	if !hit || h.foxAlertable(players, m) {
		return false
	}
	h.foxPerchResetLook(m)
	m.foxPerchLooks = 2 + h.rng.Intn(3)
	h.foxSetFlags(players, m, m.foxFlags|foxFlagSitting)
	m.vx, m.vz = 0, 0
	m.headYaw = yawToward(0, 0, m.foxPerchDX, m.foxPerchDZ)
	m.foxHeld = true
	return true
}

// foxPerchResetLook is resetLook: a random way to look, for 80 to 99 ticks.
func (h *hub) foxPerchResetLook(m *mob) {
	a := 2 * math.Pi * h.rng.Float64()
	m.foxPerchDX, m.foxPerchDZ = math.Cos(a), math.Sin(a)
	m.foxPerchLook = foxPerchLookMin + h.rng.Intn(foxPerchLookSpread)
}
