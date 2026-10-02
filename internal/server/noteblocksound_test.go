package server

import (
	"bytes"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

const testNoteSound = "minecraft:entity.pig.ambient"

// /give player_head[note_block_sound=…] sets it (namespaced or bare), a
// removal clears it, a malformed identifier is refused; the stack row keeps
// it, the wire carries it as an Identifier (one UTF-8 string, id 62), and a
// creative stack's component reads back onto the stack.
func TestNoteBlockSoundComponent(t *testing.T) {
	st, msg := parseItemArg(`player_head[note_block_sound="entity.pig.ambient"]`)
	if msg != "" || st.noteSound != testNoteSound {
		t.Fatalf("give: %q, noteSound %q", msg, st.noteSound)
	}
	if _, msg := parseItemArg(`player_head[!note_block_sound]`); msg != "" {
		t.Fatalf("a removal should be accepted: %q", msg)
	}
	cleared := st
	if msg := removeItemComponent(&cleared, "minecraft:note_block_sound"); msg != "" || cleared.noteSound != "" {
		t.Fatalf("a removal should clear it: %q %q", msg, cleared.noteSound)
	}
	if _, msg := parseItemArg(`player_head[note_block_sound="Not Valid"]`); msg == "" {
		t.Fatal("a malformed identifier should be refused")
	}

	st.count = 1
	if got := unpackStack(packStack(st)); got != st {
		t.Fatalf("stack row round trip: %+v", got)
	}

	comps := stackComponents(st)
	want := protocol.AppendString(protocol.AppendVarInt(nil, componentNoteBlockSound), testNoteSound)
	if !bytes.Contains(comps, want) {
		t.Fatalf("the stack's components should carry note_block_sound as id 62 + string: % x", comps)
	}
	h := newTestHub(world.New(1))
	if back := h.creativeStack(st.item, 1, comps); back.noteSound != testNoteSound {
		t.Fatalf("a creative stack's note_block_sound read back as %q", back.noteSound)
	}
	d := &stackDecode{h: h, st: invStack{item: itemPlayerHead, count: 1}}
	if r := bytes.NewReader(protocol.AppendString(nil, "Bad Id")); d.component(componentNoteBlockSound, r) {
		t.Fatal("a malformed identifier on the wire should be refused")
	}
}

// Through the entry path: a creative client's set_creative_mode_slot with a
// note_block_sound component puts it on the slot's stack.
func TestCreativeSlotKeepsNoteBlockSound(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	h.rules.DoMobSpawning = false
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative)}
	w.ForceLoad(0, 0, 1)
	startHub(t, h)
	p := newPlayer(h.allocEID(), "maker", [16]byte{9})
	sy := w.SurfaceY(0, 0)
	h.post(evJoin{p: p, x: 0.5, y: sy, z: 0.5, gamemode: gmCreative})
	waitJoined(t, h, "maker")

	head := invStack{item: itemPlayerHead, count: 1, noteSound: testNoteSound}
	s.applyCreativeSlot(p, 36, head.item, head.count, "", stackComponents(head))
	got := make(chan invStack, 1)
	s.onHub(func(players map[int32]*tracked) {
		if tr := players[p.eid]; tr != nil {
			got <- tr.inv.slots[0]
			return
		}
		got <- invStack{}
	})
	select {
	case sl := <-got:
		if sl != head {
			t.Errorf("hotbar 0 = %+v, want %+v", sl, head)
		}
	case <-time.After(hubTestWait):
		t.Fatal("the hub never answered")
	}
}

// A player head placed from a stack with a note_block_sound keeps it
// (SkullBlockEntity.applyImplicitComponents): viewers get it in the head's
// update tag, the chunk's block-entity section carries it, a note block
// under the head plays it (NoteBlock.getCustomSoundId: pitch 1, records,
// volume 3, plus the block event), the store survives a save, and the
// broken head drops with it (copy_components). A head without one plays
// nothing at all.
func TestNoteBlockPlaysHeadNoteSound(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 100, 3.5
	head := worldgen.BlockID("player_head")
	nb := simPos{blockPos: blockPos{0, 99, 0}}
	hp := simPos{blockPos: blockPos{0, 100, 0}}
	h.world.SetBlock(nb.x, nb.y-1, nb.z, worldgen.BlockBase("stone"))
	h.world.SetBlock(nb.x, nb.y, nb.z, noteBlockBase+1)

	// A plain head on top: the note block is silent and sends no event.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemPlayerHead, count: 1}
	h.world.SetBlock(hp.x, hp.y, hp.z, head)
	h.onBlock(players, evBlock{x: hp.x, y: hp.y, z: hp.z, state: head, by: pl.p.eid, placed: true})
	drainEvs(pl.p)
	h.onNoteBlock(players, evNoteBlock{eid: pl.p.eid, x: nb.x, y: nb.y, z: nb.z})
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.Sound:
			t.Fatalf("a head without note_block_sound played %q", e.Name)
		case attachproto.BlockEvent:
			t.Fatal("a head without note_block_sound sent the block event")
		}
	}

	// Replace it with a head carrying the sound.
	h.world.SetBlock(hp.x, hp.y, hp.z, worldgen.Air)
	h.onBlock(players, evBlock{x: hp.x, y: hp.y, z: hp.z, state: worldgen.Air, by: pl.p.eid, broken: head})
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemPlayerHead, count: 1, noteSound: testNoteSound}
	h.world.SetBlock(hp.x, hp.y, hp.z, head)
	h.onBlock(players, evBlock{x: hp.x, y: hp.y, z: hp.z, state: head, by: pl.p.eid, placed: true})
	if got := h.skulls.note(hp); got != testNoteSound {
		t.Fatalf("the placed head should keep its note_block_sound, got %q", got)
	}
	shown := ""
	for _, ev := range drainEvs(pl.p) {
		if d, ok := ev.(attachproto.BlockDisplay); ok && d.Kind == attachproto.DisplaySkull {
			shown = d.Name
		}
	}
	if shown != testNoteSound {
		t.Fatalf("viewers should be sent the head's note_block_sound, got %q", shown)
	}
	be := appendBlockEntities(nil, h.world, 0, 0, 0, nil, nil, nil, nil, nil, h.skulls)
	if !bytes.Contains(be, []byte("note_block_sound")) || !bytes.Contains(be, []byte(testNoteSound)) {
		t.Fatal("the chunk's block entities should carry the head's note_block_sound")
	}

	h.onNoteBlock(players, evNoteBlock{eid: pl.p.eid, x: nb.x, y: nb.y, z: nb.z})
	var snd *attachproto.Sound
	gotEvent := false
	for _, ev := range drainEvs(pl.p) {
		switch e := ev.(type) {
		case attachproto.Sound:
			got := e
			snd = &got
		case attachproto.BlockEvent:
			if e.Y == int32(nb.y) {
				gotEvent = true
			}
		}
	}
	if snd == nil || snd.Name != testNoteSound || snd.Pitch != 1 || snd.Volume != 3 || snd.Category != sndRecord {
		t.Fatalf("the note block should play the head's sound at pitch 1, got %+v", snd)
	}
	if !gotEvent {
		t.Fatal("the note block's block event should go out with the sound")
	}

	// Saved and restored.
	r := newSkullStore()
	r.restoreNotes(h.skulls.snapshotNotes())
	if r.note(hp) != testNoteSound {
		t.Fatal("the head's note sound should survive a save")
	}

	// Broken: the drop carries it.
	h.world.SetBlock(hp.x, hp.y, hp.z, worldgen.Air)
	h.onBlock(players, evBlock{x: hp.x, y: hp.y, z: hp.z, state: worldgen.Air, by: pl.p.eid, broken: head})
	h.dropLoose(players, 0, hp.blockPos, head)
	found := false
	for _, it := range h.items {
		if it.item == itemPlayerHead && it.noteSound == testNoteSound {
			found = true
		}
	}
	if !found {
		t.Fatal("the dropped head should carry its note_block_sound")
	}
	if h.skulls.note(hp) != "" {
		t.Fatal("the note sound must leave with the block")
	}
}

// Through the session's placement path: a head is a standing-or-wall block
// item, and its placement now reaches the hub as a placement, so the head
// takes what the stack carries.
func TestPlacedHeadTakesStackNoteSound(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	w.SetBlock(x, y, z, worldgen.BlockBase("stone"))
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemPlayerHead)
	selectSlot(p, 0)
	s.onHub(func(players map[int32]*tracked) {
		if tr := players[p.eid]; tr != nil {
			tr.inv.slots[0] = invStack{item: itemPlayerHead, count: 1, noteSound: testNoteSound}
		}
	})
	s.handlePlace(p, placeBody(x, y, z, 1))
	pos := simPos{blockPos: blockPos{x, y + 1, z}}
	if !pollUntil(3*time.Second, func() bool { return h.skulls.note(pos) == testNoteSound }) {
		t.Fatalf("the placed head should take the stack's note_block_sound (block %d)", w.At(x, y+1, z))
	}
}
