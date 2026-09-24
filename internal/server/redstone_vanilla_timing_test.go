package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Redstone timing against vanilla, measured from the moment of a player's
// click. A click is handled between ticks, as vanilla handles the use-item
// packet before the next level tick: "0" means the change is already there
// when the click returns, "2" that it lands on the second tick after it.
//
// The model these numbers pin down: a block change notifies its neighbours
// AT ONCE, within the same tick (the neighbour updater), and components that
// wait do it with a scheduled tick (delay, then priority, then the order it
// was scheduled in). Every expected value cites the vanilla constant it
// comes from.

// timingSetup is a bigger version of redSetup: a flat stone pad with room
// for a line of repeaters, a clock loop and a torch tower.
func timingSetup(t *testing.T) (*hub, *world.World, map[int32]*tracked, int, int, int) {
	t.Helper()
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	lx, lz := h.findLand(60, 60)
	y := h.world.SurfaceFeet(lx, lz)
	w.ForceLoad(lx, lz, 2)
	for dx := -3; dx < 14; dx++ {
		for dz := -3; dz < 4; dz++ {
			w.SetBlock(lx+dx, y-1, lz+dz, worldgen.Stone)
			for dy := 0; dy < 8; dy++ {
				w.SetBlock(lx+dx, y+dy, lz+dz, worldgen.Air)
			}
		}
	}
	return h, w, players, lx, y, lz
}

// ticksUntil returns how many ticks after the action cond first held (0 =
// already true when the action returned), or -1 within limit.
func ticksUntil(h *hub, players map[int32]*tracked, limit int, cond func() bool) int {
	for i := 0; i <= limit; i++ {
		if cond() {
			return i
		}
		stepTicks(h, players, 1)
	}
	return -1
}

// firstTicks steps up to limit ticks and records, per condition, the first
// tick at which it held (-1 = never).
func firstTicks(h *hub, players map[int32]*tracked, limit int, conds ...func() bool) []int {
	out := make([]int, len(conds))
	for i := range out {
		out[i] = -1
	}
	for tick := 0; tick <= limit; tick++ {
		for i, c := range conds {
			if out[i] < 0 && c() {
				out[i] = tick
			}
		}
		stepTicks(h, players, 1)
	}
	return out
}

func timingLever(t *testing.T) uint32 {
	return withProps(t, worldgen.BlockBase("lever"), map[string]string{"face": "floor", "facing": "east", "powered": "false"})
}

func repeaterWithDelay(t *testing.T, facing string, delay int) uint32 {
	return withProps(t, repeaterMin, map[string]string{"facing": facing, "delay": itoa(delay), "powered": "false", "locked": "false"})
}

func rsWallTorch(facing string, lit bool) uint32 {
	idx := map[string]uint32{"north": 0, "south": 1, "west": 2, "east": 3}[facing]
	return torchWithLit(rsWallTorchMin+idx*2, lit)
}

func floorTorch(lit bool) uint32 { return torchWithLit(rsTorchMin, lit) }

// Lever → dust → lamp: dust and lamp both answer in the click's own tick
// (RedStoneWireBlock.neighborChanged → updatePowerStrength and
// RedstoneLampBlock.neighborChanged both act immediately); the lamp goes
// dark 4 ticks after the power leaves (RedstoneLampBlock: scheduleTick 4).
func TestVanillaTimingLampViaDust(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x, y, z, timingLever(t))
	for i := 1; i <= 3; i++ {
		w.SetBlock(x+i, y, z, dust(t))
	}
	w.SetBlock(x+4, y, z, lampOff)
	lever := blockPos{x, y, z}
	h.toggleLever(players, lever, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return w.At(x+4, y, z) == lampOn }); n != 0 {
		t.Errorf("lamp via dust lit at tick %d, vanilla 0 (neighbour updates are immediate)", n)
	}
	if p := wirePower(w.At(x+3, y, z)); p != 13 {
		t.Errorf("third dust carries %d, want 13", p)
	}
	stepTicks(h, players, 3)
	h.toggleLever(players, lever, w.At(x, y, z))
	if p := wirePower(w.At(x+1, y, z)); p != 0 {
		t.Errorf("dust still at %d right after the lever opened, vanilla drops it in the same tick", p)
	}
	if n := ticksUntil(h, players, 10, func() bool { return w.At(x+4, y, z) == lampOff }); n != 4 {
		t.Errorf("lamp went dark at tick %d, vanilla 4 (RedstoneLampBlock scheduleTick 4)", n)
	}
}

// A lever on a block drives a wall torch on the block's far side: the torch
// turns off 2 ticks later and back on 2 ticks after the lever opens
// (RedstoneTorchBlock.neighborChanged: scheduleTick 2).
func TestVanillaTimingWallTorch(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x, y, z, wallLever(t, "west")) // hangs on the stone's west face
	w.SetBlock(x+2, y, z, rsWallTorch("east", true))
	lever := blockPos{x, y, z}
	h.toggleLever(players, lever, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return !torchLit(w.At(x+2, y, z)) }); n != 2 {
		t.Errorf("wall torch went off at tick %d, vanilla 2 (RedstoneTorchBlock scheduleTick 2)", n)
	}
	stepTicks(h, players, 3)
	h.toggleLever(players, lever, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return torchLit(w.At(x+2, y, z)) }); n != 2 {
		t.Errorf("wall torch relit at tick %d, vanilla 2", n)
	}
}

// The same for a torch standing on the block (RedstoneTorchBlock, 2 ticks).
func TestVanillaTimingFloorTorch(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x, y, z, wallLever(t, "west"))
	w.SetBlock(x+1, y+1, z, floorTorch(true))
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return !torchLit(w.At(x+1, y+1, z)) }); n != 2 {
		t.Errorf("floor torch went off at tick %d, vanilla 2", n)
	}
}

// A torch tower: each torch strongly powers the block above it, whose torch
// inverts in turn — every stage adds the torch's 2 ticks.
func TestVanillaTimingTorchChain(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone) // A
	w.SetBlock(x, y, z, wallLever(t, "west"))
	w.SetBlock(x+1, y+1, z, floorTorch(true)) // T1: A unpowered → lit
	w.SetBlock(x+1, y+2, z, worldgen.Stone)   // B: powered by T1
	w.SetBlock(x+1, y+3, z, floorTorch(false))
	w.SetBlock(x+1, y+4, z, worldgen.Stone)
	w.SetBlock(x+1, y+5, z, floorTorch(true))
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	got := firstTicks(h, players, 12,
		func() bool { return !torchLit(w.At(x+1, y+1, z)) },
		func() bool { return torchLit(w.At(x+1, y+3, z)) },
		func() bool { return !torchLit(w.At(x+1, y+5, z)) },
	)
	want := []int{2, 4, 6} // RedstoneTorchBlock scheduleTick 2 per stage
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("torch tower stage %d changed at tick %d, vanilla %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

// A repeater answers its input after delay×2 ticks (RepeaterBlock.getDelay:
// DELAY * 2), turning on and off alike.
func TestVanillaTimingRepeaterDelays(t *testing.T) {
	for delay := 1; delay <= 4; delay++ {
		h, w, players, x, y, z := timingSetup(t)
		w.SetBlock(x, y, z, timingLever(t))
		w.SetBlock(x+1, y, z, repeaterWithDelay(t, "west", delay))
		lever := blockPos{x, y, z}
		h.toggleLever(players, lever, w.At(x, y, z))
		if n := ticksUntil(h, players, 20, func() bool { return boolProp(w.At(x+1, y, z), "powered") }); n != 2*delay {
			t.Errorf("delay-%d repeater turned on at tick %d, vanilla %d (RepeaterBlock.getDelay = delay*2)", delay, n, 2*delay)
		}
		stepTicks(h, players, 10)
		h.toggleLever(players, lever, w.At(x, y, z))
		if n := ticksUntil(h, players, 20, func() bool { return !boolProp(w.At(x+1, y, z), "powered") }); n != 2*delay {
			t.Errorf("delay-%d repeater turned off at tick %d, vanilla %d", delay, n, 2*delay)
		}
	}
}

// Five delay-1 repeaters in a line: +2 per stage, both edges.
func TestVanillaTimingRepeaterChain(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x, y, z, timingLever(t))
	for i := 1; i <= 5; i++ {
		w.SetBlock(x+i, y, z, repeaterWithDelay(t, "west", 1))
	}
	stage := func(i int, on bool) func() bool {
		return func() bool { return boolProp(w.At(x+i, y, z), "powered") == on }
	}
	lever := blockPos{x, y, z}
	h.toggleLever(players, lever, w.At(x, y, z))
	got := firstTicks(h, players, 14, stage(1, true), stage(2, true), stage(3, true), stage(4, true), stage(5, true))
	want := []int{2, 4, 6, 8, 10} // RepeaterBlock.getDelay = 2 per stage
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repeater %d turned on at tick %d, vanilla %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
	h.toggleLever(players, lever, w.At(x, y, z))
	got = firstTicks(h, players, 14, stage(1, false), stage(2, false), stage(3, false), stage(4, false), stage(5, false))
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("repeater %d turned off at tick %d, vanilla %d (all: %v)", i+1, got[i], want[i], got)
		}
	}
}

// A powered repeater facing into another's side locks it in the tick the
// locker turns on (RepeaterBlock.updateShape → LOCKED); the locked repeater
// ignores its input, and once unlocked it takes its own 2 ticks.
func TestVanillaTimingRepeaterLocking(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x+1, y, z, timingLever(t))                     // the victim's input
	w.SetBlock(x+2, y, z, repeaterWithDelay(t, "west", 1))    // the victim
	w.SetBlock(x+2, y, z+1, repeaterWithDelay(t, "south", 1)) // the locker, output north into the victim's side
	w.SetBlock(x+2, y, z+2, timingLever(t))                   // the locker's input
	locker, input := blockPos{x + 2, y, z + 2}, blockPos{x + 1, y, z}
	h.toggleLever(players, locker, w.At(locker.x, locker.y, locker.z))
	got := firstTicks(h, players, 6,
		func() bool { return boolProp(w.At(x+2, y, z+1), "powered") },
		func() bool { return boolProp(w.At(x+2, y, z), "locked") },
	)
	if got[0] != 2 || got[1] != 2 {
		t.Errorf("locker on at %d and victim locked at %d, vanilla 2 and 2", got[0], got[1])
	}
	h.toggleLever(players, input, w.At(input.x, input.y, input.z))
	stepTicks(h, players, 10)
	if boolProp(w.At(x+2, y, z), "powered") {
		t.Fatal("a locked repeater must ignore its input (DiodeBlock.checkTickOnNeighbor returns when locked)")
	}
	h.toggleLever(players, locker, w.At(locker.x, locker.y, locker.z))
	got = firstTicks(h, players, 10,
		func() bool { return !boolProp(w.At(x+2, y, z), "locked") },
		func() bool { return boolProp(w.At(x+2, y, z), "powered") },
	)
	if got[0] != 2 || got[1] != 4 {
		t.Errorf("victim unlocked at %d and turned on at %d, vanilla 2 and 4 (locker's 2, then its own 2)", got[0], got[1])
	}
}

// A comparator takes 2 ticks (ComparatorBlock.getDelay = 2) to follow its
// rear, and 2 to follow a side input in subtract mode; flipping its mode
// refreshes the output at once (ComparatorBlock.useWithoutItem →
// refreshOutputState).
func TestVanillaTimingComparator(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x, y, z, timingLever(t)) // rear
	comp := withProps(t, comparatorMin, map[string]string{"facing": "west", "mode": "subtract", "powered": "false"})
	w.SetBlock(x+1, y, z, comp)
	w.SetBlock(x+1, y, z+1, dust(t))        // side input, fed by…
	w.SetBlock(x+1, y, z+2, timingLever(t)) // …this lever
	cpos := blockPos{x + 1, y, z}
	out := func() int { return h.compOut[simPos{blockPos: cpos}] }
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return out() == 15 && boolProp(w.At(cpos.x, cpos.y, cpos.z), "powered") }); n != 2 {
		t.Errorf("comparator followed its rear at tick %d, vanilla 2", n)
	}
	h.toggleLever(players, blockPos{x + 1, y, z + 2}, w.At(x+1, y, z+2))
	if n := ticksUntil(h, players, 10, func() bool { return out() == 0 }); n != 2 {
		t.Errorf("subtract 15-15 reached 0 at tick %d, vanilla 2", n)
	}
	h.useRedstone1b(players, cpos, w.At(cpos.x, cpos.y, cpos.z)) // → compare
	if n := ticksUntil(h, players, 10, func() bool { return out() == 15 }); n != 0 {
		t.Errorf("compare mode reached 15 at tick %d, vanilla 0 (the click refreshes the output)", n)
	}
}

// An observer's pulse starts 2 ticks after the change it sees
// (ObserverBlock.startSignal: scheduleTick 2) and lasts 2 (ObserverBlock.tick
// schedules its own end 2 later).
func TestVanillaTimingObserver(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x, y, z, timingLever(t))
	w.SetBlock(x+1, y, z, withProps(t, observerMin, map[string]string{"facing": "west", "powered": "false"}))
	w.SetBlock(x+2, y, z, lampOff) // behind the observer
	h.scheduleAround(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 8) // let the observer take its first look
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	obsOn := func() bool { return boolProp(w.At(x+1, y, z), "powered") }
	got := firstTicks(h, players, 12,
		obsOn,
		func() bool { return w.At(x+2, y, z) == lampOn },
	)
	if got[0] != 2 || got[1] != 2 {
		t.Errorf("observer pulse began at %d (lamp at %d), vanilla 2", got[0], got[1])
	}
	// Measure the pulse's length on a fresh change.
	stepTicks(h, players, 10)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	start := ticksUntil(h, players, 10, obsOn)
	length := ticksUntil(h, players, 10, func() bool { return !obsOn() })
	if start != 2 || length != 2 {
		t.Errorf("observer pulse began at %d and lasted %d, vanilla 2 and 2", start, length)
	}
}

// Buttons stay pressed 20 ticks (stone) or 30 (wood) — ButtonBlock
// ticksToStayPressed via BlockSetType — and the lamp they drive lights at
// once and goes dark the lamp's 4 ticks after.
func TestVanillaTimingButtons(t *testing.T) {
	for _, c := range []struct {
		name  string
		ticks int
	}{{"stone_button", 20}, {"oak_button", 30}} {
		h, w, players, x, y, z := timingSetup(t)
		w.SetBlock(x, y, z+1, worldgen.Stone)
		w.SetBlock(x, y, z, withProps(t, worldgen.BlockBase(c.name), map[string]string{"face": "wall", "facing": "north", "powered": "false"}))
		w.SetBlock(x+1, y, z, lampOff)
		h.pressButton(players, blockPos{x, y, z}, w.At(x, y, z))
		wasLit := false
		got := firstTicks(h, players, c.ticks+8,
			func() bool { return w.At(x+1, y, z) == lampOn },
			func() bool { return !boolProp(w.At(x, y, z), "powered") },
			func() bool {
				wasLit = wasLit || w.At(x+1, y, z) == lampOn
				return wasLit && w.At(x+1, y, z) == lampOff
			},
		)
		if got[0] != 0 || got[1] != c.ticks || got[2] != c.ticks+4 {
			t.Errorf("%s: lamp on at %d, released at %d, lamp off at %d; vanilla 0, %d, %d", c.name, got[0], got[1], got[2], c.ticks, c.ticks+4)
		}
	}
}

// A piston moves on a block event, which vanilla runs at the end of the next
// tick (ServerLevel.runBlockEvents), and the moved block lands 2 ticks after
// that (PistonMovingBlockEntity: progress +0.5 a tick, placed on the tick
// after it reaches 1).
func TestVanillaTimingPiston(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	planks := worldgen.BlockID("oak_planks")
	w.SetBlock(x, y, z, timingLever(t))
	w.SetBlock(x+1, y, z, pistonEast(false))
	w.SetBlock(x+2, y, z, planks)
	lever := blockPos{x, y, z}
	h.toggleLever(players, lever, w.At(x, y, z))
	got := firstTicks(h, players, 8,
		func() bool { return boolProp(w.At(x+1, y, z), "extended") },
		func() bool { return w.At(x+3, y, z) == planks },
		func() bool { return isPistonHead(w.At(x+2, y, z)) },
	)
	if got[0] != 1 || got[1] != 3 || got[2] != 3 {
		t.Errorf("piston extended at %d, block landed at %d, head at %d; vanilla 1, 3, 3", got[0], got[1], got[2])
	}
	h.toggleLever(players, lever, w.At(x, y, z))
	got = firstTicks(h, players, 8,
		func() bool { return !isPistonHead(w.At(x+2, y, z)) },
		func() bool { s := w.At(x+1, y, z); return isPistonBase(s) && !boolProp(s, "extended") },
	)
	if got[0] != 1 || got[1] != 3 {
		t.Errorf("piston head gone at %d, base back at %d; vanilla 1 and 3", got[0], got[1])
	}
}

// The classic inverter loop: a torch on a block, a repeater off the torch,
// dust back into the block. Each half-period is the torch's 2 plus the
// repeater's delay×2, so a delay-4 loop has period 2×(2+8) = 20 and lives;
// a delay-1 loop (period 8) toggles the torch 8 times inside 60 ticks and
// burns it out (RedstoneTorchBlock.isToggledTooFrequently: 8 in 60).
func TestVanillaTimingTorchRepeaterClock(t *testing.T) {
	build := func(delay int) (*hub, *world.World, map[int32]*tracked, blockPos) {
		h, w, players, x, y, z := timingSetup(t)
		w.SetBlock(x, y, z, worldgen.Stone)                        // S
		w.SetBlock(x+1, y, z, rsWallTorch("east", true))           // T on S's east face
		w.SetBlock(x+2, y, z, repeaterWithDelay(t, "west", delay)) // fed by T
		for _, c := range [][2]int{{3, 0}, {3, 1}, {3, 2}, {2, 2}, {1, 2}, {0, 2}, {0, 1}} {
			w.SetBlock(x+c[0], y, z+c[1], dust(t)) // back round into S from the south
		}
		h.scheduleAround(blockPos{x + 2, y, z}, 1) // wake the repeater
		return h, w, players, blockPos{x + 1, y, z}
	}
	offTicks := func(h *hub, w *world.World, players map[int32]*tracked, torch blockPos, n int) []int {
		var offs []int
		lit := torchLit(w.At(torch.x, torch.y, torch.z))
		for i := 0; i < n; i++ {
			stepTicks(h, players, 1)
			now := torchLit(w.At(torch.x, torch.y, torch.z))
			if lit && !now {
				offs = append(offs, i+1)
			}
			lit = now
		}
		return offs
	}
	h, w, players, torch := build(4)
	offs := offTicks(h, w, players, torch, 120)
	if len(offs) < 4 {
		t.Fatalf("delay-4 clock turned the torch off only %d times in 120 ticks: %v", len(offs), offs)
	}
	for i := 1; i < len(offs); i++ {
		if d := offs[i] - offs[i-1]; d != 20 {
			t.Errorf("delay-4 torch clock period %d, vanilla 20 = 2*(torch 2 + repeater 8); offs %v", d, offs)
		}
	}
	h, w, players, torch = build(1)
	offs = offTicks(h, w, players, torch, 200)
	if len(offs) < 3 || offs[1]-offs[0] != 8 {
		t.Errorf("delay-1 torch clock offs %v, vanilla period 8 = 2*(2+2)", offs)
	}
	if len(offs) != 8 {
		t.Errorf("delay-1 clock toggled the torch %d times before burning out, vanilla 8 (8 toggles in 60 ticks)", len(offs))
	}
}

// A dispenser fires 4 ticks after its rising edge (DispenserBlock.
// neighborChanged: scheduleTick 4).
func TestVanillaTimingDispenser(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x, y, z, timingLever(t))
	pos := blockPos{x + 1, y, z}
	w.SetBlock(pos.x, pos.y, pos.z, dispEast(dispenserMin))
	h.bins[simPos{blockPos: pos}] = &bin{slots: make([]invStack, 9)}
	h.bins[simPos{blockPos: pos}].slots[0] = invStack{item: itemArrowAmmo, count: 1}
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return len(h.arrows) == 1 }); n != 4 {
		t.Errorf("dispenser fired at tick %d, vanilla 4 (DispenserBlock scheduleTick 4)", n)
	}
}

// A copper bulb flips in the tick its power arrives (CopperBulbBlock.
// neighborChanged → checkAndFlip, no scheduled tick).
func TestVanillaTimingCopperBulb(t *testing.T) {
	h, w, players, x, y, z := timingSetup(t)
	w.SetBlock(x, y, z, timingLever(t))
	w.SetBlock(x+1, y, z, worldgen.BlockBase("copper_bulb")+3) // unlit, unpowered
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	if n := ticksUntil(h, players, 10, func() bool { return worldgen.CopperBulbLit(w.At(x+1, y, z)) }); n != 0 {
		t.Errorf("copper bulb lit at tick %d, vanilla 0", n)
	}
}
