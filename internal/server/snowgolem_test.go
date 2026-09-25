package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSnowGolemTrailShootMelt: a snow golem leaves snow where it stands,
// throws a snowball at a hostile within ten blocks (which hurts only a
// blaze), and melts in the Nether.
func TestSnowGolemTrailShootMelt(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 180, z, worldgen.Air)
		}
	}
	g := h.spawnMob(players, entitySnowGolem, 0.5, 180, 0.5)
	h.snowGolemStep(players, g)
	if w.At(0, 180, 0) != snowLayerState {
		t.Fatalf("no snow trail: %d", w.At(0, 180, 0))
	}
	z := h.spawnMob(players, entityZombie, 6.5, 180, 0.5)
	z.hostile = true
	before := len(h.arrows)
	g.attackCD = 0
	for i := 0; i < 50 && len(h.arrows) == before; i++ { // the target goal looks one tick in ten
		h.snowGolemStep(players, g)
	}
	if len(h.arrows) != before+1 || g.attackCD != snowGolemShootEvery {
		t.Fatalf("a hostile in range draws a snowball: arrows %d→%d cd %d", before, len(h.arrows), g.attackCD)
	}
	var ball *arrowEntity
	for _, a := range h.arrows {
		if a.shooter == g.eid {
			ball = a
		}
	}
	if ball == nil || ball.etype != entitySnowball || !ball.mobShot || !ball.breaks || ball.vx <= 0 {
		t.Fatalf("snowball %+v", ball)
	}
	if projectileHitDamage(ball, z) != 0 {
		t.Fatal("a snowball does nothing to a zombie")
	}
	blaze := h.spawnMob(players, entityBlaze, 8.5, 180, 0.5)
	if projectileHitDamage(ball, blaze) != 3 {
		t.Fatal("a snowball does three to a blaze")
	}
	// The Nether melts it.
	g.health = 4
	g.dim = 1
	if h.nether == nil {
		g.dim = 0
		if !h.snowGolemMelts(g) {
			t.Skip("no Nether here and the origin biome does not melt")
		}
	}
	if !h.snowGolemMelts(g) {
		t.Fatal("the Nether melts snow golems")
	}
}

// SnowGolem's target goal reaches its FOLLOW_RANGE of 16, and RangedAttackGoal
// walks it in at 1.25 toward a monster beyond its ten-block throw.
func TestSnowGolemClosesIn(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.worldFor(0)
	w.ForceLoad(0, 0, 2)
	for x := -2; x <= 16; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 180, z, worldgen.Air)
			w.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	g := h.spawnMob(players, entitySnowGolem, 0.5, 180, 0.5)
	z := h.spawnMob(players, entityZombie, 14.5, 180, 0.5)
	z.hostile = true
	h.gridDirty()
	for i := 0; i < 50 && g.snowTarget == 0; i++ {
		h.snowGolemStep(players, g)
	}
	if g.snowTarget != z.eid {
		t.Fatalf("a snow golem targets a monster 14 blocks off (follow range 16), got %d", g.snowTarget)
	}
	if !h.snowGolemChaseStep(g) || g.vx <= 0 {
		t.Fatalf("out of its throwing range it walks toward the monster: vx %v", g.vx)
	}
}
