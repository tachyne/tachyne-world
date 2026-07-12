package server

import (
	"path/filepath"
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

func TestSnapCenter(t *testing.T) {
	// Scale 0 maps tile a 128-block grid with centres on its multiples.
	if c := snapCenter(0, 0); c != 0 {
		t.Fatalf("snap(0,0) = %d", c)
	}
	if c := snapCenter(-65, 0); c != -128 {
		t.Fatalf("snap(-65,0) = %d", c)
	}
	if c := snapCenter(200, 1); c != 320 {
		t.Fatalf("snap(200,1) = %d", c)
	}
}

// drainMapData pulls MapData events for one player until a color patch
// arrives (or fails at the deadline).
func drainMapData(t *testing.T, p *player) attachproto.MapData {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case pkt := <-p.out:
			if ev, ok := pkt.ev.(attachproto.MapData); ok && ev.Width > 0 {
				return ev
			}
		case <-deadline:
			t.Fatal("no MapData color patch arrived")
		}
	}
}

func TestMapCreateScanAndPersist(t *testing.T) {
	s, h, p := breakPlaceServer(t)
	dir := t.TempDir()
	statePath := filepath.Join(dir, "maps.json")
	onHub(t, h, func() {
		h.maps = newMapStore(statePath)
	})
	_ = s

	// Hand the player an empty map and use it.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemEmptyMap, count: 2}
		h.mapCreateFilled(h.playersRef, tr)
		st := heldStack(tr)
		if st.item == itemEmptyMap && st.count != 1 {
			t.Errorf("empty map not consumed: %+v", st)
		}
	})

	// The filled map landed somewhere in the inventory with id 1.
	var mapID int32
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		for _, st := range tr.inv.slots {
			if st.item == itemFilledMap {
				mapID = st.mapID
			}
		}
	})
	if mapID != 1 {
		t.Fatalf("filled map id %d, want 1", mapID)
	}

	// Put it in hand; ticks scan terrain and a color patch flows out.
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: itemFilledMap, count: 1, mapID: mapID}
	})
	ev := drainMapData(t, p)
	if ev.MapID != mapID || ev.Scale != 0 {
		t.Fatalf("patch for map %d scale %d", ev.MapID, ev.Scale)
	}
	nonZero := 0
	for _, c := range ev.Colors {
		if c != 0 {
			nonZero++
		}
	}
	if nonZero == 0 {
		t.Fatal("patch has no colored pixels — the scan found nothing")
	}

	// The map is centred on the vanilla grid around the player (0,0 → 0,0).
	onHub(t, h, func() {
		md := h.maps.get(mapID)
		if md.CenterX != 0 || md.CenterZ != 0 {
			t.Errorf("map centre (%d,%d)", md.CenterX, md.CenterZ)
		}
		md.holders = map[int32]*mapHolder{} // quiesce before flush check
		h.maps.flushIfDirty()
	})

	// Reload from disk: colors and meta survive.
	ms := newMapStore(statePath)
	md := ms.get(mapID)
	if md == nil {
		t.Fatal("map missing after reload")
	}
	colored := 0
	for _, c := range md.Colors {
		if c != 0 {
			colored++
		}
	}
	if colored == 0 {
		t.Fatal("persisted map has no colors")
	}
	if ms.create(0, 0, 0, 0).ID != mapID+1 {
		t.Fatal("last_id did not persist")
	}
}

func TestMapCloneAndZoomRecipes(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	onHub(t, h, func() {
		h.maps = newMapStore("")
		src := h.maps.create(100, 100, 0, 0)

		// Clone: filled map + two empty maps → three copies, same identity.
		grid := make([]invStack, 9)
		grid[0] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
		grid[1] = invStack{item: itemEmptyMap, count: 1}
		grid[2] = invStack{item: itemEmptyMap, count: 1}
		res, kind := h.craftResult(grid, 3)
		if kind != mapCraftClone || res.count != 3 || res.mapID != src.ID {
			t.Errorf("clone = %+v kind %d", res, kind)
			return
		}

		// Zoom: map ringed by 8 paper → a map result (fresh id at take time).
		grid = make([]invStack, 9)
		for i := range grid {
			grid[i] = invStack{item: itemPaper, count: 1}
		}
		grid[4] = invStack{item: itemFilledMap, count: 1, mapID: src.ID}
		res, kind = h.craftResult(grid, 3)
		if kind != mapCraftZoom || res.count != 1 {
			t.Errorf("zoom = %+v kind %d", res, kind)
			return
		}
		zoomed := h.maps.derive(h.maps.get(src.ID), 1, false)
		if zoomed.Scale != 1 || zoomed.ID == src.ID {
			t.Errorf("derived %+v", zoomed)
			return
		}

		// A maxed-out map refuses to zoom.
		h.maps.get(src.ID).Scale = mapMaxScale
		if _, kind := h.craftResult(grid, 3); kind != mapCraftNone {
			t.Error("scale-4 map zoomed")
		}
	})
}
