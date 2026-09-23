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
	h.acquirePoi(players, m, poiJob) // AcquirePoi(acquirableJobSite → POTENTIAL_JOB_SITE)
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

// villagerBedTick is the bed half of the villager's POI life: its held bed is
// dropped when the block is gone (ValidateNearbyPoi), and a villager with no
// bed, child or adult, claims one (AcquirePoi(HOME), acquirepoi.go).
func (h *hub) villagerBedTick(players map[int32]*tracked, m *mob) {
	if m.etype != entityVillager || m.dying > 0 {
		return
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	if m.bed != (blockPos{}) {
		if w.Loaded(int32(m.bed.x>>4), int32(m.bed.z>>4)) && !isBedBlock(w.At(m.bed.x, m.bed.y, m.bed.z)) {
			m.releasePoi(poiHome) // somebody took the bed away
		}
		return
	}
	h.acquirePoi(players, m, poiHome)
}

// bedClaimed reports whether another villager already sleeps in this bed,
// by either half: HOME is the head, but older saves hold whichever half the
// old scan found.
func (h *hub) bedClaimed(pos blockPos, self *mob) bool {
	other, hasOther := bedOtherHalf(h.worldFor(self.dim), pos)
	for _, o := range h.mobs {
		if o != self && o.etype == entityVillager && o.dying == 0 && (o.bed == pos || (hasOther && o.bed == other)) {
			return true
		}
	}
	return false
}

// bedOtherHalf is the other half of the bed at pos: the foot is behind the
// head along the bed's facing.
func bedOtherHalf(w interface{ At(x, y, z int) uint32 }, pos blockPos) (blockPos, bool) {
	if w == nil {
		return blockPos{}, false
	}
	s := w.At(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(s)
	if !ok || !isBed(info) {
		return blockPos{}, false
	}
	dx, dz := 0, 0
	switch worldgen.GetProperty(info, s, "facing") {
	case "north":
		dz = -1
	case "south":
		dz = 1
	case "west":
		dx = -1
	case "east":
		dx = 1
	}
	if worldgen.GetProperty(info, s, "part") == "head" {
		return blockPos{pos.x - dx, pos.y, pos.z - dz}, true
	}
	return blockPos{pos.x + dx, pos.y, pos.z + dz}, true
}

// villagerMeetTick is the meeting point's POI life: a bell that is gone is
// let go (ValidateNearbyPoi), and an adult with none claims one
// (AcquirePoi(MEETING)). Villagers used to be handed the village's first
// bell at birth and keep it for life.
func (h *hub) villagerMeetTick(players map[int32]*tracked, m *mob) {
	if m.etype != entityVillager || m.dying > 0 {
		return
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	if m.meet != (blockPos{}) {
		if w.Loaded(int32(m.meet.x>>4), int32(m.meet.z>>4)) && !isBell(w.At(m.meet.x, m.meet.y, m.meet.z)) {
			m.releasePoi(poiMeet)
		}
		return
	}
	h.acquirePoi(players, m, poiMeet)
}
