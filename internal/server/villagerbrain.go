package server

import (
	"math"
	"sort"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The villager's errands (VillagerGoalPackages): the RunOne choices of its
// IDLE and PLAY activities, the POI strolls of WORK and MEET, and the one
// shared WALK_TARGET that MoveToTargetSink walks it to.
//
//   - IDLE: InteractWith a villager (2) or a cat (1), VillageBoundRandomStroll
//     (1), JumpOnBed (1, children only) or DoNothing 30-60 ticks (1). The
//     breed InteractWith is villagerBreedStep's.
//   - PLAY, with no other child in sight (tag is villagerplay.go's): the same
//     InteractWiths, the stroll, JumpOnBed (2) and DoNothing 20-40 (2).
//   - WORK, within reach of the job site: StrollAroundPoi (2), StrollToPoi
//     (5) and StrollToPoiList to the farmer's farmland (5). WorkAtPoi and the
//     farmer's field work are their own steps (villagerwork.go, farmer.go).
//   - MEET, within reach of the bell: StrollAroundPoi or SocializeAtBell.
//
// A RunOne shuffles its choices by weight (ShufflingList) and starts the
// first that can; a one-shot choice that finds nothing to do still counts as
// the pick. SetWalkTargetFromLookTarget has no look target to follow here
// and never starts.

const (
	vSpeedBase        = 1.0       // speedModifier 0.5: the engine's villager pace
	vSpeedPoiStroll   = 0.4 / 0.5 // VillagerGoalPackages.STROLL_SPEED_MODIFIER
	vSpeedSocialize   = 0.3 / 0.5 // SocializeAtBell.SPEED_MODIFIER
	vInteractRange    = 8         // InteractWith(.., 8, ..)
	vInteractStop     = 2         // …stopDistance
	vSeeChecks        = 8         // line-of-sight tests a sight scan spends at most
	vStrollAroundXZ   = 8         // StrollAroundPoi: LandRandomPos.getPos(body, 8, 6)
	vStrollAroundY    = 6         //
	vStrollAroundWait = 180       // StrollAroundPoi.MIN_TIME_BETWEEN_STROLLS
	vStrollToWait     = 80        // StrollToPoi
	vStrollListWait   = 100       // StrollToPoiList
	vWorkStrollReach  = 4         // StrollAroundPoi(JOB_SITE, 0.4, 4)
	vWorkToReach      = 10        // StrollToPoi(JOB_SITE, 0.4, 1, 10)
	vWorkListReach    = 6         // StrollToPoiList(SECONDARY_JOB_SITE, 0.5, 1, 6, JOB_SITE)
	vMeetStrollReach  = 40        // StrollAroundPoi(MEETING_POINT, 0.4, 40)
	vWorkCloseEnough  = 9         // SetWalkTargetFromBlockMemory(JOB_SITE, .., 9, ..)
	vMeetCloseEnough  = 6         // SetWalkTargetFromBlockMemory(MEETING_POINT, .., 6, ..)
	vSocializeOdds    = 100       // SocializeAtBell: nextInt(100) == 0
	vSocializeBell    = 4.0       // …within four of the bell
	vSocializeReachSq = 32.0      // …a villager within √32
	vWalkTimeMin      = 150       // MoveToTargetSink(): 150-250 ticks a walk
	vWalkTimeMax      = 250
	vPlayWalkTimeMin  = 80 // the PLAY package's MoveToTargetSink(80, 120)
	vPlayWalkTimeMax  = 120
	secondaryPoiXZ    = 4  // SecondaryPoiSensor: x and z within four,
	secondaryPoiY     = 2  // …y within two
	secondaryPoiScan  = 40 // …every forty ticks
	nearestBedRange   = 48 // NearestBedSensor: HOME POIs within 48
	nearestBedBatch   = 4  // …at most four tried a scan (++triedCount >= 5 refuses)
	jumpBedReachTime  = 100
	jumpBedJumpsMin   = 3 // 3 + nextInt(4)
	jumpBedJumpsRand  = 4
	jumpBedCooldown   = 5
	jumpBedPower      = 0.42 // JumpControl.jump: jumpFromGround at the base jump power

	vsPlay = 4 // a child's PLAY activity (villagerActivity)

	// vStrollNext slots
	vStrollAroundJob  = 0
	vStrollToJob      = 1
	vStrollToList     = 2
	vStrollAroundBell = 3
)

// villagerWalk is the villager's WALK_TARGET: a spot, a block, or another
// mob followed as it moves (an EntityTracker).
type villagerWalk struct {
	set     bool
	x, y, z float64
	block   bool    // a block position: walked to over its height (pathSteerTo)
	eid     int32   // a mob followed (0 = none)
	speed   float64 // a multiple of the engine's villager pace
	close   int     // closeEnoughDist, Manhattan in blocks
	until   uint64  // MoveToTargetSink's time limit
}

// bedJump is a running JumpOnBed.
type bedJump struct {
	on        bool
	bed       blockPos
	reachLeft int // remainingTimeToReachBed
	jumpsLeft int
	cooldown  int
}

// villagerActivity is the schedule's activity for this villager: the
// villager timeline for an adult (WORK and MEET only with the job site or
// bell they need, IDLE otherwise), and the baby timeline for a child —
// IDLE from 10, PLAY from 3000, IDLE from 6000, PLAY from 10000, then REST.
func (h *hub) villagerActivity(m *mob) int {
	seg := villagerSegment(h.dayTime.Load())
	if m.baby {
		if seg == vsSleep {
			return vsSleep
		}
		switch t := h.dayTime.Load() % dayLengthTicks; {
		case t >= 3000 && t < 6000, t >= 10000 && t < 12000:
			return vsPlay
		}
		return vsRoam
	}
	switch {
	case seg == vsWork && m.work == (blockPos{}):
		return vsRoam
	case seg == vsGather && m.meet == (blockPos{}):
		return vsRoam
	}
	return seg
}

// villagerBrainStep runs a villager's errands each mob update and reports
// whether they hold it: a walk under way, a JumpOnBed, a DoNothing, or a
// fresh pick. Work, the meeting point and the night are left to the
// schedule walk (villagerBehavior) whenever no errand is running.
func (h *hub) villagerBrainStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityVillager || m.dying > 0 || m.sleeping {
		return false
	}
	now := h.tick.Load()
	held := false
	if m.vJump.on {
		held = h.jumpOnBedStep(m)
	}
	if m.vWalk.set && h.villagerWalkStep(m, now) {
		return true
	}
	if held {
		m.vx, m.vz = m.vx*0.6, m.vz*0.6
		return true
	}
	if now < m.vIdleUntil {
		m.vx, m.vz = m.vx*0.6, m.vz*0.6 // DoNothing: it stands about
		return true
	}
	act := h.villagerActivity(m)
	switch act {
	case vsWork:
		h.villagerWorkStrolls(m, now)
	case vsGather:
		h.villagerMeetStrolls(m, now)
	case vsRoam, vsPlay:
		if act == vsPlay && len(h.visibleBabies(m)) > 0 {
			return false // RunOne(VISIBLE_VILLAGER_BABIES absent): tag's time
		}
		h.villagerIdlePick(m, now, act == vsPlay)
		if m.vJump.on {
			h.jumpOnBedStep(m)
		}
		if m.vWalk.set {
			h.villagerWalkStep(m, now)
		} else {
			m.vx, m.vz = m.vx*0.6, m.vz*0.6
		}
		return true // the idle RunOne always has something: it strolls or waits
	default:
		return false
	}
	if m.vWalk.set {
		return h.villagerWalkStep(m, now)
	}
	return false
}

// shuffledPicks is ShufflingList.shuffle: each entry draws -u^(1/weight) and
// the list is sorted on it, so a heavier entry tends to come first.
func (h *hub) shuffledPicks(weights []int) []int {
	keys := make([]float64, len(weights))
	idx := make([]int, len(weights))
	for i, w := range weights {
		idx[i] = i
		keys[i] = -math.Pow(h.rng.Float64(), 1/float64(w))
	}
	sort.SliceStable(idx, func(a, b int) bool { return keys[idx[a]] < keys[idx[b]] })
	return idx
}

// setWalk sets the WALK_TARGET to a spot.
func (h *hub) setWalk(m *mob, x, y, z, speed float64, close int, play bool) {
	m.vWalk = villagerWalk{set: true, x: x, y: y, z: z, speed: speed, close: close, until: h.walkLimit(play)}
}

// setWalkBlock sets it to a block, walked to over its height.
func (h *hub) setWalkBlock(m *mob, p blockPos, speed float64, close int, play bool) {
	m.vWalk = villagerWalk{set: true, x: float64(p.x) + 0.5, y: float64(p.y), z: float64(p.z) + 0.5, block: true,
		speed: speed, close: close, until: h.walkLimit(play)}
}

// setWalkMob sets it to follow another mob.
func (h *hub) setWalkMob(m, o *mob, speed float64, close int, play bool) {
	m.vWalk = villagerWalk{set: true, x: o.x, y: o.y, z: o.z, eid: o.eid, speed: speed, close: close, until: h.walkLimit(play)}
	m.headYaw = float32(math.Atan2(-(o.x-m.x), o.z-m.z) * 180 / math.Pi) // LOOK_TARGET: it faces whom it walks to
}

func (h *hub) walkLimit(play bool) uint64 {
	lo, hi := vWalkTimeMin, vWalkTimeMax
	if play {
		lo, hi = vPlayWalkTimeMin, vPlayWalkTimeMax
	}
	return h.tick.Load() + uint64(lo+h.rng.Intn(hi-lo+1))
}

// villagerBlockPos is the villager's blockPosition. The engine stands a mob
// on a bed at the block's full height where vanilla's bed top is 9/16, so a
// villager standing on a bed is counted in the bed's own cell, as there.
func (h *hub) villagerBlockPos(m *mob) blockPos {
	x, y, z := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	if m.y-float64(y) < 1e-3 && isBedBlock(h.worldFor(m.dim).At(x, y-1, z)) {
		y--
	}
	return blockPos{x, y, z}
}

// villagerWalkStep is MoveToTargetSink: it walks the villager to its
// WALK_TARGET and drops it once reached (Manhattan within closeEnoughDist),
// out of time, or when the mob it follows is gone. It reports whether the
// villager is still walking.
func (h *hub) villagerWalkStep(m *mob, now uint64) bool {
	wt := &m.vWalk
	if wt.eid != 0 {
		o := h.mobs[wt.eid]
		if o == nil || o.dying > 0 || o.dim != m.dim {
			*wt = villagerWalk{}
			return false
		}
		wt.x, wt.y, wt.z = o.x, o.y, o.z
	}
	tb := blockPos{floorInt(wt.x), floorInt(wt.y), floorInt(wt.z)}
	b := h.villagerBlockPos(m)
	if abs(tb.x-b.x)+abs(tb.y-b.y)+abs(tb.z-b.z) <= wt.close || now >= wt.until {
		*wt = villagerWalk{}
		return false
	}
	var vx, vz float64
	switch {
	case math.Hypot(wt.x-m.x, wt.z-m.z) < 1.5:
		vx, vz = straightSteer(m, wt.x, wt.z, 0.05) // the last step, onto the spot
	case wt.block:
		vx, vz = h.pathSteerTo(m, tb, max(wt.close, 1))
	default:
		vx, vz = h.pathSteer(m, wt.x, wt.z)
	}
	m.vx, m.vz = vx*wt.speed, vz*wt.speed
	m.rest = 0
	return true
}

// villagerIdlePick is the IDLE (or, for a child, PLAY) RunOne, run when
// nothing it started is still going.
func (h *hub) villagerIdlePick(m *mob, now uint64, play bool) {
	const (
		pVillager = iota
		pCat
		pStroll
		pJump
		pWait
	)
	weights := []int{2, 1, 1, 1, 1}
	if play {
		weights = []int{2, 1, 1, 2, 2}
	}
	for _, pick := range h.shuffledPicks(weights) {
		switch pick {
		case pVillager:
			if h.villagerInteractWith(m, entityVillager, play) {
				return
			}
		case pCat:
			if h.villagerInteractWith(m, entityCat, play) {
				return
			}
		case pStroll: // VillageBoundRandomStroll(0.5)
			x, z := h.villageStroll(m)
			h.setWalk(m, x, m.y, z, vSpeedBase, 0, play)
			return
		case pJump:
			if h.startJumpOnBed(m, play) {
				return
			}
		case pWait: // DoNothing(30, 60), or (20, 40) at play
			lo, hi := 30, 60
			if play {
				lo, hi = 20, 40
			}
			m.vIdleUntil = now + uint64(lo+h.rng.Intn(hi+1-lo))
			return
		}
	}
}

// villagerInteractWith is InteractWith(type, 8, INTERACTION_TARGET, 0.5, 2):
// with one of that kind in sight it starts, and walks up to the closest
// within eight blocks if there is one.
func (h *hub) villagerInteractWith(m *mob, etype int, play bool) bool {
	o, ok := h.nearestVisible(m, etype)
	if !ok {
		return false
	}
	if o != nil && dist3sq(o.x, o.y, o.z, m.x, m.y, m.z) <= vInteractRange*vInteractRange {
		h.setWalkMob(m, o, vSpeedBase, vInteractStop, play)
	}
	return true
}

// nearestVisible reads NEAREST_VISIBLE_LIVING_ENTITIES for one kind: whether
// any is in sight within the follow range, and the closest such. The line of
// sight is tested nearest first, and only so many times a scan.
func (h *hub) nearestVisible(m *mob, etype int) (*mob, bool) {
	r := m.followRange()
	var cands []*mob
	h.grid().nearby(m.dim, m.x, m.z, r, func(o *mob) {
		if o != m && o.etype == etype && o.dying == 0 && math.Abs(o.y-m.y) <= r {
			cands = append(cands, o)
		}
	})
	sort.Slice(cands, func(i, j int) bool {
		di, dj := dist3sq(cands[i].x, cands[i].y, cands[i].z, m.x, m.y, m.z), dist3sq(cands[j].x, cands[j].y, cands[j].z, m.x, m.y, m.z)
		if di != dj {
			return di < dj
		}
		return cands[i].eid < cands[j].eid
	})
	for i, o := range cands {
		if i >= vSeeChecks {
			break
		}
		if h.mobSeesMob(m, o) {
			return o, true
		}
	}
	return nil, false
}

// villagerWorkStrolls is the WORK RunOne's strolls, within reach of the job
// site; farther off, the schedule walk brings the villager in
// (SetWalkTargetFromBlockMemory(JOB_SITE, 0.5, 9, ..)).
func (h *hub) villagerWorkStrolls(m *mob, now uint64) {
	b := h.villagerBlockPos(m)
	site := m.work
	if abs(site.x-b.x)+abs(site.y-b.y)+abs(site.z-b.z) > vWorkCloseEnough {
		return
	}
	h.senseSecondaryPoi(m, now)
	near := func(r float64) bool { // BlockPos.closerToCenterThan
		return dist3sq(float64(site.x)+0.5, float64(site.y)+0.5, float64(site.z)+0.5, m.x, m.y, m.z) < r*r
	}
	const (
		pAround = iota
		pTo
		pList
	)
	for _, pick := range h.shuffledPicks([]int{2, 5, 5}) {
		switch pick {
		case pAround: // StrollAroundPoi(JOB_SITE, 0.4, 4)
			if !near(vWorkStrollReach) {
				continue
			}
			if now > m.vStrollNext[vStrollAroundJob] {
				h.strollAround(m, now, vStrollAroundJob)
			}
			return
		case pTo: // StrollToPoi(JOB_SITE, 0.4, 1, 10)
			if !near(vWorkToReach) {
				continue
			}
			if now > m.vStrollNext[vStrollToJob] {
				h.setWalkBlock(m, site, vSpeedPoiStroll, 1, false)
				m.vStrollNext[vStrollToJob] = now + vStrollToWait
			}
			return
		case pList: // StrollToPoiList(SECONDARY_JOB_SITE, 0.5, 1, 6, JOB_SITE)
			if len(m.vSecondary) == 0 {
				continue
			}
			p := m.vSecondary[h.rng.Intn(len(m.vSecondary))]
			if !near(vWorkListReach) {
				continue
			}
			if now > m.vStrollNext[vStrollToList] {
				h.setWalkBlock(m, p, vSpeedBase, 1, false)
				m.vStrollNext[vStrollToList] = now + vStrollListWait
			}
			return
		}
	}
}

// strollAround is StrollAroundPoi's walk: a standable spot within eight
// across and six up or down, or no walk at all when there is none.
func (h *hub) strollAround(m *mob, now uint64, slot int) {
	if x, z, ok := h.landRandomPos(m, vStrollAroundXZ, vStrollAroundY); ok {
		h.setWalk(m, x, m.y, z, vSpeedPoiStroll, 1, false)
	} else {
		m.vWalk = villagerWalk{}
	}
	m.vStrollNext[slot] = now + vStrollAroundWait
}

// senseSecondaryPoi is SecondaryPoiSensor: every forty ticks, the blocks of
// the villager's secondary POI (a farmer's farmland) four across and two up
// or down about it.
func (h *hub) senseSecondaryPoi(m *mob, now uint64) {
	if now < m.vSecondaryAt {
		return
	}
	m.vSecondaryAt = now + secondaryPoiScan
	m.vSecondary = m.vSecondary[:0]
	if m.profession != profFarmer {
		return
	}
	w := h.worldFor(m.dim)
	b := blockPos{floorInt(m.x), floorInt(m.y), floorInt(m.z)}
	for x := -secondaryPoiXZ; x <= secondaryPoiXZ; x++ {
		for y := -secondaryPoiY; y <= secondaryPoiY; y++ {
			for z := -secondaryPoiXZ; z <= secondaryPoiXZ; z++ {
				if isFarmland(w.At(b.x+x, b.y+y, b.z+z)) {
					m.vSecondary = append(m.vSecondary, blockPos{b.x + x, b.y + y, b.z + z})
				}
			}
		}
	}
}

// villagerMeetStrolls is the MEET package's triggerOneShuffled of
// StrollAroundPoi(MEETING_POINT, 0.4, 40) and SocializeAtBell, within reach
// of the bell; farther off the schedule walk brings the villager in
// (SetWalkTargetFromBlockMemory(MEETING_POINT, 0.5, 6, ..)).
func (h *hub) villagerMeetStrolls(m *mob, now uint64) {
	b := h.villagerBlockPos(m)
	bell := m.meet
	if abs(bell.x-b.x)+abs(bell.y-b.y)+abs(bell.z-b.z) > vMeetCloseEnough {
		return
	}
	bellD2 := dist3sq(float64(bell.x)+0.5, float64(bell.y)+0.5, float64(bell.z)+0.5, m.x, m.y, m.z)
	for _, pick := range h.shuffledPicks([]int{2, 2}) {
		if pick == 0 { // StrollAroundPoi
			if bellD2 >= vMeetStrollReach*vMeetStrollReach {
				continue
			}
			if now > m.vStrollNext[vStrollAroundBell] {
				h.strollAround(m, now, vStrollAroundBell)
			}
			return
		}
		// SocializeAtBell: now and then, near the bell with a villager in
		// sight, over to the closest within √32 at a stroll.
		if h.rng.Intn(vSocializeOdds/mobMoveInterval) != 0 || bellD2 >= vSocializeBell*vSocializeBell {
			continue
		}
		o, ok := h.nearestVisible(m, entityVillager)
		if !ok {
			continue
		}
		if o != nil && dist3sq(o.x, o.y, o.z, m.x, m.y, m.z) <= vSocializeReachSq {
			h.setWalkMob(m, o, vSpeedSocialize, 1, false)
		}
		return
	}
}

// senseNearestBed is NearestBedSensor for a child, every twenty ticks: the
// nearest bed within 48 it has a path to (four tried a scan). The memory is
// only ever replaced, never cleared.
func (h *hub) senseNearestBed(m *mob) {
	if !m.baby || m.etype != entityVillager || m.dying > 0 {
		return
	}
	w := h.poiWorld(m.dim)
	if w == nil {
		return
	}
	isHome := func(p world.POI) bool { return p.Kind == poiKindHome }
	homes := w.POIsNear(floorInt(m.x), floorInt(m.y), floorInt(m.z), nearestBedRange, isHome)
	for i, p := range homes {
		if i >= nearestBedBatch {
			break
		}
		pos := blockPos{p.X, p.Y, p.Z}
		if h.poiReachable(m, pos, poiValidRange[poiHome]) {
			m.vBed, m.vBedSet = pos, true
			return
		}
	}
}

// startJumpOnBed is JumpOnBed's start: a child with a bed in memory walks to
// it (WalkTarget(bed, 0.5, 0)) to jump on it three to six times.
func (h *hub) startJumpOnBed(m *mob, play bool) bool {
	if !m.baby || !m.vBedSet {
		return false
	}
	if !isBedBlock(h.worldFor(m.dim).At(m.vBed.x, m.vBed.y, m.vBed.z)) {
		return false // it would stop on its first tick (canStillUse: isJumpable)
	}
	m.vJump = bedJump{on: true, bed: m.vBed, reachLeft: jumpBedReachTime,
		jumpsLeft: jumpBedJumpsMin + h.rng.Intn(jumpBedJumpsRand)}
	h.setWalkBlock(m, m.vBed, vSpeedBase, 0, play)
	return true
}

// onOrOverBed: a bed where the child stands, or under it.
func (h *hub) onOrOverBed(m *mob) bool {
	w := h.worldFor(m.dim)
	b := h.villagerBlockPos(m)
	return isBedBlock(w.At(b.x, b.y, b.z)) || isBedBlock(w.At(b.x, b.y-1, b.z))
}

// jumpOnBedStep is JumpOnBed's canStillUse and tick, for the ticks of one
// mob update. It reports whether the behaviour is still running.
func (h *hub) jumpOnBedStep(m *mob) bool {
	j := &m.vJump
	w := h.worldFor(m.dim)
	over := h.onOrOverBed(m)
	if !m.baby || !isBedBlock(w.At(j.bed.x, j.bed.y, j.bed.z)) ||
		(!over && j.reachLeft <= 0) || (over && j.jumpsLeft <= 0) {
		*j = bedJump{}
		return false
	}
	switch {
	case !over:
		j.reachLeft -= mobMoveInterval
	case j.cooldown > 0:
		j.cooldown = max(0, j.cooldown-mobMoveInterval)
	default:
		b := h.villagerBlockPos(m)
		if !m.leaping && isBedBlock(w.At(b.x, b.y, b.z)) { // onBedSurface
			m.leaping, m.leapVX, m.leapVY, m.leapVZ = true, 0, jumpBedPower, 0
			j.jumpsLeft--
			j.cooldown = jumpBedCooldown
		}
	}
	return true
}

// poiCompetitorScan is PoiCompetitorScan: of the living villagers about that
// hold the same job site and whose trade it serves, the one with the most
// experience keeps it (a tie goes to the other villager) and every other
// loses its JOB_SITE — and, never having traded, its profession
// (ResetProfession).
func (h *hub) poiCompetitorScan(players map[int32]*tracked, m *mob) {
	if m.work == (blockPos{}) {
		return
	}
	prof := jobBlockProfession(h.worldFor(m.dim).At(m.work.x, m.work.y, m.work.z))
	if prof < 0 {
		return // no job-site POI there any more: ValidateNearbyPoi's business
	}
	r := m.followRange()
	var rivals []*mob
	for _, o := range h.mobs {
		if o == m || o.etype != entityVillager || o.dying > 0 || o.dim != m.dim ||
			o.work != m.work || o.profession != prof {
			continue
		}
		if math.Abs(o.x-m.x) > r || math.Abs(o.y-m.y) > r || math.Abs(o.z-m.z) > r {
			continue // NEAREST_LIVING_ENTITIES: the follow range about it
		}
		rivals = append(rivals, o)
	}
	sort.Slice(rivals, func(i, j int) bool {
		a, b := rivals[i], rivals[j]
		da, db := dist3sq(a.x, a.y, a.z, m.x, m.y, m.z), dist3sq(b.x, b.y, b.z, m.x, m.y, m.z)
		if da != db {
			return da < db
		}
		return a.eid < b.eid
	})
	win := m
	for _, o := range rivals {
		lose := o
		if win.tradeXP <= o.tradeXP {
			lose, win = win, o
		}
		h.loseJobSite(players, lose)
	}
}

// loseJobSite erases JOB_SITE, and ResetProfession then unemploys a
// villager that never traded.
func (h *hub) loseJobSite(players map[int32]*tracked, v *mob) {
	v.work = blockPos{}
	if v.profession >= 0 && v.tradeLevel <= 1 && v.tradeXP == 0 {
		v.profession = profUnemployed
		v.offers = nil
		h.sendVillagerData(players, v)
	}
}
