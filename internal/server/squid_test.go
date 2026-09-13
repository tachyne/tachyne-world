package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSquidInkAndEndermite: a hurt glow squid squirts, goes dark for a
// hundred ticks and jets away from its attacker; an endermite lives two
// minutes; one pearl in twenty lands one.
func TestSquidInkAndEndermite(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 184; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	pl.x, pl.y, pl.z = 3.5, 181, 0.5
	sq := h.spawnMob(players, entityGlowSquid, 0.5, 181, 0.5)
	sq.lastAttacker = pl.p.eid
	sq.hurtKind(1, dtMobAttack)
	if !sq.squidHurt {
		t.Fatal("the blow should be recorded")
	}
	if !h.squidStep(players, sq) || sq.glowDark != glowDarkTicks || sq.vx >= 0 {
		t.Fatalf("ink, dark and fleeing west: dark %d vx %.3f", sq.glowDark, sq.vx)
	}
	pl.x = 20
	if h.squidStep(players, sq) {
		t.Fatal("beyond ten blocks the flight ends")
	}
	for i := 0; i < 60 && sq.glowDark > 0; i++ {
		h.squidStep(players, sq)
	}
	if sq.glowDark != 0 {
		t.Fatal("the glow returns after a hundred ticks")
	}
	em := h.spawnMob(players, entityEndermite, 0.5, 185, 0.5)
	for i := 0; i < endermiteLife/mobMoveInterval+1 && h.mobs[em.eid] != nil; i++ {
		h.endermiteTick(players, em)
	}
	if h.mobs[em.eid] != nil {
		t.Fatal("an endermite lives two minutes")
	}
	a := &arrowEntity{dim: 0, x: 0.5, y: 185, z: 0.5}
	spawned := 0
	for i := 0; i < 400; i++ {
		before := len(h.mobs)
		h.pearlEndermite(players, a)
		if len(h.mobs) > before {
			spawned++
		}
	}
	if spawned < 5 || spawned > 50 {
		t.Fatalf("about one pearl in twenty: %d of 400", spawned)
	}
}
