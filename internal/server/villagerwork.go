package server

// Working at the job site (WorkAtPoi, and WorkAtComposter for a farmer).
// A villager standing at its workstation during the work part of the day
// gets down to it at most once every fifteen seconds: it plays its trade's
// work sound, restocks its offers if they need it, and a farmer tends its
// composter — bakes bread from its wheat and tips its spare seeds in.

const (
	workCheckCooldown  = 300 // WorkAtPoi.CHECK_COOLDOWN
	workReachSq        = 1.73 * 1.73
	composterSeedsKeep = 10 // WorkAtComposter: a farmer keeps ten of each seed for sowing
	composterSeedsMax  = 20 // …and composts at most twenty a visit
	breadKeepMax       = 36 // makeBread: only while it carries 36 bread or fewer
	breadPerVisit      = 3
	wheatPerBread      = 3
)

var (
	itemBread = int32(itemByName["bread"])
	// WorkAtComposter.COMPOSTABLE_ITEMS, in its order.
	composterSeeds = []int32{int32(itemByName["wheat_seeds"]), int32(itemByName["beetroot_seeds"])}
)

// villagerWorkTick is WorkAtPoi's start check, run each mob update: the work
// part of the day, a job site, the cooldown and a coin toss, and standing
// within 1.73 of the block's centre.
func (h *hub) villagerWorkTick(players map[int32]*tracked, m *mob) {
	if m.etype != entityVillager || m.baby || m.dying > 0 || m.sleeping || m.work == (blockPos{}) ||
		m.profession < 0 || m.profession >= len(professionNames) ||
		villagerSegment(h.dayTime.Load()) != vsWork {
		return
	}
	now := h.tick.Load()
	if m.lastWorkCheck != 0 && now-m.lastWorkCheck < workCheckCooldown {
		return
	}
	if h.rng.Intn(2) != 0 {
		return
	}
	m.lastWorkCheck = now
	cx, cy, cz := float64(m.work.x)+0.5, float64(m.work.y)+0.5, float64(m.work.z)+0.5
	if dist3sq(cx, cy, cz, m.x, m.y, m.z) >= workReachSq {
		return
	}
	// start: the work sound, the workstation, and the restock.
	h.playSoundDim(players, m.dim, "minecraft:entity.villager.work_"+professionNames[m.profession], sndNeutral, m.x, m.y, m.z, 1, h.voicePitch(m))
	if m.profession == profFarmer {
		h.workAtComposter(players, m)
	}
	if h.shouldRestock(m) {
		h.restockOffers(m)
	}
}

// workAtComposter is WorkAtComposter.useWorkstation: with a composter at the
// job site, bake bread, then empty a ready composter and feed it seeds.
func (h *hub) workAtComposter(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	pos := m.work
	level, ok := composterLevel(w.At(pos.x, pos.y, pos.z))
	if !ok {
		return
	}
	h.villagerMakeBread(players, m)
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	if level == composterReady { // extractProduce: the bone meal pops out on top
		ox, oz := (h.rng.Float64()-0.5)*0.7, (h.rng.Float64()-0.5)*0.7
		h.spawnItemIn(players, m.dim, itemBoneMeal, 1, cx+ox, float64(pos.y)+1.01, cz+oz)
		h.setBlockAt(players, m.dim, pos, composterBase)
		h.vib(m.dim, freqBlockChange, pos.x, pos.y, pos.z, m.eid)
		h.playSoundDim(players, m.dim, "minecraft:block.composter.empty", sndBlock, cx, cy, cz, 1, 1)
		level = 0
	}
	start := level
	budget := composterSeedsMax
	seen := make([]int, len(composterSeeds))
	// The pockets from the last slot to the first, keeping ten of each seed.
feed:
	for i := len(m.hoard) - 1; i >= 0 && budget > 0; i-- {
		st := &m.hoard[i]
		k := -1
		for j, s := range composterSeeds {
			if st.item == s {
				k = j
			}
		}
		if k < 0 || st.count <= 0 {
			continue
		}
		seen[k] += st.count
		use := min(seen[k]-composterSeedsKeep, budget, st.count)
		if use <= 0 {
			continue
		}
		budget -= use
		for j := 0; j < use; j++ {
			if level >= composterFull {
				break feed
			}
			level = h.composterLayer(m.dim, pos, level, st.item, m.eid)
			st.count--
			if level == composterFull {
				break feed
			}
		}
	}
	villagerCompact(m)
	// Level event 1500: the fill sound, a success if the pile rose at all.
	snd := "minecraft:block.composter.fill"
	if level != start {
		snd = "minecraft:block.composter.fill_success"
	}
	h.playSoundDim(players, m.dim, snd, sndBlock, cx, cy, cz, 1, 1)
}

// villagerMakeBread is WorkAtComposter.makeBread: up to three loaves from
// three wheat each, while it carries no more than 36; what will not fit is
// dropped at its feet.
func (h *hub) villagerMakeBread(players map[int32]*tracked, m *mob) {
	if villagerCount(m, itemBread) > breadKeepMax {
		return
	}
	n := min(breadPerVisit, villagerCount(m, itemWheat)/wheatPerBread)
	if n == 0 {
		return
	}
	for left := n * wheatPerBread; left > 0; {
		for i := range m.hoard {
			if st := &m.hoard[i]; st.item == itemWheat && st.count > 0 {
				take := min(left, st.count)
				st.count -= take
				left -= take
				break
			}
		}
	}
	villagerCompact(m)
	if put := villagerAddItem(m, invStack{item: itemBread, count: n}); put < n {
		h.spawnItemIn(players, m.dim, itemBread, n-put, m.x, m.y+0.5, m.z)
	}
}

// villagerCompact drops emptied stacks from the pockets.
func villagerCompact(m *mob) {
	out := m.hoard[:0]
	for _, st := range m.hoard {
		if st.count > 0 {
			out = append(out, st)
		}
	}
	m.hoard = out
}

// composterLayer is ComposterBlock.addLayer for one item from a mob or a
// hopper: the pile may rise one level (an empty composter always takes the
// first item), and a pile that reaches seven arms its ready tick. It returns
// the new level; the caller plays the sound.
func (h *hub) composterLayer(dim int, pos blockPos, level int, item int32, src int32) int {
	chance, ok := compostChance[item]
	if !ok || level >= composterFull {
		return level
	}
	if level != 0 && h.rng.Float64() >= chance {
		return level
	}
	level++
	h.setBlockAt(h.playersRef, dim, pos, composterBase+uint32(level))
	h.vib(dim, freqBlockChange, pos.x, pos.y, pos.z, src)
	if level == composterFull {
		h.armComposter(dim, pos)
	}
	return level
}
