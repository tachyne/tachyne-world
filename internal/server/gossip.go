package server

import (
	"math"
	"sort"
)

// Villager gossip — vanilla's GossipContainer. Each villager keeps, per
// player, a value for each of five gossip types; a player's reputation with
// that villager is the weighted sum. Gossip is earned by trading, curing,
// hurting and murdering, fades a little every day, and spreads between
// villagers that meet.

type gossipType uint8

const (
	gossipMajorNegative gossipType = iota // a villager murdered in sight
	gossipMinorNegative                   // a villager hurt
	gossipMinorPositive                   // a zombie villager cured (the small part)
	gossipMajorPositive                   // a zombie villager cured (the big part)
	gossipTrading                         // a trade completed
	gossipTypeCount
)

// gossipRules are GossipType's weight, max, decayPerDay and decayPerTransfer.
var gossipRules = [gossipTypeCount]struct {
	weight, max, decayPerDay, decayPerTransfer int
}{
	gossipMajorNegative: {-5, 100, 10, 10},
	gossipMinorNegative: {-1, 200, 20, 20},
	gossipMinorPositive: {1, 25, 1, 5},
	gossipMajorPositive: {5, 20, 0, 20},
	gossipTrading:       {1, 25, 2, 20},
}

const (
	gossipDiscardBelow  = 2     // GossipContainer.DISCARD_THRESHOLD
	gossipDecayInterval = 24000 // Villager.GOSSIP_DECAY_INTERVAL (a day)
	gossipTransferCount = 10    // entries a villager passes on per chat
	gossipChatCooldown  = 1200  // ticks between a villager's chats
	gossipWitnessRange  = 16.0  // villagers that see a murder
	golemDefendRange    = 10.0  // DefendVillageTargetGoal's box
	golemDefendRep      = -100  // reputation at which a golem turns on a player
)

// gossipBook is one villager's gossip: player name → value per type.
type gossipBook map[string][gossipTypeCount]int

// add is GossipContainer.add: the sum, capped at the type's max (an entry
// already over the cap keeps what it has), and a target with nothing left
// is forgotten.
func (g *gossipBook) add(name string, typ gossipType, amount int) {
	if *g == nil {
		*g = gossipBook{}
	}
	e := (*g)[name]
	old := e[typ]
	sum := old + amount
	if sum > gossipRules[typ].max {
		sum = max(gossipRules[typ].max, old)
	}
	if sum < gossipDiscardBelow {
		sum = 0
	}
	e[typ] = sum
	if e == ([gossipTypeCount]int{}) {
		delete(*g, name)
	} else {
		(*g)[name] = e
	}
}

// reputation is the weighted sum of everything a villager holds about a
// player (Villager.getPlayerReputation).
func (g gossipBook) reputation(name string) int {
	e, ok := g[name]
	if !ok {
		return 0
	}
	rep := 0
	for t := gossipType(0); t < gossipTypeCount; t++ {
		rep += e[t] * gossipRules[t].weight
	}
	return rep
}

// decay is GossipContainer.decay: every entry loses its type's daily decay,
// and what falls under the threshold is forgotten.
func (g *gossipBook) decay() {
	for name, e := range *g {
		for t := gossipType(0); t < gossipTypeCount; t++ {
			if e[t] == 0 {
				continue
			}
			if v := e[t] - gossipRules[t].decayPerDay; v < gossipDiscardBelow {
				e[t] = 0
			} else {
				e[t] = v
			}
		}
		if e == ([gossipTypeCount]int{}) {
			delete(*g, name)
		} else {
			(*g)[name] = e
		}
	}
}

// gossipEntry is one (player, type, value) triple.
type gossipEntry struct {
	name  string
	typ   gossipType
	value int
}

// entries unpacks the book in a stable order.
func (g gossipBook) entries() []gossipEntry {
	names := make([]string, 0, len(g))
	for n := range g {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []gossipEntry
	for _, n := range names {
		e := g[n]
		for t := gossipType(0); t < gossipTypeCount; t++ {
			if e[t] != 0 {
				out = append(out, gossipEntry{n, t, e[t]})
			}
		}
	}
	return out
}

// transferFrom is GossipContainer.transferFrom: up to count picks from the
// source, weighted by |value × weight| (an entry may be picked more than
// once and counts once), each landing minus its type's transfer decay, kept
// only if that leaves at least the threshold, merged by max.
func (g *gossipBook) transferFrom(src gossipBook, intn func(int) int, count int) {
	entries := src.entries()
	if len(entries) == 0 {
		return
	}
	ranges := make([]int, len(entries))
	end := 0
	for i, e := range entries {
		w := e.value * gossipRules[e.typ].weight
		if w < 0 {
			w = -w
		}
		end += w
		ranges[i] = end - 1
	}
	if end <= 0 {
		return
	}
	picked := map[int]bool{}
	for i := 0; i < count; i++ {
		choice := intn(end)
		idx := sort.SearchInts(ranges, choice)
		picked[idx] = true
	}
	if *g == nil {
		*g = gossipBook{}
	}
	for idx := range picked {
		e := entries[idx]
		v := e.value - gossipRules[e.typ].decayPerTransfer
		if v < gossipDiscardBelow {
			continue
		}
		cur := (*g)[e.name]
		if v > cur[e.typ] {
			cur[e.typ] = v
		}
		(*g)[e.name] = cur
	}
}

// villagerHurtBy is Villager.hurt's reputation event and, on death,
// tellWitnessesThatIWasMurdered: the hurt villager holds MINOR_NEGATIVE 25
// against the player; a murder gives every villager in sight (16 blocks)
// MAJOR_NEGATIVE 25.
func (h *hub) villagerHurtBy(players map[int32]*tracked, m *mob, t *tracked, killed bool) {
	m.gossip.add(t.p.name, gossipMinorNegative, 25)
	if !killed {
		return
	}
	h.grid().nearby(m.dim, m.x, m.z, gossipWitnessRange, func(w *mob) {
		if w.etype != entityVillager || w.dying > 0 || w == m {
			return
		}
		if dist3(w.x, w.y, w.z, m.x, m.y, m.z) <= gossipWitnessRange {
			w.gossip.add(t.p.name, gossipMajorNegative, 25)
		}
	})
}

// villagerGossipTick runs once a survival step (20 ticks): the daily decay,
// and a chat with a villager standing beside it (TradeWithVillager →
// Villager.gossip: up to ten entries pass over, at most once per 1200 ticks
// for either party).
func (h *hub) villagerGossipTick(m *mob) {
	now := h.tick.Load()
	if m.gossipDecayAt == 0 {
		m.gossipDecayAt = now
	} else if now >= m.gossipDecayAt+gossipDecayInterval {
		m.gossip.decay()
		m.gossipDecayAt = now
	}
	if now < m.gossipAt+gossipChatCooldown && m.gossipAt != 0 {
		return
	}
	var partner *mob
	h.grid().nearby(m.dim, m.x, m.z, 2, func(o *mob) {
		if partner != nil || o == m || o.etype != entityVillager || o.dying > 0 || len(o.gossip) == 0 {
			return
		}
		if o.gossipAt != 0 && now < o.gossipAt+gossipChatCooldown {
			return
		}
		if dist3(o.x, o.y, o.z, m.x, m.y, m.z) <= 2 {
			partner = o
		}
	})
	if partner == nil {
		return
	}
	m.gossip.transferFrom(partner.gossip, h.rng.Intn, gossipTransferCount)
	m.gossipAt, partner.gossipAt = now, now
}

// golemGrudge is DefendVillageTargetGoal: a survival player within ten
// blocks of the golem whose reputation with a villager in the same box is
// −100 or worse.
func (h *hub) golemGrudge(players map[int32]*tracked, m *mob) *tracked {
	var villagers []*mob
	h.grid().nearby(m.dim, m.x, m.z, golemDefendRange, func(o *mob) {
		if o.etype == entityVillager && o.dying == 0 && len(o.gossip) > 0 && math.Abs(o.y-m.y) <= 8 {
			villagers = append(villagers, o)
		}
	})
	if len(villagers) == 0 {
		return nil
	}
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator {
			continue
		}
		if math.Abs(t.x-m.x) > golemDefendRange || math.Abs(t.z-m.z) > golemDefendRange || math.Abs(t.y-m.y) > 8 {
			continue
		}
		for _, v := range villagers {
			if v.gossip.reputation(t.p.name) <= golemDefendRep {
				return t
			}
		}
	}
	return nil
}

// golemPunchPlayer lands the golem's punch on the player it holds a grudge
// against, when in reach (IronGolem.doHurtTarget: 7.5–21.5).
func (h *hub) golemPunchPlayer(players map[int32]*tracked, m *mob) bool {
	t := h.golemGrudge(players, m)
	if t == nil || dist3(t.x, t.y, t.z, m.x, m.y, m.z) > 2.5 {
		return false
	}
	m.attackCD = 5
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSound(players, "minecraft:entity.iron_golem.attack", sndNeutral, m.x, m.y, m.z, 1, 1)
	h.hurtFrom(players, t, float32(7.5+float64(h.rng.Intn(15))), mobMeleeDamage(m.etype),
		deathCause{key: causeMob, by: mobDisplayName(m.etype)}, from(m.x, m.z))
	h.knockback(t, m.x, m.z)
	return true
}
