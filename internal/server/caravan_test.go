package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
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

// TEMPT_RANGE is an attribute: ten for an animal, sixteen for a happy ghast,
// eight for a sulfur cube.
func TestTemptRangeAttribute(t *testing.T) {
	for _, tc := range []struct {
		etype int
		want  float64
	}{{entityCow, 10}, {entityHappyGhast, 16}, {entitySulfurCube, 8}} {
		m := &mob{etype: tc.etype}
		if got := m.mobAttrs().Value(attr.TemptRange); got != tc.want {
			t.Errorf("%s tempt range %v, want %v", advEntityName[tc.etype], got, tc.want)
		}
	}
}

// LlamaAttackWolfGoal: a llama spits at a wild wolf within ten blocks, and
// leaves a tamed one alone.
func TestLlamaSpitsAtWildWolves(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	l := h.spawnMob(players, entityLlama, 0.5, 200, 0.5)
	wolf := h.spawnMob(players, entityWolf, 6.5, 200, 0.5)
	wolf.tamed = true
	for i := 0; i < 100; i++ {
		h.llamaWolfTick(players, l)
	}
	if len(h.arrows) != 0 {
		t.Fatal("a llama spat at a tamed wolf")
	}
	wolf.tamed = false
	for i := 0; i < 100 && len(h.arrows) == 0; i++ {
		h.llamaWolfTick(players, l)
	}
	if len(h.arrows) == 0 || l.llamaWolf != wolf.eid {
		t.Fatal("a llama never spat at a wild wolf beside it")
	}
}

// Efficiency and Sweeping Edge on the held item are attribute modifiers the
// client reads: level² + 1 mining efficiency, level / (level + 1) sweep.
func TestHeldEnchantAttributes(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pick := invStack{item: itemByName["diamond_pickaxe"], count: 1}
	pick.ench = enchSetLevel(pick.ench, enchEfficiency, 5)
	pl.inv.slots[pl.p.heldSlot()] = pick
	pl.refreshGearIfChanged()
	if got := pl.playerAttrs().Value(attr.MiningEfficiency); got != 26 {
		t.Errorf("Efficiency V: mining efficiency %v, want 26", got)
	}
	sword := invStack{item: itemByName["diamond_sword"], count: 1}
	sword.ench = enchSetLevel(sword.ench, enchSweepingEdge, 3)
	pl.inv.slots[pl.p.heldSlot()] = sword
	pl.refreshGearIfChanged()
	if got := pl.playerAttrs().Value(attr.MiningEfficiency); got != 0 {
		t.Errorf("mining efficiency stayed at %v after switching to a sword", got)
	}
	if got := pl.playerAttrs().Value(attr.SweepingDamageRatio); got != 0.75 {
		t.Errorf("Sweeping Edge III: ratio %v, want 0.75", got)
	}
}

// A held weapon's ATTACK_SPEED reaches the attribute the client draws its
// cooldown from, copper tools included, and the server's period is unchanged.
func TestWeaponAttackSpeed(t *testing.T) {
	if p := attackPeriod(itemByName["copper_axe"]); p != 25 {
		t.Errorf("copper axe period %d, want 25 (speed 0.8)", p)
	}
	if p := attackPeriod(itemByName["iron_hoe"]); p != 7 {
		t.Errorf("iron hoe period %d, want 7", p)
	}
	if p := attackPeriod(itemByName["diamond_sword"]); p != 12 {
		t.Errorf("sword period %d, want 12", p)
	}
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["diamond_sword"], count: 1}
	pl.refreshGearIfChanged()
	if got := pl.playerAttrs().Value(attr.AttackSpeed); math.Abs(got-1.6) > 1e-9 {
		t.Errorf("sword in hand: attack speed %v, want 1.6", got)
	}
	if got := pl.attackPeriodTicks(attackPeriod(itemByName["diamond_sword"])); got != 12 {
		t.Errorf("sword period through the attribute %d, want 12", got)
	}
}
