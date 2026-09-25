package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bats hang (Bat.customServerAiStep): a flying bat under a solid block
// settles one tick in a hundred and hangs there, upside down, until the
// block goes or a player comes within four blocks — then it drops with
// the takeoff flutter. Parrots mimic (Parrot.imitateNearbyMobs): one tick
// in four hundred, half the time, a parrot picks a mob within twenty and,
// if it is one of the monsters parrots know, squawks its call.

const (
	metaIndexBatFlags    = 16  // Bat DATA_ID_FLAGS (byte; a bat is not ageable): 1 = resting
	batRestOdds          = 100 // nextInt(100) == 0 under a solid block
	batWakeRange         = 4.0 // BAT_RESTING_TARGETING range
	worldEventBatTakeoff = 1025
	parrotImitateOdds    = 400 // nextInt(400) == 0, then nextInt(2) == 0
	parrotImitateRange   = 20.0
)

var parrotImitates = map[int]string{}

func init() {
	for _, n := range []string{"blaze", "bogged", "breeze", "cave_spider", "creaking", "creeper", "drowned", "elder_guardian",
		"ender_dragon", "endermite", "evoker", "ghast", "guardian", "hoglin", "husk", "illusioner", "magma_cube", "phantom",
		"piglin", "piglin_brute", "pillager", "ravager", "shulker", "silverfish", "skeleton", "slime", "spider", "stray",
		"vex", "vindicator", "warden", "witch", "wither", "wither_skeleton", "zoglin", "zombie"} {
		parrotImitates[entityID(n)] = "minecraft:entity.parrot.imitate." + n
	}
}

func batFlagsMeta(m *mob) []byte {
	var f byte
	if m.batResting {
		f = 1
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexBatFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, f)
	return protocol.AppendU8(b, itemMetaEnd)
}

func (h *hub) setBatResting(players map[int32]*tracked, m *mob, on bool) {
	if m.batResting == on {
		return
	}
	m.batResting = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(batFlagsMeta(m)))
}

// batStep runs each mob update. Returns whether it holds the bat.
func (h *hub) batStep(players map[int32]*tracked, m *mob) bool {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	solidAbove := worldgen.Collides(w.At(bx, by+1, bz))
	if m.batResting {
		wake := !solidAbove
		if !wake {
			if t := h.nearestPlayer(players, m.x, m.z, batWakeRange); t != nil && t.dim == m.dim && math.Abs(t.y-m.y) <= batWakeRange {
				wake = true
			}
		}
		if wake {
			h.setBatResting(players, m, false)
			h.toDimEv(players, m.dim, attachproto.WorldFX{Event: worldEventBatTakeoff, X: bx, Y: by, Z: bz})
			return false
		}
		m.vx, m.vy, m.vz = 0, 0, 0
		if h.rng.Intn(200/mobMoveInterval) == 0 { // hanging, it looks about now and then
			m.headYaw = float32(h.rng.Intn(360))
		}
		return true
	}
	if solidAbove && h.rng.Intn(batRestOdds/mobMoveInterval) == 0 {
		h.setBatResting(players, m, true)
		m.vx, m.vy, m.vz = 0, 0, 0
		return true
	}
	h.batFly(m)
	return true
}

// batFly is the flying half of Bat.customServerAiStep: a target cell up to
// six blocks off sideways and two below to three above, re-picked when it
// stops being air, one tick in thirty, or once the bat is within two of it;
// each tick the velocity closes a tenth of the way on 0.5 a tick along each
// axis toward it (0.7 up or down), under the air's 0.91 drag and the bat's
// 0.6 vertical damping. Two ticks make one mob update.
func (h *hub) batFly(m *mob) {
	w := h.worldFor(m.dim)
	if m.batHasT && (!isAirState(w.At(m.batT.x, m.batT.y, m.batT.z)) || m.batT.y <= worldgen.MinY) {
		m.batHasT = false
	}
	cx, cy, cz := float64(m.batT.x)+0.5, float64(m.batT.y)+0.5, float64(m.batT.z)+0.5
	if !m.batHasT || h.rng.Intn(30/mobMoveInterval) == 0 || dist3(cx, cy, cz, m.x, m.y, m.z) < 2 {
		m.batT = blockPos{
			floorInt(m.x + float64(h.rng.Intn(7)-h.rng.Intn(7))),
			floorInt(m.y + float64(h.rng.Intn(6)) - 2),
			floorInt(m.z + float64(h.rng.Intn(7)-h.rng.Intn(7))),
		}
		m.batHasT = true
	}
	sign := func(v float64) float64 {
		switch {
		case v > 0:
			return 1
		case v < 0:
			return -1
		}
		return 0
	}
	x, y, z := m.x, m.y, m.z
	for i := 0; i < mobMoveInterval; i++ {
		dx := float64(m.batT.x) + 0.5 - x
		dy := float64(m.batT.y) + 0.1 - y
		dz := float64(m.batT.z) + 0.5 - z
		m.batVX += (sign(dx)*0.5 - m.batVX) * 0.1
		m.batVY += (sign(dy)*0.7 - m.batVY) * 0.1
		m.batVZ += (sign(dz)*0.5 - m.batVZ) * 0.1
		x, y, z = x+m.batVX, y+m.batVY, z+m.batVZ
		m.batVX, m.batVZ = m.batVX*0.91, m.batVZ*0.91
		m.batVY *= 0.98 * 0.6
	}
	m.vx, m.vz = x-m.x, z-m.z
	m.flyAim(h.tick.Load(), y)
	if m.vx != 0 || m.vz != 0 {
		m.yaw = float32(math.Atan2(-m.vx, m.vz) * 180 / math.Pi)
	}
	m.rest = 0
}

// parrotImitateTick is the ambient mimicry.
func (h *hub) parrotImitateTick(players map[int32]*tracked, m *mob) bool {
	if h.rng.Intn(parrotImitateOdds/mobMoveInterval) != 0 {
		return false
	}
	return h.parrotImitateNearby(players, m.dim, m.x, m.y, m.z, sndNeutral)
}

// parrotImitateNearby is Parrot.imitateNearbyMobs, from wherever the parrot
// is — flying, or riding a shoulder, where the player is the one it sounds
// from: one time in two, a random mob within twenty blocks that a parrot can
// imitate (NOT_PARROT_PREDICATE: one in MOB_SOUND_MAP) has its call copied.
func (h *hub) parrotImitateNearby(players map[int32]*tracked, dim int, x, y, z float64, src int32) bool {
	if h.rng.Intn(2) != 0 {
		return false
	}
	var pick *mob
	n := 0
	h.grid().nearby(dim, x, z, parrotImitateRange, func(o *mob) {
		if _, ok := parrotImitates[o.etype]; !ok || o.dying > 0 || math.Abs(o.y-y) > parrotImitateRange {
			return
		}
		n++
		if h.rng.Intn(n) == 0 {
			pick = o
		}
	})
	if pick == nil {
		return false
	}
	h.playSoundDim(players, dim, parrotImitates[pick.etype], src, x, y, z, 0.7, parrotPitch(h))
	return true
}

// parrotPitch is Parrot.getPitch.
func parrotPitch(h *hub) float32 { return (h.rng.Float32()-h.rng.Float32())*0.2 + 1 }

// parrotFollowMobStep is FollowMobGoal(1.0, 3, 7): a parrot with nothing
// better to do keeps company with the nearest mob that is not a parrot
// within seven blocks (its box grown by seven), flying in to three blocks
// and backing off when closer than √3, re-deciding every ten ticks. Its
// wander (priority 2) outranks it (parrotWanderStep).
// It reports whether it holds the parrot.
func (h *hub) parrotFollowMobStep(m *mob) bool {
	if m.sitting || m.leash != 0 || (m.tamed && m.hasTarget) {
		m.parrotFollow = 0
		return false
	}
	o := h.mobs[m.parrotFollow]
	if o == nil || o.dying > 0 || o.dim != m.dim || !h.parrotFollowable(m, o) {
		m.parrotFollow, o = 0, nil
		best := math.Inf(1)
		h.grid().nearby(m.dim, m.x, m.z, parrotFollowArea+2, func(c *mob) {
			if !h.parrotFollowable(m, c) {
				return
			}
			if d := dist3(c.x, c.y, c.z, m.x, m.y, m.z); d < best {
				o, best = c, d
			}
		})
		if o == nil {
			return false
		}
		m.parrotFollow, m.followRecalc = o.eid, 0
	}
	if m.followRecalc--; m.followRecalc > 0 {
		return true // keep the heading chosen last time
	}
	m.followRecalc = 10 / mobMoveInterval
	d2 := sq(m.x-o.x) + sq(m.y-o.y) + sq(m.z-o.z)
	switch {
	case d2 > parrotFollowStop*parrotFollowStop:
		m.vx, m.vz = straightSteer(m, o.x, o.z, 0)
		m.flyAim(h.tick.Load(), o.y)
	case d2 <= parrotFollowStop:
		m.vx, m.vz = straightSteer(m, 2*m.x-o.x, 2*m.z-o.z, 0) // too close: away from it
	default:
		m.vx, m.vz = 0, 0
	}
	m.yaw = yawToward(m.x, m.z, o.x, o.z)
	m.headYaw = m.yaw
	m.rest = 0
	return true
}

const (
	parrotFollowArea = 7.0 // FollowMobGoal areaSize
	parrotFollowStop = 3.0 // …stopDistance
)

// parrotFollowable is FollowMobGoal's followPredicate and box: another
// kind of mob, visible, whose box meets the parrot's grown by seven.
func (h *hub) parrotFollowable(m, o *mob) bool {
	if o == m || o.etype == entityParrot || o.dying > 0 || o.dim != m.dim || o.hasEffect(effInvisibility) > 0 {
		return false
	}
	mb, ob := m.box(), o.box()
	reach := parrotFollowArea + (mb.w+ob.w)/2
	return math.Abs(o.x-m.x) <= reach && math.Abs(o.z-m.z) <= reach &&
		o.y+ob.h >= m.y-parrotFollowArea && o.y <= m.y+mb.h+parrotFollowArea
}
