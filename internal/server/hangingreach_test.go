package server

import (
	"math"
	"testing"
)

// The boxes the reach is measured to: ItemFrame.createBoundingBox (a 0.75
// plate, a full block with a map in it), Painting.calculateBoundingBox (a
// 2 × 2 canvas shifted counterclockwise and up) and the knot's 0.375 box.
func TestFixtureBoxes(t *testing.T) {
	near := func(a, b aabb) bool {
		return math.Abs(a.x0-b.x0)+math.Abs(a.y0-b.y0)+math.Abs(a.z0-b.z0)+
			math.Abs(a.x1-b.x1)+math.Abs(a.y1-b.y1)+math.Abs(a.z1-b.z1) < 1e-9
	}
	f := &itemFrame{x: 0, y: 200, z: 0, dir: 2} // facing north, wall to the south
	if got := f.frameBox(); !near(got, aabb{0.125, 200.125, 0.9375, 0.875, 200.875, 1}) {
		t.Fatalf("frame box %+v", got)
	}
	up := &itemFrame{x: 0, y: 200, z: 0, dir: 1} // on a floor
	if got := up.frameBox(); !near(got, aabb{0.125, 200, 0.125, 0.875, 200.0625, 0.875}) {
		t.Fatalf("floor frame box %+v", got)
	}
	pt := &painting{x: 3, y: 200, z: 0, dir: 2, w: 2, h: 2} // left of north is west
	if got := pt.paintingBox(); !near(got, aabb{2, 200, 0.9375, 4, 202, 1}) {
		t.Fatalf("painting box %+v", got)
	}
	east := &painting{x: 0, y: 200, z: 0, dir: 5, w: 1, h: 1}
	if got := east.paintingBox(); !near(got, aabb{0, 200, 0, 0.0625, 201, 1}) {
		t.Fatalf("east painting box %+v", got)
	}
	k := &leashKnot{pos: blockPos{2, 70, 2}}
	if got := k.knotBox(); !near(got, aabb{2.3125, 70.375, 2.3125, 2.6875, 70.875, 2.6875}) {
		t.Fatalf("knot box %+v", got)
	}
}

// handleAttack and handleInteract drop a click on a frame, painting, knot
// or crystal beyond entity_interaction_range + 3, through the hub events.
func TestFixturesNeedEntityReach(t *testing.T) {
	h, players, f, pt := hangingWall(t)
	pl := players[1]
	pl.x, pl.y, pl.z = 2.5, 200, -6.5 // 7.4 blocks from the wall: past 3 + 3
	k := &leashKnot{eid: h.allocEID(), pos: blockPos{1, 200, 0}}
	h.knots[k.eid] = k
	c := &crystal{eid: h.allocEID(), x: -2.5, y: 200, z: 0.5}
	h.crystals[c.eid] = c

	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["stone"], count: 4}
	h.onInteractMob(players, pl, evInteractMob{eid: 1, target: f.eid})
	for _, id := range []int32{pt.eid, k.eid, c.eid, f.eid} {
		h.onAttack(players, evAttack{attacker: 1, target: id})
	}
	if f.held.item != 0 || h.itemFrames[f.eid] == nil || h.paintings[pt.eid] == nil ||
		h.knots[k.eid] == nil || h.crystals[c.eid] == nil {
		t.Fatal("a click from beyond reach touched a fixture")
	}

	pl.z = -3.5 // within reach of all four
	h.onInteractMob(players, pl, evInteractMob{eid: 1, target: f.eid})
	if f.held.item != itemByName["stone"] {
		t.Fatal("a frame within reach takes the item")
	}
	h.onAttack(players, evAttack{attacker: 1, target: pt.eid})
	h.onAttack(players, evAttack{attacker: 1, target: k.eid})
	if h.paintings[pt.eid] != nil || h.knots[k.eid] != nil {
		t.Fatal("a painting and a knot within reach break")
	}
}
