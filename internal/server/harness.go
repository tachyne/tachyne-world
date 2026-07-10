package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
)

// The happy ghast harness + rideable flight (1.21.6). An adult happy ghast wears
// a dyed harness in its body-equipment slot; once harnessed it carries up to four
// players and the pilot (first aboard) flies it. Ghastlings (babies) can't be
// harnessed or ridden. Riding reuses the vehicle_move authority (client-driven,
// hub-validated) like the saddled mounts in mount.go, but with multiple seats and
// free flight (the mob already has m.flies).

const (
	ghastMaxRiders  = 4
	ghastRideHeight = 2.0 // rider offset above the ghast origin (chunk-stream only)
)

// harnessColors are the 16 dye variants, in registry order (matches the
// contiguous <color>_harness item ids).
var harnessColors = []string{
	"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black",
}

var harnessItems = func() map[int32]bool {
	m := make(map[int32]bool, len(harnessColors))
	for _, c := range harnessColors {
		if id, ok := itemByName[c+"_harness"]; ok {
			m[id] = true
		}
	}
	return m
}()

func isHarness(item int32) bool { return harnessItems[item] }

// ghastHarnessEquip renders the harness in the body equipment slot (empty clears
// it). Everything else in the loadout stays empty — a happy ghast wears nothing
// else.
func ghastHarnessEquip(eid, harness int32) attachproto.Equipment {
	var e attachproto.Equipment
	e.EID = eid
	e.Slots[attachproto.EquipBody] = stackEv(invStack{item: harness, count: 1})
	return e
}

// tryHappyGhast handles a right-click on a happy ghast: fit a harness, pop one
// off with shears, or board a harnessed adult. Returns true if consumed.
func (h *hub) tryHappyGhast(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityHappyGhast || m.dying > 0 || m.baby {
		return false // ghastlings can't be harnessed or ridden
	}
	held := heldStack(t).item
	switch {
	case m.harness == 0 && isHarness(held):
		m.harness = held
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.toNearbyEv(players, m.dim, m.x, m.z, ghastHarnessEquip(m.eid, m.harness))
		h.playSound(players, "minecraft:item.armor.equip_generic", sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	case m.harness != 0 && held == itemShears:
		h.spawnItem(players, m.harness, 1, m.x, m.y, m.z) // pop the harness off
		m.harness = 0
		h.toNearbyEv(players, m.dim, m.x, m.z, ghastHarnessEquip(m.eid, 0))
		return true
	case m.harness != 0:
		h.boardGhast(players, t, m)
		return true
	}
	return false
}

// boardGhast seats a player on a harnessed happy ghast (first aboard pilots it),
// pausing its AI and relaying the full passenger list.
func (h *hub) boardGhast(players map[int32]*tracked, t *tracked, m *mob) {
	if len(m.riders) >= ghastMaxRiders || dist3(t.x, t.y, t.z, m.x, m.y, m.z) > maxMeleeReach+2 {
		return
	}
	for _, r := range m.riders {
		if r == t.p.eid {
			return // already aboard
		}
	}
	m.riders = append(m.riders, t.p.eid)
	m.vx, m.vz, m.hasTarget = 0, 0, false
	h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.riders...))
}

// leaveGhast removes a player from any happy ghast they're riding, standing them
// beside it. Returns true if they were aboard one (so the mount/vehicle dismount
// paths are skipped).
func (h *hub) leaveGhast(players map[int32]*tracked, t *tracked) bool {
	for _, m := range h.mobs {
		idx := -1
		for i, r := range m.riders {
			if r == t.p.eid {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		m.riders = append(m.riders[:idx], m.riders[idx+1:]...)
		if len(m.riders) == 0 {
			m.sx, m.sy, m.sz = m.x, m.y, m.z // realign the relative-move baseline
		}
		h.toNearbyEv(players, m.dim, m.x, m.z, passengersBody(m.eid, m.riders...))
		t.x, t.y, t.z = m.x+0.9, m.y+0.6, m.z
		t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
		return true
	}
	return false
}

// applyGhastMove is the vehicle_move authority for a piloted happy ghast: the
// pilot's client simulates the flight, the hub validates the delta, adopts it,
// drags every rider along (chunk streaming), and relays the move to onlookers.
func (h *hub) applyGhastMove(players map[int32]*tracked, t *tracked, e evVehicleMove) bool {
	var m *mob
	for _, c := range h.mobs {
		if len(c.riders) > 0 && c.riders[0] == e.eid { // only the pilot drives
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
	riding := make(map[int32]bool, len(m.riders))
	for _, rid := range m.riders {
		riding[rid] = true
		if o := players[rid]; o != nil {
			o.x, o.y, o.z = e.x, e.y+ghastRideHeight, e.z
			o.p.setHubPos(e.x, e.z)
		}
	}
	if moved {
		m.sx, m.sy, m.sz = e.x, e.y, e.z
		move := entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, true)
		cx, cz := chunkFloor(m.x), chunkFloor(m.z)
		for _, o := range players {
			if !riding[o.p.eid] && o.dim == m.dim &&
				abs(chunkFloor(o.x)-cx) <= viewRadius && abs(chunkFloor(o.z)-cz) <= viewRadius {
				o.p.trySendEv(move)
			}
		}
		m.syaw = m.yaw
	}
	return true
}
