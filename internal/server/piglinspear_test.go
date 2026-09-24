package server

import (
	"testing"
)

// A piglin with a golden spear fights with it, as PiglinAi's fight activity
// wires SpearApproach, SpearAttack and SpearRetreat: it lowers the spear and
// charges, never swinging it as a sword. A player in gold is left alone.
func TestPiglinFightsWithSpear(t *testing.T) {
	for _, gold := range []bool{false, true} {
		h, _, players, x, y, z := netherPad(t)
		h.rules.Difficulty = diffNormal
		pl := survPlayer(h)
		pl.dim = dimNether
		pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+8.5
		if gold {
			pl.armor[3] = invStack{item: int32(itemByName["golden_helmet"]), count: 1}
		}
		players[pl.p.eid] = pl
		m := h.spawnMobIn(players, entityPiglin, dimNether, float64(x)+0.5, float64(y), float64(z)+0.5)
		if m == nil {
			t.Fatal("no piglin")
		}
		m.baby, m.held, m.hostile = false, itemGoldSpear, true // as a natural spawn arms and marks it
		lowered, swung := false, false
		for i := 0; i < 80; i++ {
			h.tick.Add(1)
			before := pl.health
			h.updateMobs(players)
			if m.spearUseAt != 0 && m.handActive {
				lowered = true
			}
			if pl.health < before && m.spearUseAt == 0 {
				swung = true // hurt with the spear up: a sword swing
			}
			pl.x, pl.z = float64(x)+0.5, float64(z)+8.5 // hold the player still
		}
		switch {
		case gold && lowered:
			t.Fatal("a piglin charged a player wearing gold")
		case !gold && !lowered:
			t.Fatal("a spear piglin with a target never lowered its spear to charge")
		case swung:
			t.Fatal("a spear piglin swung its spear like a sword")
		}
	}
}
