package server

import "testing"

// A carrot is horse food: it heals a tamed horse three and settles a wild one.
func TestHorseEatsCarrots(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	m.tamed, m.owner, m.health = true, pl.p.eid, 1
	give(pl, int32(itemByName["carrot"]))
	if !h.interactMob(players, pl, m, false) || m.health != 4 {
		t.Fatalf("a carrot heals a horse 3: health %d", m.health)
	}
}

// Golden food courts only a tamed horse; a wild one takes it for temper.
func TestWildHorseDoesNotCourt(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	give(pl, int32(itemByName["golden_carrot"]))
	if !h.interactMob(players, pl, m, false) || m.loveTicks != 0 || m.temper != 5 {
		t.Fatalf("wild horse: love %d temper %d (want 0, 5)", m.loveTicks, m.temper)
	}
}

// A zombie horse eats red mushrooms and nothing of the horse's, and follows
// a player holding one.
func TestZombieHorseFood(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityZombieHorse)
	give(pl, int32(itemByName["golden_carrot"]))
	if h.interactMob(players, pl, m, false) && (m.rider != 0 || m.temper != 0 || pl.inv.slots[0].count != 1) {
		t.Fatalf("a golden carrot is not zombie horse food: rider %d temper %d left %d", m.rider, m.temper, pl.inv.slots[0].count)
	}
	if h.temptingPlayer(players, m) != nil {
		t.Fatal("a golden carrot must not tempt a zombie horse")
	}
	give(pl, int32(itemByName["red_mushroom"]))
	if h.temptingPlayer(players, m) != pl {
		t.Fatal("a red mushroom tempts a zombie horse")
	}
	if !h.interactMob(players, pl, m, false) || m.temper != 3 || pl.inv.slots[0].count != 0 {
		t.Fatalf("a red mushroom settles a wild zombie horse: temper %d left %d", m.temper, pl.inv.slots[0].count)
	}
	// An empty hand climbs on, as on any wild horse.
	give(pl, 0)
	pl.inv.slots[0] = invStack{}
	if !h.interactMob(players, pl, m, false) || m.rider != pl.p.eid {
		t.Fatalf("an empty hand should mount a wild zombie horse: rider %d", m.rider)
	}
}

// A wild horse rears at a saddle (or anything else not food) and takes no rider.
func TestWildHorseRefusesASaddle(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	give(pl, itemSaddle)
	h.interactMob(players, pl, m, false)
	if m.saddled || m.rider != 0 || pl.inv.slots[0].count != 1 {
		t.Fatalf("wild horse + saddle: saddled %v rider %d left %d", m.saddled, m.rider, pl.inv.slots[0].count)
	}
}

// A skeleton horse outside its trap ignores the player, is tempted by
// nothing, and sinks.
func TestSkeletonHorseIgnoresPlayers(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entitySkeletonHorse)
	m.health = 1
	give(pl, int32(itemByName["wheat"]))
	if h.interactMob(players, pl, m, false) || m.health != 1 {
		t.Fatalf("an untamed skeleton horse does not eat: health %d", m.health)
	}
	give(pl, int32(itemByName["golden_carrot"]))
	if h.temptingPlayer(players, m) != nil {
		t.Fatal("a skeleton horse has no TemptGoal")
	}
	give(pl, 0)
	pl.inv.slots[0] = invStack{}
	if h.interactMob(players, pl, m, false) || m.rider != 0 {
		t.Fatal("an untamed skeleton horse takes no rider")
	}
	if mobFloats(m) {
		t.Fatal("a skeleton horse has no FloatGoal")
	}
}
