package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The gossip maths: caps, weights, daily decay and the transfer between
// villagers, as vanilla's GossipContainer does them.
func TestGossipBook(t *testing.T) {
	var g gossipBook
	for i := 0; i < 20; i++ {
		g.add("wes", gossipTrading, 2)
	}
	if g["wes"][gossipTrading] != 25 {
		t.Errorf("trading caps at 25, got %d", g["wes"][gossipTrading])
	}
	g.add("wes", gossipMajorPositive, 20)
	g.add("wes", gossipMinorPositive, 25)
	if rep := g.reputation("wes"); rep != 25+100+25 {
		t.Errorf("reputation weights: got %d want 150", rep)
	}
	g.add("bad", gossipMajorNegative, 25)
	if rep := g.reputation("bad"); rep != -125 {
		t.Errorf("a murder is worth -125: %d", rep)
	}
	g.add("tiny", gossipMinorPositive, 1)
	if _, ok := g["tiny"]; ok {
		t.Error("a value under the threshold is dropped")
	}
	g.decay()
	if g["wes"][gossipTrading] != 23 || g["wes"][gossipMajorPositive] != 20 || g["bad"][gossipMajorNegative] != 15 {
		t.Errorf("daily decay: %+v", g)
	}
	// Transfer: the listener gets the big entries minus the transfer decay.
	var other gossipBook
	n := 0
	other.transferFrom(g, func(k int) int { n++; return (n * 7919) % k }, gossipTransferCount)
	if other["bad"][gossipMajorNegative] != 5 { // 15 - 10
		t.Errorf("transferred major negative: %d, want 5", other["bad"][gossipMajorNegative])
	}
	if other["wes"][gossipMajorPositive] != 0 { // 20 - 20 < threshold: not passed on
		t.Errorf("a major positive does not survive a transfer: %d", other["wes"][gossipMajorPositive])
	}
	if _, ok := other["wes"]; !ok {
		t.Error("the trading/minor gossip about wes should have spread")
	}
}

// Gossip in the world: a hurt villager and the witnesses of a murder hold
// it against the player, it spreads to a villager standing beside them,
// it survives a save, and the village golem turns on a player at −100.
func TestGossipEventsSpreadAndGolem(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = 10.5, 200, 10.5
	v := h.spawnMob(players, entityVillager, 11.5, 200, 10.5)
	w := h.spawnMob(players, entityVillager, 13.5, 200, 10.5) // a witness beside it
	h.gridDirty()
	h.villagerHurtBy(players, v, pl, false)
	if rep := v.gossip.reputation(pl.p.name); rep != -25 {
		t.Errorf("a hurt villager holds MINOR_NEGATIVE 25: rep %d", rep)
	}
	h.villagerHurtBy(players, v, pl, true)
	if rep := w.gossip.reputation(pl.p.name); rep != -125 {
		t.Errorf("a witness of the murder holds MAJOR_NEGATIVE 25: rep %d", rep)
	}
	// Spread: a third villager beside the witness picks the story up.
	u := h.spawnMob(players, entityVillager, 14.5, 200, 10.5)
	h.gridDirty()
	h.tick.Store(100)
	h.villagerGossipTick(u)
	if got := u.gossip["tester"][gossipMajorNegative]; got != 25-10 {
		t.Errorf("gossip spreads minus its transfer decay: %d, want 15", got)
	}
	if u.gossipAt == 0 || w.gossipAt == 0 {
		t.Error("both parties remember the chat")
	}
	// The golem defends: a player at ≤ −100 with a villager within ten blocks.
	g := h.spawnMob(players, entityIronGolem, 12.5, 200, 12.5)
	h.gridDirty()
	if h.golemGrudge(players, g) != pl {
		t.Error("the golem should hold the murder against the player")
	}
	pl.gamemode = gmCreative
	if h.golemGrudge(players, g) != nil {
		t.Error("creative players are spared")
	}
	// Persistence round-trip.
	sm := toSavedMob(w)
	if sm.Gossip["tester"][gossipMajorNegative] != 25 {
		t.Errorf("gossip saved: %+v", sm.Gossip)
	}
	back := h.reloadMob(players, &sm)
	if back == nil || back.gossip.reputation("tester") != -125 {
		t.Errorf("gossip reloaded: %+v", back)
	}
}
