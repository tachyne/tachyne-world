package server

// sign.go — sign block entities with editable text, mirroring the vanilla
// server-side model (SignBlockEntity/SignText): two independent text sides
// (4 lines, dye color, glow), waxing, and a transient single-editor lock
// (vanilla playerWhoMayEdit). Placement follows SignItem/HangingSignItem:
// top face → standing (16-way rotation from yaw), side face → wall sign;
// bottom face → ceiling-hanging (attached under non-full blocks or when
// sneaking), side face → wall-hanging. Text reaches clients two ways: the
// chunk packet's block-entity NBT (appendBlockEntities) for (re)loads, and
// SignText frames (block_entity_data) for live edits.

import (
	"math"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"tachyne/internal/worldgen"
)

var (
	itemInkSac     = itemByName["ink_sac"]
	itemGlowInkSac = itemByName["glow_ink_sac"]

	// dyeColorByItem maps the 16 dye items to their DyeColor name.
	dyeColorByItem = func() map[int32]string {
		m := map[int32]string{}
		for _, c := range []string{
			"white", "orange", "magenta", "light_blue", "yellow", "lime",
			"pink", "gray", "light_gray", "cyan", "purple", "blue",
			"brown", "green", "red", "black",
		} {
			if id, ok := itemByName[c+"_dye"]; ok {
				m[id] = c
			}
		}
		return m
	}()
)

// Sign edit events. evSignPlaced also opens the editor on the front side,
// vanilla's post-placement openTextEdit.
type evSignPlaced struct {
	eid     int32
	x, y, z int
	dim     int
	hanging bool
}

type evUseSign struct {
	eid     int32
	x, y, z int
	item    int32
	slot    int32
}

type evSignUpdate struct {
	eid     int32
	x, y, z int
	front   bool
	lines   [4]string
}

func (evSignPlaced) isHubEvent() {}
func (evUseSign) isHubEvent()    {}
func (evSignUpdate) isHubEvent() {}

// --- placement (session goroutine) -----------------------------------------

// yawToRotation16 converts a yaw in degrees to the 16-segment rotation
// property (vanilla RotationSegment.convertToSegment).
func yawToRotation16(deg float32) int {
	return ((int(math.Round(float64(deg)*16/360)) % 16) + 16) % 16
}

// cardinalSegment is the rotation segment of a cardinal facing name.
func cardinalSegment(facing string) int {
	switch facing {
	case "east":
		return 4
	case "south":
		return 8
	case "west":
		return 12
	}
	return 0 // north
}

// faceName maps a clicked block face (2-5) to its direction name.
func faceName(dir int32) string {
	switch dir {
	case 2:
		return "north"
	case 3:
		return "south"
	case 4:
		return "west"
	}
	return "east"
}

func facingAxisX(f string) bool { return f == "west" || f == "east" }

// placeSign places a sign item as a standing sign (top face clicked; 16-way
// rotation faces the player) or a wall sign (side face). Vanilla's
// StandingAndWallBlockItem selection, simplified to the clicked face.
func (s *Server) placeSign(p *player, standingDef, wallDef uint32, tx, ty, tz int, dir int32, seq int32) bool {
	w := s.worldFor(p)
	var state uint32
	switch {
	case dir == 1: // standing on the ground
		if !worldgen.IsSolidFull(w.Block(tx, ty-1, tz)) {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		info, ok := worldgen.InfoForState(standingDef)
		if !ok {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		rot := yawToRotation16(p.yaw + 180)
		state = worldgen.SetProperty(info, standingDef, "rotation", strconv.Itoa(rot))
	case dir >= 2 && dir <= 5: // on a wall, facing away from it
		info, ok := worldgen.InfoForState(wallDef)
		if !ok {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		state = worldgen.SetProperty(info, wallDef, "facing", faceName(dir))
	default: // clicked a bottom face — a sign can't hang
		s.abortPlace(p, tx, ty, tz, seq)
		return false
	}
	s.putBlock(p, tx, ty, tz, state, true, seq)
	s.hub.post(evSignPlaced{eid: p.eid, x: tx, y: ty, z: tz, dim: p.dim})
	return true
}

// placeHangingSign places a hanging-sign item as a ceiling-hanging sign
// (bottom face clicked; ATTACHED under non-full support or when sneaking,
// vanilla's attach-to-middle rule) or a wall-hanging sign (side face; the
// bracket axis runs along the wall, so FACING is perpendicular to the
// clicked face).
func (s *Server) placeHangingSign(p *player, ceilingDef, wallDef uint32, tx, ty, tz int, dir int32, seq int32) bool {
	w := s.worldFor(p)
	var state uint32
	switch {
	case dir == 0: // hanging under the clicked block
		above := w.Block(tx, ty+1, tz)
		attached := p.sneaking || !worldgen.IsSolidFull(above)
		info, ok := worldgen.InfoForState(ceilingDef)
		if !ok {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		rot := yawToRotation16(p.yaw + 180)
		if !attached {
			rot = cardinalSegment(oppositeFacing(playerFacing(p.yaw)))
		}
		state = worldgen.SetProperty(info, ceilingDef, "rotation", strconv.Itoa(rot))
		if attached {
			state = worldgen.SetProperty(info, state, "attached", "true")
		}
	case dir >= 2 && dir <= 5: // bracket on the side of the clicked block
		f := playerFacing(p.yaw)
		if facingAxisX(f) == facingAxisX(faceName(dir)) {
			f = leftOf(f) // vanilla skips looking directions along the wall normal
		}
		info, ok := worldgen.InfoForState(wallDef)
		if !ok {
			s.abortPlace(p, tx, ty, tz, seq)
			return false
		}
		state = worldgen.SetProperty(info, wallDef, "facing", oppositeFacing(f))
	default: // top face — hanging signs only hang
		s.abortPlace(p, tx, ty, tz, seq)
		return false
	}
	s.putBlock(p, tx, ty, tz, state, true, seq)
	s.hub.post(evSignPlaced{eid: p.eid, x: tx, y: ty, z: tz, dim: p.dim, hanging: true})
	return true
}

// --- hub handlers -----------------------------------------------------------

// signAngle is a sign's front-face yaw in degrees, from its block state.
func signAngle(state uint32) float32 {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return 0
	}
	if rot := worldgen.GetProperty(info, state, "rotation"); rot != "" {
		n, _ := strconv.Atoi(rot)
		return float32(n) * 22.5
	}
	switch worldgen.GetProperty(info, state, "facing") {
	case "south":
		return 0
	case "west":
		return 90
	case "east":
		return 270
	}
	return 180 // north
}

// signFrontFacing reports whether the player is on a sign's front side —
// vanilla SignBlockEntity.isFacingFrontText (position-based, not look-based).
func signFrontFacing(t *tracked, x, y, z int, state uint32) bool {
	f := signAngle(state)
	a := float32(math.Atan2(t.z-(float64(z)+0.5), t.x-(float64(x)+0.5))*180/math.Pi) - 90
	d := float64(a - f)
	d = math.Mod(math.Mod(d, 360)+540, 360) - 180 // wrap to (-180, 180]
	return math.Abs(d) <= 90
}

func signTextEv(x, y, z int, sd signData) attachproto.SignText {
	return attachproto.SignText{
		X: int32(x), Y: int32(y), Z: int32(z),
		Front:   attachproto.SignSide{Lines: sd.Front.Lines, Color: sd.Front.Color, Glow: sd.Front.Glow},
		Back:    attachproto.SignSide{Lines: sd.Back.Lines, Color: sd.Back.Color, Glow: sd.Back.Glow},
		Waxed:   sd.Waxed,
		Hanging: sd.Hanging,
	}
}

// signBroadcast pushes a sign's state to everyone who may hold the chunk.
func (h *hub) signBroadcast(players map[int32]*tracked, dim, x, y, z int, sd signData) {
	ev := signTextEv(x, y, z, sd)
	bcx, bcz := chunkFloor(float64(x)), chunkFloor(float64(z))
	for _, t := range players {
		if t.dim != dim || abs(chunkFloor(t.x)-bcx) > viewRadius || abs(chunkFloor(t.z)-bcz) > viewRadius {
			continue
		}
		t.p.trySendEv(ev)
	}
}

// signEditorFree reports whether nobody else holds the edit lock — vanilla
// otherPlayerIsEditingSign, with the lock treated stale once its holder is
// gone or out of interaction range (vanilla clears it by ticking).
func (h *hub) signEditorFree(players map[int32]*tracked, key string, eid int32, x, y, z int) bool {
	other, held := h.signMayEdit[key]
	if !held || other == eid {
		return true
	}
	t := players[other]
	if t == nil {
		delete(h.signMayEdit, key)
		return true
	}
	dx, dy, dz := t.x-(float64(x)+0.5), t.y-(float64(y)+0.5), t.z-(float64(z)+0.5)
	if dx*dx+dy*dy+dz*dz > 5*5 {
		delete(h.signMayEdit, key)
		return true
	}
	return false
}

// onSignPlaced registers the fresh (blank) sign and opens the editor on its
// front side for the placer — vanilla SignItem.updateCustomBlockEntityTag.
func (h *hub) onSignPlaced(players map[int32]*tracked, e evSignPlaced) {
	t := players[e.eid]
	if t == nil {
		return
	}
	sd := signData{Hanging: e.hanging}
	h.signs.set(e.dim, e.x, e.y, e.z, sd)
	h.signMayEdit[signKey(e.dim, e.x, e.y, e.z)] = e.eid
	t.p.trySendEv(signTextEv(e.x, e.y, e.z, sd))
	t.p.trySendEv(attachproto.SignEditor{X: int32(e.x), Y: int32(e.y), Z: int32(e.z), Front: true})
}

// signConsume uses up one of the applied item (dye/ink/honeycomb), survival only.
func (h *hub) signConsume(t *tracked, slot int32) {
	if t.gamemode != gmSurvival || t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	if sl := &t.inv.slots[slot]; sl.count > 0 {
		sl.count--
		if sl.count == 0 {
			sl.item = 0
		}
		h.sendSlot(t, int(slot))
	}
}

// onUseSign handles a right click on a sign: apply a sign applicator
// (dye / glow ink / ink sac / honeycomb — vanilla SignApplicator rules) or
// open the editor on the side the player is facing.
func (h *hub) onUseSign(players map[int32]*tracked, e evUseSign) {
	t := players[e.eid]
	if t == nil {
		return
	}
	state := h.worldFor(t.dim).Block(e.x, e.y, e.z)
	kind, isSign := signKind(state)
	if !isSign {
		return
	}
	sd, ok := h.signs.get(t.dim, e.x, e.y, e.z)
	if !ok { // a sign without an entry (placed before this feature) edits as blank
		sd = signData{Hanging: kind == signHangingCeiling || kind == signHangingWall}
	}
	key := signKey(t.dim, e.x, e.y, e.z)
	if sd.Waxed {
		h.playSoundDim(players, t.dim, "minecraft:block.sign.waxed_interact_fail", sndBlock,
			float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
		return
	}
	if !h.signEditorFree(players, key, e.eid, e.x, e.y, e.z) {
		return
	}
	front := signFrontFacing(t, e.x, e.y, e.z, state)
	side := &sd.Front
	if !front {
		side = &sd.Back
	}
	sound := ""
	switch {
	case e.item == itemHoneycomb: // waxing works on blank signs too
		sd.Waxed = true
		sound = "minecraft:item.honeycomb.wax_on"
	case e.item == itemGlowInkSac && side.hasMessage() && !side.Glow:
		side.Glow = true
		sound = "minecraft:item.glow_ink_sac.use"
	case e.item == itemInkSac && side.hasMessage() && side.Glow:
		side.Glow = false
		sound = "minecraft:item.ink_sac.use"
	default:
		if c := dyeColorByItem[e.item]; c != "" && side.hasMessage() && effectiveColor(side.Color) != c {
			side.Color = c
			sound = "minecraft:item.dye.use"
			break
		}
		// no applicator — open the edit GUI on the facing side
		h.signMayEdit[key] = e.eid
		t.p.trySendEv(signTextEv(e.x, e.y, e.z, sd)) // sync the client's copy before the GUI reads it
		t.p.trySendEv(attachproto.SignEditor{X: int32(e.x), Y: int32(e.y), Z: int32(e.z), Front: front})
		return
	}
	h.signs.set(t.dim, e.x, e.y, e.z, sd)
	h.signConsume(t, e.slot)
	h.playSoundDim(players, t.dim, sound, sndBlock, float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 1, 1)
	h.signBroadcast(players, t.dim, e.x, e.y, e.z, sd)
}

func effectiveColor(c string) string {
	if c == "" {
		return "black"
	}
	return c
}

// sanitizeSignLine mirrors vanilla handleSignUpdate: strip legacy §-format
// codes and cap at the wire limit. Characters outside the basic multilingual
// plane are dropped too (they can't ride Java's modified-UTF-8 NBT strings).
func sanitizeSignLine(s string) string {
	if len(s) > 384 {
		s = s[:384]
	}
	var b strings.Builder
	skip := false
	for _, r := range s {
		switch {
		case skip:
			skip = false
		case r == '§':
			skip = true
		case r <= 0xffff:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// onSignUpdate applies an edit-GUI result — vanilla updateSignText: rejected
// unless this player holds the edit lock and the sign isn't waxed; the lock
// clears on success.
func (h *hub) onSignUpdate(players map[int32]*tracked, e evSignUpdate) {
	t := players[e.eid]
	if t == nil {
		return
	}
	state := h.worldFor(t.dim).Block(e.x, e.y, e.z)
	if _, isSign := signKind(state); !isSign {
		return
	}
	key := signKey(t.dim, e.x, e.y, e.z)
	if h.signMayEdit[key] != e.eid {
		return
	}
	sd, ok := h.signs.get(t.dim, e.x, e.y, e.z)
	if !ok || sd.Waxed {
		return
	}
	side := &sd.Front
	if !e.front {
		side = &sd.Back
	}
	for i, l := range e.lines {
		side.Lines[i] = sanitizeSignLine(l)
	}
	delete(h.signMayEdit, key)
	h.signs.set(t.dim, e.x, e.y, e.z, sd)
	h.signBroadcast(players, t.dim, e.x, e.y, e.z, sd)
}
