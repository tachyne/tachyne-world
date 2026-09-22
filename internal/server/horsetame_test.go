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

// A camel needs no taming and a llama is never ridden, so neither buck.
func TestOnlyTheThreeEquinesBuck(t *testing.T) {
	for _, etype := range []int{entityHorse, entityDonkey, entityMule} {
		if !horseNeedsTaming(etype) {
			t.Errorf("etype %d should need taming", etype)
		}
	}
	for _, etype := range []int{entityCamel, entityLlama, entitySkeletonHorse, entityPig} {
		if horseNeedsTaming(etype) {
			t.Errorf("etype %d should not buck", etype)
		}
	}
}
