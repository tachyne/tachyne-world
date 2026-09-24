package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// LlamaFollowCaravanGoal: free llamas near a led one fall in behind it in a
// line, and the line breaks up when the lead comes off.
func TestLlamaCaravan(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	lead := h.spawnMob(players, entityLlama, 0.5, 180, 0.5)
	a := h.spawnMob(players, entityLlama, 4.5, 180, 0.5)
	b := h.spawnMob(players, entityLlama, 8.5, 180, 0.5)
	if h.caravanStep(a) {
		t.Fatal("a llama joined a caravan with nobody on a lead")
	}
	lead.leash = pl.p.eid
	if !h.caravanStep(a) || a.caravanHead != lead.eid || lead.caravanTail != a.eid {
		t.Fatalf("a did not fall in behind the led llama (head %d)", a.caravanHead)
	}
	if !h.caravanStep(b) || b.caravanHead != a.eid {
		t.Fatalf("b did not join the end of the line (head %d, want %d)", b.caravanHead, a.eid)
	}
	if a.vx >= 0 {
		t.Errorf("a is not walking towards the llama ahead (vx %v)", a.vx)
	}
	if h.caravanStep(lead) {
		t.Error("the led llama follows its own caravan")
	}
	lead.leash = 0
	h.caravanStep(a)
	h.caravanStep(b)
	if a.caravanHead != 0 || b.caravanHead != 0 || lead.caravanTail != 0 {
		t.Error("the caravan held together with nobody on a lead")
	}
}

// TraderLlamaDefendWanderingTraderGoal: hit the trader and its llamas turn
// on you.
func TestTraderLlamasDefendTheirTrader(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 2.5, 180, 0.5
	trader := h.spawnMob(players, entityWanderingTrader, 0.5, 180, 0.5)
	trader.health = 1000
	l := h.spawnMob(players, entityTraderLlama, 0.5, 180, 4.5)
	stray := h.spawnMob(players, entityTraderLlama, 0.5, 180, -4.5)
	h.setLeash(players, l, trader.eid)
	h.attackMob(players, pl.p.eid, trader.eid)
	if !l.hostile || l.targetEID != pl.p.eid {
		t.Error("the trader's llama did not turn on its attacker")
	}
	if stray.hostile {
		t.Error("a llama on nobody's lead defended the trader")
	}
}
