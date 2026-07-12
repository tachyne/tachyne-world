package server

import (
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// State math pinned against the vanilla report: note_block base 581 is
// harp/note0/powered-true; radices instrument(23) × note(25) × powered(2).
func TestNoteStateMath(t *testing.T) {
	if noteBlockBase != 581 {
		t.Fatalf("note_block base %d", noteBlockBase)
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

	// A note block on stone: tune twice, note advances, sound+particle flow.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		sy := int(tr.y)
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

	// Sound + note particle reached the player's queue.
	deadline := time.After(10 * time.Second)
	gotSound, gotParticle := false, false
	for !(gotSound && gotParticle) {
		select {
		case pkt := <-p.out:
			switch ev := pkt.ev.(type) {
			case attachproto.Sound:
				if ev.Name == "minecraft:block.note_block.basedrum" && ev.Category == sndRecord {
					gotSound = true
				}
			case attachproto.Particles:
				if ev.PID == particleNote {
					gotParticle = true
				}
			}
		case <-deadline:
			t.Fatalf("sound=%v particle=%v never arrived", gotSound, gotParticle)
		}
	}

	// Jukebox: insert a disc → has_record + play event; click again ejects.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		sy := int(tr.y)
		w.SetBlock(6, sy, 0, jukeboxState(false))
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: int32(itemByName["music_disc_cat"]), count: 1}
		h.onUseJukebox(h.playersRef, evUseJukebox{eid: p.eid, x: 6, y: sy, z: 0, slot: int32(tr.p.heldSlot())})
		jb := h.jukeboxes[blockPos{6, sy, 0}]
		if jb == nil || jb.disc.item != int32(itemByName["music_disc_cat"]) || jb.started == 0 {
			t.Errorf("jukebox after insert: %+v", jb)
			return
		}
		if w.At(6, sy, 0) != jukeboxState(true) {
			t.Error("has_record not set")
		}
		h.onUseJukebox(h.playersRef, evUseJukebox{eid: p.eid, x: 6, y: sy, z: 0, slot: int32(tr.p.heldSlot())})
		if h.jukeboxes[blockPos{6, sy, 0}] != nil {
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
	deadline = time.After(10 * time.Second)
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
