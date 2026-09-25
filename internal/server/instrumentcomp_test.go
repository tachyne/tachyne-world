package server

import (
	"bytes"
	"testing"
)

// A goat horn carries its instrument (EitherHolder: flag, then the holder
// id+1 in the registry we declare), so its tooltip names the call.
func TestGoatHornCarriesItsInstrument(t *testing.T) {
	declared := []string{"admire", "call", "dream", "feel", "ponder", "seek", "sing", "yearn"} // the registry we declare
	calls := []string{"ponder", "sing", "seek", "feel", "admire", "call", "yearn", "dream"}    // instrumentSounds order
	for i, c := range calls {
		if got := declared[instrumentRegistryIndex(int8(i))]; got != c {
			t.Errorf("instrument %d (%s) maps to registry entry %s", i, c, got)
		}
	}
	comps := stackComponents(invStack{item: itemGoatHorn, count: 1, instrument: 2}) // seek
	if !bytes.Equal(comps, []byte{1, 0, componentInstrument, 1, 5 + 1}) {
		t.Fatalf("a seek horn's components %x, want the instrument alone, first", comps)
	}
	if c := stackComponents(invStack{item: int32(itemByName["stick"]), count: 1}); c[0] != 0 {
		t.Fatalf("a stick carries %d components", c[0])
	}
}
