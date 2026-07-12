package server

// Filled maps, mirroring the vanilla server-side model (MapItemSavedData +
// MapItem.update): the engine owns per-map color buffers and per-holder
// dirty tracking; holders' updates flow to gateways as MapData frames and
// render as map_item_data. The color scan is the vanilla algorithm — 1/16
// column striping per tick with cascade-on-change, top-down first-colored-
// block sampling, water-depth and height-slope shading with the (x+z)&1
// checkerboard dither.

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

const (
	mapSize     = 128
	mapMaxScale = 4
)

var (
	itemEmptyMap  = int32(itemByName["map"])
	itemFilledMap = int32(itemByName["filled_map"])
	itemPaper     = int32(itemByName["paper"])
)

// Base map color ids the scan refers to directly (full table in
// mapcolors_gen.go).
const (
	mapColorNone  = 0
	mapColorDirt  = 10
	mapColorStone = 11
	mapColorWater = 12
)

// Map decoration type registry ids (identical 1.21.5 through 26.2 —
// verified against the per-version registry reports).
const (
	decorPlayer       = 0
	decorPlayerOffMap = 6
)

// Brightness steps for packed colors (id<<2 | brightness).
const (
	brightLow    = 0
	brightNormal = 1
	brightHigh   = 2
)

// mapData is one map's authoritative state (vanilla MapItemSavedData).
type mapData struct {
	ID      int32
	CenterX int32
	CenterZ int32
	Scale   int8
	Dim     int
	Locked  bool
	Colors  [mapSize * mapSize]byte
	holders map[int32]*mapHolder // eid → per-viewer dirty tracking
}

// mapHolder tracks what one viewer still needs (vanilla HoldingPlayer).
type mapHolder struct {
	dirty                  bool
	minX, minY, maxX, maxY int
	dirtyDecor             bool
	lastDecor              []attachproto.MapDecoration
	tick                   int
	step                   int
}

func (md *mapData) holder(eid int32) *mapHolder {
	hp, ok := md.holders[eid]
	if !ok {
		// A new viewer needs everything: full-surface patch + decorations.
		hp = &mapHolder{dirty: true, maxX: mapSize - 1, maxY: mapSize - 1, dirtyDecor: true}
		md.holders[eid] = hp
	}
	return hp
}

// setColor writes one pixel and marks every holder's dirty rectangle.
func (md *mapData) setColor(x, y int, packed byte) bool {
	i := x + y*mapSize
	if md.Colors[i] == packed {
		return false
	}
	md.Colors[i] = packed
	for _, hp := range md.holders {
		if hp.dirty {
			hp.minX, hp.minY = min(hp.minX, x), min(hp.minY, y)
			hp.maxX, hp.maxY = max(hp.maxX, x), max(hp.maxY, y)
		} else {
			hp.dirty = true
			hp.minX, hp.minY, hp.maxX, hp.maxY = x, y, x, y
		}
	}
	return true
}

// snapCenter is vanilla's map-center alignment: maps tile a fixed world
// grid per scale so adjacent maps join seamlessly.
func snapCenter(origin int, scale int8) int32 {
	size := mapSize * (1 << scale)
	area := int(math.Floor(float64(origin+64) / float64(size)))
	return int32(area*size + size/2 - 64)
}

// --- the vanilla update algorithm ---

// mapSurfaceY finds the edit-aware surface for a column: the worldgen
// surface, pushed up past any player-built blocks above it (mined-out
// columns resolve on the way down in the sampler).
func mapSurfaceY(w *world.World, x, z int) int {
	y := w.GroundY(x, z)
	top := w.Ceiling() - 1
	for y < top && w.At(x, y+1, z) != 0 {
		y++
	}
	return y
}

// mapSample is one pixel's sampling result.
type mapSample struct {
	color  uint8
	height float64 // average color-block Y over the sampled area
	depth  int     // averaged fluid depth when color is water
}

// mapSamplePixel picks the pixel's dominant color, average surface height,
// and water depth over the scale×scale block area (vanilla per-pixel
// sampling; the nether uses the hash checkerboard instead of a scan).
func mapSamplePixel(w *world.World, md *mapData, imgX, imgZ int) mapSample {
	scale := 1 << md.Scale
	baseX := (int(md.CenterX)/scale + imgX - 64) * scale
	baseZ := (int(md.CenterZ)/scale + imgZ - 64) * scale

	if md.Dim == 1 {
		n := baseX + baseZ*231871
		n = n*n*31287121 + n*11
		if n>>20&1 == 0 {
			return mapSample{color: mapColorDirt, height: 100}
		}
		return mapSample{color: mapColorStone, height: 100}
	}

	counts := map[uint8]int{}
	waterDepth := 0
	avgHeight := 0.0
	for sx := 0; sx < scale; sx++ {
		for sz := 0; sz < scale; sz++ {
			x, z := baseX+sx, baseZ+sz
			colY := mapSurfaceY(w, x, z) + 1
			var state uint32
			for colY > worldgen.MinY {
				colY--
				state = w.At(x, colY, z)
				if mapColorFor(state) != mapColorNone {
					break
				}
			}
			if worldgen.IsFluid(state) {
				for fy := colY - 1; fy > worldgen.MinY && worldgen.IsFluid(w.At(x, fy, z)); fy-- {
					waterDepth++
				}
			}
			avgHeight += float64(colY) / float64(scale*scale)
			counts[mapColorFor(state)]++
		}
	}

	best, bestN := uint8(mapColorNone), -1
	for c, n := range counts {
		if c != mapColorNone && n > bestN {
			best, bestN = c, n
		}
	}
	return mapSample{color: best, height: avgHeight, depth: waterDepth / (scale * scale)}
}

// mapBrightness picks the 0-3 shade: water shades by depth, land by the
// north-south slope, both dithered by the (x+z)&1 checkerboard.
func mapBrightness(md *mapData, imgX, imgZ int, s mapSample, prevHeight float64) byte {
	if s.color == mapColorWater {
		d := float64(s.depth)*0.1 + float64((imgX+imgZ)&1)*0.2
		switch {
		case d < 0.5:
			return brightHigh
		case d > 0.9:
			return brightLow
		}
		return brightNormal
	}
	d := (s.height-prevHeight)*4.0/float64(int(1)<<md.Scale+4) + (float64((imgX+imgZ)&1)-0.5)*0.4
	switch {
	case d > 0.6:
		return brightHigh
	case d < -0.6:
		return brightLow
	}
	return brightNormal
}

// mapUpdateHeld runs one tick of the color scan for a player holding the
// map (vanilla MapItem.update): every 16th column per step, cascading into
// the next column whenever one changed.
func (h *hub) mapUpdateHeld(md *mapData, t *tracked) {
	if md.Locked || t.dim != md.Dim {
		return
	}
	hp := md.holder(t.p.eid)
	hp.step++
	scale := 1 << md.Scale
	pImgX := (int(math.Floor(t.x))-int(md.CenterX))/scale + 64
	pImgZ := (int(math.Floor(t.z))-int(md.CenterZ))/scale + 64
	radius := mapSize / scale
	if md.Dim == 1 { // the nether's ceiling halves the scan radius
		radius /= 2
	}
	w := h.worldFor(md.Dim)

	cascade, anyChanged := false, false
	for imgX := pImgX - radius + 1; imgX < pImgX+radius; imgX++ {
		if imgX&15 != hp.step&15 && !cascade {
			continue
		}
		cascade = false
		if imgX < 0 || imgX >= mapSize {
			continue
		}
		prevHeight := math.NaN()
		for imgZ := pImgZ - radius - 1; imgZ < pImgZ+radius; imgZ++ {
			if imgZ < -1 || imgZ >= mapSize {
				continue
			}
			dx, dz := imgX-pImgX, imgZ-pImgZ
			distSqr := dx*dx + dz*dz
			s := mapSamplePixel(w, md, imgX, imgZ)
			if imgZ < 0 { // priming row: sets the slope baseline only
				prevHeight = s.height
				continue
			}
			if math.IsNaN(prevHeight) {
				prevHeight = s.height
			}
			bright := mapBrightness(md, imgX, imgZ, s, prevHeight)
			prevHeight = s.height
			if s.color == mapColorNone || distSqr >= radius*radius {
				continue
			}
			if distSqr > (radius-2)*(radius-2) && (imgX+imgZ)&1 == 0 {
				continue // dithered circular edge
			}
			if md.setColor(imgX, imgZ, s.color<<2|bright) {
				cascade = true
				anyChanged = true
			}
		}
	}
	if anyChanged {
		h.maps.markDirty()
	}
}

// --- decorations + update delivery ---

// mapDecorations builds the live marker set: every online player currently
// holding this map, in its dimension (vanilla tickCarriedBy's player pass).
func (h *hub) mapDecorations(md *mapData, players map[int32]*tracked) []attachproto.MapDecoration {
	var out []attachproto.MapDecoration
	scale := 1 << md.Scale
	for _, t := range players {
		if t.inv == nil || heldStack(t).mapID != md.ID || t.dim != md.Dim {
			continue
		}
		xd := (t.x - float64(md.CenterX)) / float64(scale)
		zd := (t.z - float64(md.CenterZ)) / float64(scale)
		if xd >= -63 && xd <= 63 && zd >= -63 && zd <= 63 {
			yaw := t.yaw
			if yaw < 0 {
				yaw -= 8
			} else {
				yaw += 8
			}
			out = append(out, attachproto.MapDecoration{
				Type: decorPlayer,
				X:    int8(xd*2 + 0.5), Z: int8(zd*2 + 0.5),
				Rot: uint8(int(yaw*16/360)) & 15,
			})
		} else if math.Abs(xd) < 320 && math.Abs(zd) < 320 {
			out = append(out, attachproto.MapDecoration{
				Type: decorPlayerOffMap,
				X:    clampMapByte(xd), Z: clampMapByte(zd),
			})
		}
	}
	return out
}

func clampMapByte(d float64) int8 {
	switch {
	case d <= -63:
		return -128
	case d >= 63:
		return 127
	}
	return int8(d*2 + 0.5)
}

func decorEqual(a, b []attachproto.MapDecoration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mapSendUpdate flushes one holder's pending state (vanilla
// nextUpdatePacket): the dirty color rectangle, plus decorations on the
// 5-tick throttle when they changed.
func (h *hub) mapSendUpdate(md *mapData, t *tracked, decor []attachproto.MapDecoration) {
	hp := md.holder(t.p.eid)
	ev := attachproto.MapData{EID: t.p.eid, MapID: md.ID, Scale: md.Scale, Locked: md.Locked}
	send := false
	if !decorEqual(decor, hp.lastDecor) {
		hp.dirtyDecor = true
	}
	if hp.dirtyDecor {
		if hp.tick%5 == 0 {
			hp.dirtyDecor = false
			hp.lastDecor = decor
			ev.HasDecor = true
			ev.Decor = decor
			send = true
		}
		hp.tick++
	}
	if hp.dirty {
		hp.dirty = false
		w := hp.maxX + 1 - hp.minX
		ht := hp.maxY + 1 - hp.minY
		colors := make([]byte, w*ht)
		for y := 0; y < ht; y++ {
			row := hp.minX + (hp.minY+y)*mapSize
			copy(colors[y*w:(y+1)*w], md.Colors[row:row+w])
		}
		ev.X, ev.Y = uint8(hp.minX), uint8(hp.minY)
		ev.Width, ev.Height = uint8(w), uint8(ht)
		ev.Colors = colors
		send = true
	}
	if send {
		t.p.trySendEv(ev)
	}
}

// mapsTick is the per-tick driver: scan + deliver for every player holding
// a filled map (vanilla inventoryTick on hand slots).
func (h *hub) mapsTick(players map[int32]*tracked) {
	if h.maps == nil {
		return
	}
	decorFor := map[int32][]attachproto.MapDecoration{}
	for _, t := range players {
		if t.inv == nil {
			continue
		}
		st := heldStack(t)
		if st.item != itemFilledMap || st.mapID == 0 {
			continue
		}
		md := h.maps.get(st.mapID)
		if md == nil {
			continue
		}
		h.mapUpdateHeld(md, t)
		if _, ok := decorFor[md.ID]; !ok {
			decorFor[md.ID] = h.mapDecorations(md, players)
		}
		h.mapSendUpdate(md, t, decorFor[md.ID])
	}
}

// mapCreateFilled handles using an empty map: consume it, allocate a fresh
// map centred on the player (vanilla EmptyMapItem.use → MapItem.create).
func (h *hub) mapCreateFilled(players map[int32]*tracked, t *tracked) {
	if h.maps == nil || t.inv == nil {
		return
	}
	slot := t.p.heldSlot()
	st := t.inv.slots[slot]
	if st.item != itemEmptyMap || st.count <= 0 {
		return
	}
	st.count--
	if st.count == 0 {
		st = invStack{}
	}
	t.inv.slots[slot] = st
	h.sendSlot(t, slot)

	md := h.maps.create(int(math.Floor(t.x)), int(math.Floor(t.z)), 0, t.dim)
	give := invStack{item: itemFilledMap, count: 1, mapID: md.ID}
	changed, left := t.inv.addStack(give)
	for _, sl := range changed {
		h.sendSlot(t, sl)
	}
	_ = left // a full inventory drops nothing in v1; the empty map refunds
}

// --- map crafting (vanilla's special recipes) ---

// Map-recipe kinds craftResult can classify.
const (
	mapCraftNone = iota
	mapCraftClone
	mapCraftZoom
)

// mapCraftMatch recognises the two dynamic map recipes the data-driven
// tables can't express (the result depends on the input map's identity):
// cloning (one filled map + empty maps → copies sharing the map id) and
// zoom-out (a filled map ringed by 8 paper → a fresh map one scale up).
func mapCraftMatch(grid []invStack, w int) (invStack, int) {
	maps, empties, paper, other := 0, 0, 0, 0
	var src invStack
	mapPos := -1
	for i, st := range grid {
		switch {
		case st.item == 0 || st.count == 0:
		case st.item == itemFilledMap && st.mapID != 0:
			maps++
			src = st
			mapPos = i
		case st.item == itemEmptyMap:
			empties++ // one consumed per cell per craft
		case st.item == itemPaper:
			paper++
		default:
			other++
		}
	}
	if maps != 1 || other != 0 {
		return invStack{}, mapCraftNone
	}
	if empties >= 1 && paper == 0 {
		return invStack{item: itemFilledMap, count: empties + 1, mapID: src.mapID}, mapCraftClone
	}
	// Zoom: full 3×3, paper on the ring, the map in the middle.
	if paper == 8 && empties == 0 && w == 3 && mapPos == 4 {
		return invStack{item: itemFilledMap, count: 1, mapID: src.mapID}, mapCraftZoom
	}
	return invStack{}, mapCraftNone
}

// craftResult is matchRecipe plus the dynamic map recipes (which win when
// they apply — the data tables can't carry a map identity).
func (h *hub) craftResult(grid []invStack, w int) (invStack, int) {
	if h.maps != nil {
		if res, kind := mapCraftMatch(grid, w); kind != mapCraftNone {
			if kind == mapCraftZoom {
				md := h.maps.get(res.mapID)
				if md == nil || md.Locked || md.Scale >= mapMaxScale {
					return invStack{}, mapCraftNone
				}
			}
			return res, kind
		}
	}
	item, count := matchRecipe(grid, w)
	return invStack{item: item, count: count}, mapCraftNone
}
