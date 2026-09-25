package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A shulker clings to a face (Shulker.DATA_ATTACH_FACE_ID): the floor, the
// ceiling or any wall. Each tick it checks it can still stay where it is —
// its cell empty, the block on the attached side sturdy, and room to open
// fully the other way — and if not it looks for another face around the
// same cell (Direction order: down, up, north, south, west, east), else
// teleports. Its box grows along the way it opens as it peeks
// (getPhysicalPeek); only the floor-clinging box is kept as a box here, the
// engine's boxes standing on their feet.

const (
	metaIndexShulkerAttach = 16 // DATA_ATTACH_FACE_ID (Direction): Shulker is no AgeableMob, the same on 26.2 and 26.3
	metaTypeDirection      = 12 // EntityDataSerializers.DIRECTION (canonical 770)
)

// dirStep is Direction's 3D-data-value order: down, up, north, south, west, east.
var dirStep = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func shulkerAttachMeta(m *mob) []byte {
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexShulkerAttach)
	b = protocol.AppendVarInt(b, metaTypeDirection)
	b = protocol.AppendVarInt(b, int32(m.shAttach))
	return protocol.AppendU8(b, itemMetaEnd)
}

// shulkerCanStayAt is Shulker.canStayAt.
func (h *hub) shulkerCanStayAt(dim int, p blockPos, face int8) bool {
	w := h.worldFor(dim)
	if !isAnyAir(w.At(p.x, p.y, p.z)) { // isPositionBlocked
		return false
	}
	s := dirStep[face]
	if !worldgen.IsSolidFull(w.At(p.x+s[0], p.y+s[1], p.z+s[2])) { // a sturdy face to cling to
		return false
	}
	o := dirStep[face^1] // room to open fully (getProgressAabb at 1.0)
	return !worldgen.Collides(w.At(p.x+o[0], p.y+o[1], p.z+o[2]))
}

// shulkerAttachableFace is findAttachableSurface (-1 for none).
func (h *hub) shulkerAttachableFace(dim int, p blockPos) int8 {
	for f := int8(0); f < 6; f++ {
		if h.shulkerCanStayAt(dim, p, f) {
			return f
		}
	}
	return -1
}

// setShulkerAttach is setAttachFace.
func (h *hub) setShulkerAttach(players map[int32]*tracked, m *mob, face int8) {
	if m.shAttach == face {
		return
	}
	m.shAttach = face
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(shulkerAttachMeta(m)))
}

// shulkerCheckAttach is Shulker.tick's stay check and findNewAttachment. It
// reports whether the shulker moved away (teleported).
func (h *hub) shulkerCheckAttach(players map[int32]*tracked, m *mob) bool {
	p := blockPos{floorInt(m.x), floorInt(m.y), floorInt(m.z)}
	if m.mount != 0 || h.shulkerCanStayAt(m.dim, p, m.shAttach) {
		return false
	}
	if f := h.shulkerAttachableFace(m.dim, p); f >= 0 {
		h.setShulkerAttach(players, m, f)
		return false
	}
	return h.shulkerTeleport(players, m)
}

// shulkerEasePeek is updatePeekAmount: the shell eases toward what it was
// told, a twentieth a tick.
func (m *mob) shulkerEasePeek() {
	want := float64(m.shPeek) * 0.01
	step := 0.05 * mobMoveInterval
	switch {
	case m.shPeekCur < want:
		m.shPeekCur = math.Min(want, m.shPeekCur+step)
	case m.shPeekCur > want:
		m.shPeekCur = math.Max(want, m.shPeekCur-step)
	}
}

// shulkerPhysicalPeek is getPhysicalPeek: how far the box reaches out.
func shulkerPhysicalPeek(amount float64) float64 {
	return 0.5 - math.Sin((0.5+amount)*math.Pi)*0.5
}

// shulkerAxis is the axis of the face it clings to (the bullet's first leg
// avoids it).
func shulkerAxis(face int8) int {
	switch face {
	case 0, 1:
		return axisY
	case 2, 3:
		return axisZ
	}
	return axisX
}
