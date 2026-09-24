package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ghast's charge (GhastShootFireballGoal) and the blaze's volleys
// (BlazeAttackGoal): a ghast with a target within sixty-four charges for
// twenty ticks — the warning cry at ten, the red eyes and open mouth
// while charged — fires, and rests forty; a blaze within reach bites
// every twenty ticks, else it flares up (the charged flag), waits sixty,
// then fires three small fireballs six ticks apart on a spread that
// widens with distance, and rests a hundred.

const (
	metaIndexGhastCharging = 16 // Ghast DATA_IS_CHARGING (bool)
	metaIndexBlazeFlags    = 16 // Blaze DATA_FLAGS_ID (byte): 1 = charged
	ghastChargeWarn        = 10 // levelEvent 1015
	ghastChargeFire        = 20
	ghastChargeRest        = -40
	ghastRangeSq           = 4096.0
	worldEventGhastWarn    = 1015
	worldEventGhastShoot   = 1016
	worldEventBlazeShoot   = 1018
	blazeMeleeSq           = 4.0
	blazeFlareTicks        = 60
	blazeVolleyGap         = 6
	blazeVolleyShots       = 3
	blazeRestTicks         = 100
	blazeSpread            = 2.297
)

func ghastChargingMeta(m *mob) []byte {
	return boolMeta(m.eid, metaIndexGhastCharging, m.ghastCharge > ghastChargeWarn)
}

func blazeFlagsMeta(m *mob) []byte {
	var f byte
	if m.blazeCharged {
		f = 1
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexBlazeFlags)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, f)
	return protocol.AppendU8(b, itemMetaEnd)
}

// ghastTick runs each mob update from the hostile switch.
func (h *hub) ghastTick(players map[int32]*tracked, m *mob) {
	t := h.nearestHuntable(players, m.dim, m.x, m.z, 64)
	was := m.ghastCharge > ghastChargeWarn
	// The target selector only accepts somebody within four blocks of the
	// ghast's own height — the reason one high above a lava lake ignores you.
	if t != nil && !ghastCanTarget(m, t.y) {
		t = nil
	}
	if t == nil || dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) >= ghastRangeSq || !h.mobSees(m, t) {
		if m.ghastCharge > 0 {
			m.ghastCharge -= mobMoveInterval
			if m.ghastCharge < 0 {
				m.ghastCharge = 0
			}
		}
	} else {
		for i := 0; i < mobMoveInterval; i++ {
			m.ghastCharge++
			if m.ghastCharge == ghastChargeWarn {
				h.toDimEv(players, m.dim, attachproto.WorldFX{Event: worldEventGhastWarn, X: int(math.Floor(m.x)), Y: int(math.Floor(m.y)), Z: int(math.Floor(m.z))})
			}
			if m.ghastCharge == ghastChargeFire {
				ux, uy, uz := aimAt(m.x, m.y+2, m.z, t.x, t.y+0.5, t.z)
				a := h.launchProjectileIn(players, entityLargeFireball, m.dim, m.x+ux*4, m.y+2, m.z+uz*4, ux*hurtingSpeed, uy*hurtingSpeed, uz*hurtingSpeed)
				// The fireball burns (it lights a campfire or TNT it strikes), but its
				// hit only deals damage: see ignitesOnHit.
				a.shooter, a.dmg, a.explode, a.fire = m.eid, 6, 1, true
				h.playSoundDim(players, m.dim, "minecraft:entity.ghast.shoot", sndHostile, m.x, m.y, m.z, 3, 1)
				m.ghastCharge = ghastChargeRest
				break
			}
		}
	}
	if now := m.ghastCharge > ghastChargeWarn; now != was {
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(ghastChargingMeta(m)))
	}
}

func (h *hub) setBlazeCharged(players map[int32]*tracked, m *mob, on bool) {
	if m.blazeCharged == on {
		return
	}
	m.blazeCharged = on
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(blazeFlagsMeta(m)))
}

// blazeTick runs each mob update from the hostile switch.
func (h *hub) blazeTick(players map[int32]*tracked, m *mob) {
	m.blazeTime -= mobMoveInterval
	t := h.nearestHuntable(players, m.dim, m.x, m.z, m.followRange())
	if t == nil {
		if m.blazeStep != 0 || m.blazeCharged {
			m.blazeStep = 0
			h.setBlazeCharged(players, m, false)
		}
		return
	}
	d := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z)
	los := h.mobSees(m, t) // BlazeAttackGoal: neither bite nor volley without it
	if d < blazeMeleeSq {
		if !los {
			return
		}
		if m.blazeTime <= 0 {
			m.blazeTime = 20
			h.mobMelee(players, m) // doHurtTarget
		}
		return
	}
	if m.blazeTime > 0 || !los {
		return
	}
	m.blazeStep++
	switch {
	case m.blazeStep == 1:
		m.blazeTime = blazeFlareTicks
		h.setBlazeCharged(players, m, true)
	case m.blazeStep <= 1+blazeVolleyShots:
		m.blazeTime = blazeVolleyGap
	default:
		m.blazeTime = blazeRestTicks
		m.blazeStep = 0
		h.setBlazeCharged(players, m, false)
	}
	if m.blazeStep > 1 {
		d5 := math.Sqrt(math.Sqrt(d)) * 0.5
		dx, dy, dz := t.x-m.x, (t.y+0.9)-(m.y+0.9), t.z-m.z
		vx := h.triangle(dx, blazeSpread*d5)
		vz := h.triangle(dz, blazeSpread*d5)
		l := math.Sqrt(vx*vx + dy*dy + vz*vz)
		if l < 1e-6 {
			return
		}
		h.toDimEv(players, m.dim, attachproto.WorldFX{Event: worldEventBlazeShoot, X: int(math.Floor(m.x)), Y: int(math.Floor(m.y)), Z: int(math.Floor(m.z))})
		v := hurtingSpeed
		a := h.launchProjectileIn(players, entitySmallFireball, m.dim, m.x, m.y+1.4, m.z, vx/l*v, dy/l*v, vz/l*v)
		a.shooter, a.dmg, a.fire = m.eid, blazeFireballDmg, true
		h.playSoundDim(players, m.dim, "minecraft:entity.blaze.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
	}
}

// triangle is RandomSource.triangle: mode ± deviation × (r − r).
func (h *hub) triangle(mode, dev float64) float64 {
	return mode + dev*(h.rng.Float64()-h.rng.Float64())
}

// smallFireballLights is SmallFireball.onHitBlock: fire in the cell on the
// struck face, if that cell is empty. A mob's fireball needs mobGriefing; a
// dispensed or batted-back one does not.
func (h *hub) smallFireballLights(players map[int32]*tracked, a *arrowEntity, hit, cell blockPos) {
	if _, byMob := h.mobs[a.shooter]; byMob && !h.rules.MobGriefing {
		return
	}
	if cell == hit {
		return
	}
	if st := h.worldFor(a.dim).At(cell.x, cell.y, cell.z); st != worldgen.Air && st != caveAirState {
		return
	}
	// BaseFireBlock.getState: soul fire over #soul_fire_base_blocks.
	if below := h.worldFor(a.dim).At(cell.x, cell.y-1, cell.z); below == worldgen.SoulSand || below == soulSoilBase {
		h.setBlockAt(players, a.dim, cell, soulFire)
		return
	}
	h.inDim(a.dim, func() { h.igniteFire(players, cell, 0) })
}

// struckFace is the cell against the face a segment from (x0,y0,z0) to
// (x1,y1,z1) enters block hit through: the face whose plane the segment
// crosses last on its way in.
func struckFace(x0, y0, z0, x1, y1, z1 float64, hit blockPos) blockPos {
	from := [3]float64{x0, y0, z0}
	d := [3]float64{x1 - x0, y1 - y0, z1 - z0}
	lo := [3]float64{float64(hit.x), float64(hit.y), float64(hit.z)}
	axis, best := -1, math.Inf(-1)
	for i := 0; i < 3; i++ {
		if d[i] == 0 {
			continue
		}
		plane := lo[i]
		if d[i] < 0 {
			plane++
		}
		if t := (plane - from[i]) / d[i]; t > best {
			axis, best = i, t
		}
	}
	out := hit
	if axis < 0 {
		return out
	}
	step := 1
	if d[axis] > 0 {
		step = -1
	}
	switch axis {
	case 0:
		out.x += step
	case 1:
		out.y += step
	case 2:
		out.z += step
	}
	return out
}
