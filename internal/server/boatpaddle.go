package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Boat paddles (AbstractBoat). The rowing client sends its paddle input
// every tick (ServerboundPaddleBoatPacket → setPaddleState); the state is
// synced entity data, so other players see the paddles move, and the
// server's tick turns each paddle stroke into the paddle sound — no client
// plays it on its own, so a boat without paddle state rows in silence.

const (
	boatMetaPaddleLeft  = 11 // DATA_ID_PADDLE_LEFT: BOOLEAN
	boatMetaPaddleRight = 12 // DATA_ID_PADDLE_RIGHT: BOOLEAN

	// PADDLE_SPEED, a paddle's turn per tick, and PADDLE_SOUND_TIME, where
	// in the turn it strikes. Floats, as vanilla's paddlePositions are: the
	// strike test is an exact <= on them.
	paddleSpeed     = float32(math.Pi / 8)
	paddleSoundTime = float32(math.Pi / 4)
	paddleTurn      = float32(math.Pi * 2)
)

type evPaddleBoat struct {
	eid         int32
	left, right bool
}

func (evPaddleBoat) isHubEvent() {}

// paddleBoat is handlePaddleBoat: only the player controlling a boat
// sets its paddles.
func (h *hub) paddleBoat(players map[int32]*tracked, t *tracked, left, right bool) {
	v := h.vehicles[t.ridingEID]
	if v == nil || !v.isBoat() || h.boatController(v) != t.p.eid {
		return
	}
	h.setPaddleState(players, v, left, right)
}

// boatController is the player at the front seat — the first passenger,
// when it is a player (AbstractBoat.getControllingPassenger); 0 otherwise.
func (h *hub) boatController(v *vehicle) int32 {
	if v.rider == 0 || (v.mobRider != 0 && v.mobFirst) {
		return 0
	}
	return v.rider
}

func (h *hub) setPaddleState(players map[int32]*tracked, v *vehicle, left, right bool) {
	if v.paddle == [2]bool{left, right} {
		return
	}
	v.paddle = [2]bool{left, right}
	h.toTracking(players, v.eid, v.dim, v.x, v.z, metaEv(boatPaddleMeta(v)))
}

func boatPaddleMeta(v *vehicle) []byte {
	b := protocol.AppendVarInt(nil, v.eid)
	b = protocol.AppendU8(b, boatMetaPaddleLeft)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, v.paddle[0])
	b = protocol.AppendU8(b, boatMetaPaddleRight)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, v.paddle[1])
	return protocol.AppendU8(b, itemMetaEnd)
}

// tickBoatPaddles is the paddle half of AbstractBoat.tick: no player at
// the front, no rowing; each rowing paddle turns PADDLE_SPEED a tick and
// sounds as it passes PADDLE_SOUND_TIME in its turn, on its own side of
// the boat — in the water, or scraping the ground on land.
func (h *hub) tickBoatPaddles(players map[int32]*tracked, v *vehicle) {
	if h.boatController(v) == 0 {
		h.setPaddleState(players, v, false, false)
	}
	for i := 0; i < 2; i++ {
		if !v.paddle[i] {
			v.paddlePos[i] = 0
			continue
		}
		p := v.paddlePos[i]
		if fmod32(p, paddleTurn) <= paddleSoundTime && fmod32(p+paddleSpeed, paddleTurn) >= paddleSoundTime {
			if sound := h.paddleSound(v); sound != "" {
				yaw := float64(v.yaw) * math.Pi / 180
				vx, vz := -math.Sin(yaw), math.Cos(yaw) // getViewVector, level
				dx, dz := vz, -vx
				if i == 1 {
					dx, dz = -vz, vx
				}
				h.playSoundDim(players, v.dim, sound, sndNeutral, v.x+dx, v.y, v.z+dz, 1, 0.8+0.4*h.rng.Float32())
			}
		}
		v.paddlePos[i] = p + paddleSpeed
	}
}

// paddleSound is getPaddleSound by the boat's status: in or under water
// the water stroke, on the ground the land one, in the air nothing.
func (h *hub) paddleSound(v *vehicle) string {
	if h.inWater(v.dim, v.x, v.y+0.1, v.z) || h.inWater(v.dim, v.x, v.y-0.1, v.z) {
		return "minecraft:entity.boat.paddle_water"
	}
	w := h.worldFor(v.dim)
	if w != nil && worldgen.IsSolid(w.At(floorInt(v.x), floorInt(v.y-0.05), floorInt(v.z))) {
		return "minecraft:entity.boat.paddle_land"
	}
	return ""
}

// fmod32 is Java's float %: exact, so the float64 fmod of the two floats
// narrows back without rounding.
func fmod32(a, b float32) float32 { return float32(math.Mod(float64(a), float64(b))) }
