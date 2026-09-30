package server

import (
	"encoding/binary"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Cushions — 26.3's decoration/Cushion, a block-attached entity one block
// wide and a quarter tall that rests on the top of a block. CushionItem
// places one on an upward face, snapped to the cell's centre at the height
// clicked and turned to the nearest quarter of the player's facing; a
// right-click sits on it (unless sneaking, or someone already is), a blow
// breaks it and drops its item (not for a creative player), and every
// hundred ticks (BlockAttachedEntity's CHECK_INTERVAL) it goes the same way
// if nothing holds it up any more or a fire has reached it.
//
// 26.2 has no cushion: the gateways show those clients an armour stand
// (the entity-substitution guard) and drop its metadata.

var entityCushion = entityID("cushion")

const (
	cushionWidth      = 1.0  // EntityTypes.CUSHION sized(1.0, 0.25)
	cushionHeight     = 0.25 //
	cushionCheckEvery = 100  // BlockAttachedEntity.CHECK_INTERVAL
	cushionRiderDrop  = 0.6  // Avatar.DEFAULT_VEHICLE_ATTACHMENT: a sitter's feet are this far under the seat
	cushionAnchorGap  = 1.0 / 64
	cushionAnchorDown = 0.125

	metaIndexCushionColor = 8 // Cushion.DATA_COLOR: DYE_COLOR (26.3 serializer 43)
	// cushionColorSynced gates DATA_COLOR. DYE_COLOR is a serializer only
	// 26.3 has, so it goes out as an INT placeholder the gateway restores
	// (as the copper golem's state does; FixCushionMeta). Roll the gateways
	// first: an older one would send the INT and the client would reject it.
	cushionColorSynced = true
)

// cushionColor maps each cushion item to its DyeColor.
var cushionColor = func() map[int32]int8 {
	m := map[int32]int8{}
	for i, c := range dyeOrder {
		if id, ok := itemByName[c+"_cushion"]; ok {
			m[int32(id)] = int8(i)
		}
	}
	return m
}()

// cushion is one placed cushion.
type cushion struct {
	eid     int32
	uuid    [16]byte
	dim     int
	x, y, z float64
	yaw     float32
	item    int32  // the cushion item it drops, which is its colour
	name    string // custom_name, from a named item
	rider   int32  // the player sitting on it, or 0
	check   int    // ticksSinceLastCheck
}

// box is the cushion's AABB.
func (c *cushion) box() aabb {
	return aabb{c.x - cushionWidth/2, c.y, c.z - cushionWidth/2, c.x + cushionWidth/2, c.y + cushionHeight, c.z + cushionWidth/2}
}

// evPlaceCushion is CushionItem.useOn: the clicked block, whether the click
// replaces it (BlockPlaceContext's replaceClicked), the face, and the
// height of the hit.
type evPlaceCushion struct {
	eid       int32
	x, y, z   int
	up        bool    // the clicked face is the top
	replacing bool    // the clicked block is replaceable: the cushion goes in its cell
	hitY      float64 // the click's absolute height
	off       bool
}

func (evPlaceCushion) isHubEvent() {}

// isCushionItem reports a cushion item of any colour.
func isCushionItem(item int32) bool { _, ok := cushionColor[item]; return ok }

// onPlaceCushion is CushionItem.useOn.
func (h *hub) onPlaceCushion(players map[int32]*tracked, e evPlaceCushion) {
	t := players[e.eid]
	if t == nil || !e.up {
		return // anything but the top face: FAIL
	}
	st := usedStack(t)
	if !isCushionItem(st.item) {
		return
	}
	pos := blockPos{e.x, e.y + 1, e.z}
	if e.replacing {
		pos.y = e.y
	}
	c := &cushion{dim: t.dim, x: float64(pos.x) + 0.5, y: e.hitY, z: float64(pos.z) + 0.5,
		// Direction.fromYRot(rotation).toYRot(): the nearest quarter.
		yaw:  float32(int(math.Floor(float64(t.yaw)/90+0.5))&3) * 90,
		item: st.item, name: st.name}
	if !h.cushionFits(c) {
		return
	}
	for _, o := range h.cushions { // one cushion to a spot
		if o.dim == c.dim && boxesOverlap(o.box(), c.box()) {
			return
		}
	}
	c.eid = h.allocEID()
	binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
	h.cushions[c.eid] = c
	if h.cushionInFire(c) { // destroyIfInFire, straight away
		h.breakCushion(players, c, nil)
	} else {
		h.playSoundDim(players, c.dim, "minecraft:entity.cushion.place", sndBlock, c.x, c.y, c.z, 0.75, 0.8)
		h.vibAt(c.dim, freqEntityPlace, c.x, c.y, c.z, t.p.eid)
	}
	h.usedItem(t, st.item)
	if t.gamemode != gmCreative { // itemStack.consume(1, player)
		h.consumeUsed(t)
	}
}

// boxesOverlap is AABB.intersects: strictly overlapping, touching is not.
func boxesOverlap(a, b aabb) bool {
	return a.x0 < b.x1 && a.x1 > b.x0 && a.y0 < b.y1 && a.y1 > b.y0 && a.z0 < b.z1 && a.z1 > b.z0
}

// cushionFits is Cushion.canBePlacedAt: wouldSurviveAt, and its resting
// slice not already filled by a block it would sink into.
func (h *hub) cushionFits(c *cushion) bool {
	w := h.worldFor(c.dim)
	if w == nil {
		return false
	}
	bx, bz := floorInt(c.x), floorInt(c.z)
	// isAnchorBuried: a block that fills the slice it rests in.
	if worldgen.IsFullCube(w.At(bx, floorInt(c.y+cushionAnchorGap/2), bz)) {
		return false
	}
	return h.cushionSurvives(c)
}

// cushionSurvives is Cushion.wouldSurviveAt: something to rest on (a block
// whose outline meets the sliver just under it) and not buried in blocks
// that would suffocate.
func (h *hub) cushionSurvives(c *cushion) bool {
	w := h.worldFor(c.dim)
	if w == nil {
		return false
	}
	bx, bz := floorInt(c.x), floorInt(c.z)
	lo, hi := c.y-cushionAnchorGap, c.y // the anchor box's height
	anchored := false
	for y := floorInt(lo - cushionAnchorDown); y <= floorInt(hi) && !anchored; y++ {
		s := w.At(bx, y, bz)
		if s == worldgen.Air {
			continue
		}
		nx, ny, nz := outlineNudge(s, bx, bz)
		for _, b := range outlineOf(s) {
			// The shape's bounds against the anchor box (the cell's full
			// width, so the horizontal test is the shape's own extent).
			if b[3] > b[0] && b[5] > b[2] && float64(y)+ny+b[1] < hi && float64(y)+ny+b[4] > lo &&
				nx+b[0] < 1 && nx+b[3] > 0 && nz+b[2] < 1 && nz+b[5] > 0 {
				anchored = true
				break
			}
		}
	}
	if !anchored {
		return false
	}
	// isCoveredBySuffocatingBlocks: every cell of the box is a full cube.
	for y := floorInt(c.y + 1e-7); y <= floorInt(c.y+cushionHeight-1e-7); y++ {
		if !fullCube(w.At(bx, y, bz)) {
			return true
		}
	}
	return false
}

// cushionInFire is destroyIfInFire's search: a fire block among the cells
// its box (shrunk by a hair) covers.
func (h *hub) cushionInFire(c *cushion) bool {
	w := h.worldFor(c.dim)
	if w == nil {
		return false
	}
	b := c.box()
	for x := floorInt(b.x0 + 1e-7); x <= floorInt(b.x1-1e-7); x++ {
		for z := floorInt(b.z0 + 1e-7); z <= floorInt(b.z1-1e-7); z++ {
			for y := floorInt(b.y0 + 1e-7); y <= floorInt(b.y1-1e-7); y++ {
				if s := w.At(x, y, z); isFire(s) || s == soulFire {
					return true
				}
			}
		}
	}
	return false
}

// cushionColorMeta is DATA_COLOR as the INT placeholder a gateway would
// restore to DYE_COLOR.
func cushionColorMeta(c *cushion) []byte {
	b := protocol.AppendVarInt(nil, c.eid)
	b = protocol.AppendU8(b, metaIndexCushionColor)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(cushionColor[c.item]))
	return protocol.AppendU8(b, itemMetaEnd)
}

// showCushionTo spawns a cushion for one viewer: its colour, its name and
// whoever sits on it.
func (h *hub) showCushionTo(t *tracked, c *cushion) {
	t.p.trySendEv(entAdd(c.eid, entityCushion, c.uuid, c.x, c.y, c.z, c.yaw, 0))
	if cushionColorSynced && cushionColor[c.item] != 0 {
		t.p.trySendEv(metaEv(cushionColorMeta(c)))
	}
	if c.name != "" {
		t.p.trySendEv(metaEv(nameMeta(c.eid, c.name)))
	}
	if c.rider != 0 {
		t.p.trySendEv(passengersBody(c.eid, c.rider))
	}
}

// interactCushion is Cushion.interact: sneaking, or a cushion already sat
// on, passes; otherwise the player sits down.
func (h *hub) interactCushion(players map[int32]*tracked, t *tracked, c *cushion, sneak bool) {
	if sneak || c.rider != 0 || t.ridingEID != 0 || t.dead {
		return
	}
	c.rider = t.p.eid
	t.ridingEID = c.eid
	t.x, t.y, t.z = c.x, c.y+cushionHeight-cushionRiderDrop, c.z
	h.vibAt(c.dim, freqMount, c.x, c.y, c.z, t.p.eid)
	h.toTracking(players, c.eid, c.dim, c.x, c.z, passengersBody(c.eid, t.p.eid))
	t.p.trySendEv(passengersBody(c.eid, t.p.eid))
	h.playSoundDim(players, c.dim, "minecraft:entity.cushion.sit", sndNeutral, c.x, c.y, c.z, 1, 1)
}

// leaveCushion stands a sitter up (removePassenger), on top of the cushion
// (Entity.getDismountLocationForPassenger); the cushion grumbles unless it
// is going away itself. Reports whether they were sitting on one.
func (h *hub) leaveCushion(players map[int32]*tracked, t *tracked, removed bool) bool {
	c := h.cushions[t.ridingEID]
	if c == nil || c.rider != t.p.eid {
		return false
	}
	c.rider, t.ridingEID = 0, 0
	h.vibAt(c.dim, freqDismount, c.x, c.y, c.z, t.p.eid)
	h.toTracking(players, c.eid, c.dim, c.x, c.z, passengersBody(c.eid))
	t.p.trySendEv(passengersBody(c.eid))
	t.x, t.y, t.z = c.x, c.y+cushionHeight, c.z
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	if !removed {
		h.playSoundDim(players, c.dim, "minecraft:entity.cushion.get_up", sndNeutral, c.x, c.y, c.z, 1, 1)
	}
	return true
}

// hitCushion is a blow on a cushion (skipAttackInteraction →
// hurtOrSimulate): one breaks it, unless the player may not build.
func (h *hub) hitCushion(players map[int32]*tracked, t *tracked, c *cushion) {
	if t != nil && !mayBuild(t.gamemode) {
		return // isBreakingDeniedFor: !player.mayBuild()
	}
	h.breakCushion(players, c, t)
}

// breakCushion is kill + dropItem: the sitter stands up, the cushion goes
// with its break sound, and its item (with the cushion's name) drops unless
// a creative player broke it or entity drops are off.
func (h *hub) breakCushion(players map[int32]*tracked, c *cushion, by *tracked) {
	if h.cushions[c.eid] != c {
		return
	}
	if r := players[c.rider]; r != nil {
		h.leaveCushion(players, r, true)
	}
	delete(h.cushions, c.eid)
	h.entityGone(players, c.dim, c.eid)
	src := int32(0)
	if by != nil {
		src = by.p.eid
	}
	h.vibAt(c.dim, freqEntityDie, c.x, c.y, c.z, src)
	h.playSoundDim(players, c.dim, "minecraft:entity.cushion.break", sndNeutral, c.x, c.y, c.z, 1, 1)
	if !h.rules.EntityDrops || (by != nil && by.gamemode == gmCreative) {
		return
	}
	if it := h.spawnItemIn(players, c.dim, c.item, 1, c.x, c.y, c.z); it != nil && c.name != "" {
		it.name = c.name
		h.refreshItemMeta(players, it)
	}
}

// tickCushions is BlockAttachedEntity.tick: every hundred ticks each
// cushion checks for fire (destroyIfInFire) and for something to rest on
// (survives), and breaks, dropping its item, when either fails.
func (h *hub) tickCushions(players map[int32]*tracked) {
	for _, c := range h.cushions {
		if w := h.worldFor(c.dim); w == nil || !w.Loaded(int32(floorInt(c.x)>>4), int32(floorInt(c.z)>>4)) {
			continue
		}
		if c.check++; c.check < cushionCheckEvery+1 {
			continue
		}
		c.check = 0
		if h.cushionInFire(c) || !h.cushionSurvives(c) {
			h.breakCushion(players, c, nil)
		}
	}
}

// explosionHitsCushions breaks the cushions a blast reaches: a cushion
// ignores only a wind burst (shouldAffectBlocklikeEntities).
func (h *hub) explosionHitsCushions(players map[int32]*tracked, dim int, cx, cy, cz, power float64) {
	if h.blastSrc.direct == entityWindCharge || h.blastSrc.direct == entityBreezeWindCharge {
		return
	}
	for _, c := range h.cushions {
		if c.dim == dim && dist3(c.x, c.y+cushionHeight/2, c.z, cx, cy, cz) <= power*2 {
			h.breakCushion(players, c, nil)
		}
	}
}
