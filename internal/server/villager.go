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
				for i, house := range v.Houses {
					m := h.spawnMob(players, entityVillager,
						float64(house.X)+0.5, float64(house.Y), float64(house.Z)+2.5)
					// Villager MOVEMENT_SPEED attr is 0.5 with ~0.6 goal
					// modifiers (vanilla 1.21.5) — brisker than livestock.
					m.speed = 0.135
					h.initVillagerTrades(m, i)
					m.home = blockPos{house.X, house.Y, house.Z}
					m.behavior = villagerBehavior{} // path home/around + open doors
					m.usesDoors = true
					// Schedule anchors from the deterministic furniture layout
					// (furnishHouse): bed head at dx=-1, workstation at dx=+1,dz=-1,
					// and the village bell as the shared midday meeting point.
					m.bed = blockPos{house.X - 1, house.Y, house.Z}
					m.work = blockPos{house.X + 1, house.Y, house.Z - 1}
					m.meet = blockPos{v.X + 2, v.Y, v.Z}
				}
				g := h.spawnMob(players, entityIronGolem,
					float64(v.X)+2.5, float64(v.Y), float64(v.Z)+2.5)
				g.health = 100
				g.noKB = true // KNOCKBACK_RESISTANCE 1.0 (vanilla 1.21.5)
				g.behavior = golemBehavior{}
				g.home = well
			}
		}
	}
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
		o.hurt(8) // golem punches respect the target's armor
		if o.health <= 0 {
			h.killMob(players, o)
		}
		return
	}
}

// openTrades shows a villager's merchant screen.
func (h *hub) openTrades(t *tracked, m *mob) {
	if t.inv == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
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
	for _, o := range m.offers {
		tr := o.trade
		b = protocol.AppendVarInt(b, tr.inItem) // ItemCost: id + count + no components
		b = protocol.AppendVarInt(b, tr.inCount)
		b = protocol.AppendVarInt(b, 0)
		b = appendStack(b, invStack{item: tr.outItem, count: int(tr.outCount)}) // output Slot
		b = protocol.AppendBool(b, false)                                       // no second cost
		b = protocol.AppendBool(b, o.uses >= tr.maxUses)                        // disabled when used up
		b = protocol.AppendI32(b, o.uses)
		b = protocol.AppendI32(b, tr.maxUses)
		b = protocol.AppendI32(b, tr.xp)
		b = protocol.AppendI32(b, 0)    // special price
		b = protocol.AppendF32(b, 0.05) // price multiplier
		b = protocol.AppendI32(b, 0)    // demand
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
	if have < int(o.trade.inCount) {
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
	need := int(o.trade.inCount)
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
		h.sendTradeList(t, m)         // refresh uses/level (and any new offers)
		h.playSound(players, "minecraft:entity.villager.yes", sndNeutral, m.x, m.y, m.z, 0.7, 1)
	}
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
	t.tradeWith = 0
}

type evSelTrade struct {
	eid  int32
	slot int32
}

func (evSelTrade) isHubEvent() {}
