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

// TestSheepEatsFernAndDryGrass: #edible_for_sheep is more than short grass —
// a sheared sheep in a fern on stone grazes it; with mobGriefing off the
// fern stays but the wool still grows back.
func TestSheepEatsFernAndDryGrass(t *testing.T) {
	for _, grief := range []bool{true, false} {
		h := newHub(world.New(1))
		h.rules.MobGriefing = grief
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		w := h.worldFor(0)
		fern := worldgen.BlockBase("fern")
		w.SetBlock(0, 179, 0, worldgen.Stone)
		w.SetBlock(0, 180, 0, fern)
		s := h.spawnMob(players, entitySheep, 0.5, 180, 0.5)
		s.baby, s.sheared = false, true
		started := false
		for i := 0; i < 20000 && !started; i++ {
			started = h.grazeStep(players, s)
		}
		if !started {
			t.Fatalf("griefing %v: a sheep in a fern should graze", grief)
		}
		for i := 0; i < 30 && s.grazeTicks > 0; i++ {
			h.grazeStep(players, s)
		}
		want := worldgen.Air
		if !grief {
			want = fern
		}
		if got := w.At(0, 180, 0); got != want || s.sheared {
			t.Fatalf("griefing %v: fern cell %d want %d, sheared %v", grief, got, want, s.sheared)
		}
	}
	if !sheepEdible(worldgen.BlockBase("short_dry_grass")) || !sheepEdible(worldgen.BlockBase("tall_dry_grass")) || sheepEdible(worldgen.BlockBase("dandelion")) {
		t.Fatal("#edible_for_sheep: short/tall dry grass yes, flowers no")
	}
}
