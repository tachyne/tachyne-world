package server

import "testing"

func TestNoteBlockRedstone(t *testing.T) {
	// Powered bit round-trips.
	if noteWithPowered(noteBlockBase, false) == noteBlockBase || notePowered(noteWithPowered(noteBlockBase, false)) {
		t.Fatal("noteWithPowered(false) should clear the powered bit")
	}
	if !notePowered(noteWithPowered(noteBlockBase, true)) {
		t.Fatal("noteWithPowered(true) should set the powered bit")
	}

	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		pos := blockPos{5, 70, 5}
		nb := noteWithPowered(noteBlockBase, false) // placed state: unpowered
		w.SetBlock(pos.x, pos.y, pos.z, nb)

		// No power adjacent → stays unpowered.
		h.updateRedstone(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if notePowered(w.At(pos.x, pos.y, pos.z)) {
			t.Fatal("note block powered with no signal")
		}

		// Add a redstone block beside it → rising edge sets powered.
		w.SetBlock(pos.x+1, pos.y, pos.z, redstoneBlock)
		h.updateRedstone(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if !notePowered(w.At(pos.x, pos.y, pos.z)) {
			t.Fatal("note block did not latch powered on a rising edge")
		}

		// Remove the signal → powered clears (falling edge).
		w.SetBlock(pos.x+1, pos.y, pos.z, 0)
		h.updateRedstone(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if notePowered(w.At(pos.x, pos.y, pos.z)) {
			t.Fatal("note block stayed powered after the signal fell")
		}
	})
}
