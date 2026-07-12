package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

func TestItemFrameLifecycle(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	w := s.world

	var f *itemFrame
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		// A wall to hang on, and the frame item in hand.
		sy := int(tr.y)
		w.SetBlock(3, sy+1, 0, 1) // stone support
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemItemFrame, count: 2}
		h.onPlaceFrame(h.playersRef, evPlaceFrame{eid: p.eid,
			x: 2, y: sy + 1, z: 0, dir: 4, slot: int32(tr.p.heldSlot())})
		for _, fr := range h.itemFrames {
			f = fr
		}
	})
	if f == nil || f.dir != 4 {
		t.Fatalf("frame not placed: %+v", f)
	}

	// Insert a map, then rotate.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		h.maps = newMapStore("")
		md := h.maps.create(0, 0, 0, 0)
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemFilledMap, count: 1, mapID: md.ID}
		h.interactFrame(h.playersRef, tr, f)
		if f.held.item != itemFilledMap || f.held.count != 1 || f.held.mapID != md.ID {
			t.Errorf("framed item %+v", f.held)
		}
		h.interactFrame(h.playersRef, tr, f)
		if f.rot != 1 {
			t.Errorf("rotation %d", f.rot)
		}

		// The framed map pins a FRAME decoration.
		decor := h.mapDecorations(h.maps.get(md.ID), h.playersRef)
		found := false
		for _, d := range decor {
			if d.Type == decorFrame {
				found = true
			}
		}
		if !found {
			t.Errorf("no frame marker in %+v", decor)
		}

		// First punch pops the item (frame stays), second pops the frame.
		h.hitFrame(h.playersRef, nil, f)
		if f.held.count != 0 || h.itemFrames[f.eid] == nil {
			t.Error("first punch should only pop the item")
		}
		h.hitFrame(h.playersRef, nil, f)
		if h.itemFrames[f.eid] != nil {
			t.Error("second punch should break the frame")
		}
		// The popped map kept its identity on the ground.
		foundMap := false
		for _, it := range h.items {
			if it.item == itemFilledMap && it.mapID == md.ID {
				foundMap = true
			}
		}
		if !foundMap {
			t.Error("popped framed map lost its identity")
		}
	})
}

func TestItemFramePersistence(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	onHub(t, h, func() {
		st := newContainerStore("")
		frames := map[int32]*itemFrame{
			5: {eid: 5, x: 1, y: 70, z: 2, dim: 0, dir: 3, glow: true, rot: 6,
				held: invStack{item: itemFilledMap, count: 1, mapID: 9}},
		}
		st.recordFrames(frames)
		next := int32(100)
		out := st.loadFrames(func() int32 { next++; return next })
		if len(out) != 1 {
			t.Errorf("%d frames after reload", len(out))
			return
		}
		for _, f := range out {
			if f.x != 1 || f.dir != 3 || !f.glow || f.rot != 6 ||
				f.held.item != itemFilledMap || f.held.mapID != 9 {
				t.Errorf("reloaded frame %+v held %+v", f, f.held)
			}
		}
	})
}

func TestFrameMetaBodyShape(t *testing.T) {
	f := &itemFrame{eid: 7, held: invStack{item: itemFilledMap, count: 1, mapID: 3}, rot: 2}
	ev := metaEv(frameMetaBody(f))
	if ev.EID != 7 || len(ev.Meta) == 0 {
		t.Fatalf("meta ev %+v", ev)
	}
	// Empty frame still carries the rotation entry.
	ev = metaEv(frameMetaBody(&itemFrame{eid: 8}))
	if ev.EID != 8 {
		t.Fatalf("meta ev %+v", ev)
	}
	var _ attachproto.EntityMeta = ev
}
