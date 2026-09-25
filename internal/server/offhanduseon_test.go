package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// handPlaceBody is a use_item_on packet for one hand (InteractionHand: 0
// main, 1 off), clicking the middle of a face.
func handPlaceBody(hand int32, x, y, z int, face int32) []byte {
	b := protocol.AppendVarInt(nil, hand)
	b = protocol.AppendPosition(b, x, y, z)
	b = protocol.AppendVarInt(b, face)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendF32(b, 0.5)
	b = protocol.AppendBool(b, false)
	b = protocol.AppendBool(b, false)
	return protocol.AppendVarInt(b, 0)
}

// offhandOnRig is a survival player on a live hub with a pickaxe selected
// in the main hand: the case where the client sends the OFF_HAND click,
// because the pickaxe does nothing to the block.
func offhandOnRig(t *testing.T) (*Server, *hub, *player) {
	t.Helper()
	s, h, p := breakPlaceServer(t)
	s.modes.set(p.name, gmSurvival)
	selectSlot(p, 0)
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival
		tr.inv.slots[0] = invStack{item: itemByName["diamond_pickaxe"], count: 1}
		h.sendSlot(tr, 0) // mirrors the hotbar onto the session
	})
	return s, h, p
}

// giveOffhandOn puts a stack in the offhand, hub and session mirror alike.
func giveOffhandOn(t *testing.T, h *hub, p *player, st invStack) {
	t.Helper()
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.offhand = st
		h.sendOffhand(tr)
	})
}

// offhandAfter waits for the hub to have run what the click posted, then
// returns the offhand and main-hand stacks.
func offhandAfter(t *testing.T, h *hub, p *player, done func(off invStack) bool) (off, main invStack) {
	t.Helper()
	for i := 0; i < 40; i++ {
		onHub(t, h, func() {
			tr := h.playersRef[p.eid]
			off, main = tr.offhand, tr.inv.slots[0]
		})
		if done(off) {
			break
		}
	}
	return off, main
}

// stoneFloor lays a stone slab of floor under an air box at (x,y,z).
func stoneFloor(w interface {
	SetBlock(x, y, z int, st uint32)
}, x, y, z int) {
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
		}
	}
}

// A block held in the offhand beside a pickaxe places, and the stack it
// came from is the one that shrinks. The engine read the main hand for
// every use_item_on, so the offhand click did nothing.
func TestOffhandBlockPlacesBesidePickaxe(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 40, 180, 40
	clearAirBox(w, x, y, z, 2)
	stoneFloor(w, x, y, z)
	cobble := itemByName["cobblestone"]
	giveOffhandOn(t, h, p, invStack{item: cobble, count: 3})

	s.handlePlace(p, handPlaceBody(0, x, y-1, z, 1)) // the main-hand click: a pickaxe places nothing
	if got := w.Block(x, y, z); got != worldgen.Air {
		t.Fatalf("the pickaxe click placed %d", got)
	}
	s.handlePlace(p, handPlaceBody(1, x, y-1, z, 1)) // then the offhand click
	if got := w.Block(x, y, z); got != worldgen.BlockBase("cobblestone") {
		t.Fatalf("the offhand cobblestone should be placed on top, got %d", got)
	}
	off, main := offhandAfter(t, h, p, func(o invStack) bool { return o.count == 2 })
	if off.item != cobble || off.count != 2 {
		t.Fatalf("one cobblestone should leave the offhand: %+v", off)
	}
	if main.item != itemByName["diamond_pickaxe"] || main.count != 1 {
		t.Fatalf("the main hand is untouched: %+v", main)
	}
}

// The classic miner's torch: a torch in the offhand goes on the wall that
// was clicked, while the pickaxe stays selected.
func TestOffhandTorchOnWall(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 60, 180, 60
	clearAirBox(w, x, y, z, 2)
	stoneFloor(w, x, y, z)
	w.SetBlock(x, y, z, worldgen.Stone)
	giveOffhandOn(t, h, p, invStack{item: itemByName["torch"], count: 5})

	s.handlePlace(p, handPlaceBody(1, x, y, z, 5)) // east face of the stone
	name, _ := worldgen.StateName(w.Block(x+1, y, z))
	if !strings.Contains(name, "wall_torch") {
		t.Fatalf("an offhand torch on a wall should be a wall torch, got %q", name)
	}
	if off, _ := offhandAfter(t, h, p, func(o invStack) bool { return o.count == 4 }); off.count != 4 {
		t.Fatalf("one torch should leave the offhand: %+v", off)
	}
}

// A water bucket in the offhand pours, and the empty bucket stays in the
// offhand.
func TestOffhandBucketPours(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 80, 180, 80
	clearAirBox(w, x, y, z, 2)
	stoneFloor(w, x, y, z)
	giveOffhandOn(t, h, p, invStack{item: itemBucketH2O, count: 1})

	s.handlePlace(p, handPlaceBody(1, x, y-1, z, 1))
	off, main := offhandAfter(t, h, p, func(o invStack) bool { return o.item == itemBucket })
	if off.item != itemBucket || off.count != 1 {
		t.Fatalf("the offhand should hold the emptied bucket: %+v", off)
	}
	if main.item != itemByName["diamond_pickaxe"] {
		t.Fatalf("the main hand is untouched: %+v", main)
	}
	if !worldgen.IsWater(w.Block(x, y, z)) {
		t.Fatalf("water should be poured on the floor, got %d", w.Block(x, y, z))
	}
}

// Bone meal in the offhand grows a crop and is spent from the offhand.
func TestOffhandBoneMealGrowsCrop(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 100, 180, 100
	clearAirBox(w, x, y, z, 2)
	stoneFloor(w, x, y, z)
	w.SetBlock(x, y-1, z, farmlandMin)
	wheat := worldgen.BlockBase("wheat") // age 0
	w.SetBlock(x, y, z, wheat)
	giveOffhandOn(t, h, p, invStack{item: itemBoneMeal, count: 4})

	s.handlePlace(p, handPlaceBody(1, x, y, z, 1))
	if off, _ := offhandAfter(t, h, p, func(o invStack) bool { return o.count == 3 }); off.count != 3 {
		t.Fatalf("one bone meal should leave the offhand: %+v", off)
	}
	if got := w.Block(x, y, z); got == wheat {
		t.Fatal("the wheat should have grown")
	}
}

// Flint and steel in the offhand lights a fire and wears in the offhand.
func TestOffhandFlintAndSteelLights(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 120, 180, 120
	clearAirBox(w, x, y, z, 2)
	stoneFloor(w, x, y, z)
	giveOffhandOn(t, h, p, invStack{item: itemFlintSteel, count: 1})

	s.handlePlace(p, handPlaceBody(1, x, y-1, z, 1))
	if got := w.Block(x, y, z); got != fireDefault {
		t.Fatalf("the offhand flint and steel should light a fire, got %d", got)
	}
	off, main := offhandAfter(t, h, p, func(o invStack) bool { return o.dmg == 1 })
	if off.item != itemFlintSteel || off.dmg != 1 {
		t.Fatalf("the offhand lighter should wear one point: %+v", off)
	}
	if main.dmg != 0 {
		t.Fatalf("the pickaxe should not wear: %+v", main)
	}
}

// An offhand click reaches only the item-driven uses of a block: a door
// toggles for the main hand's empty-handed use (useWithoutItem), never for
// the OFF_HAND click that follows a pass.
func TestOffhandClickDoesNotOpenDoor(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 140, 180, 140
	clearAirBox(w, x, y, z, 2)
	stoneFloor(w, x, y, z)
	door := worldgen.BlockID("oak_door")
	info, _ := worldgen.InfoForState(door)
	lower := worldgen.SetProperty(info, worldgen.SetProperty(info, door, "half", "lower"), "open", "false")
	w.SetBlock(x, y, z, lower)
	w.SetBlock(x, y+1, z, worldgen.SetProperty(info, lower, "half", "upper"))
	giveOffhandOn(t, h, p, invStack{item: itemByName["torch"], count: 5})

	s.handlePlace(p, handPlaceBody(1, x, y, z, 2))
	if got := w.Block(x, y, z); worldgen.GetProperty(info, got, "open") != "false" {
		t.Fatal("an offhand click must not open the door")
	}
	s.handlePlace(p, handPlaceBody(0, x, y, z, 2))
	if got := w.Block(x, y, z); worldgen.GetProperty(info, got, "open") != "true" {
		t.Fatal("the main-hand click still opens it")
	}
}

// A planted mangrove propagule goes down grown (AGE 4) and standing, as
// MangrovePropaguleBlock.getStateForPlacement sets it.
func TestPlacedPropaguleIsGrown(t *testing.T) {
	s, h, p := offhandOnRig(t)
	w := s.world
	x, y, z := 90, 180, 90
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y, z, worldgen.BlockBase("mud"))
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.inv.slots[0] = invStack{item: itemByName["mangrove_propagule"], count: 1}
		h.sendSlot(tr, 0)
	})
	s.handlePlace(p, handPlaceBody(0, x, y, z, 1))
	got := w.Block(x, y+1, z)
	info, ok := worldgen.InfoForState(got)
	if !ok || !isPropagule(got) {
		t.Fatalf("no propagule was placed: %d", got)
	}
	if worldgen.GetProperty(info, got, "age") != "4" || worldgen.GetProperty(info, got, "hanging") != "false" {
		t.Errorf("placed propagule age=%s hanging=%s, want 4 and false",
			worldgen.GetProperty(info, got, "age"), worldgen.GetProperty(info, got, "hanging"))
	}
}
