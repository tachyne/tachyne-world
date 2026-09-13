package server

import (
	"fmt"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Banner markers (MapItem.useOn → MapItemSavedData.toggleBanner): a filled
// map used on a banner inside its area pins a marker of the banner's colour
// there, and again takes it off. The marker goes when the banner does
// (checkBanners), and rides with the map through locking and copies.

// decorBannerWhite is map decoration type banner_white; the sixteen colours
// follow in dye order.
const decorBannerWhite = 10

// mapBanner is one pinned banner.
type mapBanner struct {
	X     int  `json:"x"`
	Y     int  `json:"y"`
	Z     int  `json:"z"`
	Color int8 `json:"color"`
}

type evMapBanner struct {
	eid     int32
	x, y, z int
}

func (evMapBanner) isHubEvent() {}

func bannerKey(x, y, z int) string { return fmt.Sprintf("%d,%d,%d", x, y, z) }

// bannerColorOf is a banner block's base colour in dye order (standing and
// wall banners share a colour per pair of ranges).
func bannerColorOf(s uint32) (int8, bool) {
	for i, r := range bannerRanges {
		if s >= r[0] && s <= r[1] {
			return int8(i / 2), true
		}
	}
	return 0, false
}

// toggleMapBanner adds or removes the marker for the banner the player
// clicked with the map they hold.
func (h *hub) toggleMapBanner(players map[int32]*tracked, e evMapBanner) {
	t := players[e.eid]
	if t == nil || t.inv == nil || h.maps == nil {
		return
	}
	st := heldStack(t)
	if st.item != itemFilledMap || st.mapID == 0 {
		return
	}
	md := h.maps.get(st.mapID)
	if md == nil || md.Dim != t.dim {
		return
	}
	color, ok := bannerColorOf(h.worldFor(t.dim).At(e.x, e.y, e.z))
	if !ok {
		return
	}
	scale := float64(int(1) << md.Scale)
	xd := (float64(e.x) + 0.5 - float64(md.CenterX)) / scale
	zd := (float64(e.z) + 0.5 - float64(md.CenterZ)) / scale
	if xd < -63 || xd > 63 || zd < -63 || zd > 63 {
		return
	}
	key := bannerKey(e.x, e.y, e.z)
	if b, ok := md.Banners[key]; ok && b.Color == color {
		delete(md.Banners, key)
	} else {
		if md.Banners == nil {
			md.Banners = map[string]mapBanner{}
		}
		if len(md.Banners) >= 256 {
			return // isTrackedCountOverLimit
		}
		md.Banners[key] = mapBanner{X: e.x, Y: e.y, Z: e.z, Color: color}
	}
	h.maps.markDirty()
	for _, hd := range md.holders {
		hd.dirtyDecor = true
	}
}

// mapBannerDecorations is the banner half of the decoration set, dropping
// any marker whose banner is gone or recoloured (checkBanners).
func (h *hub) mapBannerDecorations(md *mapData) []attachproto.MapDecoration {
	if len(md.Banners) == 0 {
		return nil
	}
	w := h.worldFor(md.Dim)
	scale := float64(int(1) << md.Scale)
	var out []attachproto.MapDecoration
	for key, b := range md.Banners {
		if c, ok := bannerColorOf(w.At(b.X, b.Y, b.Z)); !ok || c != b.Color {
			delete(md.Banners, key)
			h.maps.markDirty()
			continue
		}
		xd := (float64(b.X) + 0.5 - float64(md.CenterX)) / scale
		zd := (float64(b.Z) + 0.5 - float64(md.CenterZ)) / scale
		if xd < -63 || xd > 63 || zd < -63 || zd > 63 {
			continue
		}
		out = append(out, attachproto.MapDecoration{
			Type: decorBannerWhite + int32(b.Color),
			X:    int8(xd*2 + 0.5), Z: int8(zd*2 + 0.5),
			Rot: 8, // vanilla pins banners at 180°
		})
	}
	return out
}
