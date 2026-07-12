package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// stepTicks runs the scheduled-update pump n ticks.
func stepTicks(h *hub, players map[int32]*tracked, n int) {
	for i := 0; i < n; i++ {
		age := h.tick.Add(1)
		h.runUpdates(players, age)
	}
}

func redSetup(t *testing.T) (*hub, *world.World, map[int32]*tracked, int, int, int) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	lx, lz := h.findLand(60, 60)
	y := h.world.SurfaceFeet(lx, lz)
	// A flat stone pad so dust has support and geometry is known.
	for dx := -1; dx < 8; dx++ {
		for dz := -1; dz < 2; dz++ {
			w.SetBlock(lx+dx, y-1, lz+dz, worldgen.Stone)
			w.SetBlock(lx+dx, y, lz+dz, worldgen.Air)
			w.SetBlock(lx+dx, y+1, lz+dz, worldgen.Air)
		}
	}
	return h, w, players, lx, y, lz
}

func TestLeverPowersWireToLamp(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	lever := setBoolProp((worldgen.BlockBase("lever") + 9), "powered", false) // default lever state
	w.SetBlock(x, y, z, lever)
	for i := 1; i <= 4; i++ { // four dust cells
		w.SetBlock(x+i, y, z, worldgen.BlockBase("redstone_wire")+1160) // default wire (power 0)
	}
	w.SetBlock(x+5, y, z, lampOff)

	pos := blockPos{x, y, z}
	h.toggleLever(players, pos, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+5, y, z) != lampOn {
		t.Fatalf("lamp should light: wire powers %d %d %d %d, lamp=%d",
			wirePower(w.At(x+1, y, z)), wirePower(w.At(x+2, y, z)),
			wirePower(w.At(x+3, y, z)), wirePower(w.At(x+4, y, z)), w.At(x+5, y, z))
	}
	if p := wirePower(w.At(x+1, y, z)); p != 15 {
		t.Fatalf("first dust cell should carry 15, has %d", p)
	}
	if p := wirePower(w.At(x+4, y, z)); p != 12 {
		t.Fatalf("fourth dust cell should carry 12, has %d", p)
	}
	// Flip the lever off: the ripple decays back and the lamp goes out.
	h.toggleLever(players, pos, w.At(x, y, z))
	stepTicks(h, players, 40)
	if w.At(x+5, y, z) != lampOff {
		t.Fatal("lamp should go dark after the lever opens")
	}
	if p := wirePower(w.At(x+2, y, z)); p != 0 {
		t.Fatalf("dust should fully decay, cell 2 still %d", p)
	}
}

func TestButtonPulsesAndReleases(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, (worldgen.BlockBase("stone_button") + 9)) // stone button default
	w.SetBlock(x+1, y, z, lampOff)
	h.pressButton(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 3)
	if w.At(x+1, y, z) != lampOn {
		t.Fatal("pressed button should light the lamp")
	}
	stepTicks(h, players, buttonPressTicks+8)
	if boolProp(w.At(x, y, z), "powered") {
		t.Fatal("button must release after its press window")
	}
	stepTicks(h, players, 4)
	if w.At(x+1, y, z) != lampOn {
		// released → lamp off
	} else {
		t.Fatal("lamp should go out when the button releases")
	}
}

func TestTorchInverts(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y-1, z, worldgen.Stone)                         // support
	w.SetBlock(x, y, z, worldgen.BlockBase("redstone_torch"))     // lit floor torch on it
	w.SetBlock(x+1, y-1, z, worldgen.BlockBase("redstone_block")) // redstone block beside the SUPPORT
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if torchLit(w.At(x, y, z)) {
		t.Fatal("a torch whose support is powered must turn off")
	}
	w.SetBlock(x+1, y-1, z, worldgen.Stone) // remove the power
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 4)
	if !torchLit(w.At(x, y, z)) {
		t.Fatal("torch should relight when the power goes")
	}
}

func TestRedstonePrimesTNT(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, tntStateMax)
	w.SetBlock(x+1, y, z, worldgen.BlockBase("redstone_block")) // redstone block
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 3)
	if len(h.tnt) != 1 || w.At(x, y, z) != worldgen.Air {
		t.Fatalf("powered TNT must prime: entities=%d state=%d", len(h.tnt), w.At(x, y, z))
	}
}
