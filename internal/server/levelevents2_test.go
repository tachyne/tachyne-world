package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// fxWatcher is a player standing near (5, 70, 5) whose level events a test
// reads back.
func fxWatcher(h *hub) (*tracked, map[int32]*tracked) {
	pl := survPlayer(h)
	pl.p.eid = 9000
	pl.x, pl.y, pl.z = 5.5, 71, 8.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	return pl, players
}

// events is the ids of the level events queued for a player, and the data
// each carried.
func events(pl *tracked) map[int32][]int32 {
	out := map[int32][]int32{}
	for _, fx := range drainFX(pl) {
		out[fx.Event] = append(out[fx.Event], fx.Data)
	}
	return out
}

// A dispenser's clicks are the behaviour's level events: 1001 when empty,
// 1000 for a dropped item, 1002 for a projectile, 1004 for a rocket.
func TestDispenserClicksAreLevelEvents(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl, players := fxWatcher(h)
	state := eastDispenser(t)
	pos := blockPos{5, 70, 5}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	h.world.SetBlock(6, 70, 5, worldgen.Air)
	fire := func(st invStack) map[int32][]int32 {
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = st
		h.bins[simPos{blockPos: pos}] = b
		drainEvs(pl.p)
		h.ejectFromBin(players, simPos{blockPos: pos}, state)
		return events(pl)
	}
	for _, c := range []struct {
		what string
		st   invStack
		want int32
	}{
		{"an empty dispenser", invStack{}, worldEventDispenseFail},
		{"a stick", invStack{item: int32(itemByName["stick"]), count: 1}, worldEventDispense},
		{"an arrow", invStack{item: itemArrowAmmo, count: 1}, worldEventDispenseLaunch},
		{"a rocket", invStack{item: itemFireworkRocket, count: 1}, worldEventFireworkShoot},
	} {
		got := fire(c.st)
		if len(got[c.want]) != 1 {
			t.Errorf("%s: level events %v, want %d", c.what, got, c.want)
		}
	}
}

// A composter's fill is level event 1500, data 1 when the pile rose.
func TestComposterFillIsALevelEvent(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl, players := fxWatcher(h)
	pos := blockPos{5, 70, 5}
	h.world.SetBlock(pos.x, pos.y, pos.z, composterBase)
	cake := int32(itemByName["cake"]) // chance 1: the pile always rises
	pl.p.setHotbarSlot(0, cake)
	pl.inv.slots[0] = invStack{item: cake, count: 1}
	drainEvs(pl.p)
	h.useComposter(players, pl, pos)
	if got := events(pl)[worldEventComposterFill]; len(got) != 1 || got[0] != 1 {
		t.Fatalf("composter fill events %v, want one with data 1", got)
	}
}

// A crafter's click and puff (1049, 2010 with the facing) come with a stack
// dropped into the world; an empty grid fails with 1050.
func TestCrafterEventsComeWithTheDrop(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl, players := fxWatcher(h)
	state := crafterMin + 24 + uint32(9*2) + 1 // east_up
	pos := blockPos{5, 70, 5}
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	h.world.SetBlock(6, 70, 5, worldgen.Air)
	c := &bin{slots: make([]invStack, 9)}
	h.bins[simPos{blockPos: pos}] = c
	drainEvs(pl.p)
	h.crafterCraft(players, simPos{blockPos: pos}, state)
	if got := events(pl); len(got[worldEventCrafterFail]) != 1 || len(got[worldEventCrafterCraft]) != 0 {
		t.Fatalf("an empty crafter: events %v, want the fail alone", got)
	}
	c.slots[0] = invStack{item: int32(itemByName["oak_log"]), count: 1}
	h.crafterCraft(players, simPos{blockPos: pos}, state)
	got := events(pl)
	if len(got[worldEventCrafterCraft]) != 1 || len(got[worldEventWhiteSmoke]) != 1 || got[worldEventWhiteSmoke][0] != 5 {
		t.Fatalf("a crafted drop: events %v, want 1049 and 2010 facing east (5)", got)
	}
}

// Every tick of a dig shows its crumbs on the face struck (2019), with the
// hit sound on every fourth (2020).
func TestDigProgressLevelEvents(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	digger := survPlayer(h)
	players := map[int32]*tracked{digger.p.eid: digger}
	digger.x, digger.y, digger.z, digger.onGround = 0.5, 200, 0.5, true
	h.world.SetBlock(1, 200, 0, worldgen.BlockBase("stone"))
	digger.inv.slots[digger.p.held] = invStack{item: itemByName["wooden_pickaxe"], count: 1}
	h.startDig(players, evDigStart{eid: digger.p.eid, x: 1, y: 200, z: 0, face: 4})
	drainEvs(digger.p)
	var seq []int32
	for i := 0; i < 8; i++ {
		h.tickDigCracks(players)
		for _, fx := range drainFX(digger) {
			if fx.Data != 4 {
				t.Fatalf("event %d carried face %d, want 4 (west)", fx.Event, fx.Data)
			}
			seq = append(seq, fx.Event)
		}
	}
	want := []int32{2019, 2019, 2019, 2020, 2019, 2019, 2019, 2020}
	if len(seq) != len(want) {
		t.Fatalf("dig events %v, want %v", seq, want)
	}
	for i := range want {
		if seq[i] != want[i] {
			t.Fatalf("dig events %v, want %v", seq, want)
		}
	}
}

// The dragon egg's blink carries the jump in its data (packed against
// radii 16, 8, 16), fired at the cell it left.
func TestDragonEggBlinkEvent(t *testing.T) {
	h := newTestHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	x, z := h.findLand(60, 60)
	y := h.world.SurfaceFeet(x, z) + 3
	pl.x, pl.y, pl.z = float64(x), float64(y), float64(z)
	h.world.SetBlock(x, y, z, worldgen.DragonEgg)
	drainEvs(pl.p)
	h.onDragonEgg(players, evDragonEgg{eid: pl.p.eid, x: x, y: y, z: z})
	var fx *attachproto.WorldFX
	for _, e := range drainFX(pl) {
		if e.Event == worldEventDragonEggTeleport {
			e := e
			fx = &e
		}
	}
	if fx == nil || fx.X != x || fx.Y != y || fx.Z != z {
		t.Fatalf("no 2015 at the egg's old cell: %+v", fx)
	}
	to := blockPos{x + int(fx.Data>>16&0xFF) - 16, y + int(fx.Data>>8&0xFF) - 8, z + int(fx.Data&0xFF) - 16}
	if h.world.At(to.x, to.y, to.z) != worldgen.DragonEgg {
		t.Fatalf("the packed jump points at %v, which holds no egg", to)
	}
}
