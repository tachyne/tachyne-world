package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestMapBannerMarkers: a filled map used on a banner pins a marker of its
// colour, again unpins it, the marker goes with the banner, and markers
// survive the store.
func TestMapBannerMarkers(t *testing.T) {
	h := newHub(world.New(1))
	path := filepath.Join(t.TempDir(), "maps.json")
	h.maps = newMapStore(path)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	md := h.maps.create(0, 0, 0, 0)
	pl.p.setHotbarSlot(0, itemFilledMap)
	pl.inv.slots[0] = invStack{item: itemFilledMap, count: 1, mapID: md.ID}
	red := worldgen.BlockBase("red_banner")
	w.SetBlock(5, 180, 7, red)
	h.toggleMapBanner(players, evMapBanner{eid: pl.p.eid, x: 5, y: 180, z: 7})
	decor := h.mapDecorations(md, players)
	found := false
	for _, d := range decor {
		if d.Type == decorBannerWhite+14 { // red is the fifteenth dye
			found = true
			if d.X != int8((5.5-float64(md.CenterX))*2+0.5) || d.Rot != 8 {
				t.Fatalf("marker placement %+v", d)
			}
		}
	}
	if !found {
		t.Fatalf("no red banner marker: %+v", decor)
	}
	// Persisted.
	h.maps.flushIfDirty()
	if back := newMapStore(path).get(md.ID); len(back.Banners) != 1 {
		t.Fatalf("markers should persist: %+v", back.Banners)
	}
	// Toggled off.
	h.toggleMapBanner(players, evMapBanner{eid: pl.p.eid, x: 5, y: 180, z: 7})
	if len(md.Banners) != 0 {
		t.Fatal("a second use unpins the marker")
	}
	// Removed with the banner.
	h.toggleMapBanner(players, evMapBanner{eid: pl.p.eid, x: 5, y: 180, z: 7})
	w.SetBlock(5, 180, 7, worldgen.Air)
	banners := 0
	for _, d := range h.mapDecorations(md, players) {
		if d.Type >= decorBannerWhite && d.Type < decorBannerWhite+16 {
			banners++
		}
	}
	if banners != 0 || len(md.Banners) != 0 {
		t.Fatalf("a broken banner drops its marker: %d markers, %+v", banners, md.Banners)
	}
	// Out of the map's area: nothing.
	w.SetBlock(500, 180, 7, red)
	h.toggleMapBanner(players, evMapBanner{eid: pl.p.eid, x: 500, y: 180, z: 7})
	if len(md.Banners) != 0 {
		t.Fatal("a banner off the map cannot be pinned")
	}
}
