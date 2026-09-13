package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestFoxTrust: a cub born of foxes fed by a player trusts them, a
// trusted player is not fled, and what hurts a trusted player becomes the
// fox's quarry.
func TestFoxTrust(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	f := h.spawnMob(players, entityFox, 0.5, 180, 0.5)
	if avoidPlayerExempt(f, pl) {
		t.Fatal("a wild fox trusts nobody")
	}
	foxAddTrusted(f, pl.p.name)
	if !avoidPlayerExempt(f, pl) || !foxTrusts(f, pl) {
		t.Fatal("a fox that trusts a player does not run from them")
	}
	h.avoidScan(players, f)
	if f.avoidLeft != 0 {
		t.Fatal("no flight from a trusted player")
	}
	z := h.spawnMob(players, entityZombie, 9.5, 180, 0.5)
	pl.lastHurtByMob = z.eid
	if d := h.foxDefendTarget(players, f); d != z {
		t.Fatalf("what hurt a trusted player is the fox's quarry: %v", d != nil)
	}
	// Breeding: the cub trusts the breeder.
	a := h.spawnMob(players, entityFox, 0.5, 180, 3.5)
	b := h.spawnMob(players, entityFox, 1.5, 180, 3.5)
	a.lovedBy, b.lovedBy = pl.p.eid, pl.p.eid
	a.loveTicks, b.loveTicks = 100, 100
	a.baby, b.baby = false, false
	h.gridDirty()
	h.updateBreeding(players)
	cub := (*mob)(nil)
	for _, m := range h.mobs {
		if m.etype == entityFox && m.baby {
			cub = m
		}
	}
	if cub == nil || !foxTrusts(cub, pl) {
		t.Fatalf("the cub trusts whoever fed its parents: cub %v", cub != nil)
	}
}
