package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The egg cracks twice and then opens into a baby sniffer — and, crucially,
// a passing neighbour update must NOT hatch it early.
func TestSnifferEggHatches(t *testing.T) {
	if !snifferEggOK {
		t.Skip("no sniffer egg block")
	}
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := blockPos{4, 70, 4}
	h.world.SetBlock(pos.x, pos.y, pos.z, snifferEggLo)

	tick := func() {
		h.processUpdate(players, 0, pos)
	}

	// First sighting starts the clock; it must not crack yet.
	tick()
	if h.world.At(pos.x, pos.y, pos.z) != snifferEggLo {
		t.Fatal("the egg changed on first sight")
	}
	// A flurry of neighbour updates before its time changes nothing.
	for i := 0; i < 50; i++ {
		tick()
	}
	if h.world.At(pos.x, pos.y, pos.z) != snifferEggLo {
		t.Fatal("neighbour updates cracked the egg early")
	}

	// Run the clock forward through all three stages.
	info, _ := worldgen.InfoForState(snifferEggLo)
	for stage := 0; stage < 3; stage++ {
		h.tick.Store(h.tick.Load() + snifferHatchTicks)
		tick()
	}
	if got := h.world.At(pos.x, pos.y, pos.z); got != worldgen.Air {
		t.Errorf("the egg is still there as %v (hatch=%s)", got,
			worldgen.GetProperty(info, got, "hatch"))
	}
	n := 0
	for _, m := range h.mobs {
		if m.etype == entitySniffer {
			n++
			if !m.baby {
				t.Error("the egg produced a grown sniffer")
			}
		}
	}
	if n != 1 {
		t.Errorf("%d sniffers hatched, want 1", n)
	}
}

// Moss underneath halves the wait — the one bit of husbandry the egg asks for.
func TestMossSpeedsTheSnifferEgg(t *testing.T) {
	if !snifferEggOK {
		t.Skip("no sniffer egg block")
	}
	h := newHub(world.New(1))
	plain, mossy := blockPos{4, 70, 4}, blockPos{8, 70, 8}
	h.world.SetBlock(mossy.x, mossy.y-1, mossy.z, mossBlockState)

	if h.snifferHatchBoost(0, plain.x, plain.y, plain.z) {
		t.Error("bare ground counted as a hatch boost")
	}
	if !h.snifferHatchBoost(0, mossy.x, mossy.y, mossy.z) {
		t.Error("moss did not count as a hatch boost")
	}

	h.scheduleSnifferEgg(nil, 0, plain.x, plain.y, plain.z)
	h.scheduleSnifferEgg(nil, 0, mossy.x, mossy.y, mossy.z)
	slow := h.snifferEggs[simPos{dim: 0, blockPos: plain}]
	fast := h.snifferEggs[simPos{dim: 0, blockPos: mossy}]
	if fast >= slow {
		t.Errorf("mossy egg due at %d, plain at %d — moss should be sooner", fast, slow)
	}
}

// Cut the base of a chorus tree and the whole thing comes down.
func TestChorusPlantFallsWithoutSupport(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	plant := worldgen.BlockBase("chorus_plant")

	// End stone with a three-high stalk on it.
	h.world.SetBlock(0, 69, 0, endStoneBlock)
	for y := 70; y <= 72; y++ {
		h.world.SetBlock(0, y, 0, plant)
	}
	if !h.chorusPlantSupported(0, 0, 70, 0) {
		t.Fatal("a stalk rooted on end stone should be supported")
	}
	// Cut the bottom segment away.
	h.world.SetBlock(0, 70, 0, worldgen.Air)
	if h.chorusPlantSupported(0, 0, 71, 0) {
		t.Error("a stalk with nothing beneath it is still counted as supported")
	}
	// Ticking the orphaned segments pops them.
	for y := 71; y <= 72; y++ {
		h.processUpdate(players, 0, blockPos{0, y, 0})
	}
	for y := 71; y <= 72; y++ {
		if h.world.At(0, y, 0) != worldgen.Air {
			t.Errorf("chorus plant at y=%d survived losing its support", y)
		}
	}
}

// SnifferEggBlock.onPlace: an egg on moss shows level event 3009.
func TestBoostedSnifferEggSparkles(t *testing.T) {
	if !snifferEggOK {
		t.Skip("no sniffer egg block")
	}
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 8.5, 70, 8.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.world.SetBlock(8, 69, 8, mossBlockState)
	h.world.SetBlock(8, 70, 8, snifferEggLo)
	drainOut(pl.p)
	h.tickSnifferEgg(players, 0, 8, 70, 8, snifferEggLo)
	for len(pl.p.out) > 0 {
		if ev, ok := (<-pl.p.out).ev.(attachproto.WorldFX); ok && ev.Event == worldEventSnifferEggBoost {
			return
		}
	}
	t.Fatal("no 3009 for an egg on moss")
}

// Sniffer.transitionTo: every state change reaches the viewers as DATA_STATE
// (SNIFFER_STATE), in vanilla's numbering.
func TestSnifferStateSynced(t *testing.T) {
	h, _, players, pl := tickCmdHub(t)
	m := h.spawnMobIn(players, entitySniffer, 0, pl.x+2, pl.y, pl.z)
	pl.tracked = map[int32]bool{m.eid: true}
	drainOut(pl.p)
	m.sniffState = sniffDigging
	h.syncSnifferState(players, m)
	h.syncSnifferState(players, m) // unchanged: nothing more
	n := 0
	for _, ev := range drainEvs(pl.p) {
		if me, ok := ev.(attachproto.EntityMeta); ok && me.EID == m.eid {
			n++
			b := me.Meta
			if len(b) < 4 || b[len(b)-2] != 5 { // DIGGING is 5
				t.Fatalf("state entry %x, want DIGGING (5)", b)
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d state frames, want 1", n)
	}
}
