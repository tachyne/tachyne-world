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
				// The economy is built from the real vanilla village pieces: one
				// villager per BED, its profession claimed from the nearest JOB-SITE
				// block, the town-centre BELL as the shared meeting point.
				jobs := gen.VillageJobSites(v)
				meet := blockPos{v.X, v.Y, v.Z}
				if bells := gen.VillageBells(v); len(bells) > 0 {
					meet = blockPos{bells[0][0], bells[0][1], bells[0][2]}
				}
				for _, bed := range gen.VillageBeds(v) {
					m := h.spawnMob(players, entityVillager,
						float64(bed[0])+0.5, float64(bed[1]), float64(bed[2])+0.5)
					if m == nil {
						continue // plugin-cancelled spawn
					}
					m.speed = 0.135 // villager MOVEMENT_SPEED (vanilla 1.21.5)
					prof, work := nearestJobSite(bed, jobs)
					h.initVillagerTrades(m, prof)
					m.home = blockPos{bed[0], bed[1], bed[2]}
					m.bed = blockPos{bed[0], bed[1], bed[2]}
					m.work = work
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
		g.noKB = true // KNOCKBACK_RESISTANCE 1.0 (vanilla 1.21.5)
		g.behavior = golemBehavior{}
		g.home = meet
		h.villageGolem[meet] = h.tick.Load() + golemRespawnDelay
	}
}

// nearestJobSite returns the profession index + work position of the job-site
// block nearest a villager's bed (a village with no job sites leaves it a farmer
// working at its bed).
func nearestJobSite(bed [3]int, jobs [][4]int) (int, blockPos) {
	prof, work, best := 0, blockPos{bed[0], bed[1], bed[2]}, 1<<30
	for _, j := range jobs {
		dx, dy, dz := j[0]-bed[0], j[1]-bed[1], j[2]-bed[2]
		if d := dx*dx + dy*dy + dz*dz; d < best {
			best, prof, work = d, j[3], blockPos{j[0], j[1], j[2]}
		}
	}
	return prof, work
}

// golemBehavior walks the village guardian toward the nearest hostile, or
// back home; the hub's melee runs when it's in reach (mob-vs-mob).
type golemBehavior struct{}

func (golemBehavior) name() string { return "golem" }
func (golemBehavior) steer(h *hub, m *mob) (float64, float64) {
	var target *mob
	best := 16.0
	for _, o := range h.mobs {
		if !o.hostile || o.dying > 0 || o.dim != m.dim {
			continue
		}
		if d := math.Hypot(o.x-m.x, o.z-m.z); d < best {
			best, target = d, o
		}
	}
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
	for _, o := range h.mobs {
		if !o.hostile || o.dying > 0 || o.dim != m.dim {
			continue
		}
		if dist3(o.x, o.y, o.z, m.x, m.y, m.z) > 2.2 {
			continue
		}
		m.attackCD = 5 // mob-updates between swings
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
		return
	}
}

// gossipTradeMax is the TRADING gossip cap (vanilla GossipType.TRADING.max),
// and gossipTradePerTrade the +2 each completed trade adds.
const (
	gossipTradeMax      = 25
	gossipTradePerTrade = 2
)

// addTradeGossip credits a completed trade toward a player's reputation with
// this villager (vanilla onReputationEventFrom TRADE → gossips.add TRADING 2,
// capped at the type max). Reputation lowers the offer's special price.
func (h *hub) addTradeGossip(m *mob, name string) {
	if m.gossip == nil {
		m.gossip = map[string]int{}
	}
	if v := m.gossip[name] + gossipTradePerTrade; v < gossipTradeMax {
		m.gossip[name] = v
	} else {
		m.gossip[name] = gossipTradeMax
	}
}

// updateSpecialPrices is vanilla Villager.updateSpecialPrices: recompute each
// offer's special-price delta for the player opening the merchant screen — a
// reputation discount (−floor(reputation · priceMultiplier)) plus a Hero of the
// Village discount (−max(1, floor((0.3 + 0.0625·amp) · baseCost))).
func (h *hub) updateSpecialPrices(t *tracked, m *mob) {
	rep := m.gossip[t.p.name] // 0 if absent
	heroAmp := t.hasEffect(effHeroOfVillage)
	for i := range m.offers {
		o := &m.offers[i]
		o.specialPrice = 0
		if rep != 0 {
			o.specialPrice -= int32(math.Floor(float64(rep) * tradePriceMultiplier))
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

	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuMerchant), Title: "Villager"})
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
		b = appendStack(b, invStack{item: tr.outItem, count: int(tr.outCount)}) // output Slot
		b = protocol.AppendBool(b, false)                                       // no second cost
		b = protocol.AppendBool(b, o.uses >= tr.maxUses)                        // disabled when used up
		b = protocol.AppendI32(b, o.uses)
		b = protocol.AppendI32(b, tr.maxUses)
		b = protocol.AppendI32(b, tr.xp)
		b = protocol.AppendI32(b, o.specialPrice) // reputation + Hero delta
		b = protocol.AppendF32(b, float32(tradePriceMultiplier))
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
	have := 0
	for _, in := range t.trade {
		if in.item == o.trade.inItem {
			have += in.count
		}
	}
	if have < o.costCount() { // demand/reputation/Hero-adjusted price
		return invStack{}, nil
	}
	return invStack{item: o.trade.outItem, count: int(o.trade.outCount)}, o
}

// takeTradeResult consumes the cost and hands over the goods (AUTHORITY: the
// server recomputes the offer; the click is a wish).
func (h *hub) takeTradeResult(players map[int32]*tracked, t *tracked) {
	res, o := h.tradeResult(t)
	if res.item == 0 || t.cursor.item != 0 {
		h.sendTradeWindow(t)
		return
	}
	need := o.costCount() // charge the demand/reputation/Hero-adjusted price
	for i := range t.trade {
		if need == 0 {
			break
		}
		if t.trade[i].item != o.trade.inItem {
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
	t.cursor = res
	o.uses++ // toward this offer's lock
	if m := h.mobs[t.tradeWith]; m != nil {
		h.awardTradeXP(m, o.trade.xp) // may promote the villager + unlock trades
		h.addTradeGossip(m, t.p.name) // build reputation → cheaper future offers
		h.updateSpecialPrices(t, m)   // reflect the new reputation immediately
		h.sendTradeList(t, m)         // refresh uses/level/price (and any new offers)
		h.playSound(players, "minecraft:entity.villager.yes", sndNeutral, m.x, m.y, m.z, 0.7, 1)
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
