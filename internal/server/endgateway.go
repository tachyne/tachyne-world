package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// End gateways: the way out to the outer islands, and the way back.
//
// Twenty of them appear in a ring around the main island when the dragon
// falls. Step into one and it throws you a thousand blocks out, onto the
// first island along that bearing, and leaves a gateway home beside you.
//
// The ring geometry, the frame and the 1024-block cast are vanilla's
// (EnderDragonFight.spawnNewGateway, EndGatewayFeature, TheEndGatewayBlockEntity).
// So is the promise that a gateway with nowhere to land builds somewhere to
// stand rather than dropping you into the void.

const (
	endGatewayCount    = 20 // GATEWAY_COUNT
	endGatewayRing     = 96 // ring radius around the main island
	endGatewayY        = 75 // the height they hang at
	endGatewayCast     = 1024.0
	endGatewayScanMax  = 6000 // how far out to look for an island before making one
	endGatewayStep     = 8
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

// spawnEndGateways builds the whole ring. Vanilla hands out one gateway per
// dragon kill; tachyne's dragon is a one-off, so there is no second fight to
// dole them out over — the ring goes up at once.
func (h *hub) spawnEndGateways(players map[int32]*tracked) {
	for i := 0; i < endGatewayCount; i++ {
		h.buildEndGateway(players, endGatewayRingPos(i))
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

// endGatewayDestination is vanilla's findExitPortalXZPosTentative adapted to a
// world whose islands are a pure function of the seed: cast 1024 blocks along
// the gateway's own bearing, then keep going until there is an island to stand
// on. Reports whether one was found.
func (h *hub) endGatewayDestination(from blockPos) (blockPos, bool) {
	d := math.Hypot(float64(from.x), float64(from.z))
	if d < 1e-6 || h.end == nil {
		return blockPos{}, false
	}
	dx, dz := float64(from.x)/d, float64(from.z)/d
	gen := h.end.Gen()
	for r := endGatewayCast; r <= endGatewayScanMax; r += endGatewayStep {
		x, z := int(dx*r), int(dz*r)
		if top, _, ok := gen.EndOuterColumn(x, z); ok {
			return blockPos{x, top + 1, z}, true
		}
	}
	return blockPos{x: int(dx * endGatewayCast), y: endGatewayY, z: int(dz * endGatewayCast)}, false
}

// outboundEndGateway throws a player out to the islands and leaves a gateway
// home beside where they land.
func (h *hub) outboundEndGateway(players map[int32]*tracked, t *tracked, gate blockPos) {
	dest, onIsland := h.endGatewayDestination(gate)
	if !onIsland {
		// Vanilla generates an island here; a plinth keeps the same promise —
		// somewhere to stand — without a second island generator.
		for ddx := -2; ddx <= 2; ddx++ {
			for ddz := -2; ddz <= 2; ddz++ {
				h.setBlockIn(players, 2, blockPos{dest.x + ddx, dest.y - 1, dest.z + ddz}, worldgen.EndStone)
			}
		}
	}
	back := blockPos{x: dest.x + 2, y: dest.y, z: dest.z}
	if h.end.At(back.x, back.y, back.z) != endGatewayState {
		h.setBlockIn(players, 2, back, endGatewayState)
		h.setBlockIn(players, 2, blockPos{back.x, back.y - 1, back.z}, worldgen.Bedrock)
	}
	h.teleportInEnd(players, t, dest)
}

// returnFromGateway sends a player standing in a far-out gateway back to the
// centre of the main island — the other direction of the same trip.
func (h *hub) returnFromGateway(players map[int32]*tracked, t *tracked) {
	y := worldgen.EndSurfaceY + 1
	for y < worldgen.EndSurfaceY+16 && h.end.At(0, y, 0) != worldgen.Air {
		y++
	}
	h.teleportInEnd(players, t, blockPos{x: 0, y: y, z: 0})
}

// teleportInEnd moves a player within the End and tells their client.
func (h *hub) teleportInEnd(players map[int32]*tracked, t *tracked, to blockPos) {
	t.x, t.y, t.z = float64(to.x)+0.5, float64(to.y), float64(to.z)+0.5
	t.p.trySendEv(teleportEv(t.x, t.y, t.z, t.yaw, t.pitch))
	h.playSound(players, "minecraft:block.end_gateway.teleport", sndBlock, t.x, t.y, t.z, 1, 1)
}

// updateEndGateways carries anyone standing in a gateway. A gateway near the
// ring throws you out; one far from the centre brings you home.
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
		if now < t.gatewayUntil {
			continue
		}
		t.gatewayUntil = now + endGatewayCooldown
		h.advance(players, t, "enter_block", advMatch{blockState: endGatewayState})
		if math.Hypot(float64(gx), float64(gz)) < endGatewayRing*2 {
			h.outboundEndGateway(players, t, blockPos{x: gx, y: gy, z: gz})
		} else {
			h.returnFromGateway(players, t)
		}
	}
}
