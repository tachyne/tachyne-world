package server

import (
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// State math pinned against the vanilla report: the note block's first
// state is harp/note0/powered-true; radices instrument × note(25) × powered(2).
func TestNoteStateMath(t *testing.T) {
	if want := worldgen.StateWith("note_block", map[string]string{"instrument": "harp", "note": "0", "powered": "true"}); noteBlockBase != want {
		t.Fatalf("note_block base %d, want %d", noteBlockBase, want)
	}
	def := noteBlockBase + 1 // harp, note 0, powered false (the default state)
	if noteOf(def) != 0 {
		t.Fatalf("default note %d", noteOf(def))
	}
	s1 := withNote(def, 1)
	if s1 != noteBlockBase+3 || noteOf(s1) != 1 {
		t.Fatalf("note 1 state %d (note %d)", s1, noteOf(s1))
	}
	if s24 := withNote(def, 24); noteOf(s24) != 24 {
		t.Fatalf("note 24 round trip %d", noteOf(s24))
	}
	// Wrapping keeps instrument+powered bits.
	if w := withNote(withNote(def, 24), (24+1)%25); w != def {
		t.Fatalf("wrap: %d want %d", w, def)
	}
}

func TestJukeboxSongLookup(t *testing.T) {
	id, length, ok := jukeboxSongFor(int32(itemByName["music_disc_cat"]))
	if !ok || id != 4 || length != uint64(185*20+20) {
		t.Fatalf("cat: id %d length %d ok %v", id, length, ok)
	}
	if _, _, ok := jukeboxSongFor(int32(itemByName["stone"])); ok {
		t.Fatal("stone is not a disc")
	}
	// The appended 26.x songs sit after the base set.
	id, _, ok = jukeboxSongFor(int32(itemByName["music_disc_tears"]))
	if !ok || id != 21 {
		t.Fatalf("tears id %d ok %v", id, ok)
	}
}

func TestNoteBlockAndJukeboxFlow(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world

	// A note block on stone: tune twice, the note advances, the event flows.
	noteY := 0
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		sy := int(tr.y)
		noteY = sy + 1
		w.SetBlock(4, sy, 0, 1)                 // stone below → basedrum
		w.SetBlock(4, sy+1, 0, noteBlockBase+1) // note block, default state
		h.onNoteBlock(h.playersRef, evNoteBlock{eid: p.eid, x: 4, y: sy + 1, z: 0, tune: true})
		h.onNoteBlock(h.playersRef, evNoteBlock{eid: p.eid, x: 4, y: sy + 1, z: 0, tune: true})
		if got := noteOf(w.At(4, sy+1, 0)); got != 2 {
			t.Errorf("note after two tunes = %d", got)
		}
		if instr := h.noteInstrument(tr.dim, 4, sy+1, 0); instr != "basedrum" {
			t.Errorf("instrument over stone = %q", instr)
		}
	})

	// The block event reached the player's queue. The note and the particle
	// are the CLIENT's to make from it, which is why no sound is sent: vanilla
	// only calls Level.blockEvent here, and sending our own would double it.
	deadline := time.After(hubTestWait)
	gotEvent := false
	for !gotEvent {
		select {
		case pkt := <-p.out:
			switch ev := pkt.ev.(type) {
			case attachproto.BlockEvent:
				if ev.Y == int32(noteY) && ev.Action == 0 {
					gotEvent = true
				}
			case attachproto.Sound:
				if ev.Name == "minecraft:block.note_block.basedrum" {
					t.Fatal("the server must not play the note itself: the client does, from the event")
				}
			}
		case <-deadline:
			t.Fatal("the note block's event never arrived")
		}
	}

	// Jukebox: insert a disc → has_record + play event; click again ejects.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		sy := int(tr.y)
		w.SetBlock(6, sy, 0, jukeboxState(false))
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: int32(itemByName["music_disc_cat"]), count: 1}
		h.onUseJukebox(h.playersRef, evUseJukebox{eid: p.eid, x: 6, y: sy, z: 0, slot: int32(tr.p.heldSlot())})
		jb := h.jukeboxes[simPos{blockPos: blockPos{6, sy, 0}}]
		if jb == nil || jb.disc.item != int32(itemByName["music_disc_cat"]) || jb.started == 0 {
			t.Errorf("jukebox after insert: %+v", jb)
			return
		}
		if w.At(6, sy, 0) != jukeboxState(true) {
			t.Error("has_record not set")
		}
		h.onUseJukebox(h.playersRef, evUseJukebox{eid: p.eid, x: 6, y: sy, z: 0, slot: int32(tr.p.heldSlot())})
		if h.jukeboxes[simPos{blockPos: blockPos{6, sy, 0}}] != nil {
			t.Error("disc not ejected")
		}
		if w.At(6, sy, 0) != jukeboxState(false) {
			t.Error("has_record not cleared")
		}
		found := false
		for _, it := range h.items {
			if it.item == int32(itemByName["music_disc_cat"]) {
				found = true
			}
		}
		if !found {
			t.Error("ejected disc not on the ground")
		}
	})

	// The play + stop world events reached the player.
	deadline = time.After(hubTestWait)
	gotPlay, gotStop := false, false
	for !(gotPlay && gotStop) {
		select {
		case pkt := <-p.out:
			if ev, ok := pkt.ev.(attachproto.WorldFX); ok {
				if ev.Event == worldEventJukeboxPlay && ev.Data == 4 {
					gotPlay = true
				}
				if ev.Event == worldEventJukeboxStop {
					gotStop = true
				}
			}
		case <-deadline:
			t.Fatalf("play=%v stop=%v never arrived", gotPlay, gotStop)
		}
	}
}

// The client reads the instrument out of the block state, so the state has to
// be right when the event goes out. The engine works the instrument out on
// demand, so it corrects the state at the moment the note plays.
func TestPlayingCorrectsTheStoredInstrument(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 20, 70, 20
	h.world.SetBlock(x, y-1, z, worldgen.BlockBase("gold_block")) // bell
	// A note block whose stored instrument still says harp.
	st := withInstrument(noteBlockBase+1, "harp")
	h.world.SetBlock(x, y, z, st)
	h.world.SetBlock(x, y+1, z, worldgen.Air)

	h.playNoteBlock(players, 0, x, y, z, st, 0)

	info, _ := worldgen.InfoForState(h.world.At(x, y, z))
	if got := worldgen.GetProperty(info, h.world.At(x, y, z), "instrument"); got != "bell" {
		t.Fatalf("the stored instrument should have been corrected to bell, got %q", got)
	}
}
