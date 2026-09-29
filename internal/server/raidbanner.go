package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Raider.ObtainRaidLeaderBannerGoal (priority 1): a raider of an active raid
// whose wave has lost its leader, and who could lead itself, goes for an
// ominous banner lying within its follow range sideways and eight blocks up
// or down, at 1.15 of its pace, and picks it up once within 1.414 of it:
// Raider.pickUpItem puts the banner on its head (Raid.setLeader, a sure drop)
// and makes it the wave's captain. A banner it finds no path to is left alone
// for 600 ticks.

const (
	raidBannerSpeed  = 1.15
	raidBannerUpDown = 8.0
	raidBannerTake   = 1.414
	raidBannerRetry  = 600
)

// tickSkips holds ids to leave alone, each until its tick.
type tickSkips map[int32]uint64

// raidWaveLeader is Raid.getLeader(wave) among the living: the wave's raider
// that carries the captaincy.
func (h *hub) raidWaveLeader(r *raid, wave int) *mob {
	for eid := range r.alive {
		if o := h.mobs[eid]; o != nil && o.dying == 0 && o.raidWave == wave && o.patrolCaptain {
			return o
		}
	}
	return nil
}

// raiderCannotTakeBanner is the goal's cannotPickUpBanner.
func (h *hub) raiderCannotTakeBanner(m *mob) bool {
	if !h.raiderInActiveRaid(m) || !canBeLeader(m.etype) || m.dying > 0 {
		return true
	}
	if m.gear[0].count == 1 && sameItemComponents(m.gear[0], ominousBanner()) {
		return true
	}
	return h.raidWaveLeader(h.raidOf(m), m.raidWave) != nil
}

// raidBannerItem is Raider.ALLOWED_ITEMS: a live drop past its pickup delay
// that is exactly one ominous banner (ItemStack.matches).
func raidBannerItem(it *itemEntity, now uint64) bool {
	return it.count == 1 && now >= it.noPickupUntil && sameItemComponents(it.stack(), ominousBanner())
}

// raidBannerStep runs the goal. It reports whether it took the raider's move.
func (h *hub) raidBannerStep(players map[int32]*tracked, m *mob) bool {
	if !isRaider(m) {
		return false
	}
	if h.raiderCannotTakeBanner(m) {
		m.bannerEID = 0
		return false
	}
	now := h.tick.Load()
	it := h.items[m.bannerEID]
	if m.bannerEID != 0 && (it == nil || it.count <= 0 || it.dim != m.dim) {
		m.bannerEID, it = 0, nil // canContinueToUse: the banner is gone
	}
	if it == nil && !h.raidBannerFind(m, now) {
		return false
	}
	it = h.items[m.bannerEID]
	if dist3(it.x, it.y, it.z, m.x, m.y, m.z) < raidBannerTake {
		h.raiderTakesBanner(players, m, it)
		m.bannerEID = 0
		return true
	}
	vx, vz := h.pathSteerTo(m, blockPos{floorInt(it.x), floorInt(it.y), floorInt(it.z)}, 1)
	m.vx, m.vz = vx*raidBannerSpeed, vz*raidBannerSpeed
	m.rest = 0
	return true
}

// raidBannerFind is canUse: the first banner in reach it has a path to.
// Banners it has no path to are cached for 600 ticks; the cache keeps only
// the banners seen this scan.
func (h *hub) raidBannerFind(m *mob, now uint64) bool {
	r := m.followRange() + m.box().w/2
	seen := tickSkips{}
	defer func() { m.bannerSkip = seen }()
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	for eid, it := range h.items {
		if it.dim != m.dim || !raidBannerItem(it, now) ||
			math.Abs(it.x-m.x) > r || math.Abs(it.z-m.z) > r ||
			it.y < m.y-raidBannerUpDown || it.y > m.y+m.box().h+raidBannerUpDown {
			continue
		}
		if until, ok := m.bannerSkip[eid]; ok && now < until {
			seen[eid] = until
			continue
		}
		start := blockPos{floorInt(m.x), floorInt(m.y + 0.01), floorInt(m.z)}
		goal := blockPos{floorInt(it.x), floorInt(it.y), floorInt(it.z)}
		if reach3D(w, false, start, goal, 1, int(r)+1, poiPathNodes) {
			m.bannerEID = eid
			return true
		}
		seen[eid] = now + raidBannerRetry
	}
	return false
}

// raiderTakesBanner is Raider.pickUpItem for the ominous banner: whatever
// the raider wore on its head drops at the slot's odds, the banner goes on
// (Raid.setLeader: a sure drop) and it leads its wave.
func (h *hub) raiderTakesBanner(players map[int32]*tracked, m *mob, it *itemEntity) {
	if cur := m.gear[0]; cur.item != 0 && math.Max(h.rng.Float64()-replaceDropFudge, 0) < float64(h.gearDropChance(m, 0)) {
		h.dropGearStack(players, m, cur)
	}
	h.toTracking(players, it.eid, it.dim, it.x, it.z, attachproto.Collect{Collected: it.eid, Collector: m.eid, Count: int32(it.count)})
	delete(h.items, it.eid)
	h.entityGone(players, it.dim, it.eid)
	h.makeCaptain(players, m)
}
