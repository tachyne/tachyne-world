package server

import (
	"encoding/binary"
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Firework rockets. Two lives in one item: on the ground it climbs and pops,
// and in the hand of a player who is already gliding it is the only way to
// gain height on an elytra.
//
// The flight profile is vanilla's: a nudge upward at launch, then a 1.15x
// horizontal accelerator and +0.04 vertical every tick, popping after
// 10*(1+flight) ticks plus a small random tail. A rocket attached to a
// gliding player instead drags them toward where they are looking, which is
// what makes elytra travel work at all.

var (
	itemFireworkRocket = itemByName["firework_rocket"]
	entityFirework     = entityID("firework_rocket")
)

const (
	fireworkLifeBase  = 10 // ticks per unit of flight duration
	fireworkClimb     = 0.04
	fireworkAccel     = 1.15
	fireworkLaunchVY  = 0.05
	fireworkBoostPull = 1.5 // how hard a glider is pulled toward their look
	fireworkBoostAdd  = 0.1
	fireworkFlightMax = 3 // Fireworks.flightDuration is 1-3 from the recipe
)

// rocketFlight is a stack's flight duration, with vanilla's default for a
// rocket that has none (a spawn-egg rocket, a dispenser fed an old stack).
func rocketFlight(st invStack) int {
	if st.flight < 1 {
		return 1
	}
	return min(fireworkFlightMax, int(st.flight))
}

// rocketEntity is a rocket in the air. Attached rockets ride their player and
// steer them; loose ones fly their own arc.
type rocketEntity struct {
	eid        int32
	uuid       [16]byte
	dim        int
	x, y, z    float64
	vx, vy, vz float64
	sx, sy, sz float64 // last broadcast position
	life       int
	lifetime   int
	attached   int32 // eid of the gliding player being boosted (0 = loose)
}

type evUseFirework struct{ eid int32 }

func (evUseFirework) isHubEvent() {}

// gliding reports whether a player is in elytra flight — the condition that
// turns a rocket from a firework into a thruster. It is the client's own
// START_FALL_FLYING that begins it, exactly as vanilla has it: merely being
// in the air wearing an elytra is falling, not flying, and a rocket used
// while falling should go off in your hand rather than carry you.
// canStartFallFlying is ServerGamePacketListener's check on
// START_FALL_FLYING: the server does not take the client's word for it. You
// must be off the ground and in a serviceable elytra, or anyone could glide
// along the floor.
func canStartFallFlying(t *tracked) bool {
	return !t.onGround && t.armor[1].item == itemElytra
}

func (t *tracked) gliding() bool {
	// Both, deliberately: the flag is cleared on landing anyway, but reading
	// the ground here too means a clear that never arrives cannot leave
	// somebody gliding along the floor for the rest of the session.
	return t.fallFlying && !t.onGround && t.armor[1].item == itemElytra
}

// useFirework fires the held rocket. Used while gliding it attaches to the
// player; otherwise it goes off where they stand.
func (h *hub) useFirework(players map[int32]*tracked, t *tracked) {
	if t.dead || t.inv == nil || heldStack(t).item != itemFireworkRocket {
		return
	}
	// On the ground a rocket is placed against a block, not used in the air;
	// vanilla only fires from the hand when gliding.
	if !t.gliding() {
		return
	}
	st := heldStack(t)
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	h.spawnRocket(players, t.dim, t.x, t.y+1.5, t.z, t.p.eid, st)
}

// spawnRocket puts one in the air. attached is the eid it boosts, or 0. The
// STACK comes along because the rocket's own metadata carries it — that is
// what the client draws the burst from when it pops, and without it a rocket
// full of stars went off as nothing.
func (h *hub) spawnRocket(players map[int32]*tracked, dim int, x, y, z float64, attached int32, st invStack) *rocketEntity {
	flight := rocketFlight(st)
	eid := h.allocEID()
	r := &rocketEntity{
		eid: eid, dim: dim, x: x, y: y, z: z,
		sx: x, sy: y, sz: z,
		vy:       fireworkLaunchVY,
		lifetime: fireworkLifeBase*(1+flight) + h.rng.Intn(6) + h.rng.Intn(7),
		attached: attached,
	}
	binary.BigEndian.PutUint32(r.uuid[12:], uint32(eid))
	h.rockets[eid] = r
	h.toNearbyEv(players, dim, x, z, entAdd(eid, entityFirework, r.uuid, x, y, z, 0, 0))
	// FireworkRocketEntity's DATA_ID_FIREWORKS_ITEM, the same index and
	// serializer a dropped item uses for its stack (verified 8 on 1.21.11 AND
	// on 26.2/26.3 — Entity has the same eight synced fields in both).
	if st.item != 0 {
		h.toNearbyEv(players, dim, x, z, metaEv(itemMetadata(eid, st)))
	}
	h.playSound(players, "minecraft:entity.firework_rocket.launch", sndAmbient, x, y, z, 3, 1)
	return r
}

// updateRockets flies every rocket one tick.
func (h *hub) updateRockets(players map[int32]*tracked) {
	for _, r := range h.rockets {
		if t := players[r.attached]; r.attached != 0 && t != nil {
			// Riding a glider: pull them toward their look and follow along.
			if !t.gliding() || t.dead {
				h.popRocket(players, r)
				continue
			}
			lx, ly, lz := lookVector(t.yaw, t.pitch)
			t.p.trySendEv(attachproto.Velocity{
				EID: t.p.eid,
				VX:  lx * (fireworkBoostPull + fireworkBoostAdd),
				VY:  ly * (fireworkBoostPull + fireworkBoostAdd),
				VZ:  lz * (fireworkBoostPull + fireworkBoostAdd),
			})
			r.x, r.y, r.z = t.x, t.y+1, t.z
		} else if r.attached != 0 {
			h.popRocket(players, r) // the player it was boosting is gone
			continue
		} else {
			r.vx, r.vz = r.vx*fireworkAccel, r.vz*fireworkAccel
			r.vy += fireworkClimb
			r.x, r.y, r.z = r.x+r.vx, r.y+r.vy, r.z+r.vz
			if worldgen.Collides(h.worldFor(r.dim).At(int(math.Floor(r.x)), int(math.Floor(r.y)), int(math.Floor(r.z)))) {
				h.popRocket(players, r)
				continue
			}
		}
		if r.life++; r.life > r.lifetime {
			h.popRocket(players, r)
			continue
		}
		if r.x != r.sx || r.y != r.sy || r.z != r.sz {
			h.toTracking(players, r.eid, r.dim, r.x, r.z, entMove(r.eid, r.x, r.y, r.z, 0, 0, false))
			r.sx, r.sy, r.sz = r.x, r.y, r.z
		}
	}
}

// popRocket detonates and removes one. A plain rocket carries no explosion,
// so this is the bang and nothing else — the damage vanilla deals scales with
// the star components a stack does not carry here yet.
func (h *hub) popRocket(players map[int32]*tracked, r *rocketEntity) {
	delete(h.rockets, r.eid)
	h.toTracking(players, r.eid, r.dim, r.x, r.z, entityStatus(r.eid, entityStatusFireworks)) // FireworkRocketEntity.explode
	h.playSound(players, "minecraft:entity.firework_rocket.blast", sndAmbient, r.x, r.y, r.z, 3, 1)
	h.entityGone(players, r.dim, r.eid)
}
