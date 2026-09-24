package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSculkHearsABell: BellBlock.attemptToRing is a BLOCK_CHANGE game
// event, so a sculk sensor in range hears a bell being rung.
func TestSculkHearsABell(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 4, 180, 4
	w.ForceLoad(x, z, 2)
	for dx := -2; dx <= 6; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimOverworld, x: x, y: y, z: z, state: sensor})
	bell := worldgen.BlockBase("bell")
	w.SetBlock(x+3, y, z, bell)
	stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2) // let the placements settle
	if !h.ringBell(players, dimOverworld, blockPos{x + 3, y, z}, -1) {
		t.Fatal("the bell did not ring")
	}
	stepSculk(h, players, 5)
	if f := h.sculkFreq[simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}]; f != freqBlockChange {
		t.Fatalf("the sensor heard frequency %d, want the bell's BLOCK_CHANGE %d", f, freqBlockChange)
	}
}

// TestCakeBiteVibratesOnlyWhenEaten: CakeBlock.eat returns PASS before its
// EAT game event when the player cannot eat, so a full player's refused
// bite is silent to sculk; a taken bite is heard.
func TestCakeBiteVibratesOnlyWhenEaten(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 4, 180, 4
	w.ForceLoad(x, z, 2)
	for dx := -2; dx <= 6; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimOverworld, x: x, y: y, z: z, state: sensor})
	w.SetBlock(x+3, y, z, cakeBase)
	stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2)
	key := simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}
	delete(h.sculkFreq, key)

	pl.food = maxFood // full: the bite is refused
	h.eatCake(players, pl, blockPos{x + 3, y, z})
	stepSculk(h, players, 5)
	if f := h.sculkFreq[key]; f != 0 {
		t.Fatalf("a refused bite made a vibration (frequency %d)", f)
	}
	pl.food = 10
	h.eatCake(players, pl, blockPos{x + 3, y, z})
	stepSculk(h, players, 5)
	if f := h.sculkFreq[key]; f != freqEat {
		t.Fatalf("a bite taken should be heard as EAT (%d), got %d", freqEat, f)
	}
}

// TestSculkHearsARedstoneDoor: DoorBlock.neighborChanged sends BLOCK_OPEN
// when a signal swings the door open, so sculk hears it.
func TestSculkHearsARedstoneDoor(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 4, 180, 4
	w.ForceLoad(x, z, 2)
	for dx := -2; dx <= 6; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimOverworld, x: x, y: y, z: z, state: sensor})
	door := worldgen.BlockBase("oak_door")
	info, _ := worldgen.InfoForState(door)
	lower := worldgen.SetProperty(info, door, "half", "lower")
	lower = setBoolProp(setBoolProp(lower, "open", false), "powered", false)
	upper := worldgen.SetProperty(info, lower, "half", "upper")
	w.SetBlock(x+3, y, z, lower)
	w.SetBlock(x+3, y+1, z, upper)
	stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2)
	key := simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}
	delete(h.sculkFreq, key)

	w.SetBlock(x+4, y, z, worldgen.BlockBase("redstone_block"))
	h.inDim(0, func() { h.updatePoweredOpenable(players, blockPos{x + 3, y, z}, w.At(x+3, y, z)) })
	if !boolProp(w.At(x+3, y, z), "open") {
		t.Fatal("fixture: the powered door did not open")
	}
	stepSculk(h, players, 5)
	if f := h.sculkFreq[key]; f != freqBlockOpen {
		t.Fatalf("the sensor heard %d, want BLOCK_OPEN %d", f, freqBlockOpen)
	}
}
