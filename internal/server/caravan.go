package server

import "math"

// Llama caravans (LlamaFollowCaravanGoal, priority 2 — above panic). A llama
// that is neither leashed nor already in a line looks within nine blocks for
// the end of a caravan (a llama following another, with nobody following
// it), or failing that a leashed llama with nobody behind it, and falls in
// behind it — so long as the line leads back to a llama on a lead. It keeps
// two blocks behind the llama ahead at 2.1× its pace, speeds up by a fifth
// at a time (to past 3) when that llama gets more than 26 blocks away, and
// drops out of the line when it cannot catch up, when the llama ahead dies,
// or when nobody at the front is on a lead any more.

const (
	caravanSearchXZ = 9.0
	caravanSearchY  = 4.0
	caravanSpeed    = 2.1
	caravanLimit    = 8   // CARAVAN_LIMIT: firstIsLeashed looks this far up the line
	caravanFarSq    = 676 // 26 blocks
	caravanGrace    = 20  // reducedTickDelay(40), in mob updates
	caravanGap      = 2.0 // wantedDistance
)

func isLlama(etype int) bool { return etype == entityLlama || etype == entityTraderLlama }

// caravanTailOf is the llama following m, if it still is.
func (h *hub) caravanTailOf(m *mob) *mob {
	if t := h.mobs[m.caravanTail]; t != nil && t.dying == 0 && t.caravanHead == m.eid {
		return t
	}
	m.caravanTail = 0
	return nil
}

// caravanLeashed is firstIsLeashed: walking up the line from m (counter
// links already walked), is the first llama ahead that is on a lead within
// reach of the front?
func (h *hub) caravanLeashed(m *mob, counter int) bool {
	for ; counter <= caravanLimit; counter++ {
		head := h.mobs[m.caravanHead]
		if m.caravanHead == 0 || head == nil {
			return false
		}
		if head.leash != 0 {
			return true
		}
		m = head
	}
	return false
}

// leaveCaravan steps m out of its line.
func (h *hub) leaveCaravan(m *mob) {
	if head := h.mobs[m.caravanHead]; head != nil && head.caravanTail == m.eid {
		head.caravanTail = 0
	}
	m.caravanHead, m.caravanSpeed, m.caravanGrace = 0, 0, 0
}

// caravanStep runs the goal for one mob update. Reports whether it holds the
// llama's movement.
func (h *hub) caravanStep(m *mob) bool {
	if !isLlama(m.etype) || m.dying > 0 || m.rider != 0 {
		return false
	}
	if m.caravanHead == 0 && !h.joinCaravan(m) {
		return false
	}
	head := h.mobs[m.caravanHead]
	if head == nil || head.dying > 0 || head.dim != m.dim || !h.caravanLeashed(m, 0) {
		h.leaveCaravan(m)
		return false
	}
	dx, dy, dz := head.x-m.x, head.y-m.y, head.z-m.z
	d2 := dx*dx + dy*dy + dz*dz
	if d2 > caravanFarSq {
		if m.caravanSpeed <= 3 {
			m.caravanSpeed *= 1.2
			m.caravanGrace = caravanGrace
		} else if m.caravanGrace == 0 {
			h.leaveCaravan(m)
			return false
		}
	}
	if m.caravanGrace > 0 {
		m.caravanGrace--
	}
	if k := h.knots[m.leash]; m.leash != 0 && k != nil {
		return true // tied to a fence: it stays in line but does not move
	}
	d := math.Sqrt(d2)
	if d <= caravanGap+0.5 {
		m.vx, m.vz = 0, 0
		return true
	}
	// Two blocks short of the llama ahead.
	f := (d - caravanGap) / d
	vx, vz := h.pathSteer(m, m.x+dx*f, m.z+dz*f)
	m.vx, m.vz = vx*m.caravanSpeed, vz*m.caravanSpeed
	m.rest = 0
	return true
}

// joinCaravan is the goal's canUse: find the llama to follow and fall in.
func (h *hub) joinCaravan(m *mob) bool {
	if m.leash != 0 {
		return false
	}
	box := mobBoxes[m.etype]
	pick := func(ok func(c *mob) bool) (*mob, float64) {
		var best *mob
		bestD := math.MaxFloat64
		for _, c := range h.mobs {
			if c == m || !isLlama(c.etype) || c.dying > 0 || c.dim != m.dim || !ok(c) {
				continue
			}
			cb := mobBoxes[c.etype]
			if math.Abs(c.x-m.x) > caravanSearchXZ+(box.w+cb.w)/2 || math.Abs(c.z-m.z) > caravanSearchXZ+(box.w+cb.w)/2 ||
				c.y > m.y+box.h+caravanSearchY || c.y+cb.h < m.y-caravanSearchY {
				continue // outside the bounding box inflated by 9, 4, 9
			}
			if d := (c.x-m.x)*(c.x-m.x) + (c.y-m.y)*(c.y-m.y) + (c.z-m.z)*(c.z-m.z); d <= bestD {
				best, bestD = c, d
			}
		}
		return best, bestD
	}
	c, d2 := pick(func(c *mob) bool { return c.caravanHead != 0 && h.caravanTailOf(c) == nil })
	if c == nil {
		c, d2 = pick(func(c *mob) bool { return c.leash != 0 && h.caravanTailOf(c) == nil })
	}
	if c == nil || d2 < 4 || (c.leash == 0 && !h.caravanLeashed(c, 1)) {
		return false
	}
	m.caravanHead, m.caravanSpeed, m.caravanGrace = c.eid, caravanSpeed, 0
	c.caravanTail = m.eid
	return true
}

// traderLlamasDefend is TraderLlamaDefendWanderingTraderGoal: a player who
// hurts a wandering trader becomes the target of every trader llama on its
// leads.
func (h *hub) traderLlamasDefend(trader *mob, t *tracked) {
	if trader.etype != entityWanderingTrader || t == nil || t.gamemode == gmCreative || t.gamemode == gmSpectator {
		return
	}
	for _, l := range h.mobs {
		if l.etype == entityTraderLlama && l.leash == trader.eid && l.dying == 0 {
			h.provoke(l, t)
		}
	}
}
