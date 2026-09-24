package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// hopperRig is a chest of 64 cobblestone above a hopper pointing down into
// an empty chest, all at y=100.
func hopperRig(t *testing.T) (*hub, map[int32]*tracked, simPos, *chest, *chest) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	h.playersRef = players
	chestState := worldgen.BlockID("chest")
	top, hop, bottom := blockPos{0, 101, 0}, blockPos{0, 100, 0}, blockPos{0, 99, 0}
	h.world.SetBlock(top.x, top.y, top.z, chestState)
	h.world.SetBlock(hop.x, hop.y, hop.z, hopperWith(hopperMin, true)) // enabled, facing down
	h.world.SetBlock(bottom.x, bottom.y, bottom.z, chestState)
	src, dst := &chest{}, &chest{}
	src.slots[0] = invStack{item: itemByName["cobblestone"], count: 64}
	h.chests[simPos{blockPos: top}] = src
	h.chests[simPos{blockPos: bottom}] = dst
	h.tick.Store(1000)
	return h, players, simPos{blockPos: hop}, src, dst
}

func stepHoppers(h *hub, players map[int32]*tracked, n int) {
	for i := 0; i < n; i++ {
		h.tick.Add(1)
		h.tickHoppers(players)
	}
}

// An idle hopper answers on the very next tick, then moves one item every
// eight: 80 ticks move exactly ten items through it.
func TestHopperTicksEveryTickWithCooldown(t *testing.T) {
	h, players, hop, src, dst := hopperRig(t)
	h.updateHopper(players, hop, h.world.At(hop.x, hop.y, hop.z)) // joins the tickers
	stepHoppers(h, players, 1)
	if src.slots[0].count != 63 {
		t.Fatalf("after one tick the chest above holds %d, want 63: an idle hopper pulls at once", src.slots[0].count)
	}
	stepHoppers(h, players, 79)
	if got := 64 - src.slots[0].count; got != 10 {
		t.Errorf("80 ticks pulled %d items, want 10 (one per 8 ticks)", got)
	}
	if got := dst.slots[0].count; got != 9 && got != 10 {
		t.Errorf("80 ticks pushed %d into the chest below, want 9 or 10", got)
	}
}

// Block updates beside a hopper do not speed it up: each used to start a
// transfer chain of its own.
func TestHopperNeighbourUpdatesDoNotSpeedItUp(t *testing.T) {
	h, players, hop, src, _ := hopperRig(t)
	state := h.world.At(hop.x, hop.y, hop.z)
	for i := 0; i < 80; i++ {
		h.tick.Add(1)
		h.updateHopper(players, hop, state) // a neighbour update every tick
		h.tickHoppers(players)
	}
	if got := 64 - src.slots[0].count; got != 10 {
		t.Errorf("80 ticks of neighbour updates moved %d items, want 10", got)
	}
}

// A powered (disabled) hopper moves nothing but still counts its cooldown.
func TestDisabledHopperHolds(t *testing.T) {
	h, players, hop, src, _ := hopperRig(t)
	h.world.SetBlock(hop.x, hop.y, hop.z, hopperWith(hopperMin, false))
	h.registerHopper(hop)
	stepHoppers(h, players, 40)
	if src.slots[0].count != 64 {
		t.Errorf("a disabled hopper pulled %d items", 64-src.slots[0].count)
	}
}

// A hopper that receives into an empty inventory from another hopper waits
// its eight ticks (seven if it had already ticked this tick) before passing
// the item on. Side by side, so the second cannot pull from the first: A
// (under the full chest, facing east) feeds B (facing down into a chest).
func TestHopperChainHandsOnAfterCooldown(t *testing.T) {
	for _, bFirst := range []bool{false, true} {
		h, players, a, _, dst := hopperRig(t)
		h.world.SetBlock(a.x, a.y, a.z, hopperWith(hopperMin, true)+4) // facing east
		delete(h.chests, simPos{blockPos: blockPos{0, 99, 0}})
		h.world.SetBlock(0, 99, 0, worldgen.Air)
		b := simPos{blockPos: blockPos{1, 100, 0}}
		h.world.SetBlock(1, 100, 0, hopperWith(hopperMin, true)) // facing down
		h.world.SetBlock(1, 99, 0, worldgen.BlockID("chest"))
		h.chests[simPos{blockPos: blockPos{1, 99, 0}}] = dst
		if bFirst { // either ticking order lands on the same tick
			h.registerHopper(b)
			h.registerHopper(a)
		} else {
			h.registerHopper(a)
			h.registerHopper(b)
		}
		arrived := 0
		for tick := 1; tick <= 40 && arrived == 0; tick++ {
			stepHoppers(h, players, 1)
			if dst.slots[0].count > 0 {
				arrived = tick
			}
		}
		// Tick 1: A pulls. Tick 9: A pushes into the empty B, which waits
		// (8 ticks, or 7 when it ticked first) and hands it on at tick 16.
		if arrived != 16 {
			t.Errorf("B ticking first=%v: the first item reached the chest on tick %d, want 16", bFirst, arrived)
		}
	}
}

// Breaking the hopper drops it from the tickers.
func TestHopperLeavesTickersWhenBroken(t *testing.T) {
	h, players, hop, _, _ := hopperRig(t)
	h.registerHopper(hop)
	h.world.SetBlock(hop.x, hop.y, hop.z, worldgen.Air)
	stepHoppers(h, players, 1)
	if h.hopperTicking[hop] || len(h.hopperOrder) != 0 {
		t.Error("a broken hopper still ticks")
	}
}
