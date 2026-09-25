package server

import (
	"path/filepath"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A pot's contents and a brewing stand's clock come back after a restart.
func TestPotsAndBrewsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "containers.json")
	cs := newContainerStore(path)
	pot := simPos{dim: 0, blockPos: blockPos{1, 70, 2}}
	stand := simPos{dim: -1, blockPos: blockPos{3, 40, 4}}
	diamonds := invStack{item: itemByName["diamond"], count: 7}
	cs.recordPots(map[simPos]invStack{pot: diamonds})
	cs.recordBrews(map[simPos]int{stand: 220}, map[simPos]int{stand: 11},
		map[simPos]int32{stand: itemByName["nether_wart"]})
	cs.flush()

	cs2 := newContainerStore(path)
	if got := cs2.loadPots()[pot]; got != diamonds {
		t.Errorf("pot after restart: %+v, want %+v", got, diamonds)
	}
	prog, fuel, ing := cs2.loadBrews()
	if prog[stand] != 220 || fuel[stand] != 11 || ing[stand] != itemByName["nether_wart"] {
		t.Errorf("brew after restart: time=%d fuel=%d ing=%d", prog[stand], fuel[stand], ing[stand])
	}
}

// The brewing stand behaves like vanilla's: a powder is swallowed on sight,
// a brew costs a charge to start and counts down, and swapping the
// ingredient under it throws the brew away.
func TestBrewingStandTick(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := simPos{dim: 0, blockPos: blockPos{6, 70, 6}}
	h.world.ForceLoad(pos.x, pos.z, 1) // a stand brews only in a loaded chunk
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.BlockBase("brewing_stand"))
	b := &bin{slots: make([]invStack, 5)}
	h.bins[pos] = b
	b.slots[4] = invStack{item: itemBlazePowder, count: 2}

	h.updateBrewing(players)
	if h.brewFuel[pos] != brewFuelUses-0 && h.brewFuel[pos] != brewFuelUses {
		t.Fatalf("a powder burns to %d charges, got %d", brewFuelUses, h.brewFuel[pos])
	}
	if b.slots[4].count != 1 {
		t.Fatalf("one powder consumed, %d left", b.slots[4].count)
	}
	if h.brewProg[pos] != 0 {
		t.Fatal("nothing brewable: the clock must stay still")
	}

	// A water bottle and nether wart: the brew starts and spends a charge.
	b.slots[0] = potionStack(potWater)
	b.slots[3] = invStack{item: itemByName["nether_wart"], count: 1}
	fuelBefore := h.brewFuel[pos]
	h.updateBrewing(players)
	if h.brewProg[pos] != brewTicks {
		t.Fatalf("the brew should start at %d ticks, got %d", brewTicks, h.brewProg[pos])
	}
	if h.brewFuel[pos] != fuelBefore-1 {
		t.Fatalf("starting a brew spends one charge: %d → %d", fuelBefore, h.brewFuel[pos])
	}
	// The bottle flag reached the block state.
	st := h.world.At(pos.x, pos.y, pos.z)
	if info, ok := worldgen.InfoForState(st); !ok || worldgen.GetProperty(info, st, "has_bottle_0") != "true" {
		t.Error("has_bottle_0 should follow the bottle in slot 0")
	}

	h.updateBrewing(players)
	if h.brewProg[pos] != brewTicks-1 { // one tick of the 400
		t.Fatalf("the clock counts down, got %d", h.brewProg[pos])
	}
	// Swap the ingredient: the brew is lost.
	b.slots[3] = invStack{item: itemByName["glowstone_dust"], count: 1}
	h.updateBrewing(players)
	if h.brewProg[pos] != 0 {
		t.Fatalf("swapping the ingredient must abandon the brew, got %d", h.brewProg[pos])
	}
}

// A patterned banner comes back patterned when it is broken.
func TestBannerDropsWithItsLayers(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := simPos{dim: 0, blockPos: blockPos{8, 70, 8}}
	h.lastBannerPos = pos
	h.lastBannerLayers = []attachproto.BannerLayer{{Pattern: "minecraft:stripe_bottom", Color: "red"}}
	it := h.spawnItemIn(players, 0, itemByName["white_banner"], 1, 8.5, 70.5, 8.5)
	h.dropBannerLayers(players, pos, it)
	if it.pats[0].patPlus1 == 0 || it.pats[1].patPlus1 != 0 {
		t.Fatalf("the drop should carry exactly one layer: %+v", it.pats)
	}
	if it.pats[0].color != 14 { // red
		t.Errorf("layer colour %d, want red (14)", it.pats[0].color)
	}
	if h.lastBannerLayers != nil {
		t.Error("the held layers should be consumed by the drop")
	}
}
