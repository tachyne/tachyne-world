package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A trapped chest is a signal source whose strength is its viewer count: one
// player opening it powers the block beneath (strongly) and its neighbours
// (weakly); closing it drops the signal back to zero.
func TestTrappedChestSignalIsItsViewerCount(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 11.5, 70, 10.5)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	chest := withProps(t, worldgen.BlockBase("trapped_chest"), map[string]string{"facing": "north", "type": "single"})
	h.world.SetBlock(10, 70, 10, chest)
	h.world.SetBlock(10, 69, 10, worldgen.Stone)

	if !h.isSignalSource(chest) || h.ownSignal(10, 70, 10, chest) != 0 {
		t.Fatal("an unopened trapped chest is a source at strength 0")
	}
	h.openChest(pl, 10, 70, 10)
	if pl.winKind != winChest {
		t.Fatalf("chest window kind %v", pl.winKind)
	}
	if got := h.ownSignal(10, 70, 10, chest); got != 1 {
		t.Errorf("open trapped chest signal %d, want 1 (one viewer)", got)
	}
	if got := h.directSignal(10, 70, 10, chest, dUp); got != 1 {
		t.Errorf("direct signal into the block beneath %d, want 1", got)
	}
	if got := h.directSignal(10, 70, 10, chest, dNorth); got != 0 {
		t.Errorf("a trapped chest must not strongly power a side neighbour, got %d", got)
	}
	h.closeWindow(players, pl)
	if got := h.ownSignal(10, 70, 10, chest); got != 0 {
		t.Errorf("closed trapped chest signal %d, want 0", got)
	}
}

// Dust sits on a full cube, a top or double slab, an upside-down stair and a
// closed top trapdoor — and not on a bottom slab, an upright stair or an
// open trapdoor (RedStoneWireBlock.canSurviveOn = isFaceSturdy(UP)).
func TestDustSurvivesOnSturdyTopsOnly(t *testing.T) {
	slab := func(kind string) uint32 {
		return withProps(t, worldgen.BlockBase("oak_slab"), map[string]string{"type": kind})
	}
	stairs := func(half string) uint32 {
		return withProps(t, worldgen.BlockBase("stone_stairs"), map[string]string{"half": half, "facing": "north"})
	}
	trapdoor := func(half, open string) uint32 {
		return withProps(t, worldgen.BlockBase("oak_trapdoor"), map[string]string{"half": half, "open": open, "facing": "north"})
	}
	yes := []uint32{worldgen.Stone, slab("top"), slab("double"), stairs("top"), trapdoor("top", "false"), worldgen.BlockBase("hopper")}
	no := []uint32{slab("bottom"), stairs("bottom"), trapdoor("top", "true"), trapdoor("bottom", "false"), worldgen.Air}
	for _, s := range yes {
		if !canHoldDust(s) {
			t.Errorf("dust should survive on state %d", s)
		}
	}
	for _, s := range no {
		if canHoldDust(s) {
			t.Errorf("dust must not survive on state %d", s)
		}
	}
	// And the support pass agrees: dust on a bottom slab drops, on a top slab stays.
	w := world.New(1)
	wire := worldgen.BlockBase("redstone_wire")
	w.SetBlock(5, 100, 5, slab("top"))
	w.SetBlock(5, 101, 5, wire)
	if !supported(w, blockPos{5, 101, 5}, wire) {
		t.Error("dust on a top slab is supported")
	}
	w.SetBlock(5, 100, 5, slab("bottom"))
	if supported(w, blockPos{5, 101, 5}, wire) {
		t.Error("dust on a bottom slab is not supported")
	}
}

// A torch flipped eight times within sixty ticks burns out: it stays dark
// even once its support is unpowered, and only relights after the 160-tick
// restart delay has let the toggle log expire.
func TestRedstoneTorchBurnsOutWhenToggledTooOften(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	// A floor torch on stone; power the stone from beside it with a lever on
	// its side... simpler: drive supportPowered through a redstone block that
	// we place and remove under the torch's support.
	torchPos := blockPos{10, 71, 10}
	torch := worldgen.BlockBase("redstone_torch") // lit is the low bit, even = lit
	if !torchLit(torch) {
		torch++
	}
	h.world.SetBlock(10, 70, 10, worldgen.Stone)
	h.world.SetBlock(10, 71, 10, torch)
	h.world.SetBlock(10, 69, 10, worldgen.Air)

	flip := func(powered bool) {
		// A lever hanging on the support's side, powered or not, is the
		// cleanest "support has signal" switch.
		lever := withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "wall", "facing": "east", "powered": map[bool]string{true: "true", false: "false"}[powered]})
		h.world.SetBlock(11, 70, 10, lever)
		h.updateRedstone(players, torchPos, h.world.At(10, 71, 10))
	}
	onceIsTheBaseline := func() bool { return torchLit(h.world.At(10, 71, 10)) }
	flip(true)
	if onceIsTheBaseline() {
		t.Fatal("a powered support must switch the torch off")
	}
	flip(false)
	if !onceIsTheBaseline() {
		t.Fatal("an unpowered support must switch the torch back on")
	}
	// Seven more quick off-flips within the window: the eighth burns it out.
	for i := 0; i < 7; i++ {
		flip(true)
		flip(false)
	}
	if onceIsTheBaseline() {
		t.Fatal("after eight quick toggles the torch must stay dark (burnt out)")
	}
	// Time passes past the window: the restart tick relights it.
	h.tick.Store(h.tick.Load() + torchRestartDelay + 1)
	flip(false)
	if !onceIsTheBaseline() {
		t.Fatal("once the toggle log expires the torch relights")
	}
}
