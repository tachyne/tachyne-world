package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Ice and snow melt on BLOCK light > 11 (IceBlock/SnowLayerBlock.randomTick).
//
// Block light specifically, not the combined brightness — which is why daylight
// never melts ice in vanilla but a torch does. Getting that wrong would melt
// every exposed pond at dawn.

// litChamber carves a pocket underground (lighting is height-capped, so a box
// in open air proves nothing) and optionally puts a glowstone beside the target.
func litChamber(h *hub, x, y, z int, lit bool) {
	for dx := -2; dx <= 2; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	h.world.SetBlock(x, y, z, worldgen.Air)
	h.world.SetBlock(x+1, y, z, worldgen.Air)
	if lit {
		h.world.SetBlock(x+1, y, z, worldgen.BlockID("glowstone"))
	}
}

func TestIceMeltsUnderBrightBlockLight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 40, 40, 40

	litChamber(h, x, y, z, true)
	h.world.SetBlock(x, y, z, iceBlock)

	if bl := h.blockLight(0, x, y, z); bl <= 11 {
		t.Fatalf("test setup: block light %d beside the lamp, need > 11", bl)
	}
	h.tickThaw(players, 0, x, y, z, iceBlock)
	if got := h.world.At(x, y, z); got != worldgen.WaterBase {
		t.Errorf("lit ice is state %d, want water %d", got, worldgen.WaterBase)
	}
}

func TestIceSurvivesInTheDark(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 60, 40, 60

	litChamber(h, x, y, z, false)
	h.world.SetBlock(x, y, z, iceBlock)

	if bl := h.blockLight(0, x, y, z); bl > 11 {
		t.Fatalf("test setup: block light %d in the dark pocket, want <= 11", bl)
	}
	for i := 0; i < 200; i++ {
		h.tickThaw(players, 0, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != iceBlock {
		t.Errorf("unlit ice melted: state %d", got)
	}
}

// Daylight must NOT melt ice — it is sky light, and vanilla reads block light.
func TestDaylightDoesNotMeltIce(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 90, 200, 90 // open sky, full sky light, no block light

	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	h.world.SetBlock(x, y, z, iceBlock)

	sky, block := h.world.LightAt(x, y, z)
	if block > 11 {
		t.Fatalf("test setup: block light %d under open sky, want 0", block)
	}
	if sky == 0 {
		t.Fatal("test setup: expected sky light under open sky")
	}
	for i := 0; i < 200; i++ {
		h.tickThaw(players, 0, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != iceBlock {
		t.Errorf("daylight melted ice: state %d (sky %d, block %d)", got, sky, block)
	}
}

func TestSnowLayerMeltsAndDropsSnowballs(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 80, 40, 80

	litChamber(h, x, y, z, true)
	h.world.SetBlock(x, y-1, z, worldgen.Stone)
	h.world.SetBlock(x, y, z, snowLayer1)

	before := len(h.items)
	h.tickThaw(players, 0, x, y, z, snowLayer1)
	if got := h.world.At(x, y, z); got != worldgen.Air {
		t.Errorf("lit snow is state %d, want air", got)
	}
	if len(h.items) <= before {
		t.Error("melting snow dropped nothing; vanilla drops a snowball per layer")
	}
}

// Ice in the Nether evaporates rather than leaving water behind.
func TestIceEvaporatesInTheNether(t *testing.T) {
	h := dimHub()
	players := map[int32]*tracked{}
	x, y, z := 50, 40, 50

	h.nether.SetBlock(x, y, z, iceBlock)
	h.nether.SetBlock(x+1, y, z, worldgen.BlockID("glowstone"))
	if bl := h.blockLight(1, x, y, z); bl <= 11 {
		t.Skipf("nether test setup: block light %d, need > 11", bl)
	}
	h.tickThaw(players, 1, x, y, z, iceBlock)
	if got := h.nether.At(x, y, z); got != worldgen.Air {
		t.Errorf("nether ice left state %d, want air (water evaporates)", got)
	}
}
