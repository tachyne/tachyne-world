package server

import (
	"bytes"
	"encoding/binary"
	"github.com/tachyne/tachyne-world/internal/world"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
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
	p.noteAck(seq)                    // the hub acknowledges it at the end of the tick

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
	if status == digSwapHands {
		// SWAP_ITEM_WITH_OFFHAND: the F key. The hub owns both stacks.
		s.hub.post(evSwapHands{eid: p.eid})
		return
	}
	if status == digAbortBreak {
		s.hub.post(evDigStop{eid: p.eid}) // ABORT_DESTROY_BLOCK: the cracks go
		return
	}
	if status == digStab {
		s.hub.post(evSpearStab{eid: p.eid}) // STAB: the jab of a held spear
		return
	}
	if status == digFinishBreak {
		s.hub.post(evDigStop{eid: p.eid}) // STOP_DESTROY_BLOCK: broken or refused, the cracks go
	}
	if status != digStartBreak && status != digFinishBreak {
		return
	}
	x, y, z := protocol.ReadPosition(posb[:])
	if !s.hub.ownedBlock(x, z) {
		return // this pod does not own the target chunk (finite world / cross-shard)
	}
	broken := s.worldFor(p).Block(x, y, z)
	if !s.hub.cellWithinBorder(p.dim, x, z) { // ServerLevel.mayInteract: not past the border
		s.sendBlockChange(p, x, y, z, broken, seq)
		return
	}
	mode := s.modes.get(p.key())
	// Player.blockActionRestricted: an adventure or spectator player cannot
	// break anything. The client predicts the break regardless, so the block
	// has to be sent back.
	if !mayBuild(mode) {
		s.sendBlockChange(p, x, y, z, broken, seq)
		return
	}

	// The block's attack hook (BlockBehaviour.attack) fires on the first
	// punch, and the dig proceeds normally on top of it: a note block plays
	// its note, redstone ore lights up, the dragon egg blinks away.
	if status == digStartBreak {
		switch {
		case isNoteBlock(broken):
			s.hub.post(evNoteBlock{eid: p.eid, x: x, y: y, z: z})
		case isRedstoneOre(broken) && !boolProp(broken, "lit"):
			s.hub.post(evLightOre{eid: p.eid, x: x, y: y, z: z})
		case isDragonEgg(broken):
			s.hub.post(evDragonEgg{eid: p.eid, x: x, y: y, z: z})
		}
	}

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
			s.hub.post(evDigStart{eid: p.eid, dim: p.dim, x: x, y: y, z: z})
			return
		} else if status != digFinishBreak {
			return
		} else {
			// AUTHORITY: a Finish faster than the hardness allows (with generous
			// tool + latency slack) is a fast-break cheat — revert, don't apply.
			elapsed := int(s.hub.tick.Load() - p.digStartAt)
			if p.digPos != (blockPos{x, y, z}) || elapsed < minDigTicks(broken, p.heldItem(), p.digBonus(), p.digMult()) {
				s.sendBlockChange(p, x, y, z, broken, seq)
				return
			}
		}
	default: // adventure / spectator cannot break blocks
		s.sendBlockChange(p, x, y, z, broken, seq)
		return
	}

	// Plugin veto point: every authority check passed, nothing applied yet.
	// A cancel reverts the digger's client prediction — the same idiom as the
	// unbreakable/fast-break rejections above.
	if !s.hub.fireSync(&plugin.BlockBreakEvent{EID: p.eid, Name: p.name, Dim: p.dim,
		X: x, Y: y, Z: z, State: broken}) {
		s.sendBlockChange(p, x, y, z, broken, seq)
		return
	}

	after := worldgen.Air
	if worldgen.IsWaterlogged(broken) { // the water source stays when the block goes
		after = worldgen.WaterBase
	}
	if isSurvival(mode) { // a clutch loses one egg, not all of them
		if left, ok := turtleEggAfterPlayerBreak(broken); ok {
			after = left
		}
	}
	s.worldFor(p).SetBlock(x, y, z, after)
	s.sendBlockChange(p, x, y, z, after, seq)
	s.hub.post(evBlock{x: x, y: y, z: z, dim: p.dim, state: after, by: p.eid, broken: broken})
	if isSurvival(mode) { // survival drops loot (tool-gated); creative drops nothing
		s.hub.post(evDrop{dim: p.dim, x: x, y: y, z: z, state: broken, held: uint16(p.heldItem()), by: p.eid})
		if n := mineWear(p.heldItem(), broken); n > 0 { // Item/ShearsItem.mineBlock
			s.hub.post(evToolWear{eid: p.eid, slot: p.held, n: n})
		}
	}
	s.breakPairedHalf(p, x, y, z, broken)                   // remove the other half of a door/bed
	s.updateConnectNeighbors(s.worldFor(p), p.dim, x, y, z) // adjacent fences/walls re-evaluate here
	// Unsupported neighbours come down on the hub, off the evBlock above —
	// every direction, not only the cell directly overhead.
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
	hand, _ := protocol.ReadVarInt(br) // InteractionHand: 0 main, 1 off
	var posb [8]byte
	if _, err := io.ReadFull(br, posb[:]); err != nil {
		return
	}
	dir, _ := protocol.ReadVarInt(br)
	var cur [12]byte // cursor X,Y,Z (f32) — the click point within the clicked face
	if _, err := io.ReadFull(br, cur[:]); err != nil {
		return
	}
	cursorX := math.Float32frombits(binary.BigEndian.Uint32(cur[0:4]))
	cursorY := math.Float32frombits(binary.BigEndian.Uint32(cur[4:8]))
	cursorZ := math.Float32frombits(binary.BigEndian.Uint32(cur[8:12]))
	br.Seek(2, io.SeekCurrent)        // insideBlock + worldBorderHit bools
	seq, _ := protocol.ReadVarInt(br) // prediction sequence
	p.noteAck(seq)                    // the hub acknowledges it at the end of the tick

	x, y, z := protocol.ReadPosition(posb[:])
	// ServerGamePacketListenerImpl.handleUseItemOn works on the stack in the
	// hand the packet names. The client sends MAIN_HAND first and OFF_HAND
	// only when that passed, so a torch in the offhand beside a pickaxe
	// arrives as its own OFF_HAND click.
	off := hand == handOffhand

	if !s.hub.ownedBlock(x, z) {
		return // clicked block is outside this pod's region (finite world / cross-shard)
	}

	// ServerPlayerGameMode.useItemOn: a spectator's click does nothing to the
	// world. (Vanilla does let one open a container's menu to look inside;
	// this stops short of that rather than open doors and place blocks too.)
	placeMode := s.modes.get(p.key())
	if placeMode == gmSpectator || !s.hub.cellWithinBorder(p.dim, x, z) { // mayInteract: not past the border
		s.sendBlockChange(p, x, y, z, s.worldFor(p).Block(x, y, z), seq)
		return
	}

	// Right-clicking an interactive block (door/gate/trapdoor) operates it instead
	// of placing — unless the player is sneaking with something in a hand
	// (ServerPlayerGameMode.useItemOn: suppressUsingBlock = isSecondaryUseActive
	// && either hand holds an item). Sneaking empty-handed still opens a door.
	holding := p.heldItem() != 0 || p.offhandItem() != 0
	if !(p.sneaking && holding) && s.tryUseBlock(p, off, x, y, z, seq, dir, cursorX, cursorY, cursorZ) {
		return
	}

	// A hoe on tillable ground tills it instead of placing anything. Vanilla
	// runs HoeItem.useOn before BlockItem placement, and a hoe has no block to
	// place, so this has to come before the item->block lookup below.
	// ItemStack.useOn refuses every item's use on a block to a player who
	// may not build: an adventure player neither tills, flattens, strips,
	// waxes nor binds a compass.
	canUseOn := mayBuild(placeMode)
	if canUseOn && s.tryTill(p, off, x, y, z, dir, seq) {
		return
	}
	// Likewise a shovel flattening ground into a dirt path (ShovelItem.useOn).
	if canUseOn && s.tryFlatten(p, off, x, y, z, dir, seq) {
		return
	}
	// An axe stripping a log or scraping/un-waxing copper, and honeycomb waxing
	// copper (AxeItem.useOn / HoneycombItem.useOn) — same slot in the chain.
	if canUseOn && (s.tryAxeUse(p, off, x, y, z, seq) || s.tryHoneycombUse(p, off, x, y, z, seq) || s.tryCompassUse(p, off, x, y, z, seq)) {
		return
	}

	dx, dy, dz := blockFaceOffset(dir)
	tx, ty, tz := x+dx, y+dy, z+dz
	// BlockPlaceContext.canPlace → Player.mayUseItemAt: everything below this
	// line puts something INTO the world, which adventure mode may not do.
	// Using a block — a door, a button, a chest — is allowed and has already
	// happened above.
	if !mayBuild(placeMode) {
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	held, slot := p.handItem(off), p.handSlot(off) // the stack this click uses, and where it lives
	heldBlock, heldIsBlock := protocol.BlockForItem(held)
	if alias, isAlias := placeAlias[held]; isAlias {
		heldBlock, heldIsBlock = alias, true
	}
	replacingClicked := false
	// BlockBehaviour.canBeReplaced: a replaceable block gives way to anything
	// but its own item — leaf litter clicked with leaf litter stacks a
	// segment or, sneaking, stays as it is; it is never swapped for itself.
	if cs := s.worldFor(p).Block(x, y, z); (worldgen.IsReplaceable(cs) && !(heldIsBlock && sameBlock(cs, heldBlock))) || worldgen.IsWater(cs) || worldgen.IsLava(cs) {
		tx, ty, tz = x, y, z // vanilla replacingClickedOnBlock: fill the clicked cell (grass, snow, fluids)
		replacingClicked = true
	} else if heldIsBlock && isMultiface(heldBlock) && sameBlockFamily(cs, heldBlock) && !multifaceFull(cs) {
		tx, ty, tz = x, y, z // MultifaceBlock.canBeReplaced: the same item joins a block with a free face
		replacingClicked = true
	} else if heldIsBlock && blockReplaceableBy(cs, heldBlock, dir, cursorY, true, p.sneaking) {
		tx, ty, tz = x, y, z // the same item stacks into the clicked block (slab → double, one more candle/pickle/egg/layer)
		replacingClicked = true
	}

	if held == itemArmorStand { // spawn the stand at the target cell
		s.hub.post(evPlaceStand{eid: p.eid, x: tx, y: ty, z: tz, yaw: p.yaw, off: off})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if held == itemLead { // tie whatever this player is towing to a fence
		s.hub.post(evLeashFence{eid: p.eid, pos: blockPos{x, y, z}})
		return
	}
	if held == itemBoneMeal { // grow the clicked block
		s.hub.post(evBoneMeal{eid: p.eid, x: x, y: y, z: z, dx: dx, dy: dy, dz: dz, slot: slot})
		s.sendBlockChange(p, x, y, z, s.worldFor(p).Block(x, y, z), seq)
		return
	}
	if held == itemFlintSteel { // light a fire / prime TNT
		s.useFlintSteel(p, off, x, y, z, dx, dy, dz, seq)
		return
	}
	if held == itemFireCharge { // FireChargeItem.useOn: light in place, or start a fire
		s.useFireCharge(p, off, x, y, z, dx, dy, dz, seq)
		return
	}
	if held == itemBrush { // sweep a suspicious block
		s.hub.post(evBrush{eid: p.eid, x: x, y: y, z: z, dx: dx, dy: dy, dz: dz, off: off})
		s.sendBlockChange(p, x, y, z, s.worldFor(p).Block(x, y, z), seq)
		return
	}
	if held == itemBucketH2O || held == itemBucketLav ||
		held == itemBucketSnow || isMobBucket(held) { // pour into the target cell
		s.hub.post(evBucketEmpty{eid: p.eid, slot: slot, x: tx, y: ty, z: tz, cx: x, cy: y, cz: z})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if held == itemBucket { // scoop: the hub walks the look ray to a source
		s.hub.post(evBucketFill{eid: p.eid, slot: slot})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if held == itemGlassBottle && (worldgen.IsWater(s.worldFor(p).At(x, y, z)) ||
		worldgen.IsWater(s.worldFor(p).At(tx, ty, tz))) {
		s.hub.post(evFillBottle{eid: p.eid, slot: slot})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if held == itemNetherWart {
		if s.worldFor(p).At(tx, ty-1, tz) == worldgen.SoulSand {
			s.putBlock(p, tx, ty, tz, netherWartMin, true, seq)
			s.itemUsed(p, held)
			if isSurvival(s.modes.get(p.key())) {
				s.hub.post(evConsume{eid: p.eid, slot: slot})
			}
		} else {
			s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		}
		return
	}
	if held == itemPainting { // paintings hang on walls (side faces only)
		if dir >= 2 && dir <= 5 {
			s.hub.post(evPlacePainting{eid: p.eid, x: tx, y: ty, z: tz, dir: dir,
				slot: slot, variant: p.handPaintVariant(off)})
		}
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if held == itemItemFrame || held == itemGlowItemFrame {
		if dir >= 0 && dir <= 5 { // frames mount on any face, floor and ceiling too
			s.hub.post(evPlaceFrame{eid: p.eid, x: tx, y: ty, z: tz, dir: dir,
				slot: slot, glow: held == itemGlowItemFrame})
		}
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if _, isVeh := vehicleItems[held]; isVeh {
		s.hub.post(evPlaceVehicle{eid: p.eid, item: held, x: x, y: y, z: z, slot: slot})
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if held == itemFrogspawn {
		// Frogspawn is laid on the surface of water, never against a block
		// face — vanilla's PlaceOnWaterBlockItem passes on useOn entirely.
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	defState, ok := protocol.BlockForItem(held)
	if held == int32(itemString) { // string laid on a surface becomes tripwire
		defState, ok = tripwireDefaultState(), true
	}
	if alias, isAlias := placeAlias[held]; isAlias { // redstone → wire, cocoa beans → cocoa
		defState, ok = alias, true
	}
	if !ok { // planting items are named differently from their block — see farming.go
		if c, isSeed := cropForSeed(held); isSeed {
			if !s.canPlantAt(p, c, tx, ty, tz) {
				s.abortPlace(p, tx, ty, tz, seq)
				return
			}
			defState, ok = c.block, true
		}
	}
	if !ok || held == 0 {
		// Nothing placeable in hand — clear any client-side ghost and ack.
		s.sendBlockChange(p, tx, ty, tz, s.worldFor(p).Block(tx, ty, tz), seq)
		return
	}
	if ts := s.worldFor(p).Block(tx, ty, tz); !(worldgen.IsReplaceable(ts) && !sameBlock(ts, defState)) && !worldgen.IsWater(ts) && !worldgen.IsLava(ts) &&
		!(isMultiface(defState) && sameBlockFamily(ts, defState) && !multifaceFull(ts)) && // the same multiface item joins a block with a free face
		!blockReplaceableBy(ts, defState, dir, cursorY, replacingClicked, p.sneaking) { // or stacks into it
		// vanilla BlockItem.canPlace: never overwrite an occupied cell (a
		// candle placed at a stair's open half must not eat the stair);
		// fluids stay replaceable — building into the ocean must keep working
		s.abortPlace(p, tx, ty, tz, seq)
		return
	}
	// Plugin veto/mutation point: target cell + proposed default state are
	// resolved, nothing applied. Fires once per action (multi-cell placements
	// like doors and beds place both halves under this one event); a mutated
	// State swaps what gets placed.
	pev := &plugin.BlockPlaceEvent{EID: p.eid, Name: p.name, Dim: p.dim,
		X: tx, Y: ty, Z: tz, State: defState}
	if !s.hub.fireSync(pev) {
		s.abortPlace(p, tx, ty, tz, seq)
		return
	}
	defState = pev.State

	if defState == bellDefault { // bell: floor/ceiling/wall attachment from the clicked face
		if s.placeBell(p, defState, tx, ty, tz, dir, seq) {
			s.itemUsed(p, held) // BlockItem.useOn: ITEM_USED
			if isSurvival(s.modes.get(p.key())) {
				s.hub.post(evConsume{eid: p.eid, slot: slot})
			}
		}
		return
	}
	if wallDef, isSign := signWallVariant[defState]; isSign { // sign item: standing or wall
		if s.placeSign(p, defState, wallDef, tx, ty, tz, dir, replacingClicked, seq) {
			s.itemUsed(p, held) // BlockItem.useOn: ITEM_USED
			if isSurvival(s.modes.get(p.key())) {
				s.hub.post(evConsume{eid: p.eid, slot: slot})
			}
		}
		return
	}
	if wallDef, isHanging := hangingWallVariant[defState]; isHanging { // hanging-sign item: ceiling or wall bracket
		if s.placeHangingSign(p, defState, wallDef, tx, ty, tz, dir, replacingClicked, seq) {
			s.itemUsed(p, held) // BlockItem.useOn: ITEM_USED
			if isSurvival(s.modes.get(p.key())) {
				s.hub.post(evConsume{eid: p.eid, slot: slot})
			}
		}
		return
	}
	if wallDef, isBanner := bannerWallVariant[defState]; isBanner { // banner: standing or wall
		if s.placeStandingOrWall(p, defState, wallDef, tx, ty, tz, dir, replacingClicked, seq, false) {
			s.itemUsed(p, held) // BlockItem.useOn: ITEM_USED
			if isSurvival(s.modes.get(p.key())) {
				s.hub.post(evConsume{eid: p.eid, slot: slot})
			}
		}
		return
	}
	if wallDef, isHead := headWallVariant[defState]; isHead { // mob head/skull: standing or wall
		if s.placeStandingOrWall(p, defState, wallDef, tx, ty, tz, dir, replacingClicked, seq, true) {
			s.itemUsed(p, held) // BlockItem.useOn: ITEM_USED
			if isSurvival(s.modes.get(p.key())) {
				s.hub.post(evConsume{eid: p.eid, slot: slot})
			}
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
		target := s.worldFor(p).Block(tx, ty, tz)
		// Waterlogged only when placed into a water SOURCE, as vanilla's
		// getFluidState().getType() == Fluids.WATER is; a block dropped into a
		// stream stays dry, or every slab in a river would become a new source.
		intoWater := worldgen.IsFluidSource(target, worldgen.WaterBase)
		var state uint32
		lookPlaced := true
		// The face you clicked comes first (getNearestLookingDirections), so
		// a torch, lever or button sticks to the surface you highlighted.
		order := placeOrder(dir, replacingClicked, p.yaw, p.pitch)
		switch wallDef, isStandingWall := standingWallVariant[defState]; {
		case !isMultiface(defState) && blockReplaceableBy(target, defState, dir, cursorY, replacingClicked, p.sneaking):
			state, lookPlaced = stackedState(target) // one more slab half, candle, pickle, egg or layer
		case isStandingWall: // torches, coral fans: floor or wall by the look order
			state, lookPlaced = standingOrWallState(s.worldFor(p), blockPos{tx, ty, tz}, defState, wallDef, order)
		case isHangable(defState): // lanterns: hang from a ceiling or stand on a floor
			state, lookPlaced = hangableState(s.worldFor(p), blockPos{tx, ty, tz}, defState, order)
		case isCocoa(defState): // cocoa: faces its jungle log
			state, lookPlaced = cocoaState(s.worldFor(p), blockPos{tx, ty, tz}, defState, order)
		case isFaceAttached(defState): // levers, buttons, grindstones: floor, ceiling or wall by the look order
			state, lookPlaced = faceAttachedState(s.worldFor(p), blockPos{tx, ty, tz}, defState, order, p.yaw)
		case isMultiface(defState): // vines, lichen, sculk veins, resin: a face that something holds
			state, lookPlaced = multifacePlacement(s.worldFor(p), blockPos{tx, ty, tz}, defState, target, order)
		case isCrafter(defState): // crafter: front and top from the look
			state = crafterPlacedState(defState, p.yaw, p.pitch)
		case isBamboo(defState): // bamboo: a sapling on soil, a stalk on a stalk
			state, lookPlaced = bambooPlacedState(s.worldFor(p), blockPos{tx, ty, tz}, target)
		case isPotentSulfur(defState): // potent sulfur: dry, wet or a geyser by what is above and below
			state = potentSulfurValid(s.worldFor(p), blockPos{tx, ty, tz}, defState)
		case defState == dirtPathState: // a covered path goes down as dirt
			state = dirtPathPlacedState(s.worldFor(p), blockPos{tx, ty, tz}, defState)
		case isGrowingPlantHead(defState): // kelp and the vines: head or body by the plant ahead
			g, _ := growingPlantOf(defState)
			state, lookPlaced = growingPlantPlacedState(s.worldFor(p), blockPos{tx, ty, tz}, g, target)
		case isHugeMushroom(defState): // mushroom blocks: skin on faces that meet no twin
			state = hugeMushroomPlacedState(s.worldFor(p), blockPos{tx, ty, tz}, defState)
		case isSpeleothem(defState): // pointed dripstone, sulfur spikes: tip and thickness from the column
			state, lookPlaced = speleothemPlaced(s.worldFor(p), blockPos{tx, ty, tz}, defState, p.pitch, p.sneaking, target == worldgen.WaterBase)
		case isPaleMossCarpet(defState): // pale moss carpet: sides up the walls beside it
			state = mossCarpetUpdated(s.worldFor(p), blockPos{tx, ty, tz}, defState, true)
		case isTrapdoorBlock(defState): // trapdoors: hinged on a clicked side, else facing the player
			state = trapdoorPlacedState(defState, dir, cursorY, p.yaw, replacingClicked)
		case defState == ladderState: // ladders: the first wall in the look order
			state, lookPlaced = ladderPlacedState(s.worldFor(p), blockPos{tx, ty, tz}, defState, order, dir, replacingClicked, s.worldFor(p).Block(x, y, z))
		case worldgen.IsConcretePowder(defState) && powderSolidifies(s.worldFor(p), blockPos{tx, ty, tz}, target):
			state = worldgen.ConcreteFor(defState) // ConcretePowderBlock.getStateForPlacement: set into water it is concrete at once
		default:
			state = orientState(defState, dir, cursorY, p.yaw, p.pitch, s.worldFor(p).Block(x, y, z))
		}
		if defState == seagrassState && target != worldgen.WaterBase {
			lookPlaced = false // SeagrassBlock.getStateForPlacement: only into a water source
		}
		if !lookPlaced {
			s.abortPlace(p, tx, ty, tz, seq)
			return
		}
		state = s.connectState(s.worldFor(p), tx, ty, tz, state) // fences/panes/walls connect to neighbours
		if ns, ok := shapeUpdated(s.worldFor(p), blockPos{tx, ty, tz}, state, [3]int{0, -1, 0}); ok && shapeKinds[state] == shapeCampfire {
			state = ns // CampfireBlock.getStateForPlacement: a signal fire over a hay bale
		}
		if shapeKinds[state] == shapeGate { // FenceGateBlock.getStateForPlacement: in_wall beside a wall
			if gi, ok := worldgen.InfoForState(state); ok {
				dx, dz := facingDelta(clockwiseFacing(worldgen.GetProperty(gi, state, "facing")))
				if ns, ok := shapeUpdated(s.worldFor(p), blockPos{tx, ty, tz}, state, [3]int{dx, 0, dz}); ok {
					state = ns
				}
			}
		}
		if isCampfireBlock(state) {
			// CampfireBlock.getStateForPlacement: it faces the way the player
			// looks, and one set into a water source goes in unlit.
			if ci, ok := worldgen.InfoForState(state); ok {
				state = worldgen.SetProperty(ci, state, "facing", playerFacing(p.yaw))
				state = worldgen.SetProperty(ci, state, "lit", strconv.FormatBool(!intoWater))
			}
		}
		if isBigDripleaf(state) {
			// BigDripleafBlock.getStateForPlacement: set on more of the plant,
			// the leaf keeps the facing of the leaf or stem below it.
			if b := s.worldFor(p).Block(tx, ty-1, tz); isBigDripleaf(b) || inRange(b, dripleafStemRng) {
				if bi, ok := worldgen.InfoForState(b); ok {
					if li, ok := worldgen.InfoForState(state); ok {
						state = worldgen.SetProperty(li, state, "facing", worldgen.GetProperty(bi, b, "facing"))
					}
				}
			}
		}
		if shapeKinds[state] == shapeSnowy { // SnowyBlock.getStateForPlacement: grass set under snow is snowy
			if ns, ok := shapeUpdated(s.worldFor(p), blockPos{tx, ty, tz}, state, [3]int{0, 1, 0}); ok {
				state = ns
			}
		}
		if isPropagule(state) { // MangrovePropaguleBlock.getStateForPlacement: planted grown, AGE 4, standing
			if pi, ok := worldgen.InfoForState(state); ok {
				state = worldgen.SetProperty(pi, worldgen.SetProperty(pi, state, "age", "4"), "hanging", "false")
			}
		}
		if isAnyRail(state) {
			state = s.hub.placeRailShape(s.worldFor(p), tx, ty, tz, state, p.yaw)
		}
		if intoWater { // SimpleWaterloggedBlock: the block keeps the water
			if info, ok := worldgen.InfoForState(state); ok && info.HasProperty("waterlogged") {
				state = worldgen.SetProperty(info, state, "waterlogged", "true")
			}
		}
		if base, _, _, ok := leafInfo(state); ok {
			// LeavesBlock.getStateForPlacement: a placed leaf is PERSISTENT (a
			// hedge never decays) and takes its distance from its neighbours.
			d := 7
			for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				d = min(d, leafDistanceAt(s.worldFor(p).Block(tx+o[0], ty+o[1], tz+o[2]))+1)
			}
			state = leafWithDistance(state, base, d)
			if info, ok := worldgen.InfoForState(state); ok {
				state = worldgen.SetProperty(info, state, "persistent", "true")
			}
		}
		if isChestBlock(state) { // pair with an adjacent single chest → double chest
			state = s.pairChestOnPlace(p, tx, ty, tz, state)
		}
		if isScaffolding(state) { // ScaffoldingBlock.getStateForPlacement: distance + bottom
			state, _ = scaffoldUpdated(s.worldFor(p), blockPos{tx, ty, tz}, state)
		}
		if !canPlaceAt(s.worldFor(p), blockPos{tx, ty, tz}, state) {
			// vanilla canSurvive at placement: a rail, torch or flower with
			// nothing to hold it is refused rather than left floating.
			s.abortPlace(p, tx, ty, tz, seq)
			return
		}
		if s.hub.placeObstructed(p.dim, tx, ty, tz, state) {
			// BlockItem.canPlace: isUnobstructed — not into a mob, a player
			// (the placer too) or a vehicle.
			s.abortPlace(p, tx, ty, tz, seq)
			return
		}
		s.putPlaced(p, tx, ty, tz, state, true, seq)
		s.updateConnectNeighbors(s.worldFor(p), p.dim, tx, ty, tz) // neighbours connect back to the new block
		if at, body, ok := growingPlantAnchorBody(s.worldFor(p), blockPos{tx, ty, tz}, state); ok {
			s.putBlock(p, at.x, at.y, at.z, body, false, seq) // the head it was set on is now body
		}
		if isPaleMossCarpet(state) { // MossyCarpetBlock.setPlacedBy: maybe a layer above
			if top, ok := mossCarpetTopper(s.worldFor(p), blockPos{tx, ty, tz}); ok {
				s.putBlock(p, tx, ty+1, tz, top, false, seq)
				// The layer above turns the carpet's low sides beneath it tall.
				if st := mossCarpetUpdated(s.worldFor(p), blockPos{tx, ty, tz}, state, false); st != state {
					s.putBlock(p, tx, ty, tz, st, false, seq)
				}
			}
		}
	}
	if placed {
		// BlockItem.place: the block that ended up there speaks its sound
		// type's place event to everyone near it but the placer, whose own
		// client played it the moment it predicted the placement. Reading
		// the state back is what vanilla does too — a slab that stacked, a
		// block that waterlogged or a rail that bent has the final say.
		if name, vol, pitch := blockPlaceSound(s.worldFor(p).Block(tx, ty, tz)); name != "" {
			s.hub.post(evBlockSound{eid: p.eid, dim: p.dim, x: tx, y: ty, z: tz,
				name: name, volume: vol, pitch: pitch})
		}
		s.itemUsed(p, held)                   // BlockItem.useOn succeeded: ITEM_USED, in any game mode
		if isSurvival(s.modes.get(p.key())) { // survival uses up one of the stack
			s.hub.post(evConsume{eid: p.eid, slot: slot})
		}
	}
}

// putBlock applies an edit, shows it to the editor (with the placement ack on the
// first block of an action) and broadcasts it to everyone else.
func (s *Server) putBlock(p *player, x, y, z int, state uint32, ack bool, seq int32) {
	s.putBlockEv(p, x, y, z, state, ack, seq, false)
}

// putPlaced is putBlock for a block item's placement, which the hub's
// placement-time rules (placedOpenable) answer to.
func (s *Server) putPlaced(p *player, x, y, z int, state uint32, ack bool, seq int32) {
	s.putBlockEv(p, x, y, z, state, ack, seq, true)
}

func (s *Server) putBlockEv(p *player, x, y, z int, state uint32, ack bool, seq int32, placed bool) {
	s.worldFor(p).SetBlock(x, y, z, state)
	if ack {
		s.sendBlockChange(p, x, y, z, state, seq)
	} else {
		p.sendEv(blockSetEv(x, y, z, state))
	}
	s.hub.post(evBlock{x: x, y: y, z: z, dim: p.dim, state: state, by: p.eid, placed: placed})
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
		if inRange(defState, smallDripleafRng) {
			facing = oppositeFacing(facing) // SmallDripleafBlock: getHorizontalDirection().getOpposite()
		}
		lower = worldgen.SetProperty(info, lower, "facing", facing)
		if info.HasProperty("hinge") {
			lower = worldgen.SetProperty(info, lower, "hinge", s.doorHinge(s.worldFor(p), x, y, z, facing))
		}
	}
	lower = worldgen.SetProperty(info, lower, "half", "lower")
	upper := worldgen.SetProperty(info, lower, "half", "upper")
	if info.HasProperty("waterlogged") { // copyWaterloggedFrom, each half its own cell
		w := s.worldFor(p)
		lower = worldgen.SetProperty(info, lower, "waterlogged", strconv.FormatBool(w.At(x, y, z) == worldgen.WaterBase))
		upper = worldgen.SetProperty(info, upper, "waterlogged", strconv.FormatBool(w.At(x, y+1, z) == worldgen.WaterBase))
	}
	if s.hub.placeObstructed(p.dim, x, y, z, lower) || s.hub.placeObstructed(p.dim, x, y+1, z, upper) {
		s.abortPlace(p, x, y, z, seq) // isUnobstructed, for both halves
		return false
	}
	s.putPlaced(p, x, y, z, lower, true, seq)
	s.putPlaced(p, x, y+1, z, upper, false, seq)
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
	if s.hub.placeObstructed(p.dim, x, y, z, foot) || s.hub.placeObstructed(p.dim, x+hx, y, z+hz, head) {
		s.abortPlace(p, x, y, z, seq) // isUnobstructed, for both ends
		return false
	}
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
//
// off is the packet's hand. ServerPlayerGameMode.useItemOn hands the block the
// stack in THAT hand (BlockBehaviour.useItemOn), and runs the block's own
// empty-handed use (useWithoutItem: doors, chests, beds, buttons) only for
// MAIN_HAND, so an offhand click reaches just the item-driven uses.
func (s *Server) tryUseBlock(p *player, off bool, x, y, z int, seq int32, face int32, cx, cy, cz float32) bool {
	state := s.worldFor(p).Block(x, y, z)
	held, slot := p.handItem(off), p.handSlot(off)
	if _, _, isCauldron := cauldronOf(state); isCauldron && (!off || cauldronUsesItem(held)) { // bucket/bottle/wash interactions
		s.hub.post(evCauldron{eid: p.eid, slot: slot, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if !off && isFence(state) && p.leading.Load() > 0 {
		// FenceBlock.useWithoutItem → LeadItem.bindPlayerMobs: whatever is
		// in hand, a click on a fence ties the mobs this player leads to it.
		// Leading nothing, it passes and the held item acts as usual.
		s.hub.post(evLeashFence{eid: p.eid, pos: blockPos{x, y, z}})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isRedstoneOre(state) && !boolProp(state, "lit") { // RedStoneOreBlock.useItemOn: lights up (then falls through, so a block still places against it)
		s.hub.post(evLightOre{eid: p.eid, x: x, y: y, z: z})
	}
	// Item.useOn overriders (useon.go): what the held item does to this block,
	// which vanilla runs before the block's own use.
	if held != 0 {
		var ev hubEvent
		switch {
		case held == itemShears && isGrowingPlantHead(state):
			ev = evTrimPlant{eid: p.eid, x: x, y: y, z: z, off: off}
		case held == itemPotion && face != 0 && convertableToMud(state):
			ev = evMudBottle{eid: p.eid, x: x, y: y, z: z, off: off}
		case state == spawnerBlock && spawnEggEntity[held] != 0:
			ev = evEggSpawner{eid: p.eid, x: x, y: y, z: z, off: off}
		case spawnEggEntity[held] != 0:
			// SpawnEggItem.useOn anywhere else: the mob appears on the face.
			ev = evSpawnEgg{eid: p.eid, x: x, y: y, z: z, face: face, off: off}
		case held == itemEndCrystal && (state == obsidianBase || state == worldgen.Bedrock):
			ev = evPlaceCrystal{eid: p.eid, x: x, y: y, z: z, off: off}
		case held == itemFireworkRocket:
			ev = evPlaceRocket{eid: p.eid, x: x, y: y, z: z, face: face, cx: cx, cy: cy, cz: cz, off: off}
		case isMapItem(held) && isBannerState(state):
			ev = evMapBanner{eid: p.eid, x: x, y: y, z: z, off: off}
		}
		// ItemStack.useOn: a player who may not build (adventure) gets no
		// item use on a block — only the shears' trim, which is the plant's
		// own useItemOn, still goes through.
		if _, trim := ev.(evTrimPlant); ev != nil && !trim && !mayBuild(s.modes.get(p.key())) {
			ev = nil
		}
		if ev != nil {
			s.hub.post(ev)
			s.sendBlockChange(p, x, y, z, state, seq)
			return true
		}
	}
	if state == pumpkinBlock && held == itemShears { // PumpkinBlock.useItemOn: carve it
		s.hub.post(evCarvePumpkin{eid: p.eid, x: x, y: y, z: z, face: face, yaw: p.yaw, off: off})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if off {
		return s.useOffhandOnBlock(p, state, held, x, y, z, seq)
	}
	// RespawnAnchorBlock.useItemOn: a main hand without glowstone passes when
	// the offhand has some and the anchor can take it, so the offhand click
	// that follows charges it rather than this one claiming the spawn point.
	if c := anchorCharge(state); c >= 0 && c < anchorMaxCharge && held != itemGlowstoneBlock && p.offhandItem() == itemGlowstoneBlock {
		return false
	}
	if inRanges(candleRanges, state) { // snuff a candle, or eat a candle cake
		if held == itemFlintSteel || held == itemFireCharge {
			return false // the lighter handles it
		}
		if _, isCake := candleCakeOf(state); !isCake && held != 0 {
			return false // CandleBlock passes with anything in hand (stacking a candle, say)
		}
		s.hub.post(evUseCandle{eid: p.eid, x: x, y: y, z: z, cy: cy})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if _, isCake := cakeBites(state); isCake { // a bite, or a candle planted in it
		s.hub.post(evUseCake{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if anchorCharge(state) >= 0 { // charge it with glowstone, or claim it as a respawn point
		s.hub.post(evUseAnchor{eid: p.eid, slot: slot, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if _, isComposter := composterLevel(state); isComposter { // feed it, or take the bone meal
		s.hub.post(evUseComposter{eid: p.eid, slot: slot, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state == craftingTableState { // open the 3x3 crafting window
		s.hub.post(evOpenCraft{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq) // ack the interaction sequence
		return true
	}
	if _, isCooker := furnaceKindOf(state); isCooker { // furnace / blast furnace / smoker
		s.hub.post(evOpenFurnace{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	// Every 27-slot container routes through one decision, so a new one cannot
	// be added to the storage side and forgotten on the interaction side —
	// which is exactly how placed shulker boxes shipped unopenable.
	if isWoodShelf(state) { // a slot on its face: swap the held stack in or out (or the hotbar, powered)
		if woodShelfHitSlot(state, face, cx, cz) < 0 {
			return false // ShelfBlock.useItemOn: no slot hit (a side or the top) → PASS
		}
		s.hub.post(evUseWoodShelf{eid: p.eid, x: x, y: y, z: z, face: face, cx: cx, cy: cy, cz: cz})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	switch containerOpenFor(state) {
	case openChestWindow:
		s.hub.post(evOpenChest{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	case openEnderWindow:
		s.hub.post(evOpenEnder{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	case openPotSlot:
		s.hub.post(evUsePot{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	case openVaultSlot:
		s.hub.post(evUseVault{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	case openHiveHarvest:
		s.hub.post(evHarvestHive{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state == enchTableState { // open the enchanting table
		s.hub.post(evOpenEnchant{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state >= anvilStateMin && state <= anvilStateMax {
		s.hub.post(evOpenAnvil{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state >= grindstoneStateMin && state <= grindstoneStateMax {
		s.hub.post(evOpenGrind{eid: p.eid, x: int32(x), y: int32(y), z: int32(z)})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state == cartographyTableState {
		s.hub.post(evOpenCarto{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state >= stonecutterMin && state <= stonecutterMax {
		s.hub.post(evOpenStonecut{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state >= loomStateMin && state <= loomStateMax {
		s.hub.post(evOpenLoom{eid: p.eid})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state == smithingTableState {
		s.hub.post(evOpenSmith{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if state == beaconState {
		s.hub.post(evOpenBeacon{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isLectern(state) {
		hasBook := boolProp(state, "has_book")
		if hasBook || isLecternBook(int32(held)) {
			s.hub.post(evUseLectern{eid: p.eid, x: x, y: y, z: z})
			s.sendBlockChange(p, x, y, z, state, seq)
			return true
		}
		return false
	}
	if isBookshelf(state) {
		if shelfHitSlot(state, face, cx, cy, cz) < 0 {
			return false // ChiseledBookShelfBlock: not a slot face (getHitSlot empty) → PASS
		}
		s.hub.post(evUseShelf{eid: p.eid, x: x, y: y, z: z, face: face, cx: cx, cy: cy, cz: cz})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isCampfireBlock(state) {
		if _, cookable := campfireResult[held]; cookable { // lit or not, as vanilla
			s.hub.post(evCampfireAdd{eid: p.eid, x: x, y: y, z: z})
			s.sendBlockChange(p, x, y, z, state, seq)
			return true
		}
		return false // no menu — other clicks fall through (e.g. placing against it)
	}
	if isEndFrame(state) && held == itemEnderEye {
		s.hub.post(evInsertEye{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isDispenser(state) || isDropper(state) || isHopper(state) || isBrewStand(state) || isCrafter(state) {
		s.hub.post(evOpenBin{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isBell(state) { // strike it (the hub applies BellBlock.isProperHit)
		s.hub.post(evRingBell{eid: p.eid, x: x, y: y, z: z, dir: face, hitY: cy})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isNoteBlock(state) {
		// NoteBlock.useItemOn: a head clicked onto the TOP face passes, so it
		// is placed there (and sets the instrument) instead of tuning.
		if face == 1 && noteBlockTopInstruments[held] { // 1 = up
			return false
		}
		s.hub.post(evNoteBlock{eid: p.eid, x: x, y: y, z: z, tune: true})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isMovingPiston(state) { // MovingPistonBlock.useWithoutItem: an orphaned cell is cleared
		s.hub.post(evUseMovingPiston{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isDragonEgg(state) { // DragonEggBlock.useWithoutItem: it blinks away
		s.hub.post(evDragonEgg{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isJukebox(state) {
		// JukeboxBlock.useItemOn: an empty jukebox takes a disc; anything
		// else falls to useWithoutItem, which PASSes on an empty jukebox,
		// so the held block is placed against it.
		if _, _, disc := jukeboxSongFor(int32(held)); state == jukeboxState(false) && !disc {
			return false
		}
		s.hub.post(evUseJukebox{eid: p.eid, x: x, y: y, z: z, slot: slot})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	// A berry plant uses the click only when there is something to pick
	// (SweetBerryBushBlock.useItemOn / CaveVines.use); otherwise it passes
	// to the item, which is how bone meal grows a bush.
	if isWire(state) && !mayBuild(s.modes.get(p.key())) {
		return false // RedStoneWireBlock.useWithoutItem: PASS for a player who may not build
	}
	plant := (isBerryBush(state) || isCaveVine(state)) && berriesClaimClick(state, held)
	if plant || (isGolemStatue(state) && statueClaimsClick(state, held)) || isWire(state) { // the block's own use (blockclick.go)
		s.hub.post(evClickBlock{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if isButton(state) || isLever(state) || isRepeater(state) ||
		isComparator(state) || isDaylight(state) { // redstone controls
		s.hub.post(evUseRedstone{eid: p.eid, x: x, y: y, z: z})
		s.sendBlockChange(p, x, y, z, state, seq)
		return true
	}
	if s.usePot(p, off, x, y, z, state, seq) { // flower pot: pot / un-pot a plant
		return true
	}
	if kind, isSign := signKind(state); isSign && !chainsHangingSign(kind, state, held, face) { // edit the sign / apply dye, ink, wax
		s.hub.post(evUseSign{eid: p.eid, x: x, y: y, z: z, item: held, slot: slot})
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
	blockName, _ := worldgen.StateName(state)
	if !opensByHand(blockName) {
		// DoorBlock/TrapDoorBlock.useWithoutItem: iron answers only to
		// redstone, and the click PASSes, so a held block places against it.
		return false
	}
	nv := "true"
	freq := freqBlockOpen
	if worldgen.GetProperty(info, state, "open") == "true" {
		nv, freq = "false", freqBlockClose
	}
	s.hub.post(evVibration{eid: p.eid, x: x, y: y, z: z, freq: freq, quiet: true}) // BLOCK_OPEN / BLOCK_CLOSE
	s.hub.post(evBlockSound{eid: p.eid, dim: p.dim, x: x, y: y, z: z, name: openCloseSound(blockName, nv == "true")})
	ns := worldgen.SetProperty(info, state, "open", nv)
	if nv == "true" && strings.HasSuffix(blockName, "fence_gate") {
		// FenceGateBlock.useWithoutItem: opened from its back, a gate turns
		// to face the player, so it always swings away from them.
		if dir := playerFacing(p.yaw); worldgen.GetProperty(info, state, "facing") == oppositeFacing(dir) {
			ns = worldgen.SetProperty(info, ns, "facing", dir)
		}
	}
	s.putBlock(p, x, y, z, ns, true, seq)
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

// facingFromDelta is facingDelta backwards: the horizontal facing a unit
// step points along.
func facingFromDelta(dx, dz int) string {
	switch {
	case dz < 0:
		return "north"
	case dz > 0:
		return "south"
	case dx < 0:
		return "west"
	}
	return "east"
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
func (s *Server) applyCreativeSlot(p *player, slot int16, itemID int32, count int, paintVariant string) {
	if slot >= 36 && slot <= 44 { // hotbar window slots
		p.setHotbarSlot(int(slot-36), itemID)
		p.setHotbarPaint(int(slot-36), paintVariant)
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
	if st, _ := amethystStage(defaultState); st >= 0 || isShulkerBox(defaultState) {
		// ShulkerBoxBlock and AmethystClusterBlock: facing = the clicked face,
		// any of six (a bud points out of the face it was set on).
		if si, ok := worldgen.InfoForState(defaultState); ok {
			return worldgen.SetProperty(si, defaultState, "facing", faceDirName(dir))
		}
	}
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
			if isHopper(defaultState) { // HopperBlock: the spout points into the clicked block, or down
				state = worldgen.SetProperty(info, state, "facing", hopperFacing(dir))
				break
			}
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
			switch {
			case defaultState >= anvilStateMin && defaultState <= anvilStateMax:
				// AnvilBlock: getHorizontalDirection().getClockWise() — the
				// anvil lies ACROSS the look, not facing the player.
				f = clockwiseFacing(playerFacing(yaw))
			case isCalibSensor(defaultState):
				// CalibratedSculkSensorBlock: the amethyst face takes the
				// player's own look direction, not its opposite.
				f = playerFacing(yaw)
			}
			if sixWayFacing(defaultState) {
				// BlockPlaceContext.getNearestLookingDirection: the dominant axis
				// of the look. An observer watches it; pistons, dispensers,
				// droppers and barrels face back along it, toward the player.
				// This used to switch to up/down only past 60° of pitch, where
				// vanilla switches at 45° (less when looking diagonally).
				f = nearestLookingDirection(yaw, pitch)
				if !(defaultState >= observerMin && defaultState <= observerMax) {
					f = oppositeFace6(f)
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

// clockwiseFacing is Direction.getClockWise() on the horizontal ring.
func clockwiseFacing(f string) string {
	switch f {
	case "north":
		return "east"
	case "east":
		return "south"
	case "south":
		return "west"
	}
	return "north"
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

// nearestLookingDirection is Direction.orderedByNearest(entity)[0]: the
// direction whose axis the look vector points along most.
func nearestLookingDirection(yaw, pitch float32) string {
	p := float64(pitch) * math.Pi / 180
	y := -float64(yaw) * math.Pi / 180
	pitchSin, pitchCos := math.Sin(p), math.Cos(p)
	yawSin, yawCos := math.Sin(y), math.Cos(y)
	xPos, yPos, zPos := yawSin > 0, pitchSin < 0, yawCos > 0
	xYaw, yMag, zYaw := math.Abs(yawSin), math.Abs(pitchSin), math.Abs(yawCos)
	xMag, zMag := xYaw*pitchCos, zYaw*pitchCos
	axisX, axisY, axisZ := "west", "down", "north"
	if xPos {
		axisX = "east"
	}
	if yPos {
		axisY = "up"
	}
	if zPos {
		axisZ = "south"
	}
	if xYaw > zYaw {
		if yMag > xMag {
			return axisY
		}
		return axisX
	}
	if yMag > zMag {
		return axisY
	}
	return axisZ
}
