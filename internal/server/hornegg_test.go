package server

import (
	"bytes"
	"testing"
)

// TestGoatTurtleMeta: a goat's horns and scream, a turtle's egg and
// laying pose, as metadata; nothing to re-assert for a plain animal.
func TestGoatTurtleMeta(t *testing.T) {
	g := &mob{eid: 5, etype: entityGoat, hornsGone: 1, screaming: true}
	want := append(append(append([]byte{5}, 17, metaTypeBool, 1), 18, metaTypeBool, 1), 19, metaTypeBool, 0, itemMetaEnd)
	if got := goatMeta(g); !bytes.Equal(got, want) {
		t.Fatalf("goat meta %v want %v", got, want)
	}
	tu := &mob{eid: 6, etype: entityTurtle, hasEgg: true, layCounter: 4}
	want = append(append([]byte{6}, 17, metaTypeBool, 1), 18, metaTypeBool, 1, itemMetaEnd)
	if got := turtleMeta(tu); !bytes.Equal(got, want) {
		t.Fatalf("turtle meta %v want %v", got, want)
	}
	if speciesStateMeta(&mob{etype: entityGoat}) != nil || speciesStateMeta(&mob{etype: entityTurtle}) != nil {
		t.Fatal("a plain goat or turtle has nothing to re-assert")
	}
	if speciesStateMeta(g) == nil || speciesStateMeta(tu) == nil {
		t.Fatal("a hornless goat and an egg-carrying turtle do")
	}
}
