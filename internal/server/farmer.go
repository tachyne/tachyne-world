package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Farmers. Vanilla's villagers carry an eight-slot inventory and pick up
// what they want off the ground (seeds, crops, bread; a farmer also wheat
// seeds, beetroot seeds and bone meal), walking up to four blocks for it
// (GoToWantedItem). At work a farmer runs HarvestFarmland: it takes a
// mature crop in reach (the block breaks with its drops), sows a seed from
// its pockets on bare farmland, and moves on to the next, two hundred ticks
// at a stretch with forty off; with bone meal in its pockets it runs
// UseBonemeal on a growing crop, a pinch every forty ticks for eighty, then
// a hundred and sixty off. tachyne's villagers had no pockets and no
// fields: the crops around a village stood until a player came.

const (
	villagerInvSlots  = 8   // SimpleContainer(8)
	villagerStackMax  = 64  //
	wantedItemWalk    = 4.0 // GoToWantedItem maxDistToWalk
	villagerPickReach = 1.3 // Mob.getPickupReach: the 0.6 box inflated 1
	farmWorkTicks     = 200 // HarvestFarmland.HARVEST_DURATION
	farmRestTicks     = 40  // stop(): nextOkStartTime = now + 40
	farmSwitchTicks   = 20  // a new plot mid-session: nextOkStartTime = now + 20
	farmSpeed         = 0.5 // SPEED_MODIFIER
	farmReach         = 1.0 // closerToCenterThan(pos, 1.0)
	farmFieldRange    = 8   // the secondary job site: farmland within this of the villager (vanilla walks to it first)
	bonemealSession   = 80  // UseBonemeal.BONEMEALING_DURATION
	bonemealGap       = 160 // between sessions
	bonemealCycle     = 40  // between pinches
)

var (
	profFarmer = int(professionRegistryID["farmer"])

	// villagerPicksUp is #villager_picks_up (its #villager_plantable_seeds
	// folded in) and farmerRequested the farmer profession's requestedItems.
	villagerPicksUp = itemSet("wheat_seeds", "potato", "carrot", "beetroot_seeds", "torchflower_seeds", "pitcher_pod", "bread", "wheat", "beetroot")
	farmerRequested = itemSet("wheat", "wheat_seeds", "beetroot_seeds", "bone_meal")
	plantableSeeds  = itemSet("wheat_seeds", "potato", "carrot", "beetroot_seeds", "torchflower_seeds", "pitcher_pod")
)

// villagerWants is Villager.wantsToPickUp: the tag, or the profession's
// requested items, and room for it.
func villagerWants(m *mob, item int32) bool {
	if !villagerPicksUp[item] && !(m.profession == profFarmer && farmerRequested[item]) {
		return false
	}
	return villagerInvRoom(m, item) > 0
}

// villagerInvRoom is how many of item the pockets can still take.
func villagerInvRoom(m *mob, item int32) int {
	room := 0
	for _, st := range m.hoard {
		if st.item == item && st.count < villagerStackMax {
			room += villagerStackMax - st.count
		}
	}
	if len(m.hoard) < villagerInvSlots {
		room += (villagerInvSlots - len(m.hoard)) * villagerStackMax
	}
	return room
}

// villagerAddItem is SimpleContainer.addItem: tops up matching stacks, then
// fills empty slots; returns how many went in.
func villagerAddItem(m *mob, st invStack) int {
	left := st.count
	for i := range m.hoard {
		if left == 0 {
			break
		}
		if s := &m.hoard[i]; s.item == st.item && s.count < villagerStackMax {
			n := min(left, villagerStackMax-s.count)
			s.count += n
			left -= n
		}
	}
	for left > 0 && len(m.hoard) < villagerInvSlots {
		n := min(left, villagerStackMax)
		m.hoard = append(m.hoard, invStack{item: st.item, count: n})
		left -= n
	}
	return st.count - left
}

// villagerCount is countItem.
func villagerCount(m *mob, item int32) int {
	n := 0
	for _, st := range m.hoard {
		if st.item == item {
			n += st.count
		}
	}
	return n
}

// villagerTake removes one of item (or the first matching the set) from the
// pockets; returns what was taken (0 = none).
func villagerTake(m *mob, want map[int32]bool) int32 {
	for i := range m.hoard {
		if s := &m.hoard[i]; want[s.item] && s.count > 0 {
			item := s.item
			if s.count--; s.count == 0 {
				m.hoard = append(m.hoard[:i], m.hoard[i+1:]...)
			}
			return item
		}
	}
	return 0
}

// villagerPickupStep is GoToWantedItem + InventoryCarrier.pickUpItem: the
// nearest wanted item within four blocks draws the villager over at half
// pace, and one in reach goes into its pockets. Returns whether it holds
// the villager.
func (h *hub) villagerPickupStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityVillager || m.dying > 0 || m.baby {
		return false
	}
	now := h.tick.Load()
	var best *itemEntity
	bestD := wantedItemWalk
	for _, it := range h.items {
		if it.dim != m.dim || it.count <= 0 || now < it.noPickupUntil || !villagerWants(m, it.item) {
			continue
		}
		if d := dist3(it.x, it.y, it.z, m.x, m.y, m.z); d < bestD {
			best, bestD = it, d
		}
	}
	if best == nil {
		return false
	}
	if math.Abs(best.x-m.x) <= villagerPickReach && math.Abs(best.z-m.z) <= villagerPickReach && best.y >= m.y-1 && best.y <= m.y+m.box().h {
		n := villagerAddItem(m, best.stack())
		if n == 0 {
			return false
		}
		if best.count -= n; best.count <= 0 {
			delete(h.items, best.eid)
			h.entityGone(players, best.dim, best.eid)
		} else {
			h.refreshItemMeta(players, best)
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
		return true
	}
	h.steerTo(m, best.x, best.z, farmSpeed)
	return true
}

// isMaxAgeCrop is CropBlock.isMaxAge for the staged crops.
func isMaxAgeCrop(s uint32) bool {
	for _, r := range cropRanges {
		if s == r[1] {
			return true
		}
	}
	return false
}

// farmValidPos is HarvestFarmland.validPos: a mature crop, or air over
// farmland.
func (h *hub) farmValidPos(dim int, p blockPos) bool {
	w := h.worldFor(dim)
	s := w.At(p.x, p.y, p.z)
	return isMaxAgeCrop(s) || (s == worldgen.Air && isFarmland(w.At(p.x, p.y-1, p.z)))
}

// farmerFindPlot picks a valid plot at random within r blocks sideways and
// one up or down (vanilla looks one block around itself, having walked to
// its field first).
func (h *hub) farmerFindPlot(m *mob, r int) (blockPos, bool) {
	mx, my, mz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	var pick blockPos
	n := 0
	for x := -r; x <= r; x++ {
		for y := -1; y <= 1; y++ {
			for z := -r; z <= r; z++ {
				if p := (blockPos{mx + x, my + y, mz + z}); h.farmValidPos(m.dim, p) {
					n++
					if h.rng.Intn(n) == 0 {
						pick = p
					}
				}
			}
		}
	}
	return pick, n > 0
}

// farmerAtWork: a farmer, at work, with mob griefing on.
func (h *hub) farmerAtWork(m *mob) bool {
	return m.etype == entityVillager && m.profession == profFarmer && !m.baby && m.dying == 0 &&
		h.rules.MobGriefing && villagerSegment(h.dayTime.Load()) == vsWork
}

// farmerStep is HarvestFarmland for one villager each mob update; returns
// whether it holds the villager.
func (h *hub) farmerStep(players map[int32]*tracked, m *mob) bool {
	if !h.farmerAtWork(m) {
		m.farmPos = blockPos{}
		return false
	}
	now := h.tick.Load()
	if m.farmPos == (blockPos{}) {
		if now < m.farmNext {
			return false
		}
		p, ok := h.farmerFindPlot(m, farmFieldRange)
		if !ok {
			m.farmNext = now + farmRestTicks
			return false
		}
		m.farmPos, m.farmWorked = p, 0
	}
	if m.farmWorked >= farmWorkTicks { // canStillUse: two hundred ticks a session
		m.farmPos, m.farmNext = blockPos{}, now+farmRestTicks
		return false
	}
	p := m.farmPos
	tx, ty, tz := float64(p.x)+0.5, float64(p.y), float64(p.z)+0.5
	if dist3sq(tx, ty, tz, m.x, m.y, m.z) > farmReach*farmReach {
		h.steerTo(m, tx, tz, farmSpeed)
		return true
	}
	m.vx, m.vz = 0, 0
	m.farmWorked += mobMoveInterval
	if now < m.farmNext {
		return true
	}
	w := h.worldFor(m.dim)
	state := w.At(p.x, p.y, p.z)
	if isMaxAgeCrop(state) { // destroyBlock(pos, true, body)
		if h.rules.DoTileDrops {
			for _, d := range h.evalBlockLoot(lootCtx{state: state, rng: h.rng.Intn, randf: h.rng.Float64}) {
				h.spawnBlockDrop(players, m.dim, d.item, d.count, p.x, p.y, p.z)
			}
		}
		h.setBlockAt(players, m.dim, p, worldgen.Air)
		state = worldgen.Air
	}
	if state == worldgen.Air && isFarmland(w.At(p.x, p.y-1, p.z)) {
		if seed := villagerTake(m, plantableSeeds); seed != 0 {
			if c, ok := cropForSeed(seed); ok {
				h.setBlockAt(players, m.dim, p, c.block)
				h.playSoundDim(players, m.dim, "minecraft:item.crop.plant", sndBlock, tx, ty, tz, 1, 1)
				state = c.block
			}
		}
	}
	if isCropState(state) && !isMaxAgeCrop(state) { // a growing crop: on to the next plot, or done
		if next, ok := h.farmerFindPlot(m, 1); ok && next != p {
			m.farmPos, m.farmNext = next, now+farmSwitchTicks
		} else {
			m.farmPos, m.farmNext = blockPos{}, now+farmRestTicks
		}
	}
	return true
}

// farmerBonemealStep is UseBonemeal: with bone meal in its pockets, every
// hundred and sixty ticks a farmer picks a growing crop about it and
// feeds it a pinch every forty ticks for eighty. Returns whether it holds
// the villager.
func (h *hub) farmerBonemealStep(players map[int32]*tracked, m *mob) bool {
	if !h.farmerAtWork(m) {
		m.bmPos = blockPos{}
		return false
	}
	now := h.tick.Load()
	if m.bmPos == (blockPos{}) {
		if now%10 != 0 || (m.bmLast != 0 && m.bmLast+bonemealGap > now) || villagerCount(m, itemBoneMeal) == 0 {
			return false
		}
		p, ok := h.farmerFindCrop(m, farmFieldRange)
		if !ok {
			return false
		}
		m.bmPos, m.bmWorked, m.bmNext = p, 0, now
	}
	if m.bmWorked >= bonemealSession {
		m.bmPos, m.bmLast = blockPos{}, now
		return false
	}
	p := m.bmPos
	tx, ty, tz := float64(p.x)+0.5, float64(p.y), float64(p.z)+0.5
	if dist3sq(tx, ty, tz, m.x, m.y, m.z) > farmReach*farmReach {
		h.steerTo(m, tx, tz, farmSpeed)
		return true
	}
	m.vx, m.vz = 0, 0
	if now < m.bmNext {
		return true
	}
	m.bmWorked += mobMoveInterval
	state := h.worldFor(m.dim).At(p.x, p.y, p.z)
	if villagerCount(m, itemBoneMeal) > 0 && isCropState(state) && !isMaxAgeCrop(state) && h.applyBoneMeal(players, m.dim, p.x, p.y, p.z, state) {
		villagerTake(m, map[int32]bool{itemBoneMeal: true})
		h.spawnParticles(players, m.dim, particleHappyVillager, tx, ty+0.5, tz, 0.5, 0, 15) // levelEvent 1505
		if next, ok := h.farmerFindCrop(m, 1); ok {
			m.bmPos = next
		} else {
			m.bmPos, m.bmLast = blockPos{}, now
			return false
		}
		m.bmNext = now + bonemealCycle
	} else if !isCropState(state) || isMaxAgeCrop(state) {
		m.bmPos, m.bmLast = blockPos{}, now
		return false
	}
	return true
}

// farmerFindCrop is UseBonemeal.pickNextTarget: a growing crop about the
// villager, reservoir-sampled.
func (h *hub) farmerFindCrop(m *mob, r int) (blockPos, bool) {
	w := h.worldFor(m.dim)
	mx, my, mz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	var pick blockPos
	n := 0
	for x := -r; x <= r; x++ {
		for y := -1; y <= 1; y++ {
			for z := -r; z <= r; z++ {
				if s := w.At(mx+x, my+y, mz+z); isCropState(s) && !isMaxAgeCrop(s) {
					n++
					if h.rng.Intn(n) == 0 {
						pick = blockPos{mx + x, my + y, mz + z}
					}
				}
			}
		}
	}
	return pick, n > 0
}
