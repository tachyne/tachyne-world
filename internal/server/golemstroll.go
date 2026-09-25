package server

import "math"

// GolemRandomStrollInVillageGoal: an iron golem with nothing to fight walks
// about its village at 0.6 of its pace (and, outside one, back toward it —
// MoveBackToVillageGoal, also 0.6). It used to stand still unless it was more
// than six blocks from where it was made. Three walks in ten go anywhere within
// ten blocks; the rest head toward a villager within 32 that wants a golem
// (seven times in ten) or toward a claimed bed, job site or bell in a village
// section within two of it — each falling back on the other, then anywhere.

const (
	golemStrollSpeed    = 0.6
	golemStrollReach    = 10 // RANDOM_POS_XY_DISTANCE
	golemVillagerScan   = 32 // VILLAGER_SCAN_RADIUS
	golemPoiSectionScan = 2  // POI_SECTION_SCAN_RADIUS
	golemPoiRange       = 8  // getInRange(…, sectionPos.center(), 8, …)
	golemStrollGiveUp   = 240
)

// golemStrollSteer walks to the current stroll target, picking a new one
// when it has arrived or given up on it.
func (h *hub) golemStrollSteer(m *mob) (float64, float64) {
	now := h.tick.Load()
	if now >= m.roamAt || math.Hypot(m.roamX-m.x, m.roamZ-m.z) < 1.5 {
		here := [3]int{floorInt(m.x) >> 4, floorInt(m.y) >> 4, floorInt(m.z) >> 4}
		if sectionsToVillage(h.villageCentres(m.dim), here) > 1 {
			// MoveBackToVillageGoal: outside a village, ten blocks toward
			// the nearest section closer to one (a random spot with none).
			m.roamX, m.roamZ = h.villageStroll(m)
		} else {
			m.roamX, m.roamZ = h.golemStrollTarget(m)
		}
		m.roamAt = now + golemStrollGiveUp
	}
	return h.pathSteer(m, m.roamX, m.roamZ)
}

// golemStrollTarget is GolemRandomStrollInVillageGoal.getPosition.
func (h *hub) golemStrollTarget(m *mob) (float64, float64) {
	anywhere := func() (float64, float64) {
		return m.x + float64(h.rng.Intn(2*golemStrollReach+1)-golemStrollReach),
			m.z + float64(h.rng.Intn(2*golemStrollReach+1)-golemStrollReach)
	}
	if h.rng.Float32() < 0.3 {
		return anywhere()
	}
	var x, z float64
	var ok bool
	if h.rng.Float32() < 0.7 {
		if x, z, ok = h.golemTowardVillager(m); !ok {
			x, z, ok = h.golemTowardPoi(m)
		}
	} else if x, z, ok = h.golemTowardPoi(m); !ok {
		x, z, ok = h.golemTowardVillager(m)
	}
	if !ok {
		return anywhere()
	}
	return x, z
}

// golemTowardVillager: a random villager within 32 that wants a golem.
func (h *hub) golemTowardVillager(m *mob) (float64, float64, bool) {
	now := h.tick.Load()
	var want []*mob
	for _, v := range h.mobs {
		if v.etype != entityVillager || v.dim != m.dim || v.dying > 0 ||
			math.Abs(v.x-m.x) > golemVillagerScan || math.Abs(v.y-m.y) > golemVillagerScan || math.Abs(v.z-m.z) > golemVillagerScan {
			continue
		}
		if h.wantsToSpawnGolem(v, now) {
			want = append(want, v)
		}
	}
	if len(want) == 0 {
		return 0, 0, false
	}
	v := want[h.rng.Intn(len(want))]
	x, z := h.randomPosTowards(m, v.x, v.z, golemStrollReach)
	return x, z, true
}

// golemTowardPoi: a random claimed POI in a random village section near by.
func (h *hub) golemTowardPoi(m *mob) (float64, float64, bool) {
	centres := h.villageCentres(m.dim)
	here := [3]int{floorInt(m.x) >> 4, floorInt(m.y) >> 4, floorInt(m.z) >> 4}
	var sections [][3]int
	for dz := -golemPoiSectionScan; dz <= golemPoiSectionScan; dz++ {
		for dy := -golemPoiSectionScan; dy <= golemPoiSectionScan; dy++ {
			for dx := -golemPoiSectionScan; dx <= golemPoiSectionScan; dx++ {
				if s := [3]int{here[0] + dx, here[1] + dy, here[2] + dz}; centres[s] {
					sections = append(sections, s)
				}
			}
		}
	}
	if len(sections) == 0 {
		return 0, 0, false
	}
	s := sections[h.rng.Intn(len(sections))]
	cx, cy, cz := s[0]*16+8, s[1]*16+8, s[2]*16+8
	var pois []blockPos
	seen := map[blockPos]bool{} // a bell is one POI however many villagers meet there
	for _, o := range h.mobs {
		if o.etype != entityVillager || o.dim != m.dim || o.dying > 0 {
			continue
		}
		for _, p := range [3]blockPos{o.bed, o.work, o.meet} {
			if p != (blockPos{}) && !seen[p] && sq(float64(p.x-cx))+sq(float64(p.y-cy))+sq(float64(p.z-cz)) <= golemPoiRange*golemPoiRange {
				seen[p] = true
				pois = append(pois, p)
			}
		}
	}
	if len(pois) == 0 {
		return 0, 0, false
	}
	p := pois[h.rng.Intn(len(pois))]
	x, z := h.randomPosTowards(m, float64(p.x)+0.5, float64(p.z)+0.5, golemStrollReach)
	return x, z, true
}
