package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestPaintingPlacement: on a big wall the largest placeable variant wins
// (vanilla Painting.create), the spawn + variant metadata reach viewers, and
// breaking a support block pops the painting as an item drop.
func TestPaintingPlacement(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.y, pl.z = 2.5, 201, -2.5
	players[1] = pl

	// a 7×7 stone wall in the z=1 plane; paintings hang at z=0 facing north
	for x := -1; x <= 5; x++ {
		for y := 198; y <= 204; y++ {
			h.world.SetBlock(x, y, 1, stone)
		}
	}
	h.onPlacePainting(players, evPlacePainting{eid: 1, x: 2, y: 201, z: 0, dir: 2})
	if len(h.paintings) != 1 {
		t.Fatalf("paintings placed: %d", len(h.paintings))
	}
	var pt *painting
	for _, p := range h.paintings {
		pt = p
	}
	if pt.w*pt.h != 16 { // the 4×4s are the largest vanilla placeable variants
		t.Fatalf("picked %s (%dx%d), want a 4x4", pt.variant, pt.w, pt.h)
	}

	// the viewer got the spawn and the metadata (serializer 30, holder > 0)
	var adds, metas int
	for {
		select {
		case pkt := <-pl.p.out:
			switch v := pkt.ev.(type) {
			case attachproto.EntityAdd:
				if v.Type == int32(entityPainting) {
					adds++
					if v.Data != 2 {
						t.Fatalf("spawn data %d, want 2 (north)", v.Data)
					}
				}
			case attachproto.EntityMeta:
				metas++
				if v.Meta[0] != 8 || v.Meta[1] != 30 || v.Meta[2] == 0 {
					t.Fatalf("meta bytes %v: want index 8, serializer 30, holder>0", v.Meta[:3])
				}
			}
		default:
			goto drained
		}
	}
drained:
	if adds != 1 || metas != 1 {
		t.Fatalf("viewer frames: %d adds %d metas", adds, metas)
	}

	// no overlap: a second painting on the same spot must not place
	h.onPlacePainting(players, evPlacePainting{eid: 1, x: 2, y: 201, z: 0, dir: 2})
	if len(h.paintings) != 1 {
		t.Fatal("overlapping painting placed")
	}

	// breaking a support block pops it: painting gone, item drop spawned
	items := len(h.items)
	h.world.SetBlock(pt.x, pt.y, 1, 0)
	h.paintingsOnBlockChange(players, 0, pt.x, pt.y, 1)
	if len(h.paintings) != 0 {
		t.Fatal("painting survived losing its wall")
	}
	if len(h.items) != items+1 {
		t.Fatalf("drops: %d, want %d", len(h.items), items+1)
	}
}

// TestPaintingCells pins the even/odd box math (vanilla offsetForPaintingSize).
func TestPaintingCells(t *testing.T) {
	// 1x1 covers only its anchor
	c := paintingCells(10, 60, 0, 1, 1, "north")
	if len(c) != 1 || c[0] != [3]int{10, 60, 0} {
		t.Fatalf("1x1 cells: %v", c)
	}
	// 2x2 facing north extends +left (west for north) and +up
	c = paintingCells(10, 60, 0, 2, 2, "north")
	if len(c) != 4 {
		t.Fatalf("2x2 count: %d", len(c))
	}
	seen := map[[3]int]bool{}
	for _, cc := range c {
		seen[cc] = true
	}
	for _, want := range [][3]int{{10, 60, 0}, {9, 60, 0}, {10, 61, 0}, {9, 61, 0}} {
		if !seen[want] {
			t.Fatalf("2x2 missing %v: %v", want, c)
		}
	}
}
