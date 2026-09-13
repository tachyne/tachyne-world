package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSheepGrazeAndStriderShiver: a sheared sheep on grass lowers its head,
// eats the block to dirt and grows its wool back; a strider off lava
// shivers and slows, and warms up again on lava.
func TestSheepGrazeAndStriderShiver(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.GrassBlock)
		}
	}
	s := h.spawnMob(players, entitySheep, 0.5, 180, 0.5)
	s.baby, s.sheared = false, true
	started := false
	for i := 0; i < 20000 && !started; i++ {
		started = h.grazeStep(players, s)
	}
	if !started || s.grazeTicks != grazeTicks {
		t.Fatalf("a sheep on grass should graze: %v %d", started, s.grazeTicks)
	}
	for i := 0; i < 30 && s.grazeTicks > 0; i++ {
		h.grazeStep(players, s)
	}
	if w.At(0, 179, 0) != worldgen.Dirt || s.sheared {
		t.Fatalf("the grass block becomes dirt and the wool regrows: block %d sheared %v", w.At(0, 179, 0), s.sheared)
	}
	st := h.spawnMobIn(players, entityStrider, 1, 0.5, 80, 0.5)
	if st == nil {
		t.Skip("no Nether in this hub")
	}
	nw := h.worldFor(1)
	nw.SetBlock(0, 79, 0, worldgen.Stone)
	nw.SetBlock(0, 80, 0, worldgen.Air)
	base := st.moveSpeed()
	h.striderShiverTick(players, st)
	if !st.striderCold || st.moveSpeed() >= base {
		t.Fatalf("off lava it shivers and slows: cold %v speed %.3f vs %.3f", st.striderCold, st.moveSpeed(), base)
	}
	nw.SetBlock(0, 79, 0, worldgen.LavaBase)
	h.striderShiverTick(players, st)
	if st.striderCold || st.moveSpeed() != base {
		t.Fatal("warm again on lava")
	}
}
