package server

import "testing"

// A creeper shot dead by a parched drops a music disc: the creeper's loot
// table asks whether the killer is in #skeletons, and 26.3 put the parched
// there. It used to drop only gunpowder.
func TestParchedKillDropsCreeperDisc(t *testing.T) {
	h, players := preyFixture(t)
	for i := 0; i < 20; i++ {
		pa := h.spawnMob(players, entityParched, 0.5, 180, 0.5)
		cr := h.spawnMob(players, entityCreeper, 4.5, 180, 0.5)
		cr.health = 1
		h.spawnArrowAt(players, pa, cr.x, cr.y+0.6, cr.z)
		for j := 0; j < 40 && h.mobs[cr.eid] != nil && cr.dying == 0; j++ {
			h.updateArrows(players)
		}
		if cr.dying == 0 && h.mobs[cr.eid] != nil {
			t.Fatalf("the parched's arrow never killed the creeper (health %v)", cr.health)
		}
		for j := 0; j < 2*deathAnimTicks && h.mobs[cr.eid] != nil; j++ { // the death animation, then the drops
			h.tick.Add(1)
			h.updateMobs(players)
		}
		for _, it := range h.items {
			for _, d := range creeperDiscs {
				if it.item == d {
					return
				}
			}
		}
		delete(h.mobs, pa.eid)
	}
	t.Fatal("a creeper killed by a parched never dropped a music disc")
}
