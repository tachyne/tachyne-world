package server

import (
	"testing"

	"tachyne/internal/world"
)

func TestBedDropsItem(t *testing.T) {
	h := newHub(world.New(1))
	drops := h.rollDrops(1955) // red_bed
	if len(drops) != 1 || drops[0].item != itemByName["red_bed"] {
		t.Fatalf("breaking a bed should drop the bed item (1038), got %+v", drops)
	}
}

func TestChunkListsBlockEntities(t *testing.T) {
	w := world.New(1)
	w.SetBlock(0, 70, 0, 1955) // place a red_bed edit in chunk (0,0)
	b := appendBlockEntities(nil, w, 0, 0, 0, nil)
	if len(b) == 0 || b[0] == 0 {
		t.Fatalf("chunk with a bed should list >=1 block entity, count byte=%v", b)
	}
}
