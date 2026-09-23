package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Villagers, trading, and the iron golem. Villages are pure functions of the
// seed (worldgen.VillageIn): each creature its pieces carry is placed once,
// when its part of the village first comes into view (placeVillageMobs).
// Right-clicking a villager opens the merchant screen; the offer list depends
// on the villager's deterministic profession, and the trade result slot is
// server-owned like every other window.

const (
	menuMerchant       = 19
	playServerSelTrade = 0x31
)

var (
	entityIronGolem = entityID("iron_golem") // (entityVillager comes from the NPC layer)
)

// updateVillages populates villages as players approach (100-tick cadence).
func (h *hub) updateVillages(players map[int32]*tracked) {
	gen := h.world.Gen()
	for _, t := range players {
		if t.dim != dimOverworld {
			continue
		}
		px, pz := int(t.x), int(t.z)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				v := gen.VillageIn(px+dx*384, pz+dz*384)
				if !v.Exists {
					continue
				}
				well := blockPos{v.X, v.Y, v.Z}
				if h.villageSettled[well] {
					continue
				}
				// Only villages whose pieces can reach this player's view.
				if reach := villageReach + float64(t.p.radius()*16); math.Hypot(t.x-float64(v.X), t.z-float64(v.Z)) > reach {
					continue
				}
				h.placeVillageMobs(players, v, well)
			}
		}
	}
}

// villageReach is how far a village's pieces reach from its centre
// (max_distance_from_center).
const villageReach = 80

// placeVillageMobs is StructureTemplate.placeEntities for a village: every
// entity its pieces carry is placed once, when the chunk holding it is first
// in a player's view — tachyne's moment of chunk generation, when vanilla
// places a structure's entities. What has been placed persists by entity
// (type and block), so a village is populated once and only once.
//
// A village the older path populated (villageDone, no placed record) had its
// villagers spawned all at once and nothing else, so its villagers count as
// placed and the rest (the animals, and the golem and cats if it has none)
// come now.
func (h *hub) placeVillageMobs(players map[int32]*tracked, v worldgen.Village, well blockPos) {
	gen := h.world.Gen()
	mobs := gen.VillageMobs(v)
	placed := h.villagePlaced[well]
	if placed == nil {
		placed = map[string]bool{}
		if h.villageDone[well] {
			// Its golem came from the census and its cats from the cat
			// spawner; where those already live, the template's are not
			// added on top.
			hasGolem, hasCat := false, false
			for _, o := range h.mobs {
				if o.dim != dimOverworld || math.Hypot(o.x-float64(v.X), o.z-float64(v.Z)) > villageReach {
					continue
				}
				hasGolem = hasGolem || o.etype == entityIronGolem
				hasCat = hasCat || o.etype == entityCat
			}
			for _, vm := range mobs {
				if vm.Type == "villager" || vm.Type == "iron_golem" && hasGolem || vm.Type == "cat" && hasCat {
					placed[vm.Key()] = true
				}
			}
		}
		h.villagePlaced[well] = placed
	}
	h.villageDone[well] = true
	all := true
	for _, vm := range mobs {
		if placed[vm.Key()] {
			continue
		}
		if !h.chunkInView(players, dimOverworld, int32(vm.BX>>4), int32(vm.BZ>>4)) {
			all = false
			continue
		}
		placed[vm.Key()] = true
		h.spawnVillageMob(players, v, vm)
	}
	if all {
		h.villageSettled[well] = true
	}
}

// chunkInView reports whether a chunk is in some player's view window.
func (h *hub) chunkInView(players map[int32]*tracked, dim int, cx, cz int32) bool {
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		r := t.p.radius()
		pcx, pcz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		if cx >= pcx-r && cx <= pcx+r && cz >= pcz-r && cz <= pcz+r {
			return true
		}
	}
	return false
}

// spawnVillageMob places one of a village's template entities: created as
// the template's NBT has it, then finalizeSpawn(STRUCTURE) — the species'
// own spawn rolls (a cat's coat by the biome and the moon, a horse's coat,
// a sheep's fleece, a farm animal's climate variant, a zombie's equipment).
func (h *hub) spawnVillageMob(players map[int32]*tracked, v worldgen.Village, vm worldgen.VillageMob) {
	etype, ok := entityByName[vm.Type]
	if !ok {
		return
	}
	switch vm.Type {
	case "villager":
		h.spawnTemplateVillager(players, v, vm)
		return
	case "armor_stand":
		return // not a mob: the taiga armourer's stand is a block-entity stand-in here
	}
	var m *mob
	switch vm.Type {
	case "zombie_villager":
		m = h.spawnHostileY(players, etype, vm.X, vm.Y, vm.Z)
		if m != nil {
			h.setTemplateVillagerData(m, vm)
			h.sendVillagerData(players, m)
		}
	case "iron_golem":
		m = h.spawnMob(players, etype, vm.X, vm.Y, vm.Z)
		if m != nil { // the village's own golem (PlayerCreated false)
			m.health = 100
			m.setKBResist(1) // IronGolem KNOCKBACK_RESISTANCE
			m.behavior = golemBehavior{}
			m.home = blockPos{v.X, v.Y, v.Z}
		}
	default:
		m = h.spawnSpecies(players, etype, dimOverworld, vm.X, vm.Y, vm.Z)
		if m != nil && vm.Age < 0 {
			m.baby, m.growLeft = true, -vm.Age
			h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
		}
	}
	if m != nil && vm.Persist {
		m.persistent = true
	}
}

// setTemplateVillagerData applies a template entity's VillagerData.
func (h *hub) setTemplateVillagerData(m *mob, vm worldgen.VillageMob) {
	switch vm.Prof {
	case "nitwit":
		m.profession = profNitwit
	case "none", "":
		m.profession = profUnemployed
	default:
		m.profession = profUnemployed
		for i, n := range professionNames {
			if n == vm.Prof {
				m.profession = i
			}
		}
	}
	if vm.Level > 0 {
		m.tradeLevel = vm.Level
	}
}

// spawnTemplateVillager places one of the village's villagers (one per
// villagers piece) as its template has it: unemployed, a nitwit, or a baby.
// Nothing is handed to it. Like vanilla's brain, it claims a bed
// (villagerBedTick) and, if unemployed, a workstation (villagerJobTick).
// The meeting point is the bell nearest to it.
func (h *hub) spawnTemplateVillager(players map[int32]*tracked, v worldgen.Village, vm worldgen.VillageMob) {
	m := h.spawnMob(players, entityVillager, vm.X, vm.Y, vm.Z)
	if m == nil {
		return // plugin-cancelled spawn
	}
	m.setMoveSpeed(0.135) // villager MOVEMENT_SPEED
	switch {
	case vm.Prof == "nitwit":
		h.initVillagerTrades(m, profNitwit)
	case vm.Age < 0:
		m.baby, m.growLeft = true, -vm.Age
		h.initVillagerTrades(m, profUnemployed)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
	default:
		h.initVillagerTrades(m, profUnemployed)
	}
	h.sendVillagerData(players, m)
	m.meet = blockPos{v.X, v.Y, v.Z}
	best := math.MaxFloat64
	for _, b := range h.world.Gen().VillageBells(v) {
		if d := math.Hypot(float64(b[0]-vm.BX), float64(b[2]-vm.BZ)); d < best {
			best, m.meet = d, blockPos{b[0], b[1], b[2]}
		}
	}
	m.behavior = villagerBehavior{} // path home/around + open doors
	m.usesDoors = true
}

const (
	golemVillagersToAgree = 5     // vanilla Villager.spawnGolemIfNeeded villagersNeededToAgree
	golemAgreeBox         = 10.0  // the box a villager counts its neighbours in (bounding box inflated 10)
	golemSleptWithin      = 24000 // golemSpawnConditionsMet: a day's ticks since it last lay down
	golemDetectedTicks    = 600   // GolemSensor.golemDetected: the memory's life
	golemSensorRange      = 16.0  // a golem this close is one the villager can see
)

// updateVillageGolems is the GolemSensor: a villager within sight of a golem
// remembers it for thirty seconds, which is what stops a village that already
// has one from growing another. The golem itself is asked for by gossip and
// by panic (golemspawn.go), as vanilla asks; this once ran a census of every
// villager each second and spawned from it.
func (h *hub) updateVillageGolems(players map[int32]*tracked) {
	now := h.tick.Load()
	// One pass to collect the two short lists this works on; everything below
	// walks those rather than every mob in the world.
	var villagers, golems []*mob
	for _, m := range h.mobs {
		switch {
		case m.etype == entityVillager && !m.baby && m.dying == 0:
			villagers = append(villagers, m)
		case m.etype == entityIronGolem && m.dying == 0:
			golems = append(golems, m)
		}
	}
	// GolemSensor: a villager within sight of a golem remembers it, which is
	// what stops a village that already has one from growing another.
	for _, m := range villagers {
		for _, g := range golems {
			if g.dim == m.dim && dist3(g.x, g.y, g.z, m.x, m.y, m.z) < golemSensorRange {
				m.golemSeen = now + golemDetectedTicks
				break
			}
		}
	}
}

// wantsToSpawnGolem is Villager.wantsToSpawnGolem: an adult villager that lay
// down within the last day and has not seen a golem in the last thirty
// seconds.
func (h *hub) wantsToSpawnGolem(m *mob, now uint64) bool {
	if m.etype != entityVillager || m.baby || m.dying != 0 || m.lastSlept == 0 {
		return false // never lay down: no bed, or a villager that has not been home
	}
	slept := m.lastSlept - 1 // stored +1 so that zero can mean "never"
	return now >= slept && now-slept < golemSleptWithin && now >= m.golemSeen
}

// golemBehavior walks the village guardian toward the nearest hostile, or
// back home; the hub's melee runs when it's in reach (mob-vs-mob).
type golemBehavior struct{}

func (golemBehavior) name() string { return "golem" }
func (golemBehavior) steer(h *hub, m *mob) (float64, float64) {
	if p := h.golemGrudge(h.playersRef, m); p != nil {
		return (p.x - m.x) * 0.3, (p.z - m.z) * 0.3 // DefendVillageTargetGoal: a villager's enemy
	}
	var target *mob
	best := 16.0
	h.grid().nearby(m.dim, m.x, m.z, best, func(o *mob) {
		if !o.hostile || o.dying > 0 || o.etype == entityCreeper { // Enemy && !Creeper: golems leave creepers be
			return
		}
		if d := math.Hypot(o.x-m.x, o.z-m.z); d < best {
			best, target = d, o
		}
	})
	if target != nil {
		return (target.x - m.x) * 0.3, (target.z - m.z) * 0.3
	}
	// Drift home.
	hx, hz := float64(m.home.x)-m.x, float64(m.home.z)-m.z
	if math.Hypot(hx, hz) > 6 {
		return hx * 0.05, hz * 0.05
	}
	return 0, 0
}

// golemMelee punches the nearest hostile in reach (called from the mob
// update pass, which has the players map for broadcasts).
func (h *hub) golemMelee(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	if h.golemPunchPlayer(players, m) {
		return
	}
	// Pick the nearest hostile in reach via the grid, then punch it outside
	// the query — the punch may kill (killMob mutates h.mobs).
	var o *mob
	h.grid().nearby(m.dim, m.x, m.z, 2.2, func(c *mob) {
		if o != nil || !c.hostile || c.dying > 0 || c.etype == entityCreeper || dist3(c.x, c.y, c.z, m.x, m.y, m.z) > 2.2 {
			return
		}
		o = c
	})
	if o != nil {
		m.attackCD = 5                                                                         // mob-updates between swings
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusAttack)) // the arm swing
		if kdx, kdz := o.x-m.x, o.z-m.z; kdx != 0 || kdz != 0 {
			d := math.Hypot(kdx, kdz)
			o.vx, o.vz = kdx/d*1.2, kdz/d*1.2 // golems launch their victims
			o.kb = 4
			h.mobKnockVelocity(players, o)
		}
		h.playSound(players, "minecraft:entity.iron_golem.attack", sndNeutral, m.x, m.y, m.z, 1, 1)
		// vanilla IronGolem.doHurtTarget: ATTACK_DAMAGE 15 → 15/2 + nextInt(15)
		// = 7.5–21.5 per punch. Golem punches respect the target's armor.
		o.hurt(7.5 + float64(h.rng.Intn(15)))
		if o.health <= 0 {
			h.killMob(players, o)
		}
	}
}

// addTradeGossip credits a completed trade toward a player's reputation with
// this villager (vanilla onReputationEventFrom TRADE → gossips.add TRADING 2,
// capped at the type max). Reputation lowers the offer's special price.
func (h *hub) addTradeGossip(m *mob, name string) {
	m.gossip.add(name, gossipTrading, 2)
}

// updateSpecialPrices is vanilla Villager.updateSpecialPrices: recompute each
// offer's special-price delta for the player opening the merchant screen — a
// reputation discount (−floor(reputation · priceMultiplier)) plus a Hero of the
// Village discount (−max(1, floor((0.3 + 0.0625·amp) · baseCost))).
func (h *hub) updateSpecialPrices(t *tracked, m *mob) {
	rep := m.gossip.reputation(t.p.name) // 0 if absent
	heroAmp := t.hasEffect(effHeroOfVillage)
	for i := range m.offers {
		o := &m.offers[i]
		o.specialPrice = 0
		if rep != 0 {
			o.specialPrice -= int32(math.Floor(float64(rep) * o.priceMult()))
		}
		if heroAmp > 0 { // hasEffect returns amp+1 (1-based)
			d := 0.3 + 0.0625*float64(heroAmp-1)
			n3 := int(math.Floor(d * float64(o.trade.inCount)))
			if n3 < 1 {
				n3 = 1
			}
			o.specialPrice -= int32(n3)
		}
	}
}

// clearSpecialPrices resets the per-viewer special-price deltas when the
// merchant screen closes (vanilla resets specialPriceDiff on stopTrading).
func clearSpecialPrices(m *mob) {
	for i := range m.offers {
		m.offers[i].specialPrice = 0
	}
}

// openTrades shows a villager's merchant screen.
func (h *hub) openTrades(t *tracked, m *mob) {
	if t.inv == nil {
		return
	}
	// Villager.mobInteract. A sleeping villager is not interactable at all;
	// a child, an unemployed one and — the case that used to open an empty
	// screen — one whose profession has no offers left to give shake their
	// heads instead. The "talked to a villager" statistic counts either way.
	if m.sleeping {
		return
	}
	unhappy := func() {
		h.toTracking(h.playersRef, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerNo))
	}
	if m.profession < 0 || m.baby {
		unhappy()
		return
	}
	h.incCustom(t, "talked_to_villager", 1)
	if len(m.offers) == 0 {
		unhappy()
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.updateSpecialPrices(t, m) // reputation + Hero discounts for this viewer
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winKind = h.nextWin, winTrade
	t.tradeWith, t.tradeSel = m.eid, 0
	t.trade = [2]invStack{}

	title := "Villager"
	if m.etype == entityWanderingTrader {
		title = "Wandering Trader"
	}
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuMerchant), Title: title})
	h.sendTradeList(t, m)
	h.sendTradeWindow(t)
}

// sendTradeList encodes the trade_list packet for a villager's current offers.
func (h *hub) sendTradeList(t *tracked, m *mob) {
	b := protocol.AppendVarInt(nil, t.winID)
	b = protocol.AppendVarInt(b, int32(len(m.offers)))
	for i := range m.offers {
		o := &m.offers[i]
		tr := o.trade
		// The client derives the DISPLAYED cost from the base ItemCost plus the
		// special-price/multiplier/demand fields (matching costCount), so send
		// the BASE count here, not the adjusted one.
		b = protocol.AppendVarInt(b, tr.inItem) // ItemCost: id + count + no components
		b = protocol.AppendVarInt(b, tr.inCount)
		b = protocol.AppendVarInt(b, 0)
		b = appendStack(b, o.output()) // output Slot (with its enchantment, for a book)
		if o.cost2Item != 0 {          // optional second ItemCost
			b = protocol.AppendBool(b, true)
			b = protocol.AppendVarInt(b, o.cost2Item)
			b = protocol.AppendVarInt(b, o.cost2Count)
			b = protocol.AppendVarInt(b, 0)
		} else {
			b = protocol.AppendBool(b, false)
		}
		b = protocol.AppendBool(b, o.uses >= tr.maxUses) // disabled when used up
		b = protocol.AppendI32(b, o.uses)
		b = protocol.AppendI32(b, tr.maxUses)
		b = protocol.AppendI32(b, tr.xp)
		b = protocol.AppendI32(b, o.specialPrice) // reputation + Hero delta
		b = protocol.AppendF32(b, float32(o.priceMult()))
		b = protocol.AppendI32(b, o.demand)
	}
	b = protocol.AppendVarInt(b, int32(m.tradeLevel))
	b = protocol.AppendVarInt(b, int32(m.tradeXP))
	// Merchant.showProgressBar / canRestock: a villager levels up and
	// restocks, a wandering trader does neither, so its window carries no XP
	// bar and no "out of stock" restock hint.
	villager := m.etype == entityVillager
	b = protocol.AppendBool(b, villager)
	b = protocol.AppendBool(b, villager)
	t.p.trySendEv(attachproto.Trades{Data: b})
}

// sendTradeWindow refreshes the 3-slot merchant window + inventory.
func (h *hub) sendTradeWindow(t *tracked) {
	t.inv.stateId++
	slots := make([]attachproto.ItemStack, 0, 39)
	slots = append(slots, stackEv(t.trade[0]), stackEv(t.trade[1]))
	res, _ := h.tradeResult(t)
	slots = append(slots, stackEv(res))
	for i := 9; i < invSize; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	for i := 0; i < 9; i++ {
		slots = append(slots, stackEv(t.inv.slots[i]))
	}
	t.p.trySendEv(attachproto.WindowItems{ID: int32(t.winID), StateID: t.inv.stateId,
		Slots: slots, Cursor: stackEv(t.cursor)})
}

// tradeResult: the output if the current inputs satisfy the selected offer, plus
// the offer itself (nil = no valid trade / locked / not enough input).
func (h *hub) tradeResult(t *tracked) (invStack, *mobOffer) {
	m := h.mobs[t.tradeWith]
	if m == nil || t.tradeSel < 0 || t.tradeSel >= len(m.offers) {
		return invStack{}, nil
	}
	o := &m.offers[t.tradeSel]
	if o.uses >= o.trade.maxUses {
		return invStack{}, nil // exhausted until restock
	}
	have, have2 := 0, 0
	for _, in := range t.trade {
		if in.item == o.trade.inItem {
			have += in.count
		}
		if o.cost2Item != 0 && in.item == o.cost2Item {
			have2 += in.count
		}
	}
	if have < o.costCount() { // demand/reputation/Hero-adjusted price
		return invStack{}, nil
	}
	if o.cost2Item != 0 && have2 < int(o.cost2Count) { // the second cost, unadjusted
		return invStack{}, nil
	}
	return o.output(), o
}

// takeTradeResult consumes the cost and hands over the goods (AUTHORITY: the
// server recomputes the offer; the click is a wish).
func (h *hub) takeTradeResult(players map[int32]*tracked, t *tracked, mode int32) {
	res, o := h.tradeResult(t)
	if res.item == 0 || !h.canTakeResult(t, res, mode) {
		h.sendTradeWindow(t)
		return
	}
	consume := func(item int32, need int) {
		for i := range t.trade {
			if need == 0 {
				break
			}
			if t.trade[i].item != item {
				continue
			}
			take := t.trade[i].count
			if take > need {
				take = need
			}
			t.trade[i].count -= take
			need -= take
			if t.trade[i].count == 0 {
				t.trade[i] = invStack{}
			}
		}
	}
	consume(o.trade.inItem, o.costCount()) // the demand/reputation/Hero-adjusted price
	if o.cost2Item != 0 {
		consume(o.cost2Item, int(o.cost2Count))
	}
	h.resultTake(t, res, mode) // onto the cursor, or into the inventory on a shift-click
	o.uses++                   // toward this offer's lock
	if m := h.mobs[t.tradeWith]; m != nil {
		promoted := h.awardTradeXP(m, o.trade.xp) // may promote the villager + unlock trades
		// Villager.rewardTradeXp: the trader hands the PLAYER 3-6 experience
		// for the trade, and five more when that trade levelled them up.
		xp := 3 + h.rng.Intn(4)
		if promoted {
			xp += 5
		}
		h.spawnXPOrbIn(players, m.dim, xp, m.x, m.y+0.5, m.z)
		h.addTradeGossip(m, t.p.name) // build reputation → cheaper future offers
		h.updateSpecialPrices(t, m)   // reflect the new reputation immediately
		h.sendTradeList(t, m)         // refresh uses/level/price (and any new offers)
		yes := "minecraft:entity.villager.yes"
		if m.etype == entityWanderingTrader {
			yes = "minecraft:entity.wandering_trader.yes"
		}
		h.playSound(players, yes, sndNeutral, m.x, m.y, m.z, 0.7, 1)
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerHappy)) // Villager.customServerAiStep after a trade
	}
	h.advance(players, t, "villager_trade", advMatch{})
	h.incCustom(t, "traded_with_villager", 1)
	h.sendCursor(t)
	h.sendTradeWindow(t)
}

// reclaimTrade folds trade inputs back on close.
func (h *hub) reclaimTrade(players map[int32]*tracked, t *tracked) {
	for i := range t.trade {
		st := t.trade[i]
		t.trade[i] = invStack{}
		if st.item == 0 || st.count == 0 {
			continue
		}
		changed, leftover := t.inv.addStack(st)
		for _, slot := range changed {
			h.sendSlot(t, slot)
		}
		if leftover > 0 && players != nil {
			h.spawnItem(players, st.item, leftover, t.x, t.y, t.z)
		}
	}
	if m := h.mobs[t.tradeWith]; m != nil {
		clearSpecialPrices(m) // vanilla resets specialPriceDiff on stopTrading
	}
	t.tradeWith = 0
}

type evSelTrade struct {
	eid  int32
	slot int32
}

func (evSelTrade) isHubEvent() {}
