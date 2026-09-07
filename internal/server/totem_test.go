package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A totem in the off hand answers a lethal blow: one health, effects cleared
// and replaced by the totem's three, the totem spent; /kill ignores it.
func TestTotemOfUndying(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.health = 2
	pl.offhand = invStack{item: itemTotem, count: 1}
	h.applyEffect(players, pl, effPoison, 0, 30)

	h.hurtFrom(players, pl, 10, dtGeneric, deathCause{key: causeGeneric}, dmgFrom{})
	if pl.dead {
		t.Fatal("the totem should have saved the player")
	}
	if pl.health != 1 {
		t.Fatalf("health after the totem = %v, want 1", pl.health)
	}
	if pl.offhand.item != 0 {
		t.Fatal("the totem should be spent")
	}
	if pl.hasEffect(effPoison) != 0 {
		t.Fatal("the totem clears every effect")
	}
	if pl.hasEffect(effRegen) == 0 || pl.hasEffect(effAbsorption) == 0 || pl.hasEffect(effFireRes) == 0 {
		t.Fatal("the totem grants regeneration, absorption and fire resistance")
	}

	// No totem left: the next lethal hit kills. A /kill bypasses a totem too.
	pl.health = 1
	pl.offhand = invStack{item: itemTotem, count: 1}
	h.hurtFrom(players, pl, 100, dtGenericKill, deathCause{key: causeGeneric}, dmgFrom{})
	if !pl.dead {
		t.Fatal("a kill command ignores the totem")
	}
}
