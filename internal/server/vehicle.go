package server

import (
	"encoding/binary"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
	"strings"

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
	entityMinecart = entityID("minecart")
)

// boatEntities maps each boat ITEM name to its entity type. The ids are looked
// up by NAME in the generated canonical registry, never written out: hardcoded
// ordinals silently rot when the canonical version moves (these were 1.21.5
// numbers against a 1.21.11 registry, so an oak boat spawned a marker, a spruce
// boat a sniffer and a mangrove boat a llama).
var boatEntities = func() map[string]int {
	m := map[string]int{}
	for _, wood := range []string{"oak", "spruce", "birch", "jungle", "acacia",
		"dark_oak", "cherry", "mangrove", "pale_oak", "bamboo"} {
		name, chestName := wood+"_boat", wood+"_chest_boat"
		if wood == "bamboo" {
			name, chestName = "bamboo_raft", "bamboo_chest_raft" // bamboo floats a raft, not a boat
		}
		m[wood+"_boat"] = entityID(name)
		if id, ok := entityByName[chestName]; ok { // the chest variant: item and entity share the name
			m[chestName] = id
		}
	}
	return m
}()

// chestBoatTypes are the boat/raft entities that carry a 27-slot chest.
var chestBoatTypes = func() map[int]bool {
	m := map[int]bool{}
	for name, id := range entityByName {
		if strings.HasSuffix(name, "_chest_boat") || name == "bamboo_chest_raft" {
			m[id] = true
		}
	}
	return m
}()

// vehicleItems: item id → entity type, built from the generated name table.
var vehicleItems = func() map[int32]int {
	m := map[int32]int{}
	for name, et := range map[string]int{"minecart": entityMinecart, "chest_minecart": entityChestMinecart,
		"hopper_minecart": entityHopperMinecart, "tnt_minecart": entityTntMinecart, "furnace_minecart": entityFurnaceMinecart} {
		if id, ok := itemByName[name]; ok {
			m[id] = et
		}
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
	dim        int // the dimension it floats or rolls in
	uuid       [16]byte
	etype      int
	x, y, z    float64
	yaw        float32
	rider      int32   // player eid, 0 when empty
	mobRider   int32   // a mob scooped up by a rolling cart (minecart.go), 0 when none
	sx, sy, sz float64 // last broadcast position (relative-move baseline)
	syaw       float32 // last broadcast facing
	chest      *chest  // a chest boat's 27 slots (nil for the rest)
	// Minecart motion state (the server rolls carts; see minecart.go).
	vx, vy, vz  float64
	yawO        float32 // facing at the previous tick
	flipped     bool    // model turned 180° from its motion (vanilla's flip flag)
	onGround    bool
	fallFrom    float64 // highest y since it last stood on something
	landedFall  float64 // blocks fallen on this tick's landing (0 otherwise)
	hitWall     bool    // ran into a block this tick…
	hitSpeedSqr float64 // …at this horizontal speed²
	// The special carts (minecart_special.go).
	bin          *bin    // a hopper cart's five slots
	fuel         int     // furnace cart: ticks of push left
	pushX, pushZ float64 // furnace cart: the push (set by the coal-bearer's side)
	lit          bool    // furnace cart: synced fuel flag
	fuse         int     // TNT cart: ticks to the blast (-1 = not primed)
	disabled     bool    // hopper cart: switched off by a live activator rail
}

func (v *vehicle) isBoat() bool { return !cartTypes[v.etype] }

// rideable: boats and the plain cart carry a player; the special carts do not.
func (v *vehicle) rideable() bool { return v.isBoat() || v.etype == entityMinecart }

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
type evInput struct {
	eid int32
	in  attachproto.Input
}

func (evInput) isHubEvent() {}

// relayRiderLook forwards a passenger's turned head to the viewers around
// its vehicle (the body stays where the vehicle put it).
func (h *hub) relayRiderLook(players map[int32]*tracked, t *tracked, e evMove) {
	move := entMove(e.eid, t.x, t.y, t.z, e.yaw, e.pitch, e.onGround)
	head := entHead(e.eid, e.yaw)
	cx, cz := chunkFloor(t.x), chunkFloor(t.z)
	for eid, other := range players {
		if eid == e.eid || other.dim != t.dim || abs(chunkFloor(other.x)-cx) > viewRadius || abs(chunkFloor(other.z)-cz) > viewRadius {
			continue
		}
		other.p.trySendEv(move)
		other.p.trySendEv(head)
	}
}

func (evPlaceVehicle) isHubEvent() {}
func (evVehicleMove) isHubEvent()  {}
func (evDismount) isHubEvent()     {}

// spawnVehicleAt places a cart on a rail / a boat on-or-above water at a block
// cell (player-independent core, also used by dispensers). Returns whether it
// spawned.
func (h *hub) spawnVehicleAt(players map[int32]*tracked, dim, etype, bx, by, bz int) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	x, y, z := float64(bx)+0.5, float64(by), float64(bz)+0.5
	ground := w.At(bx, by, bz)
	if cartTypes[etype] {
		if !isAnyRail(ground) {
			return false // carts only go on rails
		}
		y += 0.1
	} else {
		if !worldgen.IsWater(ground) { // shore block: try the cell above
			if !worldgen.IsWater(w.At(bx, by+1, bz)) && w.At(bx, by+1, bz) != worldgen.Air {
				return false
			}
			y += 1
		}
	}
	v := &vehicle{eid: h.allocEID(), dim: dim, etype: etype, x: x, y: y, z: z, sx: x, sy: y, sz: z}
	binary.BigEndian.PutUint32(v.uuid[12:], uint32(v.eid))
	if chestBoatTypes[etype] {
		v.chest = &chest{}
	}
	initCartKind(v)
	h.vehicles[v.eid] = v
	h.toNearbyEv(players, dim, x, z, entAdd(v.eid, etype, v.uuid, x, y, z, 0, 0))
	return true
}

// placeVehicle spawns a cart on a clicked rail or a boat on/next to water.
func (h *hub) placeVehicle(players map[int32]*tracked, t *tracked, e evPlaceVehicle) {
	etype, ok := vehicleItems[e.item]
	if !ok || !h.spawnVehicleAt(players, t.dim, etype, e.x, e.y, e.z) {
		return
	}
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
	if !v.rideable() || v.rider != 0 || v.mobRider != 0 || dist3(t.x, t.y, t.z, v.x, v.y, v.z) > maxMeleeReach+1 {
		return
	}
	v.rider = t.p.eid
	t.ridingEID = v.eid
	h.toNearbyEv(players, v.dim, v.x, v.z, passengersBody(v.eid, v.rider))
}

// dismount stands the rider up beside the vehicle.
func (h *hub) dismount(players map[int32]*tracked, t *tracked) {
	for _, v := range h.vehicles {
		if v.rider != t.p.eid {
			continue
		}
		v.rider = 0
		t.ridingEID = 0
		h.toNearbyEv(players, v.dim, v.x, v.z, passengersBody(v.eid))
		t.x, t.y, t.z = v.x+0.9, v.y+0.6, v.z
		t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
		return
	}
}

// breakVehicle pops it back into an item (any punch — vanilla-lite).
func (h *hub) breakVehicle(players map[int32]*tracked, v *vehicle) {
	if v.etype == entityTntMinecart && v.vx*v.vx+v.vz*v.vz >= 0.01 {
		// MinecartTNT.destroy: a moving TNT cart that is broken lights instead.
		h.primeCart(players, v, h.rng.Intn(20)+h.rng.Intn(20))
		return
	}
	if v.rider != 0 {
		if t := players[v.rider]; t != nil {
			h.dismount(players, t)
		}
		v.rider = 0
	}
	h.releaseCartMob(players, v)
	delete(h.vehicles, v.eid)
	h.toNearbyEv(players, v.dim, v.x, v.z, entGone(v.eid))
	h.spawnItem(players, vehicleItemFor(v.etype), 1, v.x, v.y, v.z)
	if slots := v.cartSlots(); slots != nil { // ChestBoat.destroy: the cargo spills
		for _, st := range slots {
			if st.item == 0 || st.count == 0 {
				continue
			}
			if it := h.spawnItemIn(players, v.dim, st.item, st.count, v.x, v.y, v.z); it != nil {
				it.dmg, it.ench, it.mapID, it.pats = st.dmg, st.ench, st.mapID, st.pats
				it.trimMat, it.trimPat, it.bookID, it.boxID, it.hiveID = st.trimMat, st.trimPat, st.bookID, st.boxID, st.hiveID
				it.bundleID, it.potion, it.repairCost, it.instrument, it.name, it.lode = st.bundleID, st.potion, st.repairCost, st.instrument, st.name, st.lode
				h.refreshItemMeta(players, it)
			}
		}
	}
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
	if v == nil || !v.isBoat() {
		return // a minecart is server-driven: its rider's client has no say
	}
	if math.IsNaN(e.x) || math.IsNaN(e.y) || math.IsNaN(e.z) ||
		dist3(v.x, v.y, v.z, e.x, e.y, e.z) > vehicleMoveCap {
		t.p.trySendEv(vehicleMoveBody(v.x, v.y, v.z, v.yaw)) // snap back
		return
	}
	h.vehicleStats(t, v, math.Hypot(e.x-v.x, e.z-v.z))
	v.x, v.y, v.z, v.yaw = e.x, e.y, e.z, e.yaw
	// The rider rides along: hub position drives chunk streaming + interest.
	t.x, t.y, t.z = e.x, e.y+0.6, e.z
	if e.x != v.sx || e.y != v.sy || e.z != v.sz {
		move := entMove(v.eid, v.x, v.y, v.z, v.yaw, 0, true)
		cx, cz := chunkFloor(v.x), chunkFloor(v.z)
		for _, o := range players {
			if o.p.eid != e.eid && o.dim == v.dim && abs(chunkFloor(o.x)-cx) <= viewRadius && abs(chunkFloor(o.z)-cz) <= viewRadius {
				o.p.trySendEv(move)
			}
		}
		v.sx, v.sy, v.sz = e.x, e.y, e.z
	}
}

// updateVehicles: detector rails press while a cart (or its rider) sits on
// them, and release after.
func (h *hub) updateVehicles(players map[int32]*tracked) {
	for _, v := range h.vehicles {
		if !v.isBoat() {
			h.tickMinecart(players, v)
		}
	}
	occupied := map[blockPos]bool{}
	for _, v := range h.vehicles {
		if v.dim != dimOverworld {
			continue // redstone (detector rails) is simulated in the overworld only
		}
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
		if v.dim != t.dim {
			continue
		}
		t.p.trySendEv(entAdd(v.eid, v.etype, v.uuid, v.x, v.y, v.z, v.yaw, 0))
		if v.lit {
			t.p.trySendEv(metaEv(cartFuelMeta(v.eid, true)))
		}
		if v.mobRider != 0 {
			t.p.trySendEv(passengersBody(v.eid, v.mobRider))
		}
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

// snapshotVehicles / restoreVehicles: boats and carts persist across restarts
// like dropped items do (vehicles used to vanish with the pod).
func (h *hub) snapshotVehicles() []savedVehicle {
	out := make([]savedVehicle, 0, len(h.vehicles))
	for _, v := range h.vehicles {
		name := entityNameByID[v.etype]
		if name == "" {
			continue
		}
		sv := savedVehicle{Dim: v.dim, Etype: name, X: v.x, Y: v.y, Z: v.z, Yaw: v.yaw}
		for _, st := range v.cartSlots() {
			sv.Chest = append(sv.Chest, packStack(st))
		}
		sv.Fuel, sv.PushX, sv.PushZ, sv.Disabled = v.fuel, v.pushX, v.pushZ, v.disabled
		if v.etype == entityTntMinecart && v.fuse >= 0 {
			sv.Fuse = v.fuse + 1
		}
		out = append(out, sv)
	}
	return out
}

func (h *hub) restoreVehicles(saved []savedVehicle) {
	for _, sv := range saved {
		et, ok := entityByName[sv.Etype]
		if !ok {
			continue
		}
		v := &vehicle{eid: h.allocEID(), dim: sv.Dim, etype: et, x: sv.X, y: sv.Y, z: sv.Z, yaw: sv.Yaw,
			sx: sv.X, sy: sv.Y, sz: sv.Z}
		binary.BigEndian.PutUint32(v.uuid[12:], uint32(v.eid))
		if chestBoatTypes[et] {
			v.chest = &chest{}
		}
		initCartKind(v)
		if slots := v.cartSlots(); slots != nil {
			for i, r := range sv.Chest {
				if i < len(slots) {
					slots[i] = unpackStack(r)
				}
			}
		}
		v.fuel, v.pushX, v.pushZ, v.disabled = sv.Fuel, sv.PushX, sv.PushZ, sv.Disabled
		if sv.Fuse > 0 {
			v.fuse = sv.Fuse - 1
		}
		h.vehicles[v.eid] = v // boot-time: shown to players by the join pass (sendVehiclesTo)
	}
}

// openVehicleChest is ChestBoat.openCustomInventoryScreen: a sneaking click
// on a chest boat opens its cargo in the ordinary chest window (the slots
// resolve through viewChest, exactly as a placed chest's do).
func (h *hub) openVehicleChest(players map[int32]*tracked, t *tracked, v *vehicle) {
	if t.inv == nil || v.chest == nil {
		return
	}
	h.releaseContainerView(t)
	h.reclaimCraft(nil, t)
	h.nextWin++
	if h.nextWin > 100 {
		h.nextWin = 1
	}
	t.winID, t.winPos, t.winKind, t.viewChest = h.nextWin, simPos{}, winChest, v.chest
	title := "Chest Boat"
	if !v.isBoat() {
		title = "Minecart with Chest"
	}
	t.p.trySendEv(attachproto.WindowOpen{ID: int32(t.winID), Menu: int32(menuGeneric9x3), Title: title})
	h.sendChestWindow(t, v.chest)
}
