package server

import (
	"encoding/binary"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Vehicles: minecarts and boats. Riding is client-simulated (like vanilla):
// the riding client sends serverbound vehicle_move for the vehicle, the
// server validates the delta, adopts it, and relays to everyone else. Empty
// vehicles just sit; left-click breaks one back into its item. Detector
// rails press while a cart sits on them.

const (
	playServerVehicleMove = 0x20
	playServerPlayerInput = 0x29

	inputSneak = 0x20 // player_input flags bit

	vehicleMoveCap = 3.0 // max blocks per vehicle_move packet (authority)
)

var (
	entityMinecart = entityID("minecart") // minecraft:entity_type ordinals (1.21.5)
)

// boat entity ordinals by item name (1.21.5 split boats per wood type).
var boatEntities = map[string]int{
	"oak_boat": 84, "spruce_boat": 119, "birch_boat": 12, "jungle_boat": 71,
	"acacia_boat": 0, "dark_oak_boat": 31, "cherry_boat": 22, "mangrove_boat": 78,
	"pale_oak_boat": 89,
}

// vehicleItems: item id → entity type, built from the generated name table.
var vehicleItems = func() map[int32]int {
	m := map[int32]int{}
	if id, ok := itemByName["minecart"]; ok {
		m[id] = entityMinecart
	}
	for name, et := range boatEntities {
		if id, ok := itemByName[name]; ok {
			m[id] = et
		}
	}
	return m
}()

// vehicleItemFor inverts vehicleItems (drop on break).
func vehicleItemFor(etype int) int32 {
	for id, et := range vehicleItems {
		if et == etype {
			return id
		}
	}
	return 0
}

type vehicle struct {
	eid        int32
	dim        int // dimension (0 overworld — nether vehicles unsupported)
	uuid       [16]byte
	etype      int
	x, y, z    float64
	yaw        float32
	rider      int32   // player eid, 0 when empty
	sx, sy, sz float64 // last broadcast position (relative-move baseline)
}

func (v *vehicle) isBoat() bool { return v.etype != entityMinecart }

type evPlaceVehicle struct {
	eid     int32
	item    int32
	x, y, z int
	slot    int32
}
type evVehicleMove struct {
	eid     int32
	x, y, z float64
	yaw     float32
}
type evDismount struct{ eid int32 }

func (evPlaceVehicle) isHubEvent() {}
func (evVehicleMove) isHubEvent()  {}
func (evDismount) isHubEvent()     {}

// placeVehicle spawns a cart on a clicked rail or a boat on/next to water.
func (h *hub) placeVehicle(players map[int32]*tracked, t *tracked, e evPlaceVehicle) {
	etype, ok := vehicleItems[e.item]
	if !ok {
		return
	}
	x, y, z := float64(e.x)+0.5, float64(e.y), float64(e.z)+0.5
	ground := h.world.At(e.x, e.y, e.z)
	if etype == entityMinecart {
		if !isAnyRail(ground) {
			return // carts only go on rails
		}
		y += 0.1
	} else {
		if !worldgen.IsWater(ground) { // clicked a shore block: try the cell above
			if !worldgen.IsWater(h.world.At(e.x, e.y+1, e.z)) &&
				h.world.At(e.x, e.y+1, e.z) != worldgen.Air {
				return
			}
			y += 1
		}
	}
	v := &vehicle{eid: h.allocEID(), etype: etype, x: x, y: y, z: z, sx: x, sy: y, sz: z}
	binary.BigEndian.PutUint32(v.uuid[12:], uint32(v.eid))
	h.vehicles[v.eid] = v
	h.toNearbyEv(players, 0, x, z, entAdd(v.eid, etype, v.uuid, x, y, z, 0, 0))
	if t.gamemode == gmSurvival && t.inv != nil && e.slot >= 0 && e.slot < 9 {
		if sl := &t.inv.slots[e.slot]; sl.count > 0 {
			sl.count--
			if sl.count == 0 {
				sl.item = 0
			}
			h.sendSlot(t, int(e.slot))
		}
	}
}

// mountVehicle seats a player (interact with an empty vehicle).
func (h *hub) mountVehicle(players map[int32]*tracked, t *tracked, v *vehicle) {
	if v.rider != 0 || dist3(t.x, t.y, t.z, v.x, v.y, v.z) > maxMeleeReach+1 {
		return
	}
	v.rider = t.p.eid
	h.toNearbyEv(players, v.dim, v.x, v.z, passengersBody(v.eid, v.rider))
}

// dismount stands the rider up beside the vehicle.
func (h *hub) dismount(players map[int32]*tracked, t *tracked) {
	for _, v := range h.vehicles {
		if v.rider != t.p.eid {
			continue
		}
		v.rider = 0
		h.toNearbyEv(players, v.dim, v.x, v.z, passengersBody(v.eid))
		t.x, t.y, t.z = v.x+0.9, v.y+0.6, v.z
		t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
		return
	}
}

// breakVehicle pops it back into an item (any punch — vanilla-lite).
func (h *hub) breakVehicle(players map[int32]*tracked, v *vehicle) {
	if v.rider != 0 {
		if t := players[v.rider]; t != nil {
			h.dismount(players, t)
		}
		v.rider = 0
	}
	delete(h.vehicles, v.eid)
	h.toNearbyEv(players, v.dim, v.x, v.z, entGone(v.eid))
	h.spawnItem(players, vehicleItemFor(v.etype), 1, v.x, v.y, v.z)
	h.playSound(players, "minecraft:entity.minecart.riding", sndNeutral, v.x, v.y, v.z, 0.4, 1.6)
}

// applyVehicleMove is the authority gate on a rider's client-simulated
// vehicle: sane delta or the rider gets snapped back.
func (h *hub) applyVehicleMove(players map[int32]*tracked, t *tracked, e evVehicleMove) {
	var v *vehicle
	for _, c := range h.vehicles {
		if c.rider == e.eid {
			v = c
			break
		}
	}
	if v == nil {
		return
	}
	if math.IsNaN(e.x) || math.IsNaN(e.y) || math.IsNaN(e.z) ||
		dist3(v.x, v.y, v.z, e.x, e.y, e.z) > vehicleMoveCap {
		t.p.trySendEv(vehicleMoveBody(v.x, v.y, v.z, v.yaw)) // snap back
		return
	}
	v.x, v.y, v.z, v.yaw = e.x, e.y, e.z, e.yaw
	// The rider rides along: hub position drives chunk streaming + interest.
	t.x, t.y, t.z = e.x, e.y+0.6, e.z
	if e.x != v.sx || e.y != v.sy || e.z != v.sz {
		move := entMove(v.eid, v.x, v.y, v.z, v.yaw, 0, true)
		cx, cz := chunkFloor(v.x), chunkFloor(v.z)
		for _, o := range players {
			if o.p.eid != e.eid && abs(chunkFloor(o.x)-cx) <= viewRadius && abs(chunkFloor(o.z)-cz) <= viewRadius {
				o.p.trySendEv(move)
			}
		}
		v.sx, v.sy, v.sz = e.x, e.y, e.z
	}
}

// updateVehicles: detector rails press while a cart (or its rider) sits on
// them, and release after.
func (h *hub) updateVehicles(players map[int32]*tracked) {
	occupied := map[blockPos]bool{}
	for _, v := range h.vehicles {
		pos := blockPos{floorInt(v.x), floorInt(v.y + 0.01), floorInt(v.z)}
		if isDetectorRail(h.world.At(pos.x, pos.y, pos.z)) {
			occupied[pos] = true
			if !railPowered(h.world.At(pos.x, pos.y, pos.z)) {
				s := h.world.At(pos.x, pos.y, pos.z)
				h.setBlock(players, pos, railWith(s, railShape(s), true))
				h.scheduleAround(pos, 1)
				h.detectorsOn[pos] = true
			}
		}
	}
	for pos := range h.detectorsOn {
		if occupied[pos] {
			continue
		}
		delete(h.detectorsOn, pos)
		if s := h.world.At(pos.x, pos.y, pos.z); isDetectorRail(s) && railPowered(s) {
			h.setBlock(players, pos, railWith(s, railShape(s), false))
			h.scheduleAround(pos, 1)
		}
	}
}

// sendVehiclesTo shows existing vehicles to a joining player.
func (h *hub) sendVehiclesTo(t *tracked) {
	for _, v := range h.vehicles {
		t.p.trySendEv(entAdd(v.eid, v.etype, v.uuid, v.x, v.y, v.z, v.yaw, 0))
		if v.rider != 0 {
			t.p.trySendEv(passengersBody(v.eid, v.rider))
		}
	}
}

func passengersBody(vehicleEID int32, riders ...int32) attachproto.Passengers {
	return attachproto.Passengers{Vehicle: vehicleEID, Riders: append([]int32{}, riders...)}
}

func vehicleMoveBody(x, y, z float64, yaw float32) attachproto.VehicleMove {
	return attachproto.VehicleMove{X: x, Y: y, Z: z, Yaw: yaw}
}
