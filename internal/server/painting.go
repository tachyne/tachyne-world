package server

// painting.go — paintings, the first hanging entity (vanilla Painting /
// HangingEntity). Placement mirrors HangingEntityItem + Painting.create:
// side face only, the entity occupies the cell in front of the wall, and the
// variant is chosen at random among the LARGEST placeable variants that fit
// (every covered cell open, a solid wall block behind each, no overlap with
// another painting). The wire shape is add_entity with Data = the facing's
// 3D id and the position at the block's lower corner, plus one metadata
// entry (index 8) carrying the variant as a registry Holder — the holder id
// is the variant's index in OUR synced painting_variant registry (+1; 0
// would mean inline). Paintings persist with the containers file.

import (
	"encoding/binary"
	"log"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"

	"tachyne/internal/worldgen"
)

var (
	itemPainting   = itemByName["painting"]
	entityPainting = entityID("painting")
)

// painting is one placed painting entity.
type painting struct {
	eid     int32
	x, y, z int // the anchor cell (in front of the wall)
	dim     int
	dir     int32  // facing away from the wall: 2-5 (north/south/west/east)
	variant string // vanilla variant name, e.g. "wanderer"
	w, h    int    // variant size in blocks
}

type evPlacePainting struct {
	eid     int32
	x, y, z int
	dir     int32
	slot    int32
	variant string // creative-menu preset ("" = vanilla random largest fit)
}

func (evPlacePainting) isHubEvent() {}

// paintingCells lists the blocks a variant covers when anchored at the cell
// (x,y,z) facing dir — vanilla Painting.calculateBoundingBox: the box centers
// on the anchor, shifted half a block toward counterclockwise/up for even
// sizes.
func paintingCells(x, y, z, w, h int, facing string) [][3]int {
	ldx, ldz := facingDelta(leftOf(facing))
	lo := func(n int) int {
		if n%2 == 0 {
			return 1 - n/2
		}
		return -(n - 1) / 2
	}
	hi := func(n int) int {
		if n%2 == 0 {
			return n / 2
		}
		return (n - 1) / 2
	}
	var cells [][3]int
	for i := lo(w); i <= hi(w); i++ {
		for j := lo(h); j <= hi(h); j++ {
			cells = append(cells, [3]int{x + ldx*i, y + j, z + ldz*i})
		}
	}
	return cells
}

// paintingFits is vanilla Painting.survives for one variant at one anchor:
// every covered cell open (no colliding block), a sturdy wall block behind
// each, and no overlap with an already-placed painting.
func (h *hub) paintingFits(dim, x, y, z int, dir int32, w, hgt int) bool {
	facing := faceName(dir)
	bdx, bdz := facingDelta(oppositeFacing(facing)) // toward the wall
	world := h.worldFor(dim)
	for _, c := range paintingCells(x, y, z, w, hgt, facing) {
		if worldgen.Collides(world.Block(c[0], c[1], c[2])) {
			return false
		}
		if !worldgen.IsSolidFull(world.Block(c[0]+bdx, c[1], c[2]+bdz)) {
			return false
		}
	}
	for _, other := range h.paintings {
		if other.dim != dim {
			continue
		}
		for _, c := range paintingCells(x, y, z, w, hgt, facing) {
			for _, oc := range paintingCells(other.x, other.y, other.z, other.w, other.h, faceName(other.dir)) {
				if c == oc {
					return false
				}
			}
		}
	}
	return true
}

// onPlacePainting picks the variant and spawns the entity — vanilla
// Painting.create: among the placeable variants that fit, keep the largest
// by area, pick one at random; no fit means no placement (the item stays).
func (h *hub) onPlacePainting(players map[int32]*tracked, e evPlacePainting) {
	t := players[e.eid]
	if t == nil || e.dir < 2 || e.dir > 5 {
		return
	}
	log.Printf("painting place: eid=%d preset=%q at %d,%d,%d dir=%d", e.eid, e.variant, e.x, e.y, e.z, e.dir)
	if e.variant != "" { // a creative preset places exactly that variant
		for _, v := range paintingVariants {
			if v.Name == e.variant {
				if h.paintingFits(t.dim, e.x, e.y, e.z, e.dir, v.W, v.H) {
					h.spawnPainting(players, t, e, v)
				}
				return
			}
		}
		return // unknown preset — place nothing rather than something random
	}
	var fits []paintingVariant
	best := 0
	for _, v := range paintingVariants {
		if !h.paintingFits(t.dim, e.x, e.y, e.z, e.dir, v.W, v.H) {
			continue
		}
		if a := v.W * v.H; a > best {
			best = a
			fits = fits[:0]
			fits = append(fits, v)
		} else if a == best {
			fits = append(fits, v)
		}
	}
	if len(fits) == 0 {
		return
	}
	h.spawnPainting(players, t, e, fits[h.rng.Intn(len(fits))])
}

// spawnPainting registers and broadcasts one placed painting.
func (h *hub) spawnPainting(players map[int32]*tracked, t *tracked, e evPlacePainting, v paintingVariant) {
	pt := &painting{eid: h.allocEID(), x: e.x, y: e.y, z: e.z, dim: t.dim,
		dir: e.dir, variant: v.Name, w: v.W, h: v.H}
	h.paintings[pt.eid] = pt
	h.showPainting(players, pt)
	h.signConsume(t, e.slot)
	h.playSoundDim(players, t.dim, "minecraft:entity.painting.place", sndBlock,
		float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
}

// showPainting broadcasts the spawn + variant metadata to nearby viewers.
func (h *hub) showPainting(players map[int32]*tracked, pt *painting) {
	h.toNearbyEv(players, pt.dim, float64(pt.x), float64(pt.z), paintingAddEv(pt))
	h.toNearbyEv(players, pt.dim, float64(pt.x), float64(pt.z), metaEv(paintingMetaBody(pt)))
}

// sendPaintingsTo replays existing paintings to a joining session.
func (h *hub) sendPaintingsTo(t *tracked) {
	for _, pt := range h.paintings {
		if pt.dim != t.dim {
			continue
		}
		t.p.trySendEv(paintingAddEv(pt))
		t.p.trySendEv(metaEv(paintingMetaBody(pt)))
	}
}

// paintingAddEv builds the spawn event — vanilla getAddEntityPacket: the
// position is the anchor block's LOWER CORNER (trackingPosition), Data is
// the facing's 3D id (down0 up1 north2 south3 west4 east5 — our dir is
// already that), yaw matches the facing for viewers that derive look.
func paintingAddEv(pt *painting) attachproto.EntityAdd {
	var uuid [16]byte
	binary.BigEndian.PutUint32(uuid[12:], uint32(pt.eid))
	yaw := map[int32]float32{2: 180, 3: 0, 4: 90, 5: 270}[pt.dir]
	return attachproto.EntityAdd{
		EID: pt.eid, UUID: uuid, Type: int32(entityPainting),
		X: float64(pt.x), Y: float64(pt.y), Z: float64(pt.z),
		Yaw: yaw, Data: pt.dir,
	}
}

// paintingMetaBody composes the canonical metadata list: index 8, the
// PAINTING_VARIANT serializer, holder = registry index + 1.
func paintingMetaBody(pt *painting) []byte {
	b := protocol.AppendVarInt(nil, pt.eid)
	b = append(b, 8)
	b = protocol.AppendVarInt(b, protocol.PaintingVariantSerializer770)
	b = protocol.AppendVarInt(b, protocol.PaintingVariantIndex(pt.variant)+1)
	return append(b, 0xff)
}

// breakPainting pops a painting: despawn, drop the item (vanilla dropItem —
// no drop for creative breakers), break sound.
func (h *hub) breakPainting(players map[int32]*tracked, pt *painting, by *tracked) {
	delete(h.paintings, pt.eid)
	h.toDimEv(players, pt.dim, entGone(pt.eid))
	if by == nil || by.gamemode != gmCreative {
		h.spawnItemIn(players, pt.dim, itemPainting, 1, float64(pt.x)+0.5, float64(pt.y)+0.5, float64(pt.z)+0.5)
	}
	h.playSoundDim(players, pt.dim, "minecraft:entity.painting.break", sndBlock,
		float64(pt.x)+0.5, float64(pt.y)+0.5, float64(pt.z)+0.5, 1, 1)
}

// paintingsOnBlockChange pops any painting the edit invalidated — vanilla
// BlockAttachedEntity ticks survives(); we re-check on the edits that can
// matter (a block placed inside the canvas or a support block removed).
func (h *hub) paintingsOnBlockChange(players map[int32]*tracked, dim, x, y, z int) {
	for _, pt := range h.paintings {
		if pt.dim != dim {
			continue
		}
		if math.Abs(float64(pt.x-x)) > 4 || math.Abs(float64(pt.y-y)) > 4 || math.Abs(float64(pt.z-z)) > 4 {
			continue
		}
		if !h.paintingFitsIgnoringSelf(pt) {
			h.breakPainting(players, pt, nil)
		}
	}
}

func (h *hub) paintingFitsIgnoringSelf(pt *painting) bool {
	saved := h.paintings[pt.eid]
	delete(h.paintings, pt.eid)
	ok := h.paintingFits(pt.dim, pt.x, pt.y, pt.z, pt.dir, pt.w, pt.h)
	h.paintings[pt.eid] = saved
	return ok
}
