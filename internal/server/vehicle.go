package server

import (
	"encoding/binary"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"math"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
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
	// Vanilla's #boats (chest boats and rafts included): each item places the
	// entity of its own name.
	for _, name := range worldgen.ItemTag("boats") {
		m[name] = entityID(name)
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
	eid     int32
	dim     int // the dimension it floats or rolls in
	uuid    [16]byte
	etype   int
	x, y, z float64
	yaw     float32
	rider   int32 // player eid, 0 when empty
	// movedAt is the tick a rider last moved the boat horizontally, and
	// moveDX/DZ the step it took (Entity.lastKnownSpeed): what a dolphin
	// following the boat reads (FollowPlayerRiddenEntityGoal).
	movedAt        uint64
	moveDX, moveDZ float64
	mobRider       int32      // a mob aboard: scooped up by a rolling cart (minecart.go) or a boat (boatseats.go), 0 when none
	mobFirst       bool       // the mob boarded before the player: it has the front seat
	sx, sy, sz     float64    // last broadcast position (relative-move baseline)
	syaw           float32    // last broadcast facing
	chest          *chest     // a chest boat's 27 slots (nil for the rest)
	paddle         [2]bool    // a boat's rowing paddles, left and right (boatpaddle.go)
	paddlePos      [2]float32 // each paddle's turn so far
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
	igniter      int32   // TNT cart: who lit it (ignitionSource's causing entity), 0 for none
	// A structure's chest cart (a mineshaft's): the loot table it still
	// holds unrolled, and the cell it was placed on (the roll's seed) —
	// RandomizableContainer's lootTable, unpacked on first use.
	loot    string
	lootPos blockPos
	// VehicleEntity's synced hurt state: hurtTime counts the wobble down
	// from 10, hurtDirFlip is the side it rocks to (DATA_ID_HURTDIR starts
	// at 1 and flips on every blow), damage is what has built up (a blow
	// adds ten times its worth, one drains away a tick, past 40 it breaks).
	hurtTime    int
	hurtDirFlip bool
	damage      float64
	// A boat's fire (remainingFireTicks) and the synced burning flag; a
	// minecart never burns down its fire (vehicle_damage.go).
	fireTicks int
	burning   bool
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
	return h.spawnVehicleFacing(players, dim, etype, bx, by, bz, 0)
}

// spawnVehicleFacing is spawnVehicleAt with the facing a boat is given:
// BoatItem.use turns it to the placer's yaw, a dispenser to the facing it
// shoots along (Direction.toYRot). A cart takes its heading from the rails.
func (h *hub) spawnVehicleFacing(players map[int32]*tracked, dim, etype, bx, by, bz int, yaw float32) bool {
	x, y, z, ok := h.vehicleSpawnPos(dim, etype, bx, by, bz)
	if !ok {
		return false
	}
	h.addVehicle(players, dim, etype, x, y, z, yaw)
	return true
}

// addVehicle makes a cart or boat exactly at (x, y, z).
func (h *hub) addVehicle(players map[int32]*tracked, dim, etype int, x, y, z float64, yaw float32) *vehicle {
	v := &vehicle{eid: h.allocEID(), dim: dim, etype: etype, x: x, y: y, z: z, sx: x, sy: y, sz: z}
	if !cartTypes[etype] {
		v.yaw, v.syaw, v.yawO = yaw, yaw, yaw
	}
	binary.BigEndian.PutUint32(v.uuid[12:], uint32(v.eid))
	if chestBoatTypes[etype] {
		v.chest = &chest{}
	}
	initCartKind(v)
	h.vehicles[v.eid] = v
	h.toNearbyEv(players, dim, x, z, entAdd(v.eid, etype, v.uuid, x, y, z, v.yaw, 0))
	return v
}

// vehicleSpawnPos is where a cart or boat aimed at a cell goes: a cart on
// its rail, a boat in the water cell or on the shore cell's top.
func (h *hub) vehicleSpawnPos(dim, etype, bx, by, bz int) (x, y, z float64, ok bool) {
	w := h.worldFor(dim)
	if w == nil {
		return 0, 0, 0, false
	}
	x, y, z = float64(bx)+0.5, float64(by), float64(bz)+0.5
	ground := w.At(bx, by, bz)
	if cartTypes[etype] {
		if !isAnyRail(ground) {
			return 0, 0, 0, false // carts only go on rails
		}
		return x, y + 0.1, z, true
	}
	if !worldgen.IsWater(ground) { // shore block: try the cell above
		if !worldgen.IsWater(w.At(bx, by+1, bz)) && w.At(bx, by+1, bz) != worldgen.Air {
			return 0, 0, 0, false
		}
		y += 1
	}
	return x, y, z, true
}

// boatPlaceClear is BoatItem.use's level.noCollision(boat, box): the new
// boat's 1.375 × 0.5625 box may overlap no colliding block and no entity a
// boat collides with (AbstractBoat.canVehicleCollide: other vehicles and
// anything pushable — players and mobs; spectators never count).
func (h *hub) boatPlaceClear(players map[int32]*tracked, dim int, x, y, z float64) bool {
	const hw, ht = 1.375 / 2, 0.5625
	w := h.worldFor(dim)
	for bx := floorInt(x - hw); bx <= floorInt(x+hw); bx++ {
		for by := floorInt(y); by <= floorInt(y+ht); by++ {
			for bz := floorInt(z - hw); bz <= floorInt(z+hw); bz++ {
				if worldgen.Collides(w.At(bx, by, bz)) {
					return false
				}
			}
		}
	}
	hits := func(ex, ey, ez, ehw, eht float64) bool {
		return ex+ehw > x-hw && ex-ehw < x+hw && ez+ehw > z-hw && ez-ehw < z+hw && ey+eht > y && ey < y+ht
	}
	for _, t := range players {
		if t.dim == dim && !t.dead && t.gamemode != gmSpectator && hits(t.x, t.y, t.z, t.halfWidth(), 1.8*t.scale()) {
			return false
		}
	}
	for _, m := range h.mobs {
		if b := m.box(); m.dim == dim && m.dying == 0 && hits(m.x, m.y, m.z, b.w/2, b.h) {
			return false
		}
	}
	for _, v := range h.vehicles {
		if vw, vh := v.box(); v.dim == dim && hits(v.x, v.y, v.z, vw/2, vh) {
			return false
		}
	}
	return true
}

// evPlaceVehicleLook is a boat used with nothing clicked: the hub walks the
// look ray to find where it goes.
type evPlaceVehicleLook struct {
	eid  int32
	item int32
	slot int32
}

func (evPlaceVehicleLook) isHubEvent() {}

// boatPlaceReach is the interaction range Item.getPlayerPOVHitResult clips a
// boat's placement ray to.
const boatPlaceReach = 4.5

// placeVehicleFromLook is BoatItem.use: the client sends a plain use when the
// crosshair is on a fluid rather than a block, which is exactly the case that
// matters — putting a boat on open water. The ray takes the first cell that
// stops it, fluid or solid, and the boat goes there.
func (h *hub) placeVehicleFromLook(players map[int32]*tracked, t *tracked, item int32, slot int32) {
	if _, ok := vehicleItems[item]; !ok || t.dead {
		return
	}
	pos, found := h.lookRay(t, boatPlaceReach, func(_ blockPos, st uint32) bool {
		return worldgen.IsWater(st) || worldgen.IsLava(st) || rayStopsAt(st)
	})
	if !found {
		return
	}
	// Straight through the clicked-block path, so the placement, the vibration
	// and the cost of the item stay in one place.
	h.placeVehicle(players, t, evPlaceVehicle{eid: t.p.eid, item: item,
		x: pos.x, y: pos.y, z: pos.z, slot: slot})
}

// placeVehicle spawns a cart on a clicked rail or a boat on/next to water.
func (h *hub) placeVehicle(players map[int32]*tracked, t *tracked, e evPlaceVehicle) {
	etype, ok := vehicleItems[e.item]
	if !ok {
		return
	}
	if !cartTypes[etype] { // BoatItem.use: FAIL unless level.noCollision(boat, its box)
		if x, y, z, ok := h.vehicleSpawnPos(t.dim, etype, e.x, e.y, e.z); !ok || !h.boatPlaceClear(players, t.dim, x, y, z) {
			return
		}
	}
	if !h.spawnVehicleFacing(players, t.dim, etype, e.x, e.y, e.z, t.yaw) {
		return
	}
	h.vib(t.dim, freqEntityPlace, e.x, e.y, e.z, t.p.eid) // ENTITY_PLACE
	// Either hand: a boat placed from the off hand is paid for too (it came
	// back free until 2026-09-24).
	if isSurvival(t.gamemode) && t.inv != nil && ((e.slot >= 0 && e.slot < 9) || e.slot == offhandSlot) {
		if sl := t.handStack(int(e.slot)); sl != nil && sl.count > 0 {
			sl.count--
			if sl.count == 0 {
				sl.item = 0
			}
			h.sendHandSlot(t, int(e.slot))
		}
	}
}

// mountVehicle seats a player (interact with an empty vehicle).
func (h *hub) mountVehicle(players map[int32]*tracked, t *tracked, v *vehicle) {
	if !v.rideable() || v.rider != 0 || v.aboard() >= v.seats() || dist3(t.x, t.y, t.z, v.x, v.y, v.z) > maxMeleeReach+1 {
		return
	}
	v.rider = t.p.eid
	t.ridingEID = v.eid
	h.vibAt(v.dim, freqMount, v.x, v.y, v.z, t.p.eid)
	h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.passengers()...))
	h.startedRiding(players, v)
}

// dismount stands the rider up beside the vehicle.
func (h *hub) dismount(players map[int32]*tracked, t *tracked) {
	for _, v := range h.vehicles {
		if v.rider != t.p.eid {
			continue
		}
		v.rider = 0
		t.ridingEID = 0
		v.mobFirst = v.mobRider != 0 // whoever stays aboard has the front seat
		h.vibAt(v.dim, freqDismount, v.x, v.y, v.z, t.p.eid)
		h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.passengers()...))
		t.x, t.y, t.z = v.x+0.9, v.y+0.6, v.z
		t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
		return
	}
}

// Vehicle hurt metadata (VehicleEntity): Entity's 8 fields, then these three.
const (
	vehMetaHurt    = 8  // DATA_ID_HURT: INT, the wobble's ticks left
	vehMetaHurtDir = 9  // DATA_ID_HURTDIR: INT, ±1
	vehMetaDamage  = 10 // DATA_ID_DAMAGE: FLOAT
	metaTypeFloat  = 3  // EntityDataSerializers.FLOAT
	vehHurtTicks   = 10 // setHurtTime(10)
	vehBreakDamage = 40 // destroyed once the built-up damage passes this
)

func (v *vehicle) hurtDir() int32 {
	if v.hurtDirFlip {
		return -1
	}
	return 1
}

// vehicleHurtMeta is the three synced hurt fields: the client rocks the
// model from them (and counts them down itself, as its own tick does).
func vehicleHurtMeta(v *vehicle) []byte {
	b := protocol.AppendVarInt(nil, v.eid)
	b = protocol.AppendU8(b, vehMetaHurt)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(v.hurtTime))
	b = protocol.AppendU8(b, vehMetaHurtDir)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, v.hurtDir())
	b = protocol.AppendU8(b, vehMetaDamage)
	b = protocol.AppendVarInt(b, metaTypeFloat)
	b = protocol.AppendF32(b, float32(v.damage))
	return protocol.AppendU8(b, itemMetaEnd)
}

// hurtVehicle is a player's blow on a boat or minecart (Player.attack into
// VehicleEntity.hurtServer): the vehicle rocks, the blow's worth ×10 builds
// up as damage, and it breaks only once that passes 40 — a bare fist takes
// several quick punches, since a point of damage drains away every tick. A
// creative player's blow removes it on the spot with nothing dropped.
func (h *hub) hurtVehicle(players map[int32]*tracked, t *tracked, v *vehicle) {
	if t == nil || t.dim != v.dim {
		return
	}
	dx, dy, dz := t.x-v.x, t.y-v.y, t.z-v.z
	if dx*dx+dy*dy+dz*dz > maxMeleeReach*maxMeleeReach {
		return
	}
	sw := h.meleeSwing(t, 0) // Player.attack: no crit on a non-living target
	if sw.raw <= 0 {
		return
	}
	h.damageVehicle(players, v, vehHit{dmg: sw.raw, dt: dtPlayerAttack, by: t, causer: t.p.eid})
}

// tickVehicleHurt drains the hurt state a tick (AbstractBoat/AbstractMinecart
// .tick). The client runs the same count-down on its copy, so nothing is sent.
func (v *vehicle) tickVehicleHurt() {
	if v.hurtTime > 0 {
		v.hurtTime--
	}
	if v.damage > 0 {
		v.damage--
	}
}

// discardVehicle is a creative player's blow (Entity.discard): the vehicle
// goes without its item, though a container's cargo still spills
// (remove(DISCARDED) drops the contents).
func (h *hub) discardVehicle(players map[int32]*tracked, v *vehicle) {
	if v.rider != 0 {
		if t := players[v.rider]; t != nil {
			h.dismount(players, t)
		}
		v.rider = 0
	}
	h.releaseCartMob(players, v)
	delete(h.vehicles, v.eid)
	h.entityGone(players, v.dim, v.eid)
	h.spillVehicleCargo(players, v)
}

func (h *hub) spillVehicleCargo(players map[int32]*tracked, v *vehicle) {
	h.unpackCartLoot(v)                       // a structure cart broken unopened spills its loot
	if slots := v.cartSlots(); slots != nil { // ChestBoat.destroy: the cargo spills
		for _, st := range slots {
			if st.item == 0 || st.count == 0 {
				continue
			}
			if it := h.spawnItemIn(players, v.dim, st.item, st.count, v.x, v.y, v.z); it != nil {
				it.setFrom(st)
				h.refreshItemMeta(players, it)
			}
		}
	}
}

// breakVehicle destroys it (VehicleEntity.destroy): it pops back into its
// item and a container's cargo spills.
func (h *hub) breakVehicle(players map[int32]*tracked, v *vehicle) {
	if v.etype == entityTntMinecart && v.vx*v.vx+v.vz*v.vz >= 0.01 {
		// MinecartTNT.destroy: a moving TNT cart that is broken lights instead.
		h.lightBrokenCart(players, v, 0)
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
	h.entityGone(players, v.dim, v.eid)
	if !h.rules.EntityDrops {
		return // gamerule entity_drops: nothing is left behind
	}
	h.spawnItemIn(players, v.dim, vehicleItemFor(v.etype), 1, v.x, v.y, v.z)
	h.spillVehicleCargo(players, v)
	h.playSoundDim(players, v.dim, "minecraft:entity.minecart.riding", sndNeutral, v.x, v.y, v.z, 0.4, 1.6)
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
	h.noteKnownMove(t, e.x-v.x, e.y-v.y, e.z-v.z)        // the boat's movement is the rider's
	if dx, dz := e.x-v.x, e.z-v.z; dx*dx+dz*dz > 1e-10 { // hasMovedHorizontallyRecently
		v.movedAt, v.moveDX, v.moveDZ = h.tick.Load(), dx, dz
	}
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
		v.tickVehicleHurt()
		if !h.vehicleHazards(players, v) {
			continue // burnt up in lava or fire
		}
		if !v.isBoat() {
			h.tickMinecart(players, v)
		} else {
			h.tickBoatPaddles(players, v)
			h.boatPickup(players, v)
			h.boatCrushesLilyPads(players, v)
		}
	}
	// DetectorRailBlock.entityInside: a cart on an unpowered detector rail
	// checks it at once; a pressed one re-checks on its own 20-tick tick.
	for _, v := range h.vehicles {
		if v.isBoat() {
			continue
		}
		sp := simPos{v.dim, blockPos{floorInt(v.x), floorInt(v.y + 0.01), floorInt(v.z)}}
		if s := h.worldFor(v.dim).At(sp.x, sp.y, sp.z); isDetectorRail(s) && !railPowered(s) {
			h.checkDetector(players, sp)
		}
	}
	now := h.tick.Load()
	for sp, due := range h.detectorsOn {
		if now >= due {
			h.checkDetector(players, sp)
		}
	}
}

// detectorCart is DetectorRailBlock.getInteractingMinecartOfType: a cart
// whose box meets the rail's search box (the cell inset 0.2 on the four
// sides and the top) — a container cart only, when asked.
func (h *hub) detectorCart(pos simPos, container bool) *vehicle {
	x0, y0, z0 := float64(pos.x)+0.2, float64(pos.y), float64(pos.z)+0.2
	x1, y1, z1 := float64(pos.x)+0.8, float64(pos.y)+0.8, float64(pos.z)+0.8
	for _, v := range h.vehicles {
		if v.dim != pos.dim || v.isBoat() || (container && v.cartSlots() == nil) {
			continue
		}
		w, ht := v.box()
		if v.x-w/2 < x1 && v.x+w/2 > x0 && v.y < y1 && v.y+ht > y0 && v.z-w/2 < z1 && v.z+w/2 > z0 {
			return v
		}
	}
	return nil
}

// checkDetector is DetectorRailBlock.checkPressed: pressed while a cart is
// on it, the 20-tick re-check scheduled while it stays, and the comparator
// beside it told on every check (updateNeighbourForOutputSignal) so it
// reads the cart's contents as they change.
func (h *hub) checkDetector(players map[int32]*tracked, sp simPos) {
	h.inDim(sp.dim, func() {
		pos := sp.blockPos
		s := h.rsWorld().At(pos.x, pos.y, pos.z)
		if !isDetectorRail(s) {
			delete(h.detectorsOn, sp)
			return
		}
		was := railPowered(s)
		should := h.detectorCart(sp, false) != nil
		if should != was {
			h.rsSet(players, pos, railWith(s, railShape(s), should))
			h.scheduleSignalAround(players, pos)
		}
		if should {
			h.detectorsOn[sp] = h.tick.Load() + detectorCheckTicks
		} else {
			delete(h.detectorsOn, sp)
		}
		h.updateNeighbourForOutputSignal(players, pos)
	})
}

// detectorCheckTicks is DetectorRailBlock's scheduleTick(pos, this, 20).
const detectorCheckTicks = 20

// sendVehiclesTo shows existing vehicles to a joining player.
func (h *hub) sendVehiclesTo(t *tracked) {
	for _, v := range h.vehicles {
		if v.dim != t.dim {
			continue
		}
		t.p.trySendEv(entAdd(v.eid, v.etype, v.uuid, v.x, v.y, v.z, v.yaw, 0))
		// The spawn carries the synced state that is off its default
		// (ServerEntity.sendPairingData): a wobble still in progress, the
		// damage not yet drained, the side it last rocked to, a boat alight.
		if v.hurtTime > 0 || v.damage > 0 || v.hurtDirFlip {
			t.p.trySendEv(metaEv(vehicleHurtMeta(v)))
		}
		if v.burning {
			t.p.trySendEv(metaEv(fireMetadata(v.eid, true)))
		}
		if v.lit {
			t.p.trySendEv(metaEv(cartFuelMeta(v.eid, true)))
		}
		if v.paddle != [2]bool{} {
			t.p.trySendEv(metaEv(boatPaddleMeta(v)))
		}
		if v.aboard() > 0 {
			t.p.trySendEv(passengersBody(v.eid, v.passengers()...))
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
		if v.loot != "" {
			sv.Loot, sv.LootPos = v.loot, [3]int{v.lootPos.x, v.lootPos.y, v.lootPos.z}
		}
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
		v.loot, v.lootPos = sv.Loot, blockPos{sv.LootPos[0], sv.LootPos[1], sv.LootPos[2]}
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
	if name := h.unpackCartLoot(v); name != "" {
		// RandomizableContainer.unpackLootTable: the opener generates it.
		h.advance(players, t, "player_generates_container_loot", advMatch{lootTable: name})
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
	h.angerNearbyPiglins(players, t, true) // MinecartChest / AbstractChestBoat.interact: a watched container
}
