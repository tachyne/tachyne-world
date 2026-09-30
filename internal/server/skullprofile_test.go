package server

import (
	"bytes"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func testProfile() gameProfile {
	return gameProfile{id: [16]byte{0x06, 0x9a, 0x79, 0xf4, 0x44, 0xe9, 0x40, 0x26, 0xbf, 0xcc, 0xf2, 0x5b, 0x3a, 0x1e, 0xc9, 0x0f},
		name: "Notch", props: []skinProperty{{Name: "textures", Value: "e30=", Signature: "c2ln"}}}
}

// A player head placed from a stack with a profile keeps it: the block
// entity holds it, viewers are sent the face, the chunk carries it for later
// viewers, and breaking the head drops it with the profile.
func TestPlayerHeadKeepsProfile(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	head := worldgen.BlockID("player_head")
	pos := blockPos{0, 100, 0}
	pl.x, pl.y, pl.z = 0.5, 100, 2.5
	prof := profileString(testProfile())

	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemPlayerHead, count: 1, profile: prof}
	h.world.SetBlock(pos.x, pos.y, pos.z, head)
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: head, by: pl.p.eid, placed: true})
	if got := h.skulls.get(simPos{blockPos: pos}); got != prof {
		t.Fatal("the placed head should keep the stack's profile")
	}
	var shown *attachproto.GameProfile
	for _, ev := range drainEvs(pl.p) {
		if d, ok := ev.(attachproto.BlockDisplay); ok && d.Kind == attachproto.DisplaySkull {
			shown = d.Profile
		}
	}
	if shown == nil || shown.Name != "Notch" || shown.UUID != testProfile().id ||
		len(shown.Properties) != 1 || shown.Properties[0].Value != "e30=" {
		t.Fatalf("viewers should be sent the head's owner, got %+v", shown)
	}

	// A viewer loading the chunk later gets it in the block-entity section.
	be := appendBlockEntities(nil, h.world, 0, 0, 0, nil, nil, nil, nil, nil, h.skulls)
	if !bytes.Contains(be, []byte("profile")) || !bytes.Contains(be, []byte("Notch")) || !bytes.Contains(be, []byte("textures")) {
		t.Fatal("the chunk's block entities should carry the head's profile")
	}

	// Broken: the removal, then the drop, which carries the profile.
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.Air)
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: worldgen.Air, by: pl.p.eid, broken: head})
	h.dropLoose(players, 0, pos, head)
	found := 0
	for _, it := range h.items {
		if it.item == itemPlayerHead {
			if it.profile != prof {
				t.Fatal("the dropped head should carry its profile")
			}
			found++
		}
	}
	if found != 1 {
		t.Fatalf("want one head dropped, got %d", found)
	}
	if h.skulls.get(simPos{blockPos: pos}) != "" {
		t.Fatal("the owner must leave with the block")
	}

	// A plain head in the same place is plain again.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemPlayerHead, count: 1}
	h.world.SetBlock(pos.x, pos.y, pos.z, head)
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: head, by: pl.p.eid, placed: true})
	if h.skulls.get(simPos{blockPos: pos}) != "" {
		t.Fatal("a plain head has no owner")
	}
}

// /give player_head[profile=…] takes a name or a full profile; other items
// refuse it.
func TestGivePlayerHeadProfile(t *testing.T) {
	st, msg := parseItemArg(`player_head[profile={name:"Notch",id:[I;1,2,3,4],properties:[{name:"textures",value:"e30="}]}]`)
	if msg != "" {
		t.Fatalf("parse: %s", msg)
	}
	p, ok := profileOf(st.profile)
	want := [16]byte{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4}
	if !ok || p.name != "Notch" || p.id != want || len(p.props) != 1 || p.props[0].Name != "textures" {
		t.Fatalf("profile = %+v, ok %v", p, ok)
	}
	st, msg = parseItemArg(`player_head[profile=jeb_]`)
	if p, ok := profileOf(st.profile); msg != "" || !ok || p.name != "jeb_" || p.id != ([16]byte{}) {
		t.Fatalf("a bare name should be a name-only profile: %q %+v", msg, p)
	}
	if _, msg := parseItemArg(`stone[profile=Notch]`); msg == "" {
		t.Fatal("a profile on stone should be refused")
	}
	if _, msg := parseItemArg(`player_head[profile="has space"]`); msg == "" {
		t.Fatal("an invalid player name should be refused")
	}
}

// The profile survives a save (the stack row and the skull store) and reads
// back from a creative stack's component.
func TestSkullProfilePersistsAndDecodes(t *testing.T) {
	h := newTestHub(world.New(1))
	prof := profileString(testProfile())
	st := invStack{item: itemPlayerHead, count: 1, profile: prof}
	if got := unpackStack(packStack(st)); got != st {
		t.Fatalf("stack row round trip: %+v", got)
	}
	s := newSkullStore()
	pos := simPos{dim: dimNether, blockPos: blockPos{5, 70, -3}}
	s.set(pos, prof)
	r := newSkullStore()
	r.restore(s.snapshot())
	if r.get(pos) != prof {
		t.Fatal("the skull store should survive a save")
	}

	d := &stackDecode{h: h, st: invStack{item: itemPlayerHead, count: 1}}
	payload := appendProfile(nil, testProfile())
	rd := bytes.NewReader(payload)
	if !d.component(componentProfile, rd) || rd.Len() != 0 || d.finish().profile != prof {
		t.Fatal("a creative head's profile component should read onto the stack")
	}
}
