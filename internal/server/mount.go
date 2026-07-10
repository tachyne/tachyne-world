package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Rideable mounts. Horses, donkeys, mules, camels, pigs and striders can be
// saddled and ridden. Riding reuses the vehicle machinery: the mount is a
// passenger carrier, its movement is client-simulated (the riding client sends
// vehicle_move, which the hub validates and relays), and its server-side AI
// pauses while a rider is aboard. Sneaking dismounts.

var (
	itemSaddle            = itemByName["saddle"]
	itemCarrotOnStick     = itemByName["carrot_on_a_stick"]
	itemWarpedFungusStick = itemByName["warped_fungus_on_a_stick"]
)

// rideable reports whether a species can be saddled + ridden, and which held
// item (beyond a saddle) the rider needs to steer it (0 = the saddle alone
// steers, like a horse). Pigs need a carrot-on-a-stick; striders a warped
// fungus on a stick.
func rideable(etype int) (ok bool, steerItem int32) {
	switch etype {
	case entityHorse, entityDonkey, entityMule, entityCamel,
		entitySkeletonHorse, entityZombieHorse:
		return true, 0
	case entityPig:
		return true, itemCarrotOnStick
	case entityStrider:
		return true, itemWarpedFungusStick
	}
	return false, 0
}

// tryMount handles a right-click on a rideable mob: saddle it if the player
// holds a saddle and it isn't saddled yet, otherwise mount it. Returns true if
// the interaction was consumed.
func (h *hub) tryMount(players map[int32]*tracked, t *tracked, m *mob) bool {
	ok, _ := rideable(m.etype)
	if !ok || m.dying > 0 || m.baby {
		return false
	}
	held := heldStack(t).item
	if !m.saddled {
		if held != itemSaddle {
			return false // an unsaddled mount ignores an empty hand
		}
		m.saddled = true
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(saddleMeta(m.eid, m.etype)))
		h.playSound(players, "minecraft:entity.horse.saddle", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	}
	// Saddled: mount (unless the player is placing a block / feeding — a held
	// steer item still mounts, matching vanilla).
	h.mountMob(players, t, m)
	return true
}

// mountMob seats a player on a mob, pausing its AI and relaying the passenger.
func (h *hub) mountMob(players map[int32]*tracked, t *tracked, m *mob) {
	if m.rider != 0 || dist3(t.x, t.y, t.z, m.x, m.y, m.z) > maxMeleeReach+1 {
		return
	}
	m.rider = t.p.eid
	m.vx, m.vz, m.hasTarget = 0, 0, false
	h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.rider))
}

// dismountMob stands a mob's rider up beside it. Returns true if the player was
// riding a mob (so the vehicle dismount path can be skipped).
func (h *hub) dismountMob(players map[int32]*tracked, t *tracked) bool {
	for _, m := range h.mobs {
		if m.rider != t.p.eid {
			continue
		}
		m.rider = 0
		m.sx, m.sy, m.sz = m.x, m.y, m.z // realign the relative-move baseline
		h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid))
		t.x, t.y, t.z = m.x+0.9, m.y+0.6, m.z
		t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
		return true
	}
	return false
}

// applyMountMove is the vehicle_move authority for a ridden mob (mirrors
// applyVehicleMove): the client drives the mount, the hub validates the delta,
// adopts it, drags the rider along, and relays the move to everyone else.
func (h *hub) applyMountMove(players map[int32]*tracked, t *tracked, e evVehicleMove) bool {
	var m *mob
	for _, c := range h.mobs {
		if c.rider == e.eid {
			m = c
			break
		}
	}
	if m == nil {
		return false
	}
	if !finite(e.x) || !finite(e.y) || !finite(e.z) ||
		dist3(m.x, m.y, m.z, e.x, e.y, e.z) > vehicleMoveCap {
		t.p.trySendEv(vehicleMoveBody(m.x, m.y, m.z, m.yaw)) // snap back
		return true
	}
	moved := e.x != m.sx || e.y != m.sy || e.z != m.sz
	m.x, m.y, m.z, m.yaw = e.x, e.y, e.z, e.yaw
	t.x, t.y, t.z = e.x, e.y+0.6, e.z // the rider rides along (chunk streaming)
	t.p.setHubPos(e.x, e.z)
	if moved {
		m.sx, m.sy, m.sz = e.x, e.y, e.z
		move := entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, true)
		cx, cz := chunkFloor(m.x), chunkFloor(m.z)
		for _, o := range players {
			if o.p.eid != e.eid && o.dim == m.dim &&
				abs(chunkFloor(o.x)-cx) <= viewRadius && abs(chunkFloor(o.z)-cz) <= viewRadius {
				o.p.trySendEv(move)
			}
		}
		m.syaw = m.yaw
	}
	return true
}

// consumeHeld removes one of the player's held item (survival).
func (h *hub) consumeHeld(t *tracked) {
	slot := t.p.heldSlot()
	s := &t.inv.slots[slot]
	if s.count > 0 {
		if s.count--; s.count == 0 {
			*s = invStack{}
		}
		h.sendSlot(t, slot)
	}
}

// saddleMeta builds the metadata that shows a saddle on a mount. Horse saddle
// state is index 17 (bit 0x04 of the horse flags byte); pig/strider carry a
// dedicated boolean index. Kept minimal — the client renders the saddle.
func saddleMeta(eid int32, etype int) []byte {
	// Pigs (index 17) and striders (index 18) use a plain boolean "saddle";
	// horses pack it into the horse-flags byte at index 17 (bit 0x04).
	switch etype {
	case entityPig:
		return boolMeta(eid, 17, true)
	case entityStrider:
		return boolMeta(eid, 18, true)
	default: // horse family: flags byte, saddled bit
		b := protocol.AppendVarInt(nil, eid)
		b = protocol.AppendU8(b, 17)
		b = protocol.AppendVarInt(b, 0) // type 0: byte
		b = protocol.AppendU8(b, 0x04)  // is-saddled flag
		return protocol.AppendU8(b, itemMetaEnd)
	}
}

// boolMeta builds a single boolean entity-metadata entry at an index.
func boolMeta(eid int32, index byte, v bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, index)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, v)
	return protocol.AppendU8(b, itemMetaEnd)
}
