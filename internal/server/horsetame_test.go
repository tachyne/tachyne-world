package server

import "testing"

// The ritual: climb on a wild horse bareback, get thrown, climb on again. Each
// attempt raises its temper by five, and once a roll comes in under the temper
// the horse gives in.
func TestHorseTamingRitual(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)

	// No saddle needed to get on a wild one — that IS the interaction.
	if !h.tryMount(players, pl, m) || m.rider != pl.p.eid {
		t.Fatalf("a wild horse should take a bareback rider: rider=%d", m.rider)
	}
	if m.saddled {
		t.Fatal("climbing on bareback should not have saddled it")
	}

	// Ride until it settles. Each throw costs five temper, so a hundred
	// attempts is far more than enough.
	thrown := 0
	for i := 0; i < 20000 && !m.tamed; i++ {
		if m.rider == 0 {
			thrown++
			h.tryMount(players, pl, m)
		}
		h.horseRideTick(players, m)
	}
	if !m.tamed {
		t.Fatalf("the horse should have given in, temper=%d thrown=%d", m.temper, thrown)
	}
	if thrown == 0 {
		t.Fatal("it should have thrown the rider at least once on the way")
	}
	if m.owner != pl.p.eid {
		t.Fatalf("the tamer should own it, owner=%d", m.owner)
	}
}

// Feeding brings a wild horse round faster: the temper column of handleEating.
func TestFeedingBringsAHorseRound(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityHorse)
	give(pl, int32(itemByName["golden_apple"]))
	before := m.temper
	if !h.feedAnimal(players, pl, m) {
		t.Fatal("a wild horse should take a golden apple")
	}
	if m.temper != before+10 {
		t.Fatalf("a golden apple is worth ten temper: %d → %d", before, m.temper)
	}
	// A tamed horse at full temper has no use for it.
	m.tamed, m.temper = true, horseMaxTemper
	give(pl, int32(itemByName["wheat"]))
	if h.feedAnimal(players, pl, m) {
		t.Fatal("a tamed, healthy horse takes nothing")
	}
}

// A camel needs no taming and a skeleton horse comes tamed from its trap, so
// neither bucks; a zombie horse and both llamas do (Llama.registerGoals p1 is
// RunAroundLikeCrazyGoal, same as the horse).
func TestOnlyTheRiddenEquinesBuck(t *testing.T) {
	for _, etype := range []int{entityHorse, entityDonkey, entityMule, entityZombieHorse, entityLlama, entityTraderLlama} {
		if !horseNeedsTaming(etype) {
			t.Errorf("etype %d should need taming", etype)
		}
	}
	for _, etype := range []int{entityCamel, entitySkeletonHorse, entityPig} {
		if horseNeedsTaming(etype) {
			t.Errorf("etype %d should not buck", etype)
		}
	}
}

// A llama is tamed the horse's way: climb on with an empty hand, get thrown,
// climb on again. Its temper tops out at 30, so each throw is a sixth of the
// way there, and the whole ritual runs through the click and the mob tick.
func TestLlamaTamedByRiding(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityLlama)
	h.world.ForceLoad(100, 100, 2)
	if !h.interactMob(players, pl, m, false) || m.rider != pl.p.eid {
		t.Fatalf("an empty hand should climb onto a wild llama: rider=%d", m.rider)
	}
	thrown := 0
	for i := 0; i < 40000 && !m.tamed; i++ {
		if m.rider == 0 {
			thrown++
			pl.x, pl.y, pl.z = 100.5, 70, 100.5
			h.interactMob(players, pl, m, false)
		}
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if m.temper > 30 {
			t.Fatalf("a llama's temper tops out at 30, got %d", m.temper)
		}
	}
	if !m.tamed || m.owner != pl.p.eid {
		t.Fatalf("the llama should have given in to its rider: tamed=%v temper=%d thrown=%d", m.tamed, m.temper, thrown)
	}
	if thrown == 0 {
		t.Fatal("it should have thrown the rider at least once on the way")
	}
}

// A tamed llama carries a rider but never takes a saddle: a saddle in hand is
// just a ride, and the saddle stays in the hand.
func TestTamedLlamaRidesWithoutSaddle(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityLlama)
	m.tamed, m.owner = true, pl.p.eid
	give(pl, itemSaddle)
	if !h.interactMob(players, pl, m, false) || m.rider != pl.p.eid {
		t.Fatalf("a tamed llama should seat its rider: rider=%d", m.rider)
	}
	if m.saddled || pl.inv.slots[0].count != 1 {
		t.Fatalf("a llama takes no saddle: saddled=%v left=%d", m.saddled, pl.inv.slots[0].count)
	}
}

// A tamed trader llama is the player's now: the trader's despawn clock no
// longer takes it (TraderLlama.canDespawn wants it untamed).
func TestTamedTraderLlamaStays(t *testing.T) {
	h, pl, players, m := ridingSetup(t, entityTraderLlama)
	m.traderDespawn = mobMoveInterval
	m.tamed, m.owner = true, pl.p.eid
	h.traderStep(players, m)
	if h.mobs[m.eid] == nil {
		t.Fatal("a tamed trader llama must not despawn with the trader")
	}
	m.tamed = false
	h.traderStep(players, m)
	if h.mobs[m.eid] != nil {
		t.Fatal("an untamed one still leaves when its clock runs out")
	}
}
