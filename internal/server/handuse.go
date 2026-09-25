package server

import "github.com/tachyne/tachyne-common/protocol"

// onInsertEye is EnderEyeItem.useOn on an end portal frame: the eye in the
// hand used goes into the frame (creative keeps it).
func (h *hub) onInsertEye(players map[int32]*tracked, e evInsertEye) {
	t := players[e.eid]
	if t == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	st := h.world.At(e.x, e.y, e.z)
	if !isEndFrame(st) || dist3(t.x, t.y, t.z, float64(e.x), float64(e.y), float64(e.z)) >= maxMeleeReach+1 {
		return
	}
	sl := t.handStack(t.useSlot())
	if sl == nil || sl.item != itemEnderEye || sl.count <= 0 {
		return
	}
	if t.gamemode != gmCreative {
		sl.count--
		if sl.count == 0 {
			*sl = invStack{}
		}
		h.sendHandSlot(t, t.useSlot())
	}
	h.insertEye(players, t, pos, st)
}

// cauldronUsesItem reports whether an item has a CauldronInteraction: the
// buckets, bottles and the washable dyed, shulker and banner items. Anything
// else is TRY_WITH_EMPTY_HAND, which for an offhand click falls through to the
// item's own use (a torch placed against the cauldron).
func cauldronUsesItem(item int32) bool {
	switch item {
	case itemBucketH2O, itemBucketLav, itemBucketSnow, itemBucket, itemGlassBottle, itemPotion:
		return true
	}
	if isDyeable(item) || isShulkerBoxItem(item) {
		return true
	}
	if def, ok := protocol.BlockForItem(item); ok {
		_, isBanner := bannerWallVariant[def]
		return isBanner
	}
	return false
}

// useOffhandOnBlock is BlockBehaviour.useItemOn for an OFF_HAND click: the
// blocks that do something with the stack in hand, gated on the item they
// take, since the empty-handed use (useWithoutItem) is MAIN_HAND only. It
// reports whether the block claimed the click; otherwise the offhand item's
// own use runs (Item.useOn, a block placed).
func (s *Server) useOffhandOnBlock(p *player, state uint32, held int32, x, y, z int, seq int32) bool {
	var ev hubEvent
	bites, isCake := cakeBites(state)
	level, isComposter := composterLevel(state)
	switch {
	case held == 0:
		return false
	case isCake:
		// CakeBlock.useItemOn: a candle goes into an uneaten cake.
		if bites == 0 && candleCakeBases[held] != 0 {
			ev = evUseCake{eid: p.eid, x: x, y: y, z: z, off: true}
		}
	case anchorCharge(state) >= 0:
		// RespawnAnchorBlock.useItemOn: glowstone charges a not-full anchor.
		if held == itemGlowstoneBlock && anchorCharge(state) < anchorMaxCharge {
			ev = evUseAnchor{eid: p.eid, slot: offhandSlot, x: x, y: y, z: z, off: true}
		}
	case isComposter:
		// ComposterBlock.useItemOn: a compostable item into a composter not yet full.
		if level < composterReady && compostChance[held] > 0 {
			ev = evUseComposter{eid: p.eid, slot: offhandSlot, x: x, y: y, z: z, off: true}
		}
	case isCampfireBlock(state):
		if _, cookable := campfireResult[held]; cookable { // CampfireBlock.useItemOn: food on the fire
			ev = evCampfireAdd{eid: p.eid, x: x, y: y, z: z, off: true}
		}
	case isLectern(state):
		// LecternBlock.useItemOn: a book onto a lectern without one.
		if !boolProp(state, "has_book") && isLecternBook(held) {
			ev = evUseLectern{eid: p.eid, x: x, y: y, z: z, off: true}
		}
	case isEndFrame(state):
		if held == itemEnderEye { // EnderEyeItem.useOn
			ev = evInsertEye{eid: p.eid, x: x, y: y, z: z, off: true}
		}
	case isBeeHome(state):
		// BeehiveBlock.useItemOn: shears or a bottle on a full hive.
		if honeyLevel(state) >= beeMaxHoney && (held == int32(itemByName["shears"]) || held == itemGlassBottle) {
			ev = evHarvestHive{eid: p.eid, x: x, y: y, z: z, off: true}
		}
	case isDecoratedPot(state):
		// DecoratedPotBlock.useItemOn: the stack goes in (the hub checks it fits).
		ev = evUsePot{eid: p.eid, x: x, y: y, z: z, off: true}
	default:
		// FlowerPotBlock.useItemOn: only a pottable plant does anything.
		if _, pottable := pottedByItem[held]; pottable {
			return s.usePot(p, true, x, y, z, state, seq)
		}
	}
	if ev == nil {
		return false
	}
	s.hub.post(ev)
	s.sendBlockChange(p, x, y, z, state, seq)
	return true
}
