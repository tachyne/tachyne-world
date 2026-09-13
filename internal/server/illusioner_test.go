package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestIllusionerSpells: on hard, an illusioner with a target first casts
// its mirror (invisible after the warm-up), then blinds the target once.
func TestIllusionerSpells(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.rules.Difficulty = diffHard
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	il := h.spawnMob(players, entityIllusioner, 0.5, 180, 0.5)
	h.tick.Store(1000)
	h.illusionerTick(players, il)
	if il.illSpell != spellDisappear || il.illWarmup != spellWarmup || il.castLeft != spellCastAnim {
		t.Fatalf("the mirror spell first: spell %d warmup %d anim %d", il.illSpell, il.illWarmup, il.castLeft)
	}
	for i := 0; i < 10 && il.illWarmup > 0; i++ {
		h.illusionerTick(players, il)
	}
	if il.hasEffect(effInvisibility) == 0 || il.illSpell != spellNone {
		t.Fatalf("invisible after the warm-up: inv %d spell %d", il.hasEffect(effInvisibility), il.illSpell)
	}
	for i := 0; i < 12 && il.castLeft > 0; i++ {
		h.illusionerTick(players, il) // the arms drop before the next spell may start
	}
	h.illusionerTick(players, il)
	if il.illSpell != spellBlindness || il.illBlindLast != pl.p.eid {
		t.Fatalf("then blindness on the target: spell %d last %d", il.illSpell, il.illBlindLast)
	}
	for i := 0; i < 10 && il.illWarmup > 0; i++ {
		h.illusionerTick(players, il)
	}
	if pl.hasEffect(effBlindness) == 0 {
		t.Fatal("the target is blinded")
	}
	h.tick.Store(2000)
	for i := 0; i < 12 && il.castLeft > 0; i++ {
		h.illusionerTick(players, il)
	}
	h.illusionerTick(players, il)
	if il.illSpell == spellBlindness {
		t.Fatal("never the same target twice")
	}
}
