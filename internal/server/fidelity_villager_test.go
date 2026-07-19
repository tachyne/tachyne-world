package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// vTradeVillager makes a survival player and a villager with one fixed offer
// (wheat → emerald, base cost 4, maxUses 12) ready to trade.
func vTradeVillager(t *testing.T) (*hub, *tracked, *mob, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{1: pl}
	m := h.spawnMob(players, entityVillager, pl.x+1, pl.y, pl.z)
	m.offers = []mobOffer{{trade: vTrade{itemByName["wheat"], 4, itemByName["emerald"], 1, 12, 2}}}
	return h, pl, m, players
}

// TestDemandRaisesPrice — heavy use bumps demand on restock, which raises the
// offer's cost (vanilla getModifiedCostCount).
func TestDemandRaisesPrice(t *testing.T) {
	h, _, m, _ := vTradeVillager(t)
	o := &m.offers[0]
	if got := o.costCount(); got != 4 {
		t.Fatalf("fresh offer cost %d, want the base 4", got)
	}
	// Max out uses, then restock: demand = 0 + 12 - (12-12) = 12.
	o.uses = 12
	h.restockOffers(m)
	if o.demand != 12 {
		t.Fatalf("demand after a fully-used restock = %d, want 12", o.demand)
	}
	// cost = clamp(4 + floor(4*12*0.05) + 0, 1, max) = 4 + 2 = 6.
	if got := o.costCount(); got != 6 {
		t.Fatalf("demand-adjusted cost %d, want 6", got)
	}
	// An idle day drives demand negative; the markup floors at 0 (base price).
	o.uses = 0
	h.tick.Store(h.tick.Load() + restockInterval + 1)
	h.restockOffers(m)
	if got := o.costCount(); got != 4 {
		t.Fatalf("idle-day cost %d, want the base 4 (markup floored at 0)", got)
	}
}

// TestReputationAndHeroDiscount — trading builds reputation and Hero of the
// Village lowers the special price (both negative deltas).
func TestReputationAndHeroDiscount(t *testing.T) {
	h, pl, m, _ := vTradeVillager(t)
	// Reputation alone: with a bigger base cost the −floor(rep*0.05) shows.
	m.offers[0].trade.inCount = 40
	for i := 0; i < 20; i++ {
		h.addTradeGossip(m, pl.p.name) // capped at 25
	}
	if rep := m.gossip[pl.p.name]; rep != gossipTradeMax {
		t.Fatalf("trading reputation %d, want capped at %d", rep, gossipTradeMax)
	}
	h.updateSpecialPrices(pl, m)
	// special = -floor(25 * 0.05) = -1 → cost 40 - 1 = 39.
	if got := m.offers[0].costCount(); got != 39 {
		t.Fatalf("reputation-discounted cost %d, want 39", got)
	}
	// Hero of the Village I adds -max(1, floor(0.3*40)) = -12 on top.
	h.applyEffect(nil, pl, effHeroOfVillage, 0, 60)
	h.updateSpecialPrices(pl, m)
	if got := m.offers[0].costCount(); got != 40-1-12 {
		t.Fatalf("Hero+reputation cost %d, want 27", got)
	}
	// Closing the screen clears the per-viewer discount.
	clearSpecialPrices(m)
	if m.offers[0].specialPrice != 0 {
		t.Fatalf("special price not reset on close: %d", m.offers[0].specialPrice)
	}
}

// TestRestockGating — at most two restocks per day, spaced ≥2400 ticks.
func TestRestockGating(t *testing.T) {
	h, _, m, _ := vTradeVillager(t)
	m.offers[0].uses = 5

	h.restockOffers(m) // #1 (restocksToday was 0)
	if m.offers[0].uses != 0 || m.restocksToday != 1 {
		t.Fatalf("first restock should fire: uses=%d today=%d", m.offers[0].uses, m.restocksToday)
	}
	m.offers[0].uses = 5
	h.restockOffers(m) // too soon after #1 — blocked
	if m.offers[0].uses != 5 {
		t.Fatalf("a second restock within 2400 ticks must be blocked: uses=%d", m.offers[0].uses)
	}
	h.tick.Store(h.tick.Load() + restockInterval + 1)
	h.restockOffers(m) // #2 (now spaced far enough)
	if m.offers[0].uses != 0 || m.restocksToday != 2 {
		t.Fatalf("second restock should fire after the interval: uses=%d today=%d", m.offers[0].uses, m.restocksToday)
	}
	m.offers[0].uses = 5
	h.tick.Store(h.tick.Load() + restockInterval + 1)
	h.restockOffers(m) // #3 — over the daily cap
	if m.offers[0].uses != 5 {
		t.Fatalf("a third restock in one day must be blocked: uses=%d", m.offers[0].uses)
	}
}

// TestGolemDamageRange — golem punches deal vanilla's 7.5–21.5 (integer target
// health sees 7..21), never the old flat 8.
func TestGolemDamageRange(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	g := h.spawnMob(players, entityIronGolem, 0, 70, 0)
	g.behavior = golemBehavior{}
	lo, hi, varied := 99, 0, false
	for i := 0; i < 400; i++ {
		z := h.spawnMob(players, entityZombie, 0.5, 70, 0.5)
		z.hostile, z.health = true, 40
		before := z.health
		h.golemMelee(players, g)
		g.attackCD = 0 // let it swing again next call
		dealt := before - z.health
		if dealt > 0 {
			if dealt < lo {
				lo = dealt
			}
			if dealt > hi {
				hi = dealt
			}
			if dealt != 8 {
				varied = true
			}
		}
		delete(h.mobs, z.eid)
	}
	if !varied || lo < 1 || hi > 21 {
		t.Fatalf("golem damage should span ~7-21 and vary: lo=%d hi=%d varied=%v", lo, hi, varied)
	}
}

// TestRaidRiderType — pillager on wave 5, evoker (first ravager) / vindicator on
// wave 7+, nothing on earlier ravager waves.
func TestRaidRiderType(t *testing.T) {
	if raidRiderType(3, 0) != 0 {
		t.Error("wave 3 ravager should carry no rider")
	}
	if raidRiderType(5, 0) != entityPillager {
		t.Error("wave 5 ravager should carry a pillager")
	}
	if raidRiderType(7, 0) != entityEvoker {
		t.Error("first wave-7 ravager should carry an evoker")
	}
	if raidRiderType(7, 1) != entityVindicator {
		t.Error("later wave-7 ravagers should carry a vindicator")
	}
}

// TestRiderGluedToVehicle — a mounted rider tracks its vehicle and dismounts
// when the vehicle dies.
func TestRiderGluedToVehicle(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	rav := h.spawnMob(players, entityRavager, 10, 70, 10)
	rider := h.spawnMob(players, entityPillager, 10, 70, 10)
	rider.mount, rav.mobRider = rav.eid, rider.eid

	// Move the ravager; the rider should be glued to it on the next mob update.
	rav.x, rav.y, rav.z = 20, 72, 25
	h.updateMobs(players)
	if rider.x != rav.x || rider.z != rav.z {
		t.Fatalf("rider not glued to vehicle: rider(%v,%v) vehicle(%v,%v)", rider.x, rider.z, rav.x, rav.z)
	}
	// Kill the vehicle: the rider dismounts (mount cleared) and rejoins the world.
	rav.dying = 1
	h.updateMobs(players)
	if rider.mount != 0 {
		t.Fatalf("rider must dismount when its vehicle dies: mount=%d", rider.mount)
	}
}
