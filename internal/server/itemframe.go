package server

// Item frames (+ glow item frames), mirroring the vanilla ItemFrame model:
// block-attached entities on any of the six faces, holding a copy-of-one
// item that right-clicks rotate through 8 steps. Punching pops the item
// first, then the frame (vanilla's two-stage hurt). A framed filled map
// registers every nearby viewer as a map carrier (they receive color
// patches) and pins the green FRAME marker onto that map's decorations.

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

var (
	entityItemFrame     = int32(entityID("item_frame"))
	entityGlowItemFrame = int32(entityID("glow_item_frame"))
	itemItemFrame       = int32(itemByName["item_frame"])
	itemGlowItemFrame   = int32(itemByName["glow_item_frame"])
)

// Canonical metadata layout (770): the framed item is a Slot at index 8,
// the rotation a VarInt at index 9. 26.x inserted HangingEntity's synced
// DIRECTION at 8, shifting both by one — the gateway chain fixes that
// (FixItemFrameMeta), exactly like the painting variant shift.
const (
	frameMetaIndexItem     = 8
	frameMetaIndexRotation = 9
	frameMetaTypeVarInt    = 1
)

type itemFrame struct {
	eid     int32
	x, y, z int // the cell the frame occupies (in front of the support block)
	dim     int
	dir     int32 // 3D facing away from the support: 0=down 1=up 2-5 horizontal
	glow    bool
	held    invStack // what's framed (count is always 0 or 1)
	rot     int      // 0-7, 45° steps
}

type evPlaceFrame struct {
	eid     int32
	x, y, z int
	dir     int32
	slot    int32
	glow    bool
}

func (evPlaceFrame) isHubEvent() {}

// frameSupportOffset is the block the frame hangs on: opposite the facing.
var frameSupportOffset = [6][3]int{
	{0, 1, 0},  // facing down → support above
	{0, -1, 0}, // facing up → support below
	{0, 0, 1},  // north → south
	{0, 0, -1}, // south → north
	{1, 0, 0},  // west → east
	{-1, 0, 0}, // east → west
}

// frameFits: the cell is open, the block behind is solid, and no other
// frame occupies the same cell facing the same way (vanilla survives +
// canCoexist).
func (h *hub) frameFits(dim, x, y, z int, dir int32, ignore int32) bool {
	w := h.worldFor(dim)
	if worldgen.Collides(w.At(x, y, z)) {
		return false
	}
	off := frameSupportOffset[dir]
	if !worldgen.IsSolidFull(w.At(x+off[0], y+off[1], z+off[2])) {
		return false
	}
	for eid, f := range h.itemFrames {
		if eid != ignore && f.dim == dim && f.x == x && f.y == y && f.z == z && f.dir == dir {
			return false
		}
	}
	return true
}

// frameYawPitch renders the facing: horizontal = yaw quarters, floor and
// ceiling tilt the pitch (vanilla setDirection).
func frameYawPitch(dir int32) (float32, float32) {
	switch dir {
	case 0:
		return 0, 90 // facing down
	case 1:
		return 0, -90 // facing up
	}
	// 2D data value order: south=0 west=1 north=2 east=3 → yaw = v*90.
	twoD := map[int32]float32{3: 0, 4: 1, 2: 2, 5: 3}[dir]
	return twoD * 90, 0
}

func frameAddEv(f *itemFrame) attachproto.EntityAdd {
	typ := entityItemFrame
	if f.glow {
		typ = entityGlowItemFrame
	}
	yaw, pitch := frameYawPitch(f.dir)
	return attachproto.EntityAdd{
		EID: f.eid, Type: typ,
		X: float64(f.x) + 0.5, Y: float64(f.y) + 0.5, Z: float64(f.z) + 0.5,
		Yaw: yaw, Pitch: pitch, Data: f.dir,
	}
}

// frameMetaBody composes the frame's canonical metadata: the framed Slot
// (when present) + the rotation step.
func frameMetaBody(f *itemFrame) []byte {
	b := protocol.AppendVarInt(nil, f.eid)
	if f.held.count > 0 {
		b = protocol.AppendU8(b, frameMetaIndexItem)
		b = protocol.AppendVarInt(b, itemMetaTypeSlot)
		b = appendStack(b, f.held)
	}
	b = protocol.AppendU8(b, frameMetaIndexRotation)
	b = protocol.AppendVarInt(b, frameMetaTypeVarInt)
	b = protocol.AppendVarInt(b, int32(f.rot))
	return append(b, itemMetaEnd)
}

func (h *hub) showFrame(players map[int32]*tracked, f *itemFrame) {
	h.toNearbyEv(players, f.dim, float64(f.x), float64(f.z), frameAddEv(f))
	h.toNearbyEv(players, f.dim, float64(f.x), float64(f.z), metaEv(frameMetaBody(f)))
}

// sendFramesTo replays existing frames to a joining player's dimension.
func (h *hub) sendFramesTo(t *tracked) {
	for _, f := range h.itemFrames {
		if f.dim == t.dim {
			t.p.trySendEv(frameAddEv(f))
			t.p.trySendEv(metaEv(frameMetaBody(f)))
		}
	}
}

// onPlaceFrame handles using an item-frame item on a block face.
func (h *hub) onPlaceFrame(players map[int32]*tracked, e evPlaceFrame) {
	t := players[e.eid]
	if t == nil || t.inv == nil {
		return
	}
	if !h.frameFits(t.dim, e.x, e.y, e.z, e.dir, 0) {
		return
	}
	f := &itemFrame{eid: h.allocEID(), x: e.x, y: e.y, z: e.z, dim: t.dim,
		dir: e.dir, glow: e.glow}
	h.itemFrames[f.eid] = f
	h.showFrame(players, f)
	h.playSoundDim(players, t.dim, "minecraft:entity.item_frame.place", sndPlayer,
		float64(e.x), float64(e.y), float64(e.z), 1, 1)
	if t.gamemode != gmCreative {
		if sl := &t.inv.slots[e.slot]; sl.count > 0 {
			if sl.count--; sl.count == 0 {
				*sl = invStack{}
			}
			h.sendSlot(t, int(e.slot))
		}
	}
}

// interactFrame is the right-click: insert the held item, or rotate what's
// already framed (vanilla ItemFrame.interact).
func (h *hub) interactFrame(players map[int32]*tracked, t *tracked, f *itemFrame) {
	if f.held.count == 0 {
		if t.inv == nil {
			return
		}
		slot := t.p.heldSlot()
		st := t.inv.slots[slot]
		if st.count == 0 {
			return
		}
		f.held = st
		f.held.count = 1
		f.rot = 0
		if t.gamemode != gmCreative {
			if st.count--; st.count == 0 {
				st = invStack{}
			}
			t.inv.slots[slot] = st
			h.sendSlot(t, slot)
		}
		h.playSoundDim(players, f.dim, "minecraft:entity.item_frame.add_item", sndPlayer,
			float64(f.x), float64(f.y), float64(f.z), 1, 1)
	} else {
		f.rot = (f.rot + 1) % 8
		h.playSoundDim(players, f.dim, "minecraft:entity.item_frame.rotate_item", sndPlayer,
			float64(f.x), float64(f.y), float64(f.z), 1, 1)
	}
	h.toNearbyEv(players, f.dim, float64(f.x), float64(f.z), metaEv(frameMetaBody(f)))
	h.markFrameMapsDirty(f)
}

// hitFrame is the punch: pop the framed item first, then the frame itself
// (vanilla's two-stage hurt).
func (h *hub) hitFrame(players map[int32]*tracked, attacker *tracked, f *itemFrame) {
	creative := attacker != nil && attacker.gamemode == gmCreative
	if f.held.count > 0 {
		dropped := f.held
		f.held = invStack{}
		f.rot = 0
		h.markFrameMapsDirty(f)
		if !creative {
			if it := h.spawnItem(players, dropped.item, dropped.count,
				float64(f.x)+0.5, float64(f.y)+0.5, float64(f.z)+0.5); it != nil {
				it.dmg, it.ench, it.mapID = dropped.dmg, dropped.ench, dropped.mapID
			}
		}
		h.toNearbyEv(players, f.dim, float64(f.x), float64(f.z), metaEv(frameMetaBody(f)))
		h.playSoundDim(players, f.dim, "minecraft:entity.item_frame.remove_item", sndPlayer,
			float64(f.x), float64(f.y), float64(f.z), 1, 1)
		return
	}
	h.breakFrame(players, f, creative)
}

// breakFrame removes the frame entity, dropping its item form.
func (h *hub) breakFrame(players map[int32]*tracked, f *itemFrame, creative bool) {
	delete(h.itemFrames, f.eid)
	h.toNearbyEv(players, f.dim, float64(f.x), float64(f.z), entGone(f.eid))
	if !creative {
		frameItem := itemItemFrame
		if f.glow {
			frameItem = itemGlowItemFrame
		}
		h.spawnItem(players, frameItem, 1, float64(f.x)+0.5, float64(f.y)+0.5, float64(f.z)+0.5)
		if f.held.count > 0 {
			if it := h.spawnItem(players, f.held.item, f.held.count,
				float64(f.x)+0.5, float64(f.y)+0.5, float64(f.z)+0.5); it != nil {
				it.dmg, it.ench, it.mapID = f.held.dmg, f.held.ench, f.held.mapID
			}
		}
	}
	h.markFrameMapsDirty(f)
	h.playSoundDim(players, f.dim, "minecraft:entity.item_frame.break", sndPlayer,
		float64(f.x), float64(f.y), float64(f.z), 1, 1)
}

// framesOnBlockChange pops frames whose support vanished (checked on every
// block edit, like paintings).
func (h *hub) framesOnBlockChange(players map[int32]*tracked, dim, x, y, z int) {
	for _, f := range h.itemFrames {
		if f.dim != dim || abs(f.x-x) > 1 || abs(f.y-y) > 1 || abs(f.z-z) > 1 {
			continue
		}
		if !h.frameFits(f.dim, f.x, f.y, f.z, f.dir, f.eid) {
			h.breakFrame(players, f, false)
		}
	}
}

// markFrameMapsDirty pokes every holder of a framed map so decoration sets
// refresh promptly after frame changes.
func (h *hub) markFrameMapsDirty(f *itemFrame) {
	if h.maps == nil || f.held.item != itemFilledMap || f.held.mapID == 0 {
		return
	}
	if md := h.maps.get(f.held.mapID); md != nil {
		for _, hp := range md.holders {
			hp.dirtyDecor = true
		}
	}
}

// frame2D maps a 3D facing to vanilla's 2D data value (south=0 west=1
// north=2 east=3; vertical frames get 0 like vanilla's degenerate case).
func frame2D(dir int32) int {
	switch dir {
	case 3:
		return 0
	case 4:
		return 1
	case 2:
		return 2
	case 5:
		return 3
	}
	return 0
}

// mapFramesTick delivers framed-map updates: every 10 ticks each framed
// map registers nearby viewers as carriers and flushes their pending
// patches/decorations (vanilla ServerEntity.sendChanges).
func (h *hub) mapFramesTick(players map[int32]*tracked) {
	if h.maps == nil || len(h.itemFrames) == 0 {
		return
	}
	decorFor := map[int32][]attachproto.MapDecoration{}
	for _, f := range h.itemFrames {
		if f.held.item != itemFilledMap || f.held.mapID == 0 {
			continue
		}
		md := h.maps.get(f.held.mapID)
		if md == nil {
			continue
		}
		if _, ok := decorFor[md.ID]; !ok {
			decorFor[md.ID] = h.mapDecorations(md, players)
		}
		for _, t := range players {
			if t.dim != f.dim {
				continue
			}
			if abs(chunkFloor(t.x)-chunkFloor(float64(f.x))) > viewRadius ||
				abs(chunkFloor(t.z)-chunkFloor(float64(f.z))) > viewRadius {
				continue
			}
			h.mapSendUpdate(md, t, decorFor[md.ID])
		}
	}
}
