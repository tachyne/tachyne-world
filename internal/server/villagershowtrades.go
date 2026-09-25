package server

import "math"

// ShowTradesToPlayer. A grown villager with a player within four blocks
// (SetLookAndInteract) turns to them, and if the player holds something one
// of its open trades asks for, it holds up what that trade would give —
// cycling through them every two seconds when there are several. With
// nothing to show it loses interest after two seconds, and it gives the
// display up when the player walks off or after 20-80 seconds at most.

const (
	showTradesReachSq  = 4 * 4 // SetLookAndInteract(PLAYER, 4)
	showTradesKeepSq   = 17.0  // checkExtraStartConditions: distanceToSqr ≤ 17
	showTradesStart    = 40    // STARTING_LOOK_TIME
	showTradesMax      = 900   // MAX_LOOK_TIME, once there is something to show
	showTradesCycle    = 40    // ticks per item when several are shown
	showTradesMinTicks = 400   // new ShowTradesToPlayer(400, 1600)
	showTradesMaxTicks = 1600
)

// tradeShow is one run of the behaviour.
type tradeShow struct {
	player  int32      // INTERACTION_TARGET
	end     uint64     // the behaviour's end timestamp
	look    int        // lookTime
	cycle   int        // cycleCounter
	idx     int        // displayIndex
	seen    int32      // the player's main-hand item last looked at (-1 = none yet)
	items   []invStack // displayItems
	shown   invStack   // what the villager is holding up
	showing bool
}

// villagerShowTradesTick runs each mob update for a villager that is not
// trading. It returns nothing: the display never holds the villager still.
func (h *hub) villagerShowTradesTick(players map[int32]*tracked, m *mob) {
	s := &m.showTrades
	busy := m.baby || m.dying > 0 || m.sleeping || m.villagerHurtLeft > 0 || m.panicHasT
	now := h.tick.Load()
	if s.player != 0 {
		t := players[s.player]
		if busy || t == nil || t.dead || t.dim != m.dim || t.gamemode == gmSpectator ||
			dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) > showTradesKeepSq || s.look <= 0 || now > s.end {
			h.stopShowingTrades(players, m) // stop: INTERACTION_TARGET erased, the hand emptied
			return
		}
		h.showTradesTo(players, m, t)
		return
	}
	if busy {
		return
	}
	// SetLookAndInteract: the nearest player it can see within four blocks.
	var best *tracked
	bestD := float64(showTradesReachSq)
	for _, t := range players {
		if t.dead || t.dim != m.dim || t.gamemode == gmSpectator {
			continue
		}
		if d := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); d <= bestD && h.mobSees(m, t) {
			best, bestD = t, d
		}
	}
	if best == nil {
		return
	}
	*s = tradeShow{player: best.p.eid, look: showTradesStart, seen: -1,
		end: now + uint64(showTradesMinTicks+h.rng.Intn(showTradesMaxTicks-showTradesMinTicks+1))}
	h.showTradesTo(players, m, best)
}

// showTradesTo is ShowTradesToPlayer.tick.
func (h *hub) showTradesTo(players map[int32]*tracked, m *mob, t *tracked) {
	s := &m.showTrades
	m.headYaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi) // LOOK_TARGET
	// findItemsToDisplay: a change of item in the player's hand re-reads the
	// offers that ask for it.
	if held := heldStack(t).item; held != s.seen {
		s.seen, s.items = held, nil
		if held != 0 {
			for i := range m.offers {
				o := &m.offers[i]
				if o.uses >= o.trade.maxUses { // isOutOfStock
					continue
				}
				if o.trade.inItem == held || (o.cost2Item != 0 && o.cost2Item == held) {
					s.items = append(s.items, o.output())
				}
			}
			if len(s.items) > 0 {
				s.look, s.idx, s.cycle = showTradesMax, 0, 0
				h.showTradeItem(players, m, s.items[0])
			}
		}
	}
	if len(s.items) == 0 {
		h.hideTradeItem(players, m)
		s.look = min(s.look, showTradesStart)
	} else if len(s.items) >= 2 {
		if s.cycle += mobMoveInterval; s.cycle >= showTradesCycle {
			s.cycle = 0
			if s.idx++; s.idx >= len(s.items) {
				s.idx = 0
			}
			h.showTradeItem(players, m, s.items[s.idx])
		}
	}
	s.look -= mobMoveInterval
}

// showTradeItem is displayAsHeldItem: the offer's result in the main hand,
// never dropped (drop chance 0).
func (h *hub) showTradeItem(players map[int32]*tracked, m *mob, st invStack) {
	m.showTrades.shown, m.showTrades.showing = st, true
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, st, invStack{}, m.gear))
}

// hideTradeItem is clearHeldItem.
func (h *hub) hideTradeItem(players map[int32]*tracked, m *mob) {
	if !m.showTrades.showing {
		return
	}
	m.showTrades.shown, m.showTrades.showing = invStack{}, false
	h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, m.heldStack(), invStack{}, m.gear))
}

// stopShowingTrades ends the behaviour.
func (h *hub) stopShowingTrades(players map[int32]*tracked, m *mob) {
	h.hideTradeItem(players, m)
	m.showTrades = tradeShow{}
}

// handShown is what a viewer sees in the mob's main hand: a trade on show,
// or what it really holds.
func (m *mob) handShown() invStack {
	if m.showTrades.showing {
		return m.showTrades.shown
	}
	return m.heldStack()
}
