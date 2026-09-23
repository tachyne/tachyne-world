package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// How a villager claims its bed, its workstation and its meeting bell:
// vanilla's AcquirePoi, over the world's point-of-interest index
// (world/poi.go).
//
// Every 20-40 ticks, while it holds none, it takes the points of that kind
// within 48 blocks that have room (a bed or workstation serves one, a bell
// thirty-two). It skips any it recently failed to reach, keeps the five
// closest and paths to them. It claims the first it can reach. Those it
// could not reach are retried on a growing, jittered delay (+40-80 ticks
// each time, at most 400) and forgotten once 400 ticks pass without a try.
//
// A claim is let go when the villager has been unable to reach it for too
// long (SetWalkTargetFromBlockMemory): 1200 ticks for a bed or workstation,
// 200 for a bell. It also goes when its block does (ValidateNearbyPoi).
//
// This replaced a ±16/±4 block scan with no reachability, which never let
// go of an unreachable claim, and a meeting bell fixed at the village's
// first bell for life.

// Point-of-interest kinds, the world index's kind byte.
const (
	poiKindHome    uint8 = 1
	poiKindMeeting uint8 = 2
	poiKindJob     uint8 = 10 // + the profession index
)

// The three claims a villager makes.
const (
	poiHome = iota
	poiJob
	poiMeet
	poiGroups
)

const (
	poiScanRange      = 48 // AcquirePoi.SCAN_RANGE
	poiBatch          = 5  // the closest five are pathed to
	poiRetryMin       = 40 // JitteredLinearRetry: +40..79 ticks a try
	poiRetryJitter    = 40
	poiRetryMax       = 400
	poiRetryForget    = 400
	poiMeetingTickets = 32 // a bell's maxTickets
	poiPathNodes      = 1500
)

// poiValidRange is each kind's PoiType.validRange: how close a path must end.
var poiValidRange = [poiGroups]int{poiHome: 1, poiJob: 1, poiMeet: 6}

// poiUnreachableFor is SetWalkTargetFromBlockMemory's tooLongUnreachableDuration.
var poiUnreachableFor = [poiGroups]uint64{poiHome: 1200, poiJob: 1200, poiMeet: 200}

// poiKindOf classifies a block state for the index: a bed's head half, a
// bell, or a workstation (of its profession).
func poiKindOf(s uint32) uint8 {
	if info, ok := worldgen.InfoForState(s); ok && isBed(info) {
		if worldgen.GetProperty(info, s, "part") == "head" {
			return poiKindHome
		}
		return 0
	}
	if isBell(s) {
		return poiKindMeeting
	}
	if p := jobBlockProfession(s); p >= 0 {
		return poiKindJob + uint8(p)
	}
	return 0
}

// poiWorld returns a dimension's world with its index classifier installed.
func (h *hub) poiWorld(dim int) *world.World {
	w := h.worldFor(dim)
	if w != nil && !w.HasPOIKinds() {
		w.SetPOIKinds(poiKindOf)
	}
	return w
}

// poiRetry is AcquirePoi.JitteredLinearRetry.
type poiRetry struct {
	prev, next uint64
	delay      int
}

func (r *poiRetry) mark(h *hub, now uint64) {
	r.prev = now
	r.delay = min(r.delay+h.rng.Intn(poiRetryJitter)+poiRetryMin, poiRetryMax)
	r.next = now + uint64(r.delay)
}

// acquirePoi runs one claim's AcquirePoi for a villager, on its own schedule.
func (h *hub) acquirePoi(players map[int32]*tracked, m *mob, g int) {
	if m.poiHeld(g) {
		return
	}
	if g != poiHome && m.baby {
		return // onlyIfAdult: workstations and bells
	}
	if g == poiJob && m.profession == profNitwit {
		return // a nitwit's acquirable job site is none
	}
	now := h.tick.Load()
	if m.poiAt[g] == 0 {
		m.poiAt[g] = now + uint64(h.rng.Intn(20))
		return
	}
	if now < m.poiAt[g] {
		return
	}
	m.poiAt[g] = now + 20 + uint64(h.rng.Intn(20))
	if m.poiRetries[g] == nil {
		m.poiRetries[g] = map[blockPos]*poiRetry{}
	}
	retries := m.poiRetries[g]
	for p, r := range retries {
		if now-r.prev >= poiRetryForget {
			delete(retries, p)
		}
	}
	w := h.poiWorld(m.dim)
	if w == nil {
		return
	}
	// findAllClosestFirstWithType(type, cacheTest, pos, 48, HAS_SPACE): the
	// retry test runs over every candidate, then the closest five are kept.
	cands := w.POIsNear(floorInt(m.x), floorInt(m.y), floorInt(m.z), poiScanRange, func(p world.POI) bool {
		pos := blockPos{p.X, p.Y, p.Z}
		if !h.poiKindWanted(m, g, p.Kind) || !h.poiHasSpace(m, g, pos) {
			return false
		}
		if r := retries[pos]; r != nil {
			if now < r.next {
				return false
			}
			r.mark(h, now)
		}
		return true
	})
	if len(cands) > poiBatch {
		cands = cands[:poiBatch]
	}
	var picked []blockPos
	for _, c := range cands {
		pos := blockPos{c.X, c.Y, c.Z}
		if g == poiHome && !bedFree(w.At(pos.x, pos.y, pos.z)) {
			continue // validateBedPoi: a bed someone lies in is no home to claim
		}
		picked = append(picked, pos)
	}
	for _, pos := range picked {
		if h.poiReachable(m, pos, poiValidRange[g]) {
			h.claimPoi(players, m, g, pos)
			clear(retries)
			return
		}
	}
	for _, pos := range picked {
		if retries[pos] == nil {
			r := &poiRetry{}
			r.mark(h, now)
			retries[pos] = r
		}
	}
}

// poiHeld reports whether the villager already holds (or, for a workstation,
// is on its way to) this kind of point.
func (m *mob) poiHeld(g int) bool {
	switch g {
	case poiHome:
		return m.bed != (blockPos{})
	case poiJob:
		return m.work != (blockPos{}) || m.jobPos != (blockPos{})
	default:
		return m.meet != (blockPos{})
	}
}

// poiKindWanted is the acquirable type: any bed, any bell, and for a
// workstation its own profession's (or any, while unemployed).
func (h *hub) poiKindWanted(m *mob, g int, kind uint8) bool {
	switch g {
	case poiHome:
		return kind == poiKindHome
	case poiMeet:
		return kind == poiKindMeeting
	}
	if kind < poiKindJob {
		return false
	}
	prof := int(kind - poiKindJob)
	return m.profession < 0 || prof == m.profession
}

// poiHasSpace is PoiManager.Occupancy.HAS_SPACE: the point has a free ticket.
func (h *hub) poiHasSpace(m *mob, g int, pos blockPos) bool {
	switch g {
	case poiHome:
		return !h.bedClaimed(pos, m)
	case poiJob:
		return !h.jobSiteClaimed(pos, m)
	}
	n := 0
	for _, o := range h.mobs {
		if o != m && o.etype == entityVillager && o.dying == 0 && o.dim == m.dim && o.meet == pos {
			n++
		}
	}
	return n < poiMeetingTickets
}

// bedFree is validateBedPoi: a bed nobody is lying in.
func bedFree(s uint32) bool {
	info, ok := worldgen.InfoForState(s)
	return ok && isBed(info) && worldgen.GetProperty(info, s, "occupied") != "true"
}

// poiReachable is findPathToPois + Path.canReach: a walk from the villager
// that ends within the point's valid range, over cells (reach3d.go), so a
// bed indoors or up a storey is judged as it stands.
func (h *hub) poiReachable(m *mob, pos blockPos, validRange int) bool {
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	start := blockPos{floorInt(m.x), floorInt(m.y + 0.01), floorInt(m.z)}
	return reach3D(w, m.usesDoors || m.etype == entityVillager, start, pos, validRange, poiScanRange, poiPathNodes)
}

// claimPoi takes the point: a bed becomes home, a bell the meeting point
// (both with the happy-villager burst), a workstation the potential job site
// the villager then walks to.
func (h *hub) claimPoi(players map[int32]*tracked, m *mob, g int, pos blockPos) {
	switch g {
	case poiHome:
		m.bed, m.home = pos, pos
	case poiJob:
		m.jobPos = pos
		return
	case poiMeet:
		m.meet = pos
	}
	h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerHappy))
}

// releasePoi drops a claim (Villager.releasePoi + the memory's erase).
func (m *mob) releasePoi(g int) {
	switch g {
	case poiHome:
		m.bed, m.home = blockPos{}, blockPos{}
	case poiJob:
		m.work, m.jobPos = blockPos{}, blockPos{}
	case poiMeet:
		m.meet = blockPos{}
	}
	m.cantReachSince[g] = 0
}

// noteWalkToPoi is MoveToTargetSink's CANT_REACH_WALK_TARGET_SINCE feeding
// SetWalkTargetFromBlockMemory: after the villager paths toward a claimed
// point, a failed path starts the clock, a good one stops it, and a claim
// unreachable for too long is let go. Only a target inside the path search's
// reach is judged; beyond it a partial path toward it is the normal case.
func (h *hub) noteWalkToPoi(m *mob, g int, pos blockPos) {
	if max(abs(floorInt(m.x)-pos.x), abs(floorInt(m.z)-pos.z)) <= poiValidRange[g] {
		m.cantReachSince[g] = 0
		return
	}
	if math.Hypot(float64(pos.x)-m.x, float64(pos.z)-m.z) > pathMaxRange {
		return
	}
	now := h.tick.Load()
	if m.pathReached {
		m.cantReachSince[g] = 0
		return
	}
	if m.cantReachSince[g] == 0 {
		m.cantReachSince[g] = now
		return
	}
	if now-m.cantReachSince[g] > poiUnreachableFor[g] {
		m.releasePoi(g)
	}
}
