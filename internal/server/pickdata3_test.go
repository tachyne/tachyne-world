package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Stacks inside a copied container keep their own block entity data (a
// copied sign's text inside a chest) and their own hive record (a carried
// hive's bees and honey), each placed copy with a record of its own.
func TestCtrlPickNestedBlockEntityDataAndHives(t *testing.T) {
	h, players, pl := pickDataHub(t)
	chestState := worldgen.BlockBase("chest")
	src := simPos{blockPos: blockPos{2, 180, 0}}
	dst := simPos{blockPos: blockPos{0, 180, 2}}
	h.world.SetBlock(src.x, src.y, src.z, chestState)
	signID, _ := blockEntityTypeByName("sign")
	signItem := invStack{item: itemByName["oak_sign"], count: 1,
		beData: writeSNBT(map[string]any{"id": int64(signID), "front_text": signSideTag(signSide{Lines: [4]string{"Hi", "", "", ""}})})}
	h.hiveItems = map[int32]hiveStow{7: {Honey: 3, Occ: []hiveOccupant{{SecsLeft: 40, Nectar: true}}}}
	h.nextHiveID = 7
	hive := invStack{item: itemByName["beehive"], count: 1, hiveID: 7}
	c := &chest{}
	c.slots[0], c.slots[1] = signItem, hive
	h.chests[src] = c

	pickAndPlace(t, h, players, pl, src, dst, chestState)
	got := h.chests[dst]
	if got == nil {
		t.Fatal("the placed chest is empty")
	}
	if s := got.slots[0]; s.item != signItem.item || s.beData != signItem.beData {
		t.Errorf("the nested sign lost its block entity data: %+v", s)
	}
	s := got.slots[1]
	if s.item != hive.item || s.hiveID == 0 || s.hiveID == 7 {
		t.Fatalf("the nested hive should carry a record of its own: %+v", s)
	}
	if r := h.hiveItems[s.hiveID]; r.Honey != 3 || len(r.Occ) != 1 || r.Occ[0].SecsLeft != 40 || !r.Occ[0].Nectar {
		t.Errorf("the nested hive's record: %+v", r)
	}
}

// A stack's block entity data goes to the client as item components: a
// sign's sides as sign_text_front / back and its wax as waxed (26.3's
// SignText wire form), the rest as block_entity_data with the type named;
// the creative-slot decoder reads them all back.
func TestBlockEntityDataComponents(t *testing.T) {
	h, _, _ := pickDataHub(t)
	signID, _ := blockEntityTypeByName("sign")
	sign := invStack{item: itemByName["oak_sign"], count: 1, beData: writeSNBT(map[string]any{
		"id":         int64(signID),
		"front_text": signSideTag(signSide{Lines: [4]string{"Hello", "", "world", ""}, Color: "red", Glow: true}),
		"is_waxed":   true,
	})}
	comps := stackComponents(sign)
	seen := map[int32][]byte{}
	protocol.WalkCanonicalComponents(comps, func(id int32, payload []byte) { seen[id] = payload })
	front, ok := seen[componentSignTextFront]
	if !ok {
		t.Fatalf("no sign_text_front in %x", comps)
	}
	var want []byte
	for _, l := range []string{"Hello", "", "world", ""} {
		want = append(want, chatNBT(l)...)
	}
	want = append(want, 0)                                     // no filtered lines
	want = protocol.AppendVarInt(want, int32(dyeIndex("red"))) // DyeColor
	want = append(want, 1)                                     // glowing
	if !bytes.Equal(front, want) {
		t.Errorf("sign_text_front %x, want %x", front, want)
	}
	if _, ok := seen[componentWaxed]; !ok {
		t.Error("no waxed component")
	}
	if _, ok := seen[componentBlockEntityData]; ok {
		t.Error("a sign's text alone is no block_entity_data")
	}
	back := h.creativeStack(sign.item, 1, comps)
	if back.beData != sign.beData {
		t.Errorf("round trip: %q, want %q", back.beData, sign.beData)
	}

	chestID, _ := blockEntityTypeByName("chest")
	ch := invStack{item: itemByName["chest"], count: 1, beData: writeSNBT(map[string]any{
		"id":    int64(chestID),
		"Items": []any{map[string]any{"Slot": int64(0), "id": "minecraft:diamond", "count": int64(2), pickComponentsKey: "AAA="}},
	})}
	seen = map[int32][]byte{}
	protocol.WalkCanonicalComponents(stackComponents(ch), func(id int32, payload []byte) { seen[id] = payload })
	raw, ok := seen[componentBlockEntityData]
	if !ok {
		t.Fatal("no block_entity_data for a chest")
	}
	v, ok := readNetNBT(bytes.NewReader(raw))
	m, isMap := v.(map[string]any)
	if !ok || !isMap || m["id"] != "minecraft:chest" {
		t.Fatalf("block_entity_data %#v", v)
	}
	if strings.Contains(string(raw), pickComponentsKey) {
		t.Error("the engine's own keys reached the client's copy")
	}
	if got := h.creativeStack(ch.item, 1, stackComponents(ch)); !strings.Contains(got.beData, "minecraft:diamond") {
		t.Errorf("block_entity_data did not read back: %q", got.beData)
	}
}

// The local block entity type table is the canonical registry (1.21.11's order) common names.
func TestBlockEntityTypeNamesCanonical(t *testing.T) {
	for i, n := range blockEntityTypeNames {
		if id, ok := protocol.BlockEntityTypeID(774, n); !ok || id != int32(i) {
			t.Errorf("%s: %d %v, want %d", n, id, ok, i)
		}
	}
	if id, ok := protocol.BlockEntityType(worldgen.BlockBase("chest")); !ok || blockEntityTypeNames[id] != "chest" {
		t.Errorf("chest's block entity type %d", id)
	}
}

// A 26.2 creative client keeps a sign's text in block_entity_data (the
// gateway folds 26.3's sign components into it): sent back, the stack
// carries the text, and goes out again as 26.3's sign components.
func TestSignTextFromBlockEntityData(t *testing.T) {
	h, _, _ := pickDataHub(t)
	tag := protocol.NBTString(protocol.NBTRoot(), "id", "minecraft:sign")
	tag = protocol.NBTCompound(tag, "front_text")
	tag = protocol.NBTCompoundList(tag, "messages", 4)
	for _, l := range []string{"Hello", "", "world", ""} {
		tag = protocol.NBTEnd(protocol.NBTString(tag, "text", l))
	}
	tag = protocol.NBTString(tag, "color", "red")
	tag = protocol.NBTEnd(protocol.NBTBool(tag, "has_glowing_text", true))
	tag = protocol.NBTEnd(protocol.NBTBool(tag, "is_waxed", true))
	patch := protocol.AppendVarInt(protocol.AppendVarInt(nil, 1), 0)
	patch = protocol.AppendVarInt(patch, componentBlockEntityData)
	patch = append(patch, tag...)
	st := h.creativeStack(itemByName["oak_sign"], 1, patch)
	seen := map[int32][]byte{}
	protocol.WalkCanonicalComponents(stackComponents(st), func(id int32, payload []byte) { seen[id] = payload })
	var want []byte
	for _, l := range []string{"Hello", "", "world", ""} {
		want = append(want, chatNBT(l)...)
	}
	want = append(want, 0)
	want = protocol.AppendVarInt(want, int32(dyeIndex("red")))
	want = append(want, 1)
	if !bytes.Equal(seen[componentSignTextFront], want) {
		t.Errorf("sign_text_front %x, want %x (beData %q)", seen[componentSignTextFront], want, st.beData)
	}
	if _, ok := seen[componentWaxed]; !ok {
		t.Error("the wax was lost")
	}
	if _, ok := seen[componentBlockEntityData]; ok {
		t.Error("the text stayed in block_entity_data")
	}
}
