package server

import (
	"math"
)

// The allay (vanilla Allay + AllayAi): hand it an item and it becomes its
// liked player's helper — it flies to matching drops within 32 blocks,
// gathers them into one stack, and brings them back, throwing them at the
// player (or at a note block it heard within the last 30 seconds) once
// within three blocks; between errands it stays within a few blocks of the
// player. A jukebox playing within earshot sets it dancing, and a dancing
// allay given an amethyst shard splits in two, once per five minutes each.

const (
	allayItemRange     = 32.0 // GoToWantedItem
	allayCloseEnough   = 3.0  // GoAndGiveItemsToTarget.CLOSE_ENOUGH_DISTANCE_TO_TARGET
	allayStayClose     = 4.0  // StayCloseToTarget close enough
	allayStayFar       = 16.0 // …and too far to bother
	allayPickupCD      = 60   // ITEM_PICKUP_COOLDOWN_AFTER_THROWING
	allayNoteCD        = 600  // LIKED_NOTEBLOCK_COOLDOWN_TICKS
	allayDupCD         = 6000 // DUPLICATION_COOLDOWN_TICKS
	allayHearRange     = 16.0 // the vibration listener's range
	allayItemSpeed     = 1.75
	allayDeliverSpeed  = 2.25
	metaIndexDancing   = 16 // Allay DATA_DANCING (bool)
	metaIndexCanDupe   = 17 // Allay DATA_CAN_DUPLICATE (bool)
	allayMetaPerUpdate = 20 / mobMoveInterval
)

var (
	itemAmethystShard = int32(itemByName["amethyst_shard"])
	allayThrowPitches = []float32{0.5625, 0.625, 0.75, 0.9375, 1, 1, 1.125, 1.25, 1.5, 1.875, 2, 2.25, 2.5, 3, 3.75, 4}
)

// allayWants is Allay.wantsToPickUp: the same item as the one in its hand
// (same potion for potions) with room in its stack.
func (m *mob) allayWants(it *itemEntity) bool {
	if m.held == 0 || it.item != m.held || it.count <= 0 {
		return false
	}
	if m.carry.item != 0 && (m.carry.potion != it.potion || m.carry.count >= stackMax) {
		return false
	}
	return true
}

// allayStep is one AI update. Returns true when the allay steered itself.
func (h *hub) allayStep(players map[int32]*tracked, m *mob) bool {
	if m.allayPickupCD > 0 {
		m.allayPickupCD -= mobMoveInterval
	}
	if m.allayNoteCD > 0 {
		m.allayNoteCD -= mobMoveInterval
	}
	if m.dupCD > 0 {
		if m.dupCD -= mobMoveInterval; m.dupCD <= 0 {
			m.dupCD = 0
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexCanDupe, true)))
		}
	}
	if dancing := h.jukeboxPlayingNear(m); dancing != m.dancing {
		m.dancing = dancing
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexDancing, dancing)))
	}
	if m.held == 0 || !h.rules.MobGriefing {
		return false
	}
	// Collecting: the nearest matching drop within range.
	if m.allayPickupCD <= 0 && (m.carry.item == 0 || m.carry.count < stackMax) {
		var best *itemEntity
		bestD := allayItemRange
		for _, it := range h.items {
			if it.dim != m.dim || !m.allayWants(it) {
				continue
			}
			if d := dist3(it.x, it.y, it.z, m.x, m.y, m.z); d < bestD {
				best, bestD = it, d
			}
		}
		if best != nil {
			if bestD <= 1.5 { // ITEM_PICKUP_REACH
				h.allayTake(players, m, best)
				return true
			}
			h.allaySteer(m, best.x, best.y, best.z, allayItemSpeed)
			return true
		}
	}
	// Delivering what it carries to the note block it heard, or its player.
	if m.carry.item != 0 && m.carry.count > 0 {
		if tx, ty, tz, ok := h.allayDeposit(players, m); ok {
			if dist3(tx, ty, tz, m.x, m.y, m.z) <= allayCloseEnough {
				h.allayThrow(players, m, tx, ty, tz)
				return true
			}
			h.allaySteer(m, tx, ty, tz, allayDeliverSpeed)
			return true
		}
		return false
	}
	// Idle with a job: keep near the player (StayCloseToTarget 4..16).
	if tx, ty, tz, ok := h.allayDeposit(players, m); ok {
		if d := dist3(tx, ty, tz, m.x, m.y, m.z); d > allayStayClose && d <= allayStayFar {
			h.allaySteer(m, tx, ty, tz, allayDeliverSpeed)
			return true
		}
	}
	return false
}

// allayDeposit is AllayAi.getItemDepositPosition: the liked note block while
// its memory lasts, otherwise the liked player.
func (h *hub) allayDeposit(players map[int32]*tracked, m *mob) (x, y, z float64, ok bool) {
	if m.allayNoteCD > 0 && m.dim == 0 {
		return float64(m.allayNote.x) + 0.5, float64(m.allayNote.y) + 0.5, float64(m.allayNote.z) + 0.5, true
	}
	if t := players[m.owner]; t != nil && t.dim == m.dim && !t.dead {
		return t.x, t.y + 1, t.z, true
	}
	return 0, 0, 0, false
}

func (h *hub) allaySteer(m *mob, x, y, z float64, speed float64) {
	dx, dz := x-m.x, z-m.z
	hd := math.Hypot(dx, dz)
	sp := m.moveSpeed() * speed
	if hd > 1e-6 {
		m.vx, m.vz = dx/hd*sp, dz/hd*sp
	} else {
		m.vx, m.vz = 0, 0
	}
	m.hasTarget, m.tx, m.tz, m.ty = true, x, z, y // fliers aim at the target's height
	m.rest = 0
}

// allayTake gathers a matching drop into the carried stack.
func (h *hub) allayTake(players map[int32]*tracked, m *mob, it *itemEntity) {
	room := stackMax
	if m.carry.item != 0 {
		room = stackMax - m.carry.count
	}
	take := min(room, it.count)
	if take <= 0 {
		return
	}
	if m.carry.item == 0 {
		m.carry = it.stack()
		m.carry.count = take
	} else {
		m.carry.count += take
	}
	if it.count -= take; it.count <= 0 {
		delete(h.items, it.eid)
		h.toNearbyEv(players, it.dim, it.x, it.z, entGone(it.eid))
	} else {
		h.refreshItemMeta(players, it)
	}
	h.playSoundDim(players, m.dim, "minecraft:entity.item.pickup", sndNeutral, m.x, m.y, m.z, 0.2, 1)
	m.persistent = true
}

// allayThrow drops the carried stack toward the deposit point.
func (h *hub) allayThrow(players map[int32]*tracked, m *mob, x, y, z float64) {
	st := m.carry
	m.carry = invStack{}
	dx, dz := x-m.x, z-m.z
	if hd := math.Hypot(dx, dz); hd > 1 {
		dx, dz = dx/hd, dz/hd
	}
	if it := h.spawnItemIn(players, m.dim, st.item, st.count, m.x+dx*0.5, m.y+0.5, m.z+dz*0.5); it != nil {
		it.dmg, it.ench, it.potion, it.name, it.color = st.dmg, st.ench, st.potion, st.name, st.color
		h.refreshItemMeta(players, it)
	}
	pitch := allayThrowPitches[h.rng.Intn(len(allayThrowPitches))]
	h.playSoundDim(players, m.dim, "minecraft:entity.allay.item_thrown", sndNeutral, m.x, m.y, m.z, 1, pitch)
	m.allayPickupCD = allayPickupCD
}

// jukeboxPlayingNear: a jukebox with a disc running within 16 blocks.
func (h *hub) jukeboxPlayingNear(m *mob) bool {
	if m.dim != 0 {
		return false
	}
	for pos, jb := range h.jukeboxes {
		if jb.started == 0 {
			continue
		}
		if dist3(float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, m.x, m.y, m.z) <= allayHearRange {
			return true
		}
	}
	return false
}

// allaysHearNote: a note block played within 16 blocks becomes an allay's
// delivery point for the next 600 ticks (AllayAi.hearNoteblock).
func (h *hub) allaysHearNote(dim int, pos blockPos) {
	for _, m := range h.mobs {
		if m.etype != entityAllay || m.dim != dim || m.dying > 0 {
			continue
		}
		if dist3(float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, m.x, m.y, m.z) <= allayHearRange {
			m.allayNote, m.allayNoteCD = pos, allayNoteCD
		}
	}
}

// tryAllay is Allay.mobInteract: an amethyst shard splits a dancing allay,
// an item goes into an empty hand (and the giver becomes its player), an
// empty hand takes the item back along with whatever it collected.
func (h *hub) tryAllay(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityAllay || m.dying > 0 || t.inv == nil {
		return false
	}
	held := heldStack(t)
	switch {
	case m.dancing && held.item == itemAmethystShard && m.dupCD == 0:
		if twin := h.spawnSpecies(players, entityAllay, m.dim, m.x, m.y, m.z); twin != nil {
			twin.persistent = true
			twin.dupCD = allayDupCD
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(twin.eid, metaIndexCanDupe, false)))
		}
		m.dupCD = allayDupCD
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(boolMeta(m.eid, metaIndexCanDupe, false)))
		h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusTameOK)) // hearts (event 18)
		h.playSoundDim(players, m.dim, "minecraft:block.amethyst_block.chime", sndNeutral, m.x, m.y, m.z, 2, 1)
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		return true
	case m.held == 0 && held.item != 0:
		m.held = held.item
		m.owner, m.ownerUUID = t.p.eid, t.p.uuid
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.allay.item_given", sndNeutral, m.x, m.y, m.z, 2, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: 1}, invStack{}, m.gear))
		m.persistent = true
		return true
	case m.held != 0 && held.item == 0:
		back := invStack{item: m.held, count: 1}
		m.held = 0
		m.owner, m.ownerUUID = 0, [16]byte{}
		h.toNearbyEv(players, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
		h.playSoundDim(players, m.dim, "minecraft:entity.allay.item_taken", sndNeutral, m.x, m.y, m.z, 2, 1)
		if m.carry.item != 0 {
			h.allayThrow(players, m, m.x, m.y, m.z)
		}
		changed, leftover := t.inv.addStack(back)
		for _, s := range changed {
			h.sendSlot(t, s)
		}
		if leftover > 0 {
			h.tossItem(players, t, back)
		}
		return true
	}
	return false
}
