package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Villager breeding and food sharing. Vanilla villagers breed on food: one
// with twelve food points between its belly and its pockets (bread 4,
// carrot, potato and beetroot 1 each) takes up with another such villager
// within eight blocks while idling or gathering (InteractWith → BREED_TARGET),
// the two walk to each other showing hearts, and 275–325 ticks later, if a
// vacant bed lies within forty-eight blocks, a baby is born beside them and
// given that bed; each parent eats its fill, digests twelve points, and
// waits five minutes before breeding again. With no bed they show anger
// instead (VillagerMakeLove). Villagers chatting also share: one with
// twenty-four points of food throws half a stack to a farmer or to a
// villager short of twelve, a farmer with more than thirty-two wheat
// throws half of that, and anyone holding what a neighbour's profession
// asks for hands it over (TradeWithVillager.throwHalfStack). tachyne's
// villages never grew.

const (
	villagerHungry      = 12  // Villager.hungry: foodLevel < 12
	villagerBreedPoints = 12  // canBreed: foodLevel + inventory points >= 12
	villagerExcessFood  = 24  // hasExcessFood
	breedSeekRange      = 8.0 // InteractWith(VILLAGER, 8, canBreed, canBreed, BREED_TARGET)
	breedMeetDistSq     = 5.0 // VillagerMakeLove.tick: distanceToSqr(target) <= 5
	breedWalkSpeed      = 0.5 // lockGazeAndWalkToEachOther(.., 0.5F, 2)
	breedDurationMin    = 275 // 275 + nextInt(50)
	breedDurationRand   = 50  //
	breedHappyOdds      = 35  // nextInt(35) == 0: happy particles while courting
	breedBedRange       = 48  // takeVacantBed(.., 48)
	breedBedRangeY      = 8   // (the POI search is a sphere; beds sit near the villager's level)
	breedSeekOdds       = 3   // the idle RunOne reaches the breed InteractWith about this often per second
	shareThrowDist      = 1.0 // the throw lands this far toward the partner (items here have no flight)
	villagerFoodBread   = 4

	entityStatusLoveHearts     = 18 // LivingEntity.handleEntityEvent: heart particles (in love)
	entityStatusVillagerHearts = 12 // Villager.handleEntityEvent 12: heart particles (a villager is no Animal, so 18 draws nothing on it)
	entityStatusVillagerAngry  = 13 // Villager: angry particles
)

// villagerFoodPoints is Villager.FOOD_POINTS.
var villagerFoodPoints = map[int32]int{
	int32(itemByName["bread"]):    villagerFoodBread,
	int32(itemByName["potato"]):   1,
	int32(itemByName["carrot"]):   1,
	int32(itemByName["beetroot"]): 1,
}

// professionRequested is VillagerProfession.requestedItems per profession
// (only the farmer asks for anything in vanilla).
var professionRequested = map[int]map[int32]bool{
	profFarmer: farmerRequested,
}

func villagerFoodInPockets(m *mob) int {
	n := 0
	for _, st := range m.hoard {
		n += villagerFoodPoints[st.item] * st.count
	}
	return n
}

// villagerCanBreed is Villager.canBreed: fed, awake, adult, not cooling down.
func villagerCanBreed(m *mob) bool {
	return m.etype == entityVillager && m.dying == 0 && !m.baby && !m.sleeping && m.breedCD <= 0 &&
		m.vFood+villagerFoodInPockets(m) >= villagerBreedPoints
}

// villagerEatUntilFull is eatUntilFull: food from the pockets, slot by slot,
// until no longer hungry.
func villagerEatUntilFull(m *mob) {
	if m.vFood >= villagerHungry {
		return
	}
	for i := 0; i < len(m.hoard); {
		st := &m.hoard[i]
		pts := villagerFoodPoints[st.item]
		if pts == 0 {
			i++
			continue
		}
		for st.count > 0 && m.vFood < villagerHungry {
			m.vFood += pts
			st.count--
		}
		if st.count == 0 {
			m.hoard = append(m.hoard[:i], m.hoard[i+1:]...)
		} else {
			i++
		}
		if m.vFood >= villagerHungry {
			return
		}
	}
}

// villagerEatAndDigest is eatAndDigestFood.
func villagerEatAndDigest(m *mob) {
	villagerEatUntilFull(m)
	m.vFood -= villagerBreedPoints
}

// villagerIdle: the idle and meet activities, where the breed InteractWith
// lives.
func (h *hub) villagerIdle(m *mob) bool {
	seg := villagerSegment(h.dayTime.Load())
	return seg == vsRoam || seg == vsGather
}

// villagerBreedStep is InteractWith(BREED_TARGET) + VillagerMakeLove for one
// villager each mob update; returns whether it holds the villager.
func (h *hub) villagerBreedStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityVillager || m.dying > 0 {
		return false
	}
	now := h.tick.Load()
	if m.breedMate == 0 {
		if !h.villagerIdle(m) || !villagerCanBreed(m) || now%survivalTickN != 0 || h.rng.Intn(breedSeekOdds) != 0 {
			return false
		}
		var mate *mob
		bestD := breedSeekRange
		h.grid().nearby(m.dim, m.x, m.z, breedSeekRange, func(o *mob) {
			if o == m || o.breedMate != 0 || !villagerCanBreed(o) {
				return
			}
			if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d <= bestD {
				mate, bestD = o, d
			}
		})
		if mate == nil {
			return false
		}
		m.breedMate, mate.breedMate = mate.eid, m.eid
		m.breedLead, mate.breedLead = true, false
		m.breedAt = now + breedDurationMin + uint64(h.rng.Intn(breedDurationRand))
		mate.breedAt = m.breedAt
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerHearts))                // hearts (event 18)
		h.toTracking(players, mate.eid, mate.dim, mate.x, mate.z, entityStatus(mate.eid, entityStatusVillagerHearts)) //
		return true
	}
	o := h.mobs[m.breedMate]
	if o == nil || o.dim != m.dim || o.breedMate != m.eid || !villagerCanBreed(m) || !villagerCanBreed(o) {
		m.breedMate = 0 // stop: the target went, or one of them can no longer breed
		if o != nil && o.breedMate == m.eid {
			o.breedMate = 0
		}
		return false
	}
	if d2 := dist3sq(o.x, o.y, o.z, m.x, m.y, m.z); d2 > breedMeetDistSq {
		h.steerTo(m, o.x, o.z, breedWalkSpeed) // lockGazeAndWalkToEachOther
		return true
	}
	m.vx, m.vz = 0, 0
	if !m.breedLead {
		return true // the other half runs the clock
	}
	if now < m.breedAt {
		for i := 0; i < mobMoveInterval; i++ {
			if h.rng.Intn(breedHappyOdds) == 0 {
				h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerHappy))
				h.toTracking(players, o.eid, o.dim, o.x, o.z, entityStatus(o.eid, entityStatusVillagerHappy))
			}
		}
		return true
	}
	villagerEatAndDigest(m)
	villagerEatAndDigest(o)
	h.villagerGiveBirth(players, m, o)
	m.breedMate, o.breedMate = 0, 0
	return true
}

// villagerGiveBirth is tryToGiveBirth + breed + giveBedToChild.
func (h *hub) villagerGiveBirth(players map[int32]*tracked, m, o *mob) {
	bed, ok := h.vacantBedNear(m)
	if !ok {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerAngry))
		h.toTracking(players, o.eid, o.dim, o.x, o.z, entityStatus(o.eid, entityStatusVillagerAngry))
		return
	}
	m.breedCD, o.breedCD = breedCooldown, breedCooldown // setAge(6000)
	child := h.spawnMobIn(players, entityVillager, m.dim, m.x, m.y, m.z)
	if child == nil {
		return // a plugin cancelled the birth; the bed stays free
	}
	child.baby, child.growLeft = true, growUpTicks // setAge(-24000)
	child.setMoveSpeed(0.135)
	h.initVillagerTrades(child, profUnemployed) // born unemployed: it takes a workstation when it grows
	h.sendVillagerData(players, child)
	h.toTracking(players, child.eid, child.dim, child.x, child.z, metaEv(babyMeta(child.eid, true)))
	child.home, child.bed, child.meet = bed, bed, m.meet
	child.behavior = villagerBehavior{}
	child.usesDoors = true
	h.toTracking(players, child.eid, child.dim, child.x, child.z, entityStatus(child.eid, entityStatusVillagerHappy))
}

// vacantBedNear is takeVacantBed: a bed head within forty-eight blocks that
// no villager has claimed. (Vanilla asks the POI store; the engine scans
// the blocks, and skips vanilla's path-reachability check.)
func (h *hub) vacantBedNear(m *mob) (blockPos, bool) {
	taken := map[blockPos]bool{}
	for _, v := range h.mobs {
		if v.etype == entityVillager && v.dying == 0 && v.bed != (blockPos{}) {
			taken[v.bed] = true
		}
	}
	w := h.worldFor(m.dim)
	mx, my, mz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	var best blockPos
	bestD := math.MaxFloat64
	for y := my - breedBedRangeY; y <= my+breedBedRangeY; y++ {
		for x := mx - breedBedRange; x <= mx+breedBedRange; x++ {
			for z := mz - breedBedRange; z <= mz+breedBedRange; z++ {
				s := w.At(x, y, z)
				if !isBedBlock(s) {
					continue
				}
				info, _ := worldgen.InfoForState(s)
				if worldgen.GetProperty(info, s, "part") != "head" {
					continue
				}
				p := blockPos{x, y, z}
				if taken[p] || bedClaimedNear(taken, p) {
					continue
				}
				if d := dist3sq(float64(x), float64(y), float64(z), m.x, m.y, m.z); d < bestD && d <= breedBedRange*breedBedRange {
					best, bestD = p, d
				}
			}
		}
	}
	return best, bestD < math.MaxFloat64
}

// bedClaimedNear: a villager's claim may be recorded on the bed's foot.
func bedClaimedNear(taken map[blockPos]bool, head blockPos) bool {
	for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if taken[blockPos{head.x + d[0], head.y, head.z + d[1]}] {
			return true
		}
	}
	return false
}

// villagerShareFood is the giving half of TradeWithVillager, run when two
// villagers chat: excess food to a farmer or a hungry neighbour, a farmer's
// surplus wheat, and whatever the neighbour's trade asks for.
func (h *hub) villagerShareFood(players map[int32]*tracked, m, o *mob) {
	isFarmer := m.profession == profFarmer
	if villagerFoodInPockets(m) >= villagerExcessFood && (isFarmer || villagerFoodInPockets(o) < villagerHungry) {
		h.villagerThrowHalf(players, m, o, villagerFoodItems())
	}
	if isFarmer && villagerCount(m, int32(itemByName["wheat"])) > villagerStackMax/2 {
		h.villagerThrowHalf(players, m, o, map[int32]bool{int32(itemByName["wheat"]): true})
	}
	if want := professionRequested[o.profession]; len(want) > 0 {
		trades := map[int32]bool{}
		for item := range want {
			if !professionRequested[m.profession][item] {
				trades[item] = true
			}
		}
		if len(trades) > 0 {
			h.villagerThrowHalf(players, m, o, trades)
		}
	}
}

func villagerFoodItems() map[int32]bool {
	out := map[int32]bool{}
	for item := range villagerFoodPoints {
		out[item] = true
	}
	return out
}

// villagerThrowHalf is throwHalfStack: the first pocketed stack of a listed
// item over half a stack gives up half, or over twenty-four gives up the
// rest; it lands a step toward the neighbour.
func (h *hub) villagerThrowHalf(players map[int32]*tracked, m, o *mob, items map[int32]bool) {
	for i := range m.hoard {
		st := &m.hoard[i]
		if !items[st.item] {
			continue
		}
		var n int
		switch {
		case st.count > villagerStackMax/2:
			n = st.count / 2
		case st.count > villagerExcessFood:
			n = st.count - villagerExcessFood
		default:
			continue
		}
		st.count -= n
		item := st.item
		if st.count == 0 {
			m.hoard = append(m.hoard[:i], m.hoard[i+1:]...)
		}
		dx, dz := o.x-m.x, o.z-m.z
		if d := math.Hypot(dx, dz); d > 1e-6 {
			dx, dz = dx/d*shareThrowDist, dz/d*shareThrowDist
		}
		h.spawnItemIn(players, m.dim, item, n, m.x+dx, m.y, m.z+dz)
		return
	}
}
