package server

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"tachyne/internal/world"

	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/worldgen"
)

// handleDig processes a Player Action: on a block break it records the edit,
// sends the authoritative Block Update, acknowledges the client's prediction,
// and broadcasts the change to other players via the hub.
func (s *Server) handleDig(p *player, data []byte) {
	br := bytes.NewReader(data)
	status, err := protocol.ReadVarInt(br)
	if err != nil {
		return
	}
	var posb [8]byte
	if _, err := io.ReadFull(br, posb[:]); err != nil {
		return
	}
	br.ReadByte()                     // face (unused)
	seq, _ := protocol.ReadVarInt(br) // prediction sequence

	if status == digReleaseUse {
		s.hub.post(evStopEat{eid: p.eid, fire: true}) // ends an eat-hold; looses a drawn bow
		return
	}
	if status == digDropStack || status == digDropOne {
		// Q / ctrl+Q with no window open: toss from the held hotbar slot. The
		// client already removed the item from its view; the hub spawns the
		// matching item entity (or the item would vanish entirely).
		s.hub.post(evTossHeld{eid: p.eid, slot: p.held, all: status == digDropStack})
		return
	}
	if status != digStartBreak && status != digFinishBreak {
		return
	}
	x, y, z := protocol.ReadPosition(posb[:])
	if !s.hub.ownedBlock(x, z) {
		return // this pod does not own the target chunk (finite world / cross-shard)
	}
	broken := s.worldFor(p).Block(x, y, z)
	mode := s.modes.get(p.name)

	// Mining time: creative breaks instantly on Start; survival/adventure break on
	// Finish, which the client sends only after the per-block dig time (driven by
	// the block's hardness) elapses. Acting on the wrong phase per-mode either
	// breaks instantly in survival or never breaks in creative.
	switch mode {
	case gmCreative:
		if status != digStartBreak {
			return
		}
	case gmSurvival:
		// Unbreakable blocks (bedrock, barrier, portals) never yield to mining.
		if !worldgen.Diggable(broken) {
			s.sendBlockChange(p, x, y, z, broken, seq) // revert the client's prediction
			return
		}
		// Hardness-0 blocks (grass, flowers, torches, crops) break instantly and the
		// client sends only Start, never Finish. Everything else breaks on Finish,
		// which the client sends after the hardness-derived dig time elapses.
		if worldgen.Hardness(broken) == 0 {
			if status != digStartBreak {
				return
			}
		} else if status == digStartBreak {
			p.digStartAt, p.digPos = s.hub.tick.Load(), blockPos{x, y, z} // arm the timer
			return
		} else if status != digFinishBreak {
			return
		} else {
			// AUTHORITY: a Finish faster than the hardness allows (with generous
			// tool + latency slack) is a fast-break cheat — revert, don't apply.
			elapsed := int(s.hub.tick.Load() - p.digStartAt)
			if p.digPos != (blockPos{x, y, z}) || elapsed < minDigTicks(broken, p.heldItem()) {
				s.sendBlockChange(p, x, y, z, broken, seq)
				return
			}
		}
	default: // adventure / spectator cannot break blocks
		s.sendBlockChange(p, x, y, z, broken, seq)
		return
	}

	s.worldFor(p).SetBlock(x, y, z, worldgen.Air)
	s.sendBlockChange(p, x, y, z, worldgen.Air, seq)
	s.hub.post(evBlock{x: x, y: y, z: z, dim: p.dim, state: worldgen.Air, by: p.eid, broken: broken})
	if mode == gmSurvival { // survival drops loot (tool-gated); creative drops nothing
		s.hub.post(evDrop{x: x, y: y, z: z, state: broken, held: uint16(p.heldItem()), by: p.eid})
		if worldgen.Hardness(broken) > 0 { // real blocks wear the tool (vanilla)
			s.hub.post(evToolWear{eid: p.eid, slot: p.held})
		}
	}
	s.breakPairedHalf(p, x, y, z, broken)                   // remove the other half of a door/bed
	s.updateConnectNeighbors(s.worldFor(p), p.dim, x, y, z) // adjacent fences/walls re-evaluate here
	s.breakUnsupportedAbove(x, y, z)                        // grass/flowers pop off when their dirt is mined
}

// breakPairedHalf removes the matching half of a two-block block (door, bed, tall
// plant) when one half is broken, so it never leaves a floating half.
func (s *Server) breakPairedHalf(p *player, x, y, z int, broken uint32) {
	info, ok := worldgen.InfoForState(broken)
	if !ok {
		return
	}
	var ox, oy, oz int
	switch {
	case isTwoTall(info): // door / tall plant — other half above or below
		if worldgen.GetProperty(info, broken, "half") == "upper" {
			oy = -1
		} else {
			oy = 1
		}
	case isBed(info): // bed — head is one block along facing, foot the opposite way
		ox, oz = facingDelta(worldgen.GetProperty(info, broken, "facing"))
		if worldgen.GetProperty(info, broken, "part") == "head" {
			ox, oz = -ox, -oz
		}
	default:
		return
	}
	px, py, pz := x+ox, y+oy, z+oz
	if oi, ok := worldgen.InfoForState(s.worldFor(p).Block(px, py, pz)); ok && (isTwoTall(oi) || isBed(oi)) {
		s.putBlock(p, px, py, pz, worldgen.Air, false, 0)
	}
}

// handlePlace places the held block against the clicked face of a block.
func (s *Server) handlePlace(p *player, data []byte) {
	br := bytes.NewReader(data)
	protocol.ReadVarInt(br) // hand
	var posb [8]byte
	if _, err := io.ReadFull(br, posb[:]); err != nil {
		return
	}
	dir, _ := protocol.ReadVarInt(br)
	var cur [12]byte // cursor X,Y,Z (f32) — the click point within the clicked face
	if _, err := io.ReadFull(br, cur[:]); err != nil {
		return
	}
	cursorY := math.Float32frombits(binary.BigEndian.Uint32(cur[4:8]))
	br.Seek(2, io.SeekCurrent)        // insideBlock + worldBorderHit bools
	seq, _ := protocol.ReadVarInt(br) // prediction sequence

	x, y, z := protocol.ReadPosition(posb[:])

	if !s.hub.ownedBlock(x, z) {
		return // clicked block is outside this pod's region (finite world / cross-shard)
	}

	// Right-clicking an interactive block (door/gate/trapdoor) operates it instead
	// of placing — unless the player is sneaking.
	if !p.sneaking && s.tryUseBlock(p, x, y, z, seq) {
		return
	}

	dx, dy, dz := blockFaceOffset(dir)
	tx, ty, tz := x+dx, y+dy, z+dz
	if cs := s.worldFor(p).Block(x, y, z); worldgen.IsReplaceable(cs) || worldgen.IsWater(cs) || worldgen.IsLava(cs) {
		tx, ty, tz = x, y, z // vanilla replacingClickedOnBlock: fill the clicked cell (grass, snow, fluids)
	}

	if p.heldItem() == itemFlintSteel { // light a fire / prime TNT
		s.useFlintSteel(p, x, y, z, dx, dy, dz, seq)
		return
	}
	if p.heldItem() == itemGlassBottle && (worldgen.IsWater(s.worldFor(p).At(x, y, z)) ||
		worldgen.IsWater(s.worldFor(p).At(tx, ty, tz))) {
		s.hub.post(evFillBottle{eid: p.eid, slot: int32(p.held)})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if p.heldItem() == itemNetherWart {
		if s.worldFor(p).At(tx, ty-1, tz) == worldgen.SoulSand {
			s.putBlock(p, tx, ty, tz, netherWartMin, true, seq)
			if s.modes.get(p.name) == gmSurvival {
				s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
			}
		} else {
			s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		}
		return
	}
	if p.heldItem() == itemPainting { // paintings hang on walls (side faces only)
		if dir >= 2 && dir <= 5 {
			s.hub.post(evPlacePainting{eid: p.eid, x: tx, y: ty, z: tz, dir: dir, slot: int32(p.held)})
		}
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if _, isVeh := vehicleItems[p.heldItem()]; isVeh {
		s.hub.post(evPlaceVehicle{eid: p.eid, item: p.heldItem(), x: x, y: y, z: z, slot: int32(p.held)})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	defState, ok := protocol.BlockForItem(p.heldItem())
	if !ok || p.heldItem() == 0 {
		// Nothing placeable in hand — clear any client-side ghost and ack.
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if ts := s.worldFor(p).Block(tx, ty, tz); !worldgen.IsReplaceable(ts) && !worldgen.IsWater(ts) && !worldgen.IsLava(ts) {
		// vanilla BlockItem.canPlace: never overwrite an occupied cell (a
		// candle placed at a stair's open half must not eat the stair);
		// fluids stay replaceable — building into the ocean must keep working
		s.abortPlace(p, tx, ty, tz, seq)
		return
	}
	if defState == bellDefault { // bell: floor/ceiling/wall attachment from the clicked face
		if s.placeBell(p, defState, tx, ty, tz, dir, seq) && s.modes.get(p.name) == gmSurvival {
			s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		}
		return
	}
	if wallDef, isSign := signWallVariant[defState]; isSign { // sign item: standing or wall
		if s.placeSign(p, defState, wallDef, tx, ty, tz, dir, seq) && s.modes.get(p.name) == gmSurvival {
			s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		}
		return
	}
	if wallDef, isHanging := hangingWallVariant[defState]; isHanging { // hanging-sign item: ceiling or wall bracket
		if s.placeHangingSign(p, defState, wallDef, tx, ty, tz, dir, seq) && s.modes.get(p.name) == gmSurvival {
			s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		}
		return
	}
	if wallDef, isBanner := bannerWallVariant[defState]; isBanner { // banner: standing or wall
		if s.placeStandingOrWall(p, defState, wallDef, tx, ty, tz, dir, seq, true) && s.modes.get(p.name) == gmSurvival {
			s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		}
		return
	}
	if wallDef, isHead := headWallVariant[defState]; isHead { // mob head/skull: standing or wall
		if s.placeStandingOrWall(p, defState, wallDef, tx, ty, tz, dir, seq, false) && s.modes.get(p.name) == gmSurvival {
			s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
		}
		return
	}
	info, hasInfo := worldgen.OrientInfo(defState)
	placed := true
	switch {
	case hasInfo && isTwoTall(info): // doors and tall plants: place both halves
		placed = s.placeTwoTall(p, info, defState, tx, ty, tz, p.yaw, seq)
	case hasInfo && isBed(info): // beds: place foot + head
		placed = s.placeBed(p, info, defState, tx, ty, tz, p.yaw, seq)
	default:
		state := orientState(defState, dir, cursorY, p.yaw, p.pitch, s.worldFor(p).Block(x, y, z))
		state = s.connectState(s.worldFor(p), tx, ty, tz, state) // fences/panes/walls connect to neighbours
		if isAnyRail(state) {
			state = s.hub.placeRailShape(tx, ty, tz, state, p.yaw)
		}
		s.putBlock(p, tx, ty, tz, state, true, seq)
		s.updateConnectNeighbors(s.worldFor(p), p.dim, tx, ty, tz) // neighbours connect back to the new block
	}
	if placed && s.modes.get(p.name) == gmSurvival { // survival uses up one of the stack
		s.hub.post(evConsume{eid: p.eid, slot: int32(p.held)})
	}
}

// putBlock applies an edit, shows it to the editor (with the placement ack on the
// first block of an action) and broadcasts it to everyone else.
func (s *Server) putBlock(p *player, x, y, z int, state uint32, ack bool, seq int32) {
	s.worldFor(p).SetBlock(x, y, z, state)
	if ack {
		s.sendBlockChange(p, x, y, z, state, seq)
	} else {
		p.sendEv(blockSetEv(x, y, z, state))
	}
	s.hub.post(evBlock{x: x, y: y, z: z, dim: p.dim, state: state, by: p.eid})
}

// placeTwoTall places a door (or a tall plant) as its lower + upper halves. Doors
// also get a facing and a hinge side chosen from their neighbours.
func (s *Server) placeTwoTall(p *player, info worldgen.BlockInfo, defState uint32, x, y, z int, yaw float32, seq int32) bool {
	if !worldgen.IsReplaceable(s.worldFor(p).At(x, y+1, z)) { // no room for the upper half
		s.abortPlace(p, x, y, z, seq)
		return false
	}
	lower := defState
	if info.HasProperty("facing") {
		facing := playerFacing(yaw)
		lower = worldgen.SetProperty(info, lower, "facing", facing)
		if info.HasProperty("hinge") {
			lower = worldgen.SetProperty(info, lower, "hinge", s.doorHinge(s.worldFor(p), x, y, z, facing))
		}
	}
	lower = worldgen.SetProperty(info, lower, "half", "lower")
	upper := worldgen.SetProperty(info, lower, "half", "upper")
	s.putBlock(p, x, y, z, lower, true, seq)
	s.putBlock(p, x, y+1, z, upper, false, seq)
	return true
}

// placeBed places a bed as its foot (at the click) + head (one block in the facing
// direction the player is looking).
func (s *Server) placeBed(p *player, info worldgen.BlockInfo, defState uint32, x, y, z int, yaw float32, seq int32) bool {
	facing := playerFacing(yaw)
	hx, hz := facingDelta(facing)
	if !worldgen.IsReplaceable(s.worldFor(p).At(x+hx, y, z+hz)) { // no room for the head end
		s.abortPlace(p, x, y, z, seq)
		return false
	}
	foot := worldgen.SetProperty(info, defState, "facing", facing)
	foot = worldgen.SetProperty(info, foot, "part", "foot")
	head := worldgen.SetProperty(info, foot, "part", "head")
	s.putBlock(p, x, y, z, foot, true, seq)
	s.putBlock(p, x+hx, y, z+hz, head, false, seq)
	return true
}

// abortPlace rejects a placement: ack the sequence so the client rolls back its
// predicted block(s) and shows the cell as it actually is.
func (s *Server) abortPlace(p *player, x, y, z int, seq int32) {
	s.sendBlockChange(p, x, y, z, s.worldFor(p).Block(x, y, z), seq)
}

// tryUseBlock operates the clicked block if it's interactive (has an "open" state:
// doors, fence gates, trapdoors). Returns whether it handled the click. Doors are
// two-tall, so both halves toggle together.
func (s *Server) tryUseBlock(p *player, x, y, z int, seq int32) bool {
	state := s.worldFor(p).Block(x, y, z)
	if state == craftingTableState { // open the 3x3 crafting window
		s.hub.post(evOpenCraft{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq) // ack the interaction sequence
		return true
	}
	if state >= furnaceStateMin && state <= furnaceStateMax { // open the furnace
		s.hub.post(evOpenFurnace{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isChestBlock(state) { // open the chest (wooden/copper/trapped)
		s.hub.post(evOpenChest{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state == enchTableState { // open the enchanting table
		s.hub.post(evOpenEnchant{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state >= anvilStateMin && state <= anvilStateMax {
		s.hub.post(evOpenAnvil{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state >= grindstoneStateMin && state <= grindstoneStateMax {
		s.hub.post(evOpenGrind{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isEndFrame(state) && p.heldItem() == itemEnderEye {
		s.hub.post(evInsertEye{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isDispenser(state) || isDropper(state) || isHopper(state) || isBrewStand(state) {
		s.hub.post(evOpenBin{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isButton(state) || isLever(state) || isRepeater(state) ||
		isComparator(state) || isDaylight(state) { // redstone controls
		s.hub.post(evUseRedstone{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if s.usePot(p, x, y, z, state, seq) { // flower pot: pot / un-pot a plant
		return true
	}
	if _, isSign := signKind(state); isSign { // edit the sign / apply dye, ink, wax
		s.hub.post(evUseSign{eid: p.eid, x: x, y: y, z: z, item: p.heldItem(), slot: int32(p.held)})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return false
	}
	if isBed(info) { // claim the respawn point / sleep through the night
		s.hub.post(evUseBed{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if !info.HasProperty("open") {
		return false
	}
	nv := "true"
	if worldgen.GetProperty(info, state, "open") == "true" {
		nv = "false"
	}
	s.putBlock(p, x, y, z, worldgen.SetProperty(info, state, "open", nv), true, seq)
	if isTwoTall(info) { // a door — toggle its other half to match
		oy := y + 1
		if worldgen.GetProperty(info, state, "half") == "upper" {
			oy = y - 1
		}
		other := s.worldFor(p).Block(x, oy, z)
		if oi, ok := worldgen.InfoForState(other); ok && oi.HasProperty("open") {
			s.putBlock(p, x, oy, z, worldgen.SetProperty(oi, other, "open", nv), false, seq)
		}
	}
	return true
}

// doorHinge picks a door's hinge side: right if there's already a door to its left
// (so adjacent doors form a double door) or a wall on the right but not the left.
func (s *Server) doorHinge(w *world.World, x, y, z int, facing string) string {
	lx, lz := facingDelta(leftOf(facing))
	rx, rz := facingDelta(rightOf(facing))
	if s.isDoor(w.Block(x+lx, y, z+lz)) {
		return "right"
	}
	solidLeft := worldgen.IsSolidFull(w.Block(x+lx, y, z+lz)) || worldgen.IsSolidFull(w.Block(x+lx, y+1, z+lz))
	solidRight := worldgen.IsSolidFull(w.Block(x+rx, y, z+rz)) || worldgen.IsSolidFull(w.Block(x+rx, y+1, z+rz))
	if solidRight && !solidLeft {
		return "right"
	}
	return "left"
}

func (s *Server) isDoor(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && isTwoTall(info) && info.HasProperty("hinge")
}

// isTwoTall reports a door/tall-plant (half = upper/lower); isBed reports a bed.
func isTwoTall(info worldgen.BlockInfo) bool { return propHasValue(info, "half", "upper") }
func isBed(info worldgen.BlockInfo) bool     { return info.HasProperty("part") }

func propHasValue(info worldgen.BlockInfo, name, val string) bool {
	for _, p := range info.Props {
		if p.Name == name {
			for _, v := range p.Vals {
				if v == val {
					return true
				}
			}
		}
	}
	return false
}

// facingDelta is the (dx,dz) one block in a cardinal facing.
func facingDelta(facing string) (int, int) {
	switch facing {
	case "north":
		return 0, -1
	case "south":
		return 0, 1
	case "west":
		return -1, 0
	case "east":
		return 1, 0
	}
	return 0, 0
}

// leftOf / rightOf rotate a facing 90° (counter-)clockwise.
func leftOf(facing string) string {
	switch facing {
	case "north":
		return "west"
	case "south":
		return "east"
	case "west":
		return "south"
	default:
		return "north"
	}
}

func rightOf(facing string) string {
	switch facing {
	case "north":
		return "east"
	case "south":
		return "west"
	case "west":
		return "north"
	default:
		return "south"
	}
}

// handleUseEntity processes an Interact Entity packet. We act only on attacks

// handleEntityAction tracks sprint state from a Player Command packet so the hub

// handleHeldItem updates the player's selected hotbar slot.
func (p *player) handleHeldItem(data []byte) {
	if len(data) < 2 {
		return
	}
	if slot := int16(binary.BigEndian.Uint16(data[:2])); slot >= 0 && slot <= 8 {
		p.held = int(slot)
	}
}

// handleCreativeSlot records the item a creative client put in a slot, so we
// know what its hotbar holds. We only need the item id, not its components.
// AUTHORITY: gated by the player's actual game mode — a hacked survival
// client sending set_creative_slot must not poison the held-item mirror

// applyCreativeSlot records a creative-set slot — shared by the TCP parse
// above and the typed CreativeSlot action from gateways.
func (s *Server) applyCreativeSlot(p *player, slot int16, itemID int32, count int) {
	if slot >= 36 && slot <= 44 { // hotbar window slots
		p.setHotbarSlot(int(slot-36), itemID)
	}
	// Write through to the hub's authoritative inventory: modes SHARE one
	// inventory (vanilla), so a block picked in creative must survive server
	// inventory pushes (e.g. the refresh on closing a window) and a later
	// switch back to survival.
	s.hub.post(evCreativeSlot{eid: p.eid, slot: slot, st: invStack{item: itemID, count: count}})
}

// sendBlockChange sends the editor a Block Update setting (x,y,z). The
// prediction ack is the gateway's job (it acks with the client's real
// sequence number; the world only ever sees seq 0).
func (s *Server) sendBlockChange(p *player, x, y, z int, state uint32, seq int32) {
	_ = seq
	p.sendEv(blockSetEv(x, y, z, state))
}

// blockFaceOffset maps a clicked face direction to the adjacent block offset.
func blockFaceOffset(dir int32) (dx, dy, dz int) {
	switch dir {
	case 0:
		return 0, -1, 0
	case 1:
		return 0, 1, 0
	case 2:
		return 0, 0, -1
	case 3:
		return 0, 0, 1
	case 4:
		return -1, 0, 0
	case 5:
		return 1, 0, 0
	}
	return 0, 0, 0
}

// orientState turns a block's default state into the one a player would expect
// given how they placed it: logs take the clicked face's axis, slabs/stairs take
// a top/bottom half from the cursor, and facing blocks point sensibly. Blocks
// with no orientation property (most blocks) are returned unchanged.
func orientState(defaultState uint32, dir int32, cursorY, yaw, pitch float32, clicked uint32) uint32 {
	info, ok := worldgen.OrientInfo(defaultState)
	if !ok {
		return defaultState
	}
	state := defaultState
	for _, p := range info.Props {
		switch p.Name {
		case "axis": // logs, pillars: align with the clicked face
			state = worldgen.SetProperty(info, state, "axis", faceAxis(dir))
		case "type": // slabs: top or bottom half (double-slab merge is a follow-up)
			state = worldgen.SetProperty(info, state, "type", topOrBottom(dir, cursorY))
		case "half": // stairs, trapdoors
			state = worldgen.SetProperty(info, state, "half", topOrBottom(dir, cursorY))
		case "facing":
			if isRodState(defaultState) {
				// Rods point out of the clicked face (vanilla RodBlock); an end
				// rod placed on the tip of a same-facing end rod extends it
				// tip-to-tip instead (vanilla EndRodBlock).
				f := faceDirName(dir)
				if isEndRod(defaultState) && isEndRod(clicked) {
					if ci, ok := worldgen.InfoForState(clicked); ok && worldgen.GetProperty(ci, clicked, "facing") == f {
						f = oppositeFace6(f)
					}
				}
				state = worldgen.SetProperty(info, state, "facing", f)
				break
			}
			// Stairs (have a half) ascend toward the player's look; other facing
			// blocks (furnaces, pumpkins) put their front toward the player.
			f := playerFacing(yaw)
			if !info.HasProperty("half") {
				f = oppositeFacing(f)
			}
			if defaultState >= observerMin && defaultState <= observerMax {
				f = playerFacing(yaw) // observers WATCH the player's look direction
			}
			if sixWayFacing(defaultState) { // pistons/droppers/observers go vertical too
				if pitch > 60 {
					f = "up" // looking down → block faces up (toward the player)
				} else if pitch < -60 {
					f = "down"
				}
				if defaultState >= observerMin && defaultState <= observerMax && pitch > 60 {
					f = "down" // observer watches the look direction, so it inverts
				} else if defaultState >= observerMin && defaultState <= observerMax && pitch < -60 {
					f = "up"
				}
			}
			state = worldgen.SetProperty(info, state, "facing", f)
		}
	}
	return state
}

// faceAxis returns the block axis for a clicked face direction.
func faceAxis(dir int32) string {
	switch dir {
	case 0, 1:
		return "y"
	case 2, 3:
		return "z"
	default:
		return "x"
	}
}

// topOrBottom decides the half/type from the clicked face and cursor height:
// clicking a top face places low, a bottom face places high, a side splits at
// the middle of the face.
func topOrBottom(dir int32, cursorY float32) string {
	switch dir {
	case 1: // clicked the top of the block below
		return "bottom"
	case 0: // clicked the underside
		return "top"
	default:
		if cursorY > 0.5 {
			return "top"
		}
		return "bottom"
	}
}

// playerFacing maps a yaw to the cardinal direction the player is looking toward
// (Minecraft yaw: 0 = +Z south, 90 = -X west, 180 = -Z north, 270 = +X east).
func playerFacing(yaw float32) string {
	y := float64(yaw)
	y -= 360 * math.Floor(y/360) // normalise into [0,360)
	switch {
	case y < 45 || y >= 315:
		return "south"
	case y < 135:
		return "west"
	case y < 225:
		return "north"
	default:
		return "east"
	}
}

func oppositeFacing(f string) string {
	switch f {
	case "north":
		return "south"
	case "south":
		return "north"
	case "west":
		return "east"
	default:
		return "west"
	}
}

// Rods orient to the face they were placed against, unlike the look-based
// piston family. The set covers the whole vanilla RodBlock family: the end
// rod plus all eight lightning-rod oxidation/waxing variants.
var (
	endRodMin = worldgen.BlockBase("end_rod")

	rodBases = func() map[uint32]bool {
		m := map[uint32]bool{endRodMin: true}
		for _, n := range []string{
			"lightning_rod", "exposed_lightning_rod", "weathered_lightning_rod",
			"oxidized_lightning_rod", "waxed_lightning_rod",
			"waxed_exposed_lightning_rod", "waxed_weathered_lightning_rod",
			"waxed_oxidized_lightning_rod",
		} {
			m[worldgen.BlockBase(n)] = true
		}
		return m
	}()
)

func isRodState(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && rodBases[info.Min]
}

func isEndRod(state uint32) bool {
	info, ok := worldgen.InfoForState(state)
	return ok && info.Min == endRodMin
}

// faceDirName maps a clicked face to its direction name, all six faces.
func faceDirName(dir int32) string {
	switch dir {
	case 0:
		return "down"
	case 1:
		return "up"
	}
	return faceName(dir)
}

func oppositeFace6(f string) string {
	switch f {
	case "up":
		return "down"
	case "down":
		return "up"
	}
	return oppositeFacing(f)
}
