package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The eyeblossom follows the sun: open through the night, shut by day.
func TestEyeblossomFollowsTheSun(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}

	h.dayTime.Store(15000) // night
	w.SetBlock(pos.x, pos.y, pos.z, closedEyeblossom)
	h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, closedEyeblossom)
	if w.At(pos.x, pos.y, pos.z) != openEyeblossom {
		t.Error("an eyeblossom should open at night")
	}

	h.dayTime.Store(6000) // noon
	h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, openEyeblossom)
	if w.At(pos.x, pos.y, pos.z) != closedEyeblossom {
		t.Error("an eyeblossom should close by day")
	}

	// Anything else is left alone.
	if h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, worldgen.BlockBase("stone")) {
		t.Error("the eyeblossom tick claimed a block that is not one")
	}
}

// A nether portal breeds zombified piglins — but only in the Nether, and never
// on peaceful.
func TestNetherPortalBreedsPiglins(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	players := map[int32]*tracked{}
	h.rules.DoMobSpawning = true
	h.rules.Difficulty = diffHard
	pos := blockPos{0, 80, 0}
	nw.SetBlock(pos.x, pos.y-1, pos.z, worldgen.BlockBase("netherrack"))
	nw.SetBlock(pos.x, pos.y, pos.z, netherPortalBase)

	spawned := false
	for i := 0; i < 40000 && !spawned; i++ {
		before := len(h.mobs)
		h.tickNetherPortal(players, 1, pos.x, pos.y, pos.z, netherPortalBase)
		spawned = len(h.mobs) > before
	}
	if !spawned {
		t.Error("a nether portal on hard never bred a piglin")
	}

	// Peaceful breeds nothing.
	h.rules.Difficulty = diffPeaceful
	before := len(h.mobs)
	for i := 0; i < 20000; i++ {
		h.tickNetherPortal(players, 1, pos.x, pos.y, pos.z, netherPortalBase)
	}
	if len(h.mobs) != before {
		t.Error("a portal bred piglins on peaceful")
	}
}

// The flower switches on vanilla's day timeline, not on a window of our own:
// the eyeblossom's open state is keyframed TRUE at 12600 and FALSE at 23401.
func TestEyeblossomSwitchesOnTheDayTimeline(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	pos := blockPos{0, 180, 0}

	for _, tc := range []struct {
		name string
		tick uint64
		want uint32
	}{
		{"the tick before dusk", 12599, closedEyeblossom},
		{"dusk, the tick it opens", 12600, openEyeblossom},
		{"the last tick of the night", 23400, openEyeblossom},
		{"dawn, the tick it shuts", 23401, closedEyeblossom},
	} {
		// Start from the wrong face so a flower that never switches fails.
		from := openEyeblossom
		if tc.want == openEyeblossom {
			from = closedEyeblossom
		}
		h.dayTime.Store(tc.tick)
		w.SetBlock(pos.x, pos.y, pos.z, from)
		h.tickEyeblossom(players, 0, pos.x, pos.y, pos.z, from)
		if got := w.At(pos.x, pos.y, pos.z); got != tc.want {
			t.Errorf("%s (tick %d): flower is %d, want %d", tc.name, tc.tick, got, tc.want)
		}
	}
}

// The day timeline is an overworld thing, so a flower carried into the Nether
// has no hour to follow and keeps the face it went in with.
func TestEyeblossomStaysPutOutsideTheOverworld(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	players := map[int32]*tracked{}
	pos := blockPos{0, 80, 0}

	h.dayTime.Store(6000) // noon: an overworld flower would be shutting
	nw.SetBlock(pos.x, pos.y, pos.z, openEyeblossom)
	if !h.tickEyeblossom(players, 1, pos.x, pos.y, pos.z, openEyeblossom) {
		t.Fatal("the tick disowned an eyeblossom in the Nether")
	}
	if got := nw.At(pos.x, pos.y, pos.z); got != openEyeblossom {
		t.Errorf("a Nether eyeblossom is %d, want it left open (%d)", got, openEyeblossom)
	}
}

// One flower turning wakes the others near it: vanilla schedules every
// eyeblossom still wearing the old face within three blocks across and two up
// or down, at a delay drawn from how far away it is. A garden turns in a wave.
func TestEyeblossomWakesItsNeighbours(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.dayTime.Store(paleNightStart + 10) // night: closed flowers want to open

	x, y, z := 20, 70, 20
	plant := func(p blockPos) { // a flower needs ground under it to survive
		h.world.SetBlock(p.x, p.y-1, p.z, worldgen.BlockBase("dirt"))
		h.world.SetBlock(p.x, p.y, p.z, closedEyeblossom)
	}
	plant(blockPos{x, y, z})
	near := []blockPos{{x + 1, y, z}, {x + 3, y, z}, {x, y + 2, z}}
	for _, p := range near {
		plant(p)
	}
	// Far enough that no chain of boxes reaches it — the wave is transitive,
	// so a flower one step beyond the first box is still woken, by the one
	// between them.
	far := blockPos{x + 20, y, z}
	plant(far)

	if !h.tickEyeblossom(players, dimOverworld, x, y, z, closedEyeblossom) {
		t.Fatal("the random tick should have handled the flower")
	}
	if h.world.At(x, y, z) != openEyeblossom {
		t.Fatal("the flower that was ticked should be open")
	}

	// The neighbours are SCHEDULED, not switched on the spot: the wave takes
	// time to cross, which is the whole point of the distance-scaled delay.
	runTicks(h, players, h.tick.Load(), h.tick.Load()+2)
	if h.world.At(x+3, y, z) == openEyeblossom {
		t.Fatal("a flower three blocks off should not turn within two ticks")
	}
	runTicks(h, players, h.tick.Load(), h.tick.Load()+60)
	for _, p := range near {
		if h.world.At(p.x, p.y, p.z) != openEyeblossom {
			t.Fatalf("the flower at %v should have followed", p)
		}
	}
	if h.world.At(far.x, far.y, far.z) != closedEyeblossom {
		t.Fatal("a flower outside the box should not have been woken")
	}
}
