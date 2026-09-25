package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// pickVia sends a middle click through the gateway's action path and runs
// the hub's handling of it.
func pickVia(h *hub, players map[int32]*tracked, pl *tracked, e attachproto.PickItem) {
	r := &remotePlayer{s: &Server{hub: h}, p: pl.p, gm: -1}
	r.Action(e)
	for len(h.events) > 0 {
		if ev, ok := (<-h.events).(evPickItem); ok {
			h.pickItem(players, players[ev.eid], ev.e)
		}
	}
}

// Middle click on a block (handlePickItemFromBlock → tryPickItem): a stack
// already in the hotbar is selected; one in the main inventory is swapped
// into the first empty hotbar slot; a survival player with none gets
// nothing, a creative one a fresh stack. Custom-named block items and wall
// variants pick their item.
func TestPickItemFromBlock(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	stone, dirt := int32(itemByName["stone"]), int32(itemByName["dirt"])
	h.world.SetBlock(2, 180, 0, worldgen.BlockBase("stone"))
	h.world.SetBlock(0, 180, 2, worldgen.BlockBase("dirt"))
	h.world.SetBlock(-2, 180, 0, worldgen.BlockBase("redstone_wire"))

	pl.inv.slots[0] = invStack{item: dirt, count: 3}
	pl.inv.slots[1] = invStack{item: int32(itemByName["cobblestone"]), count: 1}
	pl.inv.slots[5] = invStack{item: stone, count: 20}
	pl.inv.slots[20] = invStack{item: dirt, count: 9}
	pickVia(h, players, pl, attachproto.PickItem{X: 2, Y: 180, Z: 0})
	if pl.p.heldSlot() != 5 {
		t.Fatalf("picking stone held in hotbar slot 5 selected %d", pl.p.heldSlot())
	}

	pl.inv.slots[0] = invStack{} // dirt now only in main slot 20
	pl.p.setHeldSlot(4)
	pickVia(h, players, pl, attachproto.PickItem{X: 0, Y: 180, Z: 2})
	// From the selected slot 4 on, the first empty hotbar slot is 4 itself.
	if got := pl.p.heldSlot(); got != 4 || pl.inv.slots[4].item != dirt || pl.inv.slots[20].item != 0 {
		t.Fatalf("dirt from the main inventory: held %d, slot 4 %+v, slot 20 %+v", got, pl.inv.slots[4], pl.inv.slots[20])
	}

	pickVia(h, players, pl, attachproto.PickItem{X: -2, Y: 180, Z: 0})
	for i, s := range pl.inv.slots {
		if s.item == int32(itemByName["redstone"]) {
			t.Fatalf("a survival player was given redstone in slot %d", i)
		}
	}

	pl.gamemode = gmCreative
	pickVia(h, players, pl, attachproto.PickItem{X: -2, Y: 180, Z: 0})
	if s := pl.inv.slots[pl.p.heldSlot()]; s.item != int32(itemByName["redstone"]) || s.count != 1 {
		t.Fatalf("a creative pick of redstone wire gave %+v in the held slot", s)
	}

	// Out of reach (block_interaction_range 5 creative + 1): nothing changes.
	h.world.SetBlock(12, 180, 0, worldgen.BlockBase("gold_block"))
	pickVia(h, players, pl, attachproto.PickItem{X: 12, Y: 180, Z: 0})
	for _, s := range pl.inv.slots {
		if s.item == int32(itemByName["gold_block"]) {
			t.Fatal("a block 12 away was picked")
		}
	}
}

// Middle click on a mob picks its spawn egg (Mob.getPickResult).
func TestPickItemFromEntity(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.gamemode = gmCreative
	m := h.spawnMob(players, entityByName["cow"], 2.5, 180, 0.5)
	if m == nil {
		t.Fatal("no cow")
	}
	pickVia(h, players, pl, attachproto.PickItem{Entity: true, EID: m.eid})
	if s := pl.inv.slots[pl.p.heldSlot()]; s.item != int32(itemByName["cow_spawn_egg"]) {
		t.Fatalf("picking a cow gave %+v, want its spawn egg", s)
	}
}

func TestCloneItemNames(t *testing.T) {
	for block, item := range map[string]string{
		"stone": "stone", "wall_torch": "torch", "oak_wall_sign": "oak_sign",
		"oak_wall_hanging_sign": "oak_hanging_sign", "skeleton_wall_skull": "skeleton_skull",
		"tube_coral_wall_fan": "tube_coral_fan", "wheat": "wheat_seeds", "carrots": "carrot",
		"potted_poppy": "poppy", "potted_azalea_bush": "azalea", "flower_pot": "flower_pot",
		"red_candle_cake": "cake", "kelp_plant": "kelp", "tripwire": "string",
		"piston_head": "piston", "water_cauldron": "cauldron", "cobblestone_wall": "cobblestone_wall",
	} {
		if got := cloneItem(worldgen.BlockBase(block)); got != int32(itemByName[item]) || got == 0 {
			t.Errorf("cloneItem(%s) = %d, want %s (%d)", block, got, item, itemByName[item])
		}
	}
	for _, block := range []string{"air", "water", "lava", "fire", "nether_portal", "moving_piston"} {
		if got := cloneItem(worldgen.BlockBase(block)); got != 0 {
			t.Errorf("cloneItem(%s) = %d, want nothing", block, got)
		}
	}
}
