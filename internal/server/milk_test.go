package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A bucket and an adult cow is the most basic thing a survival player expects
// to work; a calf and a pig are not cows enough.
func TestMilkingFillsTheBucket(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl

	milk := func(etype int, baby bool) int32 {
		m := h.spawnMobIn(players, etype, 0, 0, 70, 0)
		if m == nil {
			t.Fatalf("spawn of %d returned nil", etype)
		}
		m.baby = baby
		pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBucket, count: 1}
		h.tryMilk(players, pl, m)
		h.tryMilkStew(players, pl, m)
		return pl.inv.slots[pl.p.heldSlot()].item
	}

	for _, c := range []struct {
		name  string
		etype int
		baby  bool
		want  int32
	}{
		{"cow", entityCow, false, itemMilkBucket},
		{"mooshroom", entityMooshroom, false, itemMilkBucket},
		{"goat", entityGoat, false, itemMilkBucket},
		{"calf", entityCow, true, itemBucket},
		{"pig", entityPig, false, itemBucket},
	} {
		if got := milk(c.etype, c.baby); got != c.want {
			t.Errorf("%s: held %d after milking, want %d", c.name, got, c.want)
		}
	}
}

// The mooshroom's other half: a bowl comes back as stew.
func TestMooshroomFillsABowl(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl

	m := h.spawnMobIn(players, entityMooshroom, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBowlEmpty, count: 1}
	if !h.tryMilkStew(players, pl, m) {
		t.Fatal("a bowl on a mooshroom should have filled")
	}
	if got := pl.inv.slots[pl.p.heldSlot()].item; got != itemMushroomStew {
		t.Errorf("held %d after the bowl, want mushroom stew %d", got, itemMushroomStew)
	}
	// …and an ordinary cow does not fill bowls.
	cow := h.spawnMobIn(players, entityCow, 0, 0, 70, 0)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBowlEmpty, count: 1}
	if h.tryMilkStew(players, pl, cow) {
		t.Error("a plain cow filled a bowl with stew")
	}
}

// Milk's one job is stripping every effect, and it must work on a full
// stomach — curing a poison is the whole reason to carry it.
func TestDrinkingMilkClearsEffectsEvenWhenFull(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	pl.food = maxFood

	h.applyEffect(players, pl, effPoison, 0, 30)
	h.applyEffect(players, pl, effSlowness, 0, 30)
	if pl.hasEffect(effPoison) == 0 {
		t.Fatal("the poison never applied — this test would prove nothing")
	}

	slot := pl.p.heldSlot()
	pl.inv.slots[slot] = invStack{item: itemMilkBucket, count: 1}

	// A full stomach must not stop the drink from even starting.
	h.startEating(pl, slot)
	if pl.eatingSlot != slot {
		t.Fatal("a full player could not start drinking milk")
	}
	h.eat(players, pl, slot)

	if pl.hasEffect(effPoison) != 0 || pl.hasEffect(effSlowness) != 0 {
		t.Error("milk left an effect standing")
	}
	if got := pl.inv.slots[slot].item; got != itemBucket {
		t.Errorf("held %d after drinking, want the empty bucket %d", got, itemBucket)
	}
}

// The llama's one attack is the spit, and it comes from well beyond arm's
// reach — a llama that has to touch you to hurt you is not a llama.
func TestLlamaSpits(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 12, 70, 0 // far outside melee range
	players[pl.p.eid] = pl

	m := h.spawnMobIn(players, entityLlama, 0, 0, 70, 0)
	if m == nil {
		t.Fatal("spawn returned nil")
	}
	m.hostile = true // as provoke() leaves it after a hit

	before := len(h.arrows)
	h.llamaSpit(players, m)
	if len(h.arrows) != before+1 {
		t.Fatalf("the llama did not spit: %d projectiles, want %d", len(h.arrows), before+1)
	}
	var spit *arrowEntity
	for _, a := range h.arrows {
		spit = a
	}
	if spit.dmg != llamaSpitDmg {
		t.Errorf("spit does %d damage, want %d", spit.dmg, llamaSpitDmg)
	}
	if spit.shooter != m.eid {
		t.Errorf("spit credited to %d, want the llama %d", spit.shooter, m.eid)
	}

	// …and it respects its cooldown rather than firing every update.
	n := len(h.arrows)
	h.llamaSpit(players, m)
	if len(h.arrows) != n {
		t.Error("the llama spat twice without waiting out its cooldown")
	}

	// A llama has no bite at all — that is what makes the spit its attack.
	if speciesTable[entityLlama].damage != 0 {
		t.Error("llamas should carry no melee damage")
	}
}
