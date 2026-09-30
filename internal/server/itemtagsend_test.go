package server

import (
	"bytes"
	"testing"
	"time"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Expected payloads here are written from the vanilla stream codecs: Unit
// (nothing), AdventureModePredicate (a list of BlockPredicates: optional
// HolderSet<Block> — count + 1 then registry ids, or 0 then a tag name —
// optional StatePropertiesPredicate, optional NbtPredicate, then the two
// DataComponentMatchers lists), and 1.21.5's ResolvableProfile (optional
// name, optional UUID, properties).

// componentsOf walks a canonical patch into id → payload.
func componentsOf(t *testing.T, patch []byte) map[int32][]byte {
	t.Helper()
	got := map[int32][]byte{}
	if !protocol.WalkCanonicalComponents(patch, func(id int32, payload []byte) {
		got[id] = append([]byte(nil), payload...)
	}) {
		t.Fatalf("the patch %x does not walk", patch)
	}
	return got
}

// A given pickaxe's unbreakable, can_break and can_place_on, and a given
// head's profile, are in the stack the client is sent.
func TestItemTagComponentsReachTheClient(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, `give bob diamond_pickaxe[unbreakable={},can_break={blocks:"dandelion"},can_place_on=[{blocks:["stone","dirt"]},{blocks:"#minecraft:logs",state:{axis:"y"}}]]`)
	s.handleCommand(alice, `give bob player_head[profile={name:"Notch",id:[I;1,2,3,4],properties:[{name:"textures",value:"e30="}]}]`)
	settle(t, h, logs, "C1")

	id := func(name string) int32 {
		v, ok := worldgen.BlockRegistryID(name)
		if !ok {
			t.Fatalf("no block %s", name)
		}
		return int32(v)
	}
	wantBreak := protocol.AppendVarInt(nil, 1)
	wantBreak = append(wantBreak, 1)
	wantBreak = protocol.AppendVarInt(wantBreak, 2) // one block
	wantBreak = protocol.AppendVarInt(wantBreak, id("dandelion"))
	wantBreak = append(wantBreak, 0, 0, 0, 0) // no properties, no nbt, empty matchers

	wantPlace := protocol.AppendVarInt(nil, 2)
	wantPlace = append(wantPlace, 1)
	wantPlace = protocol.AppendVarInt(wantPlace, 3) // two blocks
	wantPlace = protocol.AppendVarInt(wantPlace, id("stone"))
	wantPlace = protocol.AppendVarInt(wantPlace, id("dirt"))
	wantPlace = append(wantPlace, 0, 0, 0, 0)
	wantPlace = append(wantPlace, 1)
	wantPlace = protocol.AppendVarInt(wantPlace, 0) // a tag
	wantPlace = protocol.AppendString(wantPlace, "minecraft:logs")
	wantPlace = append(wantPlace, 1)
	wantPlace = protocol.AppendVarInt(wantPlace, 1)
	wantPlace = protocol.AppendString(wantPlace, "axis")
	wantPlace = append(wantPlace, 1) // exact
	wantPlace = protocol.AppendString(wantPlace, "y")
	wantPlace = append(wantPlace, 0, 0, 0)

	wantProfile := []byte{1}
	wantProfile = protocol.AppendString(wantProfile, "Notch")
	wantProfile = append(wantProfile, 1, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4)
	wantProfile = protocol.AppendVarInt(wantProfile, 1)
	wantProfile = protocol.AppendString(wantProfile, "textures")
	wantProfile = protocol.AppendString(wantProfile, "e30=")
	wantProfile = append(wantProfile, 0) // no signature

	var pick, head invStack
	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			if tr.p.name != "bob" {
				continue
			}
			for _, st := range tr.inv.slots {
				switch st.item {
				case itemByName["diamond_pickaxe"]:
					pick = st
				case itemPlayerHead:
					head = st
				}
			}
		}
	})
	if pick.count == 0 || head.count == 0 {
		t.Fatalf("bob was not given both stacks: %+v %+v", pick, head)
	}
	comps := componentsOf(t, stackEv(pick).Components)
	if p, ok := comps[componentUnbreakable]; !ok || len(p) != 0 {
		t.Errorf("unbreakable: %x, present %v", p, ok)
	}
	if p := comps[componentCanBreak]; !bytes.Equal(p, wantBreak) {
		t.Errorf("can_break:\n got %x\nwant %x", p, wantBreak)
	}
	if p := comps[componentCanPlaceOn]; !bytes.Equal(p, wantPlace) {
		t.Errorf("can_place_on:\n got %x\nwant %x", p, wantPlace)
	}
	comps = componentsOf(t, stackEv(head).Components)
	if p := comps[componentProfile]; !bytes.Equal(p, wantProfile) {
		t.Errorf("profile:\n got %x\nwant %x", p, wantProfile)
	}
}

// A stored predicate list and the bytes it is sent as read back to the same
// list.
func TestAdvPredicateBytesRoundTrip(t *testing.T) {
	js, msg := parseAdvPredicate("minecraft:can_break", []any{
		map[string]any{"blocks": []any{"stone", "minecraft:dirt"}},
		map[string]any{"blocks": "#minecraft:logs", "state": map[string]any{"axis": "y", "age": map[string]any{"min": int64(1)}}},
		map[string]any{"state": map[string]any{"lit": true}},
	})
	if msg != "" {
		t.Fatal(msg)
	}
	b, ok := advPredicateBytes(js)
	if !ok {
		t.Fatal("nothing to send")
	}
	r := bytes.NewReader(b)
	back, ok := readAdvPredicate(r)
	if !ok || r.Len() != 0 || back != js {
		t.Fatalf("read back %q (ok %v, %d left), want %q", back, ok, r.Len(), js)
	}
	// An nbt predicate is refused, as /give refuses it.
	bad := protocol.AppendVarInt(nil, 1)
	bad = append(bad, 0, 0, 1)
	bad = append(bad, protocol.NBTEnd(protocol.NBTRoot())...)
	bad = append(bad, 0, 0)
	if _, ok := readAdvPredicate(bytes.NewReader(bad)); ok {
		t.Error("an nbt predicate was kept")
	}
}

// Through the entry path: a creative client's slot with a profiled head
// and an unbreakable adventure pickaxe keeps every one of those components.
func TestCreativeSlotKeepsProfileAndAdventureTags(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative)}
	w.ForceLoad(0, 0, 1)
	startHub(t, h)
	p := newPlayer(h.allocEID(), "maker", [16]byte{7})
	sy := w.SurfaceY(0, 0)
	h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
	waitJoined(t, h, "maker")

	br, _ := parseAdvPredicate("minecraft:can_break", map[string]any{"blocks": "minecraft:dandelion"})
	pl, _ := parseAdvPredicate("minecraft:can_place_on", []any{map[string]any{"blocks": "#minecraft:logs"}})
	pick := invStack{item: itemByName["diamond_pickaxe"], count: 1,
		tags: itemTags{unbreakable: true, canBreak: br, canPlace: pl}}
	head := invStack{item: itemPlayerHead, count: 1, profile: profileString(testProfile())}
	s.applyCreativeSlot(p, 36, pick.item, pick.count, "", stackComponents(pick))
	s.applyCreativeSlot(p, 37, head.item, head.count, "", stackComponents(head))
	got := make(chan [2]invStack, 1)
	s.onHub(func(players map[int32]*tracked) {
		if tr := players[p.eid]; tr != nil {
			got <- [2]invStack{tr.inv.slots[0], tr.inv.slots[1]}
			return
		}
		got <- [2]invStack{}
	})
	select {
	case sl := <-got:
		if sl[0] != pick {
			t.Errorf("hotbar 0 = %+v, want %+v", sl[0], pick)
		}
		if sl[1] != head {
			t.Errorf("hotbar 1 = %+v, want %+v", sl[1], head)
		}
	case <-time.After(hubTestWait):
		t.Fatal("the hub never answered")
	}
}
