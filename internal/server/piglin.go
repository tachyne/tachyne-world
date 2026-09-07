package server

import "math"

// Piglins and gold (vanilla PiglinAi): a piglin leaves alone any player
// wearing a piece of gold armour; it picks up the gold it loves, and a gold
// ingot — dropped in front of it or held out to it — goes into its off hand
// for six seconds of admiring, after which it throws something from the
// bartering table at the nearest player. Other loved gold it simply keeps
// (and drops when it dies). A player hitting it ends the admiring and puts
// it off bartering for twenty seconds.

const (
	piglinAdmireTicks  = 119
	piglinAdmireOffTck = 400
	piglinBarterRange  = 16.0
)

var (
	// piglinLoved is #minecraft:piglin_loved.
	piglinLoved = func() map[int32]bool {
		out := map[int32]bool{}
		for _, n := range []string{"gold_ore", "deepslate_gold_ore", "nether_gold_ore", "gold_block", "gilded_blackstone",
			"light_weighted_pressure_plate", "gold_ingot", "bell", "clock", "golden_carrot", "glistering_melon_slice",
			"golden_apple", "enchanted_golden_apple", "golden_helmet", "golden_chestplate", "golden_leggings", "golden_boots",
			"golden_horse_armor", "golden_sword", "golden_pickaxe", "golden_shovel", "golden_axe", "golden_hoe", "raw_gold", "raw_gold_block"} {
			if id, ok := itemByName[n]; ok {
				out[int32(id)] = true
			}
		}
		return out
	}()
	// piglinSafeArmor is #minecraft:piglin_safe_armor.
	piglinSafeArmor = func() map[int32]bool {
		out := map[int32]bool{}
		for _, n := range []string{"golden_helmet", "golden_chestplate", "golden_leggings", "golden_boots"} {
			if id, ok := itemByName[n]; ok {
				out[int32(id)] = true
			}
		}
		return out
	}()
)

// barterEntry is one line of the piglin_bartering table.
type barterEntry struct {
	item     string
	weight   int
	min, max int
	potion   int8
	ench     int8 // soul_speed on the book and the boots
}

var barterTable = []barterEntry{
	{item: "enchanted_book", weight: 5, min: 1, max: 1, ench: enchSoulSpeed},
	{item: "iron_boots", weight: 8, min: 1, max: 1, ench: enchSoulSpeed},
	{item: "potion", weight: 8, min: 1, max: 1, potion: potFireRes},
	{item: "splash_potion", weight: 8, min: 1, max: 1, potion: potFireRes},
	{item: "potion", weight: 10, min: 1, max: 1, potion: potWater},
	{item: "iron_nugget", weight: 10, min: 10, max: 36},
	{item: "ender_pearl", weight: 10, min: 2, max: 4},
	{item: "string", weight: 20, min: 3, max: 9},
	{item: "quartz", weight: 20, min: 5, max: 12},
	{item: "obsidian", weight: 40, min: 1, max: 1},
	{item: "crying_obsidian", weight: 40, min: 1, max: 3},
	{item: "fire_charge", weight: 40, min: 1, max: 1},
	{item: "leather", weight: 40, min: 2, max: 4},
	{item: "soul_sand", weight: 40, min: 2, max: 8},
	{item: "nether_brick", weight: 40, min: 2, max: 8},
	{item: "spectral_arrow", weight: 40, min: 6, max: 12},
	{item: "gravel", weight: 40, min: 8, max: 16},
	{item: "blackstone", weight: 40, min: 8, max: 16},
}

// wearsGold is PiglinAi.isWearingSafeArmor.
func wearsGold(t *tracked) bool {
	for _, a := range t.armor {
		if piglinSafeArmor[a.item] {
			return true
		}
	}
	return false
}

// nearestPiglinPrey is nearestHuntable minus the gold-clad.
func (h *hub) nearestPiglinPrey(players map[int32]*tracked, m *mob, maxDist float64) *tracked {
	var best *tracked
	bestD2 := maxDist * maxDist
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead || t.dim != m.dim || wearsGold(t) {
			continue
		}
		if d2 := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z); d2 < bestD2 {
			best, bestD2 = t, d2
		}
	}
	return best
}

// canAdmire is PiglinAi.canAdmire for a gold ingot.
func (m *mob) canAdmire(now uint64) bool {
	return !m.baby && m.admireUntil == 0 && now >= m.admireOffUntil
}

// piglinTakesItem is PiglinAi.pickUpItem for loved items: an ingot is
// admired, the rest is hoarded. Reports whether the item was taken.
func (h *hub) piglinTakesItem(players map[int32]*tracked, m *mob, it *itemEntity) bool {
	if !piglinLoved[it.item] {
		return false
	}
	st := it.stack()
	st.count = 1
	if it.item == itemGoldIngot && m.canAdmire(h.tick.Load()) {
		h.piglinAdmire(players, m, st)
	} else {
		m.hoard = append(m.hoard, st)
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
	m.persistent = true
	return true
}

// piglinAdmire puts the ingot in the off hand and starts the timer.
func (h *hub) piglinAdmire(players map[int32]*tracked, m *mob, st invStack) {
	if m.offhand.item != 0 {
		m.hoard = append(m.hoard, m.offhand)
	}
	m.offhand = st
	m.admireUntil = h.tick.Load() + piglinAdmireTicks
	m.hasTarget = false
	h.playSoundDim(players, m.dim, "minecraft:entity.piglin.admiring_item", sndHostile, m.x, m.y, m.z, 1, 1)
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: b2i(m.held != 0)}, m.offhand, m.gear))
}

// tryBarter is PiglinAi.mobInteract: a gold ingot held out to an adult
// piglin that is not busy admiring.
func (h *hub) tryBarter(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityPiglin || m.dying > 0 || heldStack(t).item != itemGoldIngot || !m.canAdmire(h.tick.Load()) {
		return false
	}
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.piglinAdmire(players, m, invStack{item: itemGoldIngot, count: 1})
	m.vx, m.vz = 0, 0
	return true
}

// piglinAdmireTick ends the admiring: an ingot buys a throw from the
// bartering table toward the nearest player; anything else is kept.
func (h *hub) piglinAdmireTick(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	if now < m.admireUntil {
		return
	}
	m.admireUntil = 0
	item := m.offhand
	m.offhand = invStack{}
	h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: b2i(m.held != 0)}, invStack{}, m.gear))
	if item.item != itemGoldIngot {
		if item.item != 0 {
			m.hoard = append(m.hoard, item)
		}
		return
	}
	// Toward the nearest player (or a random spot beside it).
	tx, tz := m.x+float64(h.rng.Intn(3)-1), m.z+float64(h.rng.Intn(3)-1)
	if t := h.nearestHuntable(players, m.dim, m.x, m.z, piglinBarterRange); t != nil {
		tx, tz = t.x, t.z
	}
	dx, dz := tx-m.x, tz-m.z
	if hd := math.Hypot(dx, dz); hd > 1 {
		dx, dz = dx/hd, dz/hd
	}
	st := h.rollBarter()
	if it := h.spawnItemIn(players, m.dim, st.item, st.count, m.x+dx*0.8, m.y+1, m.z+dz*0.8); it != nil {
		it.ench, it.potion, it.name = st.ench, st.potion, st.name
		h.refreshItemMeta(players, it)
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
}

// rollBarter is one draw from piglin_bartering.
func (h *hub) rollBarter() invStack {
	total := 0
	for _, e := range barterTable {
		total += e.weight
	}
	roll := h.rng.Intn(total)
	for _, e := range barterTable {
		if roll -= e.weight; roll >= 0 {
			continue
		}
		st := invStack{item: int32(itemByName[e.item]), count: e.min + h.rng.Intn(e.max-e.min+1)}
		if e.potion != 0 {
			st = potionStackIn(st.item, e.potion)
		}
		if e.ench != 0 {
			st.ench[0] = enchApply{id: e.ench, lvl: int8(1 + h.rng.Intn(3))}
		}
		return st
	}
	return invStack{item: int32(itemByName["gravel"]), count: 8}
}

// piglinHurtByPlayer is PiglinAi.wasHurtBy for a player's blow.
func (h *hub) piglinHurtByPlayer(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	if m.admireUntil != 0 {
		m.admireUntil = 0
		if m.offhand.item != 0 {
			m.hoard = append(m.hoard, m.offhand)
			m.offhand = invStack{}
		}
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: b2i(m.held != 0)}, invStack{}, m.gear))
	}
	m.admireOffUntil = now + piglinAdmireOffTck
}
