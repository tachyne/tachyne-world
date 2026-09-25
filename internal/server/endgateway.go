package server

import (
	"math"
	"math/rand"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// End gateways: the way out to the outer islands, and the way back.
//
// Twenty of them appear in a ring around the main island when the dragon
// falls. The first time one is used it looks about a thousand blocks out
// along its bearing for the near edge of the outer islands, hangs a gateway
// ten blocks over the island's highest point, and the two lead to each
// other from then on (EnderDragonFight.spawnNewGateway, EndGatewayFeature,
// TheEndGatewayBlockEntity). With no island to find it makes a small one.

const (
	endGatewayCount    = 20 // GATEWAY_COUNT
	endGatewayRing     = 96 // ring radius around the main island
	endGatewayY        = 75 // the height they hang at
	endGatewayCast     = 1024.0
	endGatewayCooldown = 40 // ticks before a gateway will take you again
)

var endGatewayState = worldgen.BlockBase("end_gateway")

// endGatewayRingPos is vanilla's placement for gateway i.
func endGatewayRingPos(i int) blockPos {
	ang := 2.0 * (-math.Pi + (math.Pi/float64(endGatewayCount))*float64(i))
	return blockPos{
		x: int(math.Floor(endGatewayRing * math.Cos(ang))),
		y: endGatewayY,
		z: int(math.Floor(endGatewayRing * math.Sin(ang))),
	}
}

// endGatewayOrder is the order the ring opens in: vanilla shuffles the
// twenty positions once per world (EndDragonFight's gateways list, from the
// world seed) and takes one off the end per kill.
func (h *hub) endGatewayOrder() []int {
	return rand.New(rand.NewSource(h.world.Seed() ^ 0x3E6DA7E)).Perm(endGatewayCount)
}

// endGatewaysOpen counts the ring's gateways already standing.
func (h *hub) endGatewaysOpen() int {
	n := 0
	for i := 0; i < endGatewayCount; i++ {
		if p := endGatewayRingPos(i); h.end.At(p.x, p.y, p.z) == endGatewayState {
			n++
		}
	}
	return n
}

// spawnNextEndGateway is EndDragonFight.spawnNewGateway: one more gateway
// per dragon kill, taken from the end of the shuffled list, until all
// twenty stand. The world itself records which are open.
func (h *hub) spawnNextEndGateway(players map[int32]*tracked) {
	order := h.endGatewayOrder()
	for k := len(order) - 1; k >= 0; k-- {
		p := endGatewayRingPos(order[k])
		if h.end.At(p.x, p.y, p.z) != endGatewayState {
			h.buildEndGateway(players, p)
			return
		}
	}
}

// buildEndGateway places one gateway inside its bedrock frame: a 3x5x3 box
// whose middle layer is hollow (that is the doorway you walk into), with
// bedrock down the x and z centre lines and caps two above and below.
func (h *hub) buildEndGateway(players map[int32]*tracked, pos blockPos) {
	for dy := -2; dy <= 2; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				cx, cy, cz := dx == 0, dy == 0, dz == 0
				cap := dy == -2 || dy == 2
				var state uint32
				switch {
				case cx && cy && cz:
					state = endGatewayState
				case cy:
					state = worldgen.Air
				case cap && cx && cz:
					state = worldgen.Bedrock
				case (cx || cz) && !cap:
					state = worldgen.Bedrock
				default:
					state = worldgen.Air
				}
				h.setBlockIn(players, 2, blockPos{pos.x + dx, pos.y + dy, pos.z + dz}, state)
			}
		}
	}
}

// gatewayExit is one gateway's TheEndGatewayBlockEntity exit: where it
// sends you, and whether exactly there (exact_teleport) or onto the tallest
// ground beside it. It rides settings.json with the rest of the End fight.
type gatewayExit struct {
	From  [3]int `json:"from"`
	To    [3]int `json:"to"`
	Exact bool   `json:"exact,omitempty"`
}

func (h *hub) gatewayExitOf(pos blockPos) (blockPos, bool, bool) {
	for _, g := range h.rules.EndGateways {
		if g.From == [3]int{pos.x, pos.y, pos.z} {
			return blockPos{g.To[0], g.To[1], g.To[2]}, g.Exact, true
		}
	}
	return blockPos{}, false, false
}

func (h *hub) setGatewayExit(from, to blockPos, exact bool) {
	key := [3]int{from.x, from.y, from.z}
	for i, g := range h.rules.EndGateways {
		if g.From == key {
			h.rules.EndGateways = append(h.rules.EndGateways[:i], h.rules.EndGateways[i+1:]...)
			break
		}
	}
	h.rules.EndGateways = append(h.rules.EndGateways, gatewayExit{From: key, To: [3]int{to.x, to.y, to.z}, Exact: exact})
	h.saveRules()
}

// gatewayDestination is TheEndGatewayBlockEntity.getPortalPosition: the
// first time a gateway is used it finds (or makes) an island along its
// bearing, builds a gateway ten blocks over the island's highest point that
// leads straight back, and remembers it; after that both lead to each
// other. You land on the tallest ground beside the far gateway.
func (h *hub) gatewayDestination(players map[int32]*tracked, entry blockPos) blockPos {
	exit, exact, ok := h.gatewayExitOf(entry)
	if !ok {
		if math.Hypot(float64(entry.x), float64(entry.z)) >= endGatewayRing*2 {
			// A far gateway built before gateways remembered their pair:
			// it leads to the ring gateway on its bearing.
			exit = nearestRingGateway(entry)
		} else {
			exit = h.findOrCreateValidTeleportPos(players, entry)
			exit.y += 10
			h.buildEndGateway(players, exit)
			h.setGatewayExit(exit, entry, false) // EndGatewayFeature.knownExit(entry, false)
		}
		h.setGatewayExit(entry, exit, false)
	}
	if exact {
		return exit
	}
	// findExitPosition: the tallest ground (bedrock aside) within five
	// blocks of the spot two over the gateway, and you stand on top.
	pos := h.findTallestBlock(blockPos{exit.x, exit.y + 2, exit.z}, 5, false)
	pos.y++
	return pos
}

// nearestRingGateway is the ring gateway whose bearing is closest to pos's.
func nearestRingGateway(pos blockPos) blockPos {
	best, bestD := endGatewayRingPos(0), math.Inf(1)
	ang := math.Atan2(float64(pos.z), float64(pos.x))
	for i := 0; i < endGatewayCount; i++ {
		r := endGatewayRingPos(i)
		d := math.Abs(math.Remainder(math.Atan2(float64(r.z), float64(r.x))-ang, 2*math.Pi))
		if d < bestD {
			best, bestD = r, d
		}
	}
	return best
}

// findOrCreateValidTeleportPos: the island chunk along the bearing, the end
// stone in it nearest the world's centre with two open blocks over it — or,
// where there is none, a small island made at y 75 — then the tallest block
// within sixteen of that.
func (h *hub) findOrCreateValidTeleportPos(players map[int32]*tracked, entry blockPos) blockPos {
	tx, tz := h.exitPortalXZTentative(entry)
	cx, cz := int32(math.Floor(tx/16)), int32(math.Floor(tz/16))
	spot, ok := h.validSpawnInChunk(cx, cz)
	if !ok {
		spot = blockPos{int(math.Floor(tx + 0.5)), endGatewayY, int(math.Floor(tz + 0.5))}
		h.placeEndIsland(players, spot)
	}
	return h.findTallestBlock(spot, 16, true)
}

// exitPortalXZTentative is findExitPortalXZPosTentative: 1024 blocks out
// along the gateway's bearing, back in sixteen-block steps past any chunk
// with blocks in it, then out again past empty ones — at most sixteen steps
// each way — to land on the near edge of the island band.
func (h *hub) exitPortalXZTentative(entry blockPos) (float64, float64) {
	d := math.Hypot(float64(entry.x), float64(entry.z))
	if d < 1e-6 {
		d = 1
	}
	dx, dz := float64(entry.x)/d, float64(entry.z)/d
	x, z := dx*endGatewayCast, dz*endGatewayCast
	for i := 16; !h.endChunkEmpty(x, z) && i > 0; i-- {
		x, z = x-dx*16, z-dz*16
	}
	for i := 16; h.endChunkEmpty(x, z) && i > 0; i-- {
		x, z = x+dx*16, z+dz*16
	}
	return x, z
}

// endChunkEmpty is isChunkEmpty: not one block in the chunk column.
func (h *hub) endChunkEmpty(x, z float64) bool {
	ch := h.end.Chunk(int32(math.Floor(x/16)), int32(math.Floor(z/16)))
	for s := range ch.Sections {
		for _, st := range ch.Sections[s] {
			if st != worldgen.Air {
				return false
			}
		}
	}
	return true
}

// validSpawnInChunk is findValidSpawnInChunk: of the end stone from y 30 up
// with two blocks over it that are not full cubes, the one nearest (0,0,0).
func (h *hub) validSpawnInChunk(cx, cz int32) (blockPos, bool) {
	ch := h.end.Chunk(cx, cz)
	top := -1
	for s := len(ch.Sections) - 1; s >= 0 && top < 0; s-- {
		for _, st := range ch.Sections[s] {
			if st != worldgen.Air {
				top = worldgen.MinY + s*16 + 15
				break
			}
		}
	}
	at := func(lx, y, lz int) uint32 {
		s := (y - worldgen.MinY) / 16
		if y < worldgen.MinY || s >= len(ch.Sections) {
			return worldgen.Air
		}
		return ch.Sections[s][(((y-worldgen.MinY)%16)*16+lz)*16+lx]
	}
	var best blockPos
	found, bestD := false, 0.0
	for y := 30; y <= top; y++ {
		for lz := 0; lz < 16; lz++ {
			for lx := 0; lx < 16; lx++ {
				if at(lx, y, lz) != worldgen.EndStone ||
					worldgen.IsFullCube(at(lx, y+1, lz)) || worldgen.IsFullCube(at(lx, y+2, lz)) {
					continue
				}
				x, z := int(cx)*16+lx, int(cz)*16+lz
				d := sq(float64(x)+0.5) + sq(float64(y)+0.5) + sq(float64(z)+0.5)
				if !found || d < bestD {
					best, bestD, found = blockPos{x, y, z}, d, true
				}
			}
		}
	}
	return best, found
}

// findTallestBlock: over the square of columns within dist of around (its
// own column only when bedrock counts), the highest full block, bedrock
// counted or not; around itself when there is none.
func (h *hub) findTallestBlock(around blockPos, dist int, allowBedrock bool) blockPos {
	maxY := worldgen.MinY + h.end.Ceiling() - 1
	var tallest blockPos
	found := false
	for xd := -dist; xd <= dist; xd++ {
		for zd := -dist; zd <= dist; zd++ {
			if xd == 0 && zd == 0 && !allowBedrock {
				continue
			}
			floor := worldgen.MinY
			if found {
				floor = tallest.y
			}
			for y := maxY; y > floor; y-- {
				st := h.end.At(around.x+xd, y, around.z+zd)
				if worldgen.IsFullCube(st) && (allowBedrock || st != worldgen.Bedrock) {
					tallest, found = blockPos{around.x + xd, y, around.z + zd}, true
					break
				}
			}
		}
	}
	if !found {
		return around
	}
	return tallest
}

// placeEndIsland is EndIslandFeature: a cone of end stone four to six wide
// at the top, narrowing by half a block or a block and a half a layer.
func (h *hub) placeEndIsland(players map[int32]*tracked, origin blockPos) {
	rng := rand.New(rand.NewSource(int64(origin.x)*31 + int64(origin.z)))
	size := float64(rng.Intn(3) + 4)
	for y := 0; size > 0.5; y-- {
		for x := int(math.Floor(-size)); x <= int(math.Ceil(size)); x++ {
			for z := int(math.Floor(-size)); z <= int(math.Ceil(size)); z++ {
				if float64(x*x+z*z) <= (size+1)*(size+1) {
					h.setBlockIn(players, 2, blockPos{origin.x + x, origin.y + y, origin.z + z}, worldgen.EndStone)
				}
			}
		}
		size -= float64(rng.Intn(2)) + 0.5
	}
}

// teleportInEnd moves a player within the End and tells their client.
func (h *hub) teleportInEnd(players map[int32]*tracked, t *tracked, to blockPos) {
	t.x, t.y, t.z = float64(to.x)+0.5, float64(to.y), float64(to.z)+0.5
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	h.playSoundDim(players, t.dim, "minecraft:block.end_gateway.teleport", sndBlock, t.x, t.y, t.z, 1, 1)
}

// updateEndGateways carries anyone standing in a gateway to its partner.
func (h *hub) updateEndGateways(players map[int32]*tracked) {
	if h.end == nil {
		return
	}
	now := h.tick.Load()
	for _, t := range players {
		if t.dim != 2 || t.dead {
			continue
		}
		gx, gy, gz := int(math.Floor(t.x)), int(math.Floor(t.y)), int(math.Floor(t.z))
		if h.end.At(gx, gy, gz) != endGatewayState {
			continue
		}
		// TheEndGatewayBlockEntity holds the cooldown, not the player: a
		// gateway that has just taken someone is shut to EVERYONE for its
		// forty ticks, which is what stops a queue pouring through at once.
		gpos := simPos{dim: 2, blockPos: blockPos{gx, gy, gz}}
		if now < h.gatewayCool[gpos] {
			continue
		}
		h.gatewayCool[gpos] = now + endGatewayCooldown
		h.gatewayCooldownEvent(players, blockPos{x: gx, y: gy, z: gz})
		h.advance(players, t, "enter_block", advMatch{blockState: endGatewayState})
		h.teleportInEnd(players, t, h.gatewayDestination(players, gpos.blockPos))
	}
	h.gatewayCarryEntities(players, now)
	if now > 0 && now%endGatewayAttention == 0 {
		h.gatewayAttentionBeams(players, now)
	}
}

// endGatewayAttention is TheEndGatewayBlockEntity.ATTENTION_INTERVAL: an idle
// gateway flashes its beam this often, which is how one is spotted from afar.
const endGatewayAttention = 2400

// gatewayOpenAt is the gateway half of EndGatewayBlock.entityInside: a
// gateway block at the cell and not cooling down. Taking the entity puts it
// on its forty ticks (triggerCooldown), for everyone.
func (h *hub) gatewayOpenAt(players map[int32]*tracked, x, y, z float64, now uint64) (blockPos, bool) {
	for _, dy := range [2]float64{0, 1} { // the feet cell, or the body's
		gx, gy, gz := int(math.Floor(x)), int(math.Floor(y+dy)), int(math.Floor(z))
		if h.end.At(gx, gy, gz) != endGatewayState {
			continue
		}
		gpos := simPos{dim: 2, blockPos: blockPos{gx, gy, gz}}
		if now < h.gatewayCool[gpos] {
			return blockPos{}, false
		}
		h.gatewayCool[gpos] = now + endGatewayCooldown
		h.gatewayCooldownEvent(players, gpos.blockPos)
		return gpos.blockPos, true
	}
	return blockPos{}, false
}

// gatewayCarryEntities is EndGatewayBlock.entityInside for everything that is
// not a player: mobs, dropped items and projectiles go through as well. A
// thrown ender pearl arrives at rest (getPortalDestination zeroes its motion)
// and then drops, so a pearl thrown into a gateway lands its thrower on the
// far side; anything else keeps its motion. The dragon never uses one
// (EnderDragon.canUsePortal), and neither does a mob carrying a rider — the
// engine does not move riders with their mount.
func (h *hub) gatewayCarryEntities(players map[int32]*tracked, now uint64) {
	for _, m := range h.mobs {
		if m.dim != 2 || m.portalCool > 0 || m == h.dragon || m.dying > 0 ||
			m.rider != 0 || len(m.riders) > 0 || m.mount != 0 {
			continue
		}
		entry, ok := h.gatewayOpenAt(players, m.x, m.y, m.z, now)
		if !ok {
			continue
		}
		to := h.gatewayDestination(players, entry)
		h.entityGone(players, 2, m.eid) // the tracker shows it again where it lands
		m.x, m.y, m.z = float64(to.x)+0.5, float64(to.y), float64(to.z)+0.5
		m.portalCool = entityPortalCooldown
		m.targetEID, m.hasTarget = 0, false
		h.playSoundDim(players, 2, "minecraft:block.end_gateway.teleport", sndBlock, m.x, m.y, m.z, 1, 1)
	}
	for _, it := range h.items {
		if it.dim != 2 || it.portalCool > 0 {
			continue
		}
		entry, ok := h.gatewayOpenAt(players, it.x, it.y, it.z, now)
		if !ok {
			continue
		}
		to := h.gatewayDestination(players, entry)
		h.entityGone(players, 2, it.eid)
		it.x, it.y, it.z = float64(to.x)+0.5, float64(to.y), float64(to.z)+0.5
		it.portalCool = entityPortalCooldown
	}
	for eid, a := range h.arrows {
		if a.dim != 2 || a.stuck || a.returning {
			continue
		}
		entry, ok := h.gatewayOpenAt(players, a.x, a.y, a.z, now)
		if !ok {
			continue
		}
		to := h.gatewayDestination(players, entry)
		h.entityGone(players, 2, eid)
		a.x, a.y, a.z = float64(to.x)+0.5, float64(to.y), float64(to.z)+0.5
		a.sx, a.sy, a.sz = a.x, a.y, a.z
		if a.pearl {
			a.vx, a.vy, a.vz = 0, 0, 0
		}
		add := entAdd(eid, a.etype, a.uuid, a.x, a.y, a.z, arrowYaw(a), arrowPitch(a))
		add.VX, add.VY, add.VZ = a.vx, a.vy, a.vz
		h.toNearbyEv(players, 2, a.x, a.z, add)
	}
}

// gatewayAttentionBeams is portalTick's idle flash: every standing gateway
// that is not already cooling down fires its beam.
func (h *hub) gatewayAttentionBeams(players map[int32]*tracked, now uint64) {
	seen := map[blockPos]bool{}
	fire := func(p blockPos) {
		if seen[p] || h.end.At(p.x, p.y, p.z) != endGatewayState {
			return
		}
		seen[p] = true
		gpos := simPos{dim: 2, blockPos: p}
		if now < h.gatewayCool[gpos] {
			return
		}
		h.gatewayCool[gpos] = now + endGatewayCooldown
		h.gatewayCooldownEvent(players, p)
	}
	for i := 0; i < endGatewayCount; i++ {
		fire(endGatewayRingPos(i))
	}
	for _, g := range h.rules.EndGateways {
		fire(blockPos{g.From[0], g.From[1], g.From[2]})
		fire(blockPos{g.To[0], g.To[1], g.To[2]})
	}
}

// gatewayCooldownEvent is TheEndGatewayBlockEntity.triggerCooldown: the block
// event that puts the gateway on its forty ticks. The client draws the beam
// from it, which is the visible sign that the gateway has just taken someone
// and is not ready for the next.
func (h *hub) gatewayCooldownEvent(players map[int32]*tracked, pos blockPos) {
	id, ok := worldgen.BlockRegistryID("end_gateway")
	if !ok {
		return
	}
	h.toNearbyEv(players, 2, float64(pos.x), float64(pos.z), attachproto.BlockEvent{
		X: int32(pos.x), Y: int32(pos.y), Z: int32(pos.z), Action: 1, Param: 0, Block: int32(id)})
}
