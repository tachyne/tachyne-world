package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestLiftCallLane rebuilds three lanes of a player's lift call panel and
// presses the middle button through the click a client sends. The lane is
// button → block → trapdoor → wall column → observer chain → hopper lock →
// observer → block → dropper, whose one item is its lane's token. The cell
// the dropper faces is air where an upper dropper was taken away, and the
// hub still held that dropper's storage.
//
// Vanilla: the pressed lane's dropper throws its item up into the air
// (DropperBlock: no container ahead, DefaultDispenseItemBehavior), and only
// that lane moves. The wall of the lane beside it, which was set beside a
// dirt block without joining it, is not touched: WallBlock.sideUpdate
// re-reads only the side the change came from.
//
// The engine put the item into the storage of the dropper that was no longer
// there, so nothing came out, and re-read every side of the wall beside the
// pressed one, so that lane fired too.
func TestLiftCallLane(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	s.modes.set(p.name, gmSurvival)
	x, y, z := 2, 183, 2 // the pressed button
	token := int32(itemByName["red_dye"])
	var (
		quartz = worldgen.BlockBase("quartz_bricks")
		planks = worldgen.BlockBase("oak_planks")
		wall   = withProps(t, worldgen.BlockBase("diorite_wall"), map[string]string{"up": "false", "waterlogged": "false", "north": "tall", "south": "tall", "east": "none", "west": "none"})
		trap   = withProps(t, worldgen.BlockBase("poplar_trapdoor"), map[string]string{"facing": "west", "half": "top", "open": "false", "powered": "false", "waterlogged": "false"})
		btn    = withProps(t, worldgen.BlockBase("polished_blackstone_button"), map[string]string{"face": "wall", "facing": "west", "powered": "false"})
		obsUp  = withProps(t, worldgen.BlockBase("observer"), map[string]string{"facing": "up", "powered": "false"})
		obsE   = withProps(t, worldgen.BlockBase("observer"), map[string]string{"facing": "east", "powered": "false"})
		hopper = withProps(t, worldgen.BlockBase("hopper"), map[string]string{"facing": "down", "enabled": "true"})
		drop   = withProps(t, worldgen.BlockBase("dropper"), map[string]string{"facing": "up", "triggered": "false"})
	)
	lane := func(zz int) {
		w.SetBlock(x, y, zz, btn)
		w.SetBlock(x+1, y, zz, quartz)
		w.SetBlock(x+2, y, zz, trap)
		w.SetBlock(x+3, y, zz, wall)
		w.SetBlock(x+3, y+1, zz, wall)
		w.SetBlock(x+3, y-1, zz, obsUp) // watches the foot of the wall column
		w.SetBlock(x+2, y-1, zz, obsE)  // watches that observer
		w.SetBlock(x+2, y-2, zz, obsUp) // …and locks the hopper under it
		w.SetBlock(x+2, y-3, zz, hopper)
		w.SetBlock(x+2, y-4, zz, obsUp) // sees the hopper lock and powers the block under it
		w.SetBlock(x+2, y-5, zz, planks)
		w.SetBlock(x+3, y-5, zz, drop)
		h.binAt(simPos{blockPos: blockPos{x + 3, y - 5, zz}}, drop).slots[0] = invStack{item: token, count: 1}
	}
	onHub(t, h, func() {
		for yy := y - 7; yy <= y+3; yy++ {
			for xx := x - 2; xx <= x+5; xx++ {
				for zz := z - 3; zz <= z+3; zz++ {
					w.SetBlock(xx, yy, zz, worldgen.Air)
				}
			}
		}
		for zz := z - 1; zz <= z+1; zz++ {
			lane(zz)
		}
		// The dirt beside the next lane's wall, which the wall never joined.
		w.SetBlock(x+4, y, z+1, worldgen.Dirt)
		// The storage of the upper dropper that is gone from the middle lane.
		h.bins[simPos{blockPos: blockPos{x + 3, y - 4, z}}] = &bin{slots: make([]invStack, 9), cooldown: -1}
		for zz := z - 1; zz <= z+1; zz++ {
			h.registerHopper(simPos{blockPos: blockPos{x + 2, y - 3, zz}})
		}
		h.rescheduleRedstone()
	})
	stepHub(t, h, 5) // the observers take their first look and settle
	var nextWall uint32
	onHub(t, h, func() { nextWall = w.Block(x+3, y, z+1) })

	p.x, p.y, p.z, p.yaw = float64(x)-0.7, float64(y)-1, float64(z)+0.5, -90 // in front of the panel
	blockReachChecked = true
	s.handlePlace(p, useOnBody(0, x, y, z, 4))
	blockReachChecked = false

	var thrown *itemEntity
	var rose, nextFired, orphanFilled bool
	for i := 0; i < 40; i++ {
		onHub(t, h, func() {
			for _, it := range h.items {
				if it.item == token && it.dim == 0 && floorInt(it.x) == x+3 && floorInt(it.z) == z {
					if thrown == nil {
						thrown = it
					} else if it.y > float64(y-4)+0.2 {
						rose = true
					}
				}
			}
			if w.Block(x+3, y, z+1) != nextWall || boolProp(w.Block(x+3, y-5, z+1), "triggered") {
				nextFired = true
			}
			if b := h.bins[simPos{blockPos: blockPos{x + 3, y - 4, z}}]; b != nil && !binEmpty(b.slots) {
				orphanFilled = true
			}
		})
	}
	if orphanFilled {
		t.Error("the dropper put its item into the storage of a dropper that is no longer there")
	}
	if thrown == nil {
		t.Fatal("the pressed lane's dropper never threw its item out")
	}
	if !rose {
		t.Error("the thrown item did not fly up out of the dropper (it should leave at 0.2 upward)")
	}
	if nextFired {
		t.Error("pressing one lane changed the next lane's wall or fired its dropper")
	}
}

// stepHub waits out n hub ticks.
func stepHub(t *testing.T, h *hub, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		onHub(t, h, func() {})
	}
}

// TestContainerStorageFollowsTheBlock: a hopper, dropper or comparator asks
// the BLOCK for its container (HopperBlockEntity.getContainerAt): storage
// left at a cell whose block has gone is nothing, and a container block that
// nobody has opened yet is a container all the same. The boot sweep drops
// storage with no block, its contents falling where the block stood.
func TestContainerStorageFollowsTheBlock(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	w.ForceLoad(0, 0, 1)
	stone := int32(itemByName["stone"])
	gone := simPos{blockPos: blockPos{3, 180, 3}}
	w.SetBlock(3, 180, 3, worldgen.Air)
	orphan := &bin{slots: make([]invStack, 9)}
	orphan.slots[2] = invStack{item: stone, count: 7}
	h.bins[gone] = orphan
	if h.containerSlots(gone) != nil {
		t.Error("storage at an air cell answered as a container")
	}
	if sig := h.containerSignal(gone); sig > 0 {
		t.Errorf("a comparator read %d from an air cell's leftover storage", sig)
	}

	fresh := simPos{blockPos: blockPos{5, 180, 3}}
	w.SetBlock(5, 180, 3, withProps(t, worldgen.BlockBase("dropper"), map[string]string{"facing": "up", "triggered": "false"}))
	if !h.insertByFace(fresh, -1, invStack{item: stone, count: 1}) {
		t.Error("a dropper nobody had opened refused an item")
	}
	oven := simPos{blockPos: blockPos{7, 180, 3}}
	w.SetBlock(7, 180, 3, withProps(t, worldgen.BlockBase("furnace"), map[string]string{"facing": "north", "lit": "false"}))
	if !h.insertByFace(oven, -1, invStack{item: int32(itemByName["raw_iron"]), count: 1}) {
		t.Error("a furnace nobody had opened refused a smelting input from above")
	}

	h.items = map[int32]*itemEntity{}
	h.dropOrphanStorage()
	if _, ok := h.bins[gone]; ok {
		t.Error("the boot sweep kept storage whose block has gone")
	}
	if _, ok := h.bins[fresh]; !ok {
		t.Error("the boot sweep dropped a dropper's storage")
	}
	got := 0
	for _, it := range h.items {
		if it.item == stone {
			got += it.count
		}
	}
	if got != 7 {
		t.Errorf("the leftover storage's 7 stone fell as %d", got)
	}
}

// TestDropperThrowsLikeVanilla: DefaultDispenseItemBehavior.spawnItem — the
// item leaves 0.7 out of the face, an eighth lower for a vertical facing and
// 5/32 lower for a horizontal one, thrown at 0.2-0.3 along the facing and 0.2
// upward, each spread by triangle(·, 0.103365).
func TestDropperThrowsLikeVanilla(t *testing.T) {
	const spread = 0.0172275 * 6
	for _, c := range []struct {
		facing      string
		dx, dy      float64
		sx, sy, sz  float64 // expected spawn point
		lowVX, hiVX float64
		lowVY, hiVY float64
		lowVZ, hiVZ float64
	}{
		{"up", 0, 1, 5.5, 71.075, 5.5, -spread, spread, 0.2 - spread, 0.2 + spread, -spread, spread},
		{"east", 1, 0, 6.2, 70.34375, 5.5, 0.2 - spread, 0.3 + spread, 0.2 - spread, 0.2 + spread, -spread, spread},
	} {
		h := newTestHub(world.New(1))
		state := withProps(t, worldgen.BlockBase("dropper"), map[string]string{"facing": c.facing, "triggered": "false"})
		h.world.SetBlock(5, 70, 5, state)
		h.world.SetBlock(5+int(c.dx), 70+int(c.dy), 5, worldgen.Air)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: int32(itemByName["stone"]), count: 1}
		h.bins[simPos{blockPos: blockPos{5, 70, 5}}] = b
		h.items = map[int32]*itemEntity{}
		h.ejectFromBin(nil, simPos{blockPos: blockPos{5, 70, 5}}, state)
		if len(h.items) != 1 {
			t.Fatalf("%s: %d items thrown, want 1", c.facing, len(h.items))
		}
		for _, it := range h.items {
			const eps = 1e-9
			if d := it.x - c.sx; d > eps || d < -eps || it.y-c.sy > eps || it.y-c.sy < -eps || it.z-c.sz > eps || it.z-c.sz < -eps {
				t.Errorf("%s: thrown from (%.4f %.4f %.4f), want (%.4f %.4f %.4f)", c.facing, it.x, it.y, it.z, c.sx, c.sy, c.sz)
			}
			if it.vx < c.lowVX || it.vx > c.hiVX || it.vy < c.lowVY || it.vy > c.hiVY || it.vz < c.lowVZ || it.vz > c.hiVZ {
				t.Errorf("%s: thrown at (%.3f %.3f %.3f)", c.facing, it.vx, it.vy, it.vz)
			}
		}
	}
}

// TestConnectorsReadOnlyTheChangedSide: updateShape hands a connector one
// direction at a time. A wall re-reads the side the change came from and
// keeps the rest (sideUpdate), ignores the cell below and, from above, only
// re-reads heights and post (topUpdate); a fence or pane sets only that
// side. A wall set beside dirt without joining it keeps that side open
// until the dirt itself changes.
func TestConnectorsReadOnlyTheChangedSide(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	for y := 199; y <= 202; y++ {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	wall := withProps(t, worldgen.BlockBase("diorite_wall"), map[string]string{"up": "false", "waterlogged": "false", "north": "low", "south": "low", "east": "none", "west": "none"})
	info, _ := wallInfo(wall)
	w.SetBlock(0, 200, -1, wall)
	w.SetBlock(0, 200, 1, wall)
	w.SetBlock(0, 200, 0, wall)
	w.SetBlock(1, 200, 0, worldgen.Dirt) // beside it, never joined
	n := blockPos{0, 200, 0}
	if got := wallUpdated(w, n, info, wall, [3]int{0, 0, -1}); got != wall {
		t.Errorf("a change to the north re-read the east side: %s", describeState(got))
	}
	if got := wallUpdated(w, n, info, wall, [3]int{0, -1, 0}); got != wall {
		t.Errorf("a change below changed the wall: %s", describeState(got))
	}
	got := wallUpdated(w, n, info, wall, [3]int{1, 0, 0})
	if worldgen.GetProperty(info, got, "east") != "low" || !boolProp(got, "up") {
		t.Errorf("a change to the east did not join the dirt and raise the T's post: %s", describeState(got))
	}

	fence := withProps(t, worldgen.BlockBase("oak_fence"), map[string]string{"north": "false", "south": "false", "east": "false", "west": "false", "waterlogged": "false"})
	fi, _ := worldgen.InfoForState(fence)
	w.SetBlock(0, 201, 0, fence)
	w.SetBlock(1, 201, 0, worldgen.Dirt)
	w.SetBlock(-1, 201, 0, worldgen.Dirt)
	got = connectUpdated(w, blockPos{0, 201, 0}, fi, fence, [3]int{1, 0, 0})
	if worldgen.GetProperty(fi, got, "east") != "true" || worldgen.GetProperty(fi, got, "west") != "false" {
		t.Errorf("a fence told of its east side set %s", describeState(got))
	}
}

// TestComparatorHearsContainerFill: a dropper passes its item into the
// dropper it faces, and the comparator reading that one lights two ticks
// later (BlockEntity.setChanged → updateNeighbourForOutputSignal, then
// ComparatorBlock's 2-tick delay). It used to stay dark until something else
// happened to update it.
func TestComparatorHearsContainerFill(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	east := withProps(t, worldgen.BlockBase("dropper"), map[string]string{"facing": "east", "triggered": "false"})
	comp := withProps(t, worldgen.BlockBase("comparator"), map[string]string{"facing": "west", "mode": "compare", "powered": "false"})
	w.SetBlock(x, y, z, east)
	w.SetBlock(x+1, y, z, east)
	w.SetBlock(x+2, y, z, comp)
	h.binAt(simPos{blockPos: blockPos{x, y, z}}, east).slots[0] = invStack{item: int32(itemByName["stone"]), count: 1}
	stepTicks(h, players, 3)
	if boolProp(w.Block(x+2, y, z), "powered") {
		t.Fatal("setup: the comparator reads an empty dropper as powered")
	}
	h.ejectFromBin(players, simPos{blockPos: blockPos{x, y, z}}, east)
	if got := ticksUntil(h, players, 10, func() bool { return boolProp(w.Block(x+2, y, z), "powered") }); got != comparatorDelay {
		t.Errorf("the comparator lit %d ticks after the dropper filled, want %d", got, comparatorDelay)
	}
}
