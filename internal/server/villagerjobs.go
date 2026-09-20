package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Villager jobs. Vanilla's villagers are born unemployed and take the
// nearest free workstation they can reach (AcquirePoi → POTENTIAL_JOB_SITE,
// GoToPotentialJobSite at 0.5, AssignProfessionFromJobSite within two
// blocks: the block's profession, a green-particle flourish); a workstation
// serves one villager; one whose block is gone loses the site
// (ValidateNearbyPoi) and, if it never traded, its profession too
// (ResetProfession: level one, no experience). tachyne's villagers were
// handed a profession at the village's birth and never changed.

const (
	jobSearchRange  = 16  // horizontal half-range of the workstation scan (vanilla's POI search reaches 48)
	jobSearchRangeY = 4   //
	jobSearchEvery  = 200 // ticks between scans for an unemployed villager (vanilla: 20–40 with a retry backoff per site)
	jobWalkSpeed    = 0.5 // GoToPotentialJobSite speedModifier
	jobClaimDist    = 2.0 // AssignProfessionFromJobSite: closerToCenterThan(pos, 2.0)
	jobValidateDist = 16.0
	profUnemployed  = -1
	profNitwit      = -2 // VillagerProfession.NITWIT: no trades, never a workstation

	entityStatusVillagerHappy = 14 // Villager.handleEntityEvent 14 (VILLAGER_HAPPY): the green burst — the profession flourish
	entityStatusVillagerNo    = 40 // the head shake (setUnhappy)
)

// jobBlocks maps a workstation block range to its profession index
// (PoiTypes → VillagerProfession).
var jobBlocks = func() []struct {
	lo, hi uint32
	prof   int
} {
	table := map[string]string{
		"blast_furnace": "armorer", "smoker": "butcher", "cartography_table": "cartographer", "brewing_stand": "cleric",
		"composter": "farmer", "barrel": "fisherman", "fletching_table": "fletcher", "lectern": "librarian",
		"stonecutter": "mason", "loom": "shepherd", "smithing_table": "toolsmith", "grindstone": "weaponsmith",
		"cauldron": "leatherworker", "water_cauldron": "leatherworker", "lava_cauldron": "leatherworker", "powder_snow_cauldron": "leatherworker",
	}
	idx := map[string]int{}
	for i, n := range professionNames {
		idx[n] = i
	}
	var out []struct {
		lo, hi uint32
		prof   int
	}
	for block, prof := range table {
		lo, hi, ok := worldgen.BlockRangeOK(block)
		if !ok {
			continue
		}
		out = append(out, struct {
			lo, hi uint32
			prof   int
		}{lo, hi, idx[prof]})
	}
	return out
}()

// jobBlockProfession is the profession a workstation block serves (-1 for
// any other block).
func jobBlockProfession(s uint32) int {
	for _, j := range jobBlocks {
		if s >= j.lo && s <= j.hi {
			return j.prof
		}
	}
	return -1
}

// jobSiteClaimed reports whether another living villager holds or is
// walking to the workstation at pos.
func (h *hub) jobSiteClaimed(pos blockPos, by *mob) bool {
	for _, o := range h.mobs {
		if o == by || o.etype != entityVillager || o.dying > 0 || o.dim != by.dim {
			continue
		}
		if o.work == pos || o.jobPos == pos {
			return true
		}
	}
	return false
}

// villagerJobTick runs once a survival step (20 ticks): ValidateNearbyPoi on
// a held site, ResetProfession, and AcquirePoi's scan for a free one.
func (h *hub) villagerJobTick(players map[int32]*tracked, m *mob) {
	if m.baby || m.dying > 0 || m.profession == profNitwit {
		return
	}
	w := h.worldFor(m.dim)
	now := h.tick.Load()
	// A held workstation that is gone, or the wrong block, is lost; a
	// villager that never traded is unemployed again.
	if m.work != (blockPos{}) {
		if dist3(float64(m.work.x)+0.5, float64(m.work.y), float64(m.work.z)+0.5, m.x, m.y, m.z) <= jobValidateDist {
			if p := jobBlockProfession(w.At(m.work.x, m.work.y, m.work.z)); p < 0 || (m.profession >= 0 && p != m.profession) {
				m.work = blockPos{}
				if m.profession >= 0 && m.tradeLevel <= 1 && m.tradeXP == 0 {
					m.profession = profUnemployed
					m.offers = nil
					h.sendVillagerData(players, m)
				}
			}
		}
		return
	}
	// Walking to a potential site: GoToPotentialJobSite's stop validates it,
	// and an unemployed villager yields it to a neighbour whose trade it is.
	if m.jobPos != (blockPos{}) {
		if p := jobBlockProfession(w.At(m.jobPos.x, m.jobPos.y, m.jobPos.z)); p < 0 || (m.profession >= 0 && p != m.profession) || h.jobSiteClaimed(m.jobPos, m) {
			m.jobPos = blockPos{}
		} else if m.profession < 0 {
			h.yieldJobSite(m, p)
		}
		return
	}
	if now < m.jobSearchAt {
		return
	}
	m.jobSearchAt = now + jobSearchEvery + uint64(h.rng.Intn(jobSearchEvery/2))
	// AcquirePoi: the nearest free workstation (of its own profession, if it
	// has one) within the scan.
	mx, my, mz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	best, bestD := blockPos{}, math.MaxFloat64
	for dx := -jobSearchRange; dx <= jobSearchRange; dx++ {
		for dz := -jobSearchRange; dz <= jobSearchRange; dz++ {
			for dy := -jobSearchRangeY; dy <= jobSearchRangeY; dy++ {
				p := jobBlockProfession(w.At(mx+dx, my+dy, mz+dz))
				if p < 0 || (m.profession >= 0 && p != m.profession) {
					continue
				}
				d := float64(dx*dx + dy*dy + dz*dz)
				if d >= bestD {
					continue
				}
				pos := blockPos{mx + dx, my + dy, mz + dz}
				if h.jobSiteClaimed(pos, m) {
					continue
				}
				best, bestD = pos, d
			}
		}
	}
	if bestD < math.MaxFloat64 {
		m.jobPos = best
	}
}

// villagerJobWalk is GoToPotentialJobSite + AssignProfessionFromJobSite,
// run each mob update: the walk to the site at half pace, and the claim
// within two blocks. Returns whether it holds the villager.
func (h *hub) villagerJobWalk(players map[int32]*tracked, m *mob) bool {
	if m.jobPos == (blockPos{}) || m.baby || m.dying > 0 || m.profession == profNitwit {
		return false
	}
	if seg := villagerSegment(h.dayTime.Load()); seg == vsSleep {
		return false // only while idle, at work or at play
	}
	tx, ty, tz := float64(m.jobPos.x)+0.5, float64(m.jobPos.y), float64(m.jobPos.z)+0.5
	if dist3(tx, ty, tz, m.x, m.y, m.z) > jobClaimDist {
		h.steerTo(m, tx, tz, jobWalkSpeed)
		return true
	}
	// AssignProfessionFromJobSite
	p := jobBlockProfession(h.worldFor(m.dim).At(m.jobPos.x, m.jobPos.y, m.jobPos.z))
	if p < 0 || h.jobSiteClaimed(m.jobPos, m) {
		m.jobPos = blockPos{}
		return false
	}
	m.work, m.jobPos = m.jobPos, blockPos{}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerHappy))
	if m.profession < 0 {
		h.initVillagerTrades(m, p)
		h.sendVillagerData(players, m)
	}
	m.vx, m.vz = 0, 0
	return true
}

// yieldJobSite is YieldJobSite: an unemployed villager on its way to a
// workstation gives it up to the nearest villager in sensor range that
// already holds that trade and has no workstation of its own (one that
// lost its block) — the site becomes that villager's potential job site
// instead, unless it is already walking to one.
func (h *hub) yieldJobSite(m *mob, prof int) {
	r := m.followRange()
	for _, o := range h.mobs {
		if o == m || o.etype != entityVillager || o.dying > 0 || o.dim != m.dim || o.baby {
			continue
		}
		if o.profession != prof || o.jobPos != (blockPos{}) || o.work != (blockPos{}) {
			continue
		}
		if math.Abs(o.x-m.x) > r || math.Abs(o.y-m.y) > r || math.Abs(o.z-m.z) > r {
			continue
		}
		o.jobPos, m.jobPos = m.jobPos, blockPos{}
		return
	}
}
