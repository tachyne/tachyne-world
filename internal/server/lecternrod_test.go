package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A lectern's comparator reading follows the open page: 1 on the first
// page, 15 on the last, 0 with no book; a page turn pulses it for 2 ticks.
func TestLecternSignalAndPageTurnPulse(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	lecternDef := worldgen.BlockID("lectern")
	li, _ := worldgen.InfoForState(lecternDef)
	st := worldgen.SetProperty(li, lecternDef, "has_book", "true")
	w.SetBlock(x, y, z, st)
	pos := simPos{dim: 0, blockPos: blockPos{x, y, z}}
	if got := h.lecternSignal(pos); got != 0 {
		t.Fatalf("an empty lectern reads 0, got %d", got)
	}
	id := h.books.create(savedBook{Pages: []string{"one", "two", "three"}})
	lec := &lectern{book: invStack{item: itemByName["written_book"], count: 1, bookID: id}}
	h.lecterns[pos] = lec
	if got := h.lecternSignal(pos); got != 1 {
		t.Fatalf("first page of three reads 1, got %d", got)
	}
	lec.page = 1
	if got := h.lecternSignal(pos); got != 8 {
		t.Fatalf("middle page reads floor(0.5×14)+1 = 8, got %d", got)
	}
	lec.page = 2
	if got := h.lecternSignal(pos); got != 15 {
		t.Fatalf("last page reads 15, got %d", got)
	}
	pl := testTracked()
	pl.winPos, pl.winID = pos, 5
	lec.page = 0
	h.lecternButton(players, pl, lecternButtonNext)
	if lec.page != 1 || !boolProp(w.At(x, y, z), "powered") {
		t.Fatalf("a page turn powers the lectern: page %d powered %v", lec.page, boolProp(w.At(x, y, z), "powered"))
	}
	stepTicks(h, players, 3)
	if boolProp(w.At(x, y, z), "powered") {
		t.Fatal("the page-turn pulse ends after 2 ticks")
	}
}

// A lightning strike on a rod powers it for 8 ticks.
func TestLightningRodPowersEightTicks(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	rod := worldgen.BlockID("lightning_rod")
	w.SetBlock(x, y, z, rod)
	w.SetBlock(x+1, y, z, lampOff)
	h.strikeLightning(players, float64(x)+0.5, float64(y)+1, float64(z)+0.5, false)
	if !boolProp(w.At(x, y, z), "powered") {
		t.Fatal("a struck rod is powered")
	}
	stepTicks(h, players, 2)
	if w.At(x+1, y, z) != lampOn {
		t.Fatal("a powered rod lights the lamp beside it")
	}
	stepTicks(h, players, 8)
	if boolProp(w.At(x, y, z), "powered") {
		t.Fatal("the rod's power ends after 8 ticks")
	}
}
