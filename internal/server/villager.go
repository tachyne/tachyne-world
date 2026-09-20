package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Villagers, trading, and the iron golem. Villages are pure functions of the
// seed (worldgen.VillageIn): when a player first comes near one this session,
// the hub populates it — one villager per house plus a golem by the well.
// Right-clicking a villager opens the merchant screen; the offer list depends
// on the villager's deterministic profession, and the trade result slot is
// server-owned like every other window.

const (
	menuMerchant       = 19
	playServerSelTrade = 0x31

	villageRange = 72 // populate when a player gets this close to the well
)

var (
	entityIronGolem = entityID("iron_golem") // (entityVillager comes from the NPC layer)
)

// updateVillages populates villages as players approach (100-tick cadence).
func (h *hub) updateVillages(players map[int32]*tracked) {
	gen := h.world.Gen()
	for _, t := range players {
		px, pz := int(t.x), int(t.z)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				v := gen.VillageIn(px+dx*384, pz+dz*384)
				if !v.Exists {
					continue
				}
				well := blockPos{v.X, v.Y, v.Z}
				if h.villageDone[well] {
					continue
				}
				if math.Hypot(t.x-float64(v.X), t.z-float64(v.Z)) > villageRange {
					continue
				}
				h.villageDone[well] = true
				// The villagers are the ones the jigsaw placed (the villagers
				// pool: unemployed, a nitwit or a baby one in twelve each), as
				// vanilla's Villager entities baked in the pieces. Each takes the
				// nearest free BED as home; an unemployed one's profession is
				// claimed from the nearest JOB-SITE block; the town-centre BELL
				// is the shared meeting point.
				jobs := gen.VillageJobSites(v)
				beds := gen.VillageBeds(v)
				meet := blockPos{v.X, v.Y, v.Z}
				if bells := gen.VillageBells(v); len(bells) > 0 {
					meet = blockPos{bells[0][0], bells[0][1], bells[0][2]}
				}
				usedJobs, usedBeds := map[blockPos]bool{}, map[blockPos]bool{}
				for _, sp := range gen.VillageVillagers(v) {
					m := h.spawnMob(players, entityVillager, float64(sp.X)+0.5, float64(sp.Y), float64(sp.Z)+0.5)
					if m == nil {
						continue // plugin-cancelled spawn
					}
					m.setMoveSpeed(0.135) // villager MOVEMENT_SPEED (vanilla 1.21.5)
					bed, hasBed := nearestFreeBed([3]int{sp.X, sp.Y, sp.Z}, beds, usedBeds)
					if hasBed {
						m.home = blockPos{bed[0], bed[1], bed[2]}
						m.bed = m.home
					}
					switch sp.Kind {
					case "nitwit":
						h.initVillagerTrades(m, profNitwit)
					case "baby":
						m.baby, m.growLeft = true, growUpTicks
						h.initVillagerTrades(m, profUnemployed)
						h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(babyMeta(m.eid, true)))
					default:
						prof, work := nearestJobSite(bed, jobs, usedJobs)
						if !hasBed {
							prof, work = nearestJobSite([3]int{sp.X, sp.Y, sp.Z}, jobs, usedJobs)
						}
						h.initVillagerTrades(m, prof)
						m.work = work
					}
					h.sendVillagerData(players, m)
					m.meet = meet
					m.behavior = villagerBehavior{} // path home/around + open doors
					m.usesDoors = true
				}
				// The golem is NOT spawned here unconditionally — vanilla spawns
				// one only once ≥5 villagers "agree", and re-spawns as the village
				// grows or its golem dies. updateVillageGolems drives that census.
			}
		}
	}
}

const (
	golemVillagersToAgree = 5    // vanilla Villager.spawnGolemIfNeeded villagersNeededToAgree
	golemNearRange        = 24.0 // an existing golem this close counts as "the village has one"
	golemRespawnDelay     = 1200 // ticks before a village re-agrees after losing/lacking a golem
)

// updateVillageGolems is vanilla Villager.spawnGolemIfNeeded, hoisted to the
// village scale: a meeting point (bell) with at least 5 living villagers and no
// nearby iron golem grows one, then waits out a cooldown before agreeing again.
// This replaces the old one-golem-per-village-forever spawn: hamlets under the
// quorum get none, large villages re-spawn a golem after theirs dies.
func (h *hub) updateVillageGolems(players map[int32]*tracked) {
	// Census villagers by their shared meeting point.
	census := map[blockPos]int{}
	for _, m := range h.mobs {
		if m.etype == entityVillager && m.meet != (blockPos{}) && !m.baby {
			census[m.meet]++
		}
	}
	for meet, n := range census {
		if n < golemVillagersToAgree {
			continue
		}
		if now := h.tick.Load(); now < h.villageGolem[meet] {
			continue // cooling down since the last spawn/agreement
		}
		golemNear := false
		for _, g := range h.mobs {
			if g.etype == entityIronGolem && g.dim == 0 &&
				dist3(g.x, g.y, g.z, float64(meet.x)+0.5, float64(meet.y), float64(meet.z)+0.5) < golemNearRange {
				golemNear = true
				break
			}
		}
		if golemNear {
			continue
		}
		g := h.spawnMob(players, entityIronGolem,
			float64(meet.x)+0.5, float64(meet.y), float64(meet.z)+2.5)
		if g == nil {
			continue // plugin-cancelled spawn
		}
		g.health = 100
		g.setKBResist(1) // IronGolem KNOCKBACK_RESISTANCE
		g.behavior = golemBehavior{}
		g.home = meet
		h.villageGolem[meet] = h.tick.Load() + golemRespawnDelay
	}
}

// nearestJobSite returns the profession index + work position of the job-site
// block nearest a villager's bed (a village with no job sites leaves it a farmer
// working at its bed).
func nearestJobSite(bed [3]int, jobs [][4]int, used map[blockPos]bool) (int, blockPos) {
	prof, work, best := profUnemployed, blockPos{}, 1<<30
	for _, j := range jobs {
		pos := blockPos{j[0], j[1], j[2]}
		if used[pos] {
			continue // one villager per workstation (PoiManager occupancy)
		}
		dx, dy, dz := j[0]-bed[0], j[1]-bed[1], j[2]-bed[2]
		if d := dx*dx + dy*dy + dz*dz; d < best {
			best, prof, work = d, j[3], pos
		}
	}
	if work != (blockPos{}) {
		used[work] = true
	}
	return prof, work
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
	if m.profession < 0 || m.baby { // Villager.mobInteract: NONE and children shake their heads
		h.toTracking(h.playersRef, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusVillagerNo))
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
	h.incCustom(t, "talked_to_villager", 1)
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
	b = protocol.AppendBool(b, true) // regular villager (show progress bar)
	b = protocol.AppendBool(b, true) // can restock
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

// nearestFreeBed is the unclaimed village bed nearest a villager's spawn
// (its home, as vanilla's villagers claim the nearest free bed).
func nearestFreeBed(at [3]int, beds [][3]int, used map[blockPos]bool) ([3]int, bool) {
	var best [3]int
	bestD, found := 1<<30, false
	for _, b := range beds {
		p := blockPos{b[0], b[1], b[2]}
		if used[p] {
			continue
		}
		dx, dy, dz := b[0]-at[0], b[1]-at[1], b[2]-at[2]
		if d := dx*dx + dy*dy + dz*dz; d < bestD {
			best, bestD, found = b, d, true
		}
	}
	if found {
		used[blockPos{best[0], best[1], best[2]}] = true
	}
	return best, found
}
