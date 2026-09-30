package server

import (
	"strings"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func pickDataHub(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.gamemode = gmCreative
	return h, players, pl
}

// Ctrl-pick in creative copies a chest's contents onto the picked chest,
// and placing it fills the new chest; a plain pick, or a survival player's,
// carries nothing.
func TestCtrlPickCopiesBlockEntity(t *testing.T) {
	h, players, pl := pickDataHub(t)
	chestState := worldgen.BlockBase("chest")
	src := simPos{dim: 0, blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, chestState)
	c := &chest{}
	c.slots[3] = invStack{item: int32(itemByName["diamond"]), count: 5}
	c.slots[10] = invStack{item: int32(itemByName["iron_sword"]), count: 1, dmg: 7}
	h.chests[src] = c

	pickVia(h, players, pl, attachproto.PickItem{X: 2, Y: 180, Z: 0})
	if st := heldStack(pl); st.item != int32(itemByName["chest"]) || st.beData != "" {
		t.Fatalf("a plain pick is a plain chest: %+v", st)
	}
	pl.inv.slots = [invSize]invStack{}
	pickVia(h, players, pl, attachproto.PickItem{X: 2, Y: 180, Z: 0, IncludeData: true})
	st := heldStack(pl)
	if st.item != int32(itemByName["chest"]) || !strings.Contains(st.beData, "Items") {
		t.Fatalf("ctrl-pick should carry the chest's Items: %+v", st)
	}
	// Persisted with the stack.
	if back := unpackStack(packStack(st)); back.beData != st.beData {
		t.Errorf("block_entity_data did not survive the stack row: %q", back.beData)
	}

	dst := simPos{dim: 0, blockPos: blockPos{0, 180, 2}}
	h.world.SetBlock(dst.x, dst.y, dst.z, chestState)
	h.onBlock(players, evBlock{x: dst.x, y: dst.y, z: dst.z, state: chestState, by: pl.p.eid, placed: true})
	got := h.chests[dst]
	if got == nil || got.slots[3].item != int32(itemByName["diamond"]) || got.slots[3].count != 5 ||
		got.slots[10].item != int32(itemByName["iron_sword"]) || got.slots[10].dmg != 7 {
		t.Fatalf("the placed chest should hold the copied items: %+v", got)
	}

	// Survival: includeData needs infinite materials.
	pl.gamemode = gmSurvival
	pl.inv.slots = [invSize]invStack{}
	pl.inv.slots[0] = invStack{item: int32(itemByName["chest"]), count: 1}
	pl.p.setHeldSlot(0)
	pickVia(h, players, pl, attachproto.PickItem{X: 2, Y: 180, Z: 0, IncludeData: true})
	if st := heldStack(pl); st.beData != "" {
		t.Errorf("a survival ctrl-pick carried block entity data: %q", st.beData)
	}
}

// A spawner's data is op-only: a non-op's placement leaves the fresh cage
// alone, an operator's in creative takes the entity.
func TestSpawnerBlockEntityDataIsOpOnly(t *testing.T) {
	h, players, pl := pickDataHub(t)
	src := simPos{dim: 0, blockPos: blockPos{2, 180, 0}}
	h.world.SetBlock(src.x, src.y, src.z, spawnerBlock)
	h.setSpawnerEntity(src, "zombie")
	pickVia(h, players, pl, attachproto.PickItem{X: 2, Y: 180, Z: 0, IncludeData: true})
	st := heldStack(pl)
	if !strings.Contains(st.beData, "zombie") {
		t.Fatalf("the picked spawner should carry its entity: %+v", st)
	}
	dst := simPos{dim: 0, blockPos: blockPos{0, 180, 2}}
	place := func() {
		h.world.SetBlock(dst.x, dst.y, dst.z, spawnerBlock)
		h.onBlock(players, evBlock{x: dst.x, y: dst.y, z: dst.z, state: spawnerBlock, by: pl.p.eid, placed: true})
	}
	h.isOp = func(string) bool { return false }
	place()
	if h.peekBlockEntity(dst, false).spawner != "" {
		t.Fatal("a non-operator set a spawner's data from an item")
	}
	h.isOp = func(string) bool { return true }
	place()
	if got := h.peekBlockEntity(dst, false).spawner; !strings.Contains(got, "zombie") {
		t.Fatalf("an operator's placement should load the spawner's entity, got %q", got)
	}
}

// block_entity_data only loads into a block entity of its own type.
func TestBlockEntityDataNeedsItsType(t *testing.T) {
	h, players, pl := pickDataHub(t)
	barrel := worldgen.BlockBase("barrel")
	st := invStack{item: int32(itemByName["barrel"]), count: 1,
		beData: writeSNBT(map[string]any{"id": int64(-5), "Items": []any{map[string]any{"Slot": int64(0), "id": "minecraft:stone", "count": int64(1)}}})}
	pl.inv.slots[0] = st
	pl.p.setHeldSlot(0)
	dst := simPos{dim: 0, blockPos: blockPos{0, 180, 2}}
	h.world.SetBlock(dst.x, dst.y, dst.z, barrel)
	h.onBlock(players, evBlock{x: dst.x, y: dst.y, z: dst.z, state: barrel, by: pl.p.eid, placed: true})
	if c := h.chests[dst]; c != nil && c.slots[0].item != 0 {
		t.Error("data of another block entity type was loaded")
	}
}

// writeSNBT gives back what parseSNBT reads.
func TestWriteSNBTRoundTrips(t *testing.T) {
	in := map[string]any{"a": int64(3), "b": "x \"q\" \\ y\nz", "c": []any{int64(1), 2.5, true}, "d": map[string]any{"minecraft:k": "v"}}
	v, err := parseSNBT(writeSNBT(in))
	if err != nil {
		t.Fatal(err)
	}
	if !nbtMatches(in, v, false) || !nbtMatches(v, in, false) {
		t.Errorf("round trip changed the value: %s -> %#v", writeSNBT(in), v)
	}
}
