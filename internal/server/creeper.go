package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Creepers — the walking bomb. They stalk a player silently, and once close
// enough start to swell: the client renders the swell from the state
// metadata, and 30 ticks of it later the hub carves the crater, damages
// everything in range and removes the creeper. Backing off far enough (or out
// of sight) makes it unwind again, a tick at a time, as vanilla's does.

const (
	creeperHealth      = 20
	creeperIgniteRange = 3.0 // SwellGoal.canUse: the target within 3 blocks (distanceToSqr < 9)
	creeperCancelRange = 7.0 // SwellGoal.tick: past 7 (distanceToSqr > 49) it unwinds
	creeperFuseTicks   = 30  // Creeper.maxSwell: 1.5 s of swelling before the bang

	metaIndexCreeperState = 16 // creeper metadata: fuse state (-1 idle, +1 primed)

	blastRadius     = 3  // Creeper.explosionRadius: crater and hurt alike
	blastDropChance = 30 // % of destroyed blocks that drop their loot (vanilla ~1/power)
)

var (
	entityCreeper = entityID("creeper")     // minecraft:entity_type "creeper" (1.21.5)
	itemGunpowder = itemByName["gunpowder"] // drops
	itemBone      = itemByName["bone"]      // (skeleton drops live in combat.go's loot table)
)

// creeperBehavior stalks like a zombie, but holds perfectly still while its
// SwellGoal runs (the goal takes the MOVE flag: it commits to the bang
// rather than chasing mid-swell).
type creeperBehavior struct{}

func (creeperBehavior) name() string { return "creeper" }
func (creeperBehavior) steer(h *hub, m *mob) (float64, float64) {
	if m.swellHold || m.ignited {
		return 0, 0
	}
	return hostileBehavior{}.steer(h, m)
}

// creeperFuse runs at the mob-update cadence: SwellGoal picks the swell
// direction, then Creeper.tick moves the swell one tick per tick that way —
// up to the bang, or back down to nothing. Walking away from a half-swollen
// creeper and coming back finds it still half-swollen.
func (h *hub) creeperFuse(players map[int32]*tracked, m *mob) {
	dir := int8(-1)
	if m.swellDir > 0 {
		dir = 1
	}
	// SwellGoal.canUse (and canContinueToUse): already swelling, or the
	// target within 3 blocks. While it runs it sets the direction: back
	// down with the target gone, past 7 blocks or out of sight, else up.
	t := h.nearestHuntable(players, m.dim, m.x, m.z, creeperCancelRange+1)
	d2 := math.Inf(1)
	if t != nil {
		dx, dy, dz := t.x-m.x, t.y-m.y, t.z-m.z
		d2 = dx*dx + dy*dy + dz*dz
	}
	m.swellHold = dir > 0 || d2 < creeperIgniteRange*creeperIgniteRange
	if m.swellHold {
		if t == nil || d2 > creeperCancelRange*creeperCancelRange || !h.mobSees(m, t) {
			dir = -1
		} else {
			dir = 1
			if m.swell == 0 {
				m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi) // stare down the target
			}
		}
	}
	if m.ignited { // Creeper.tick: isIgnited forces the swell up, target or no target
		dir = 1
	}
	h.creeperSetSwellDir(players, m, dir)
	if dir > 0 && m.swell == 0 {
		h.playSoundDim(players, m.dim, "minecraft:entity.creeper.primed", sndHostile, m.x, m.y, m.z, 1, 0.5)
		h.vibAt(m.dim, freqPrimeFuse, m.x, m.y, m.z, m.eid)
	}
	m.swell += int(dir) * mobMoveInterval
	if m.swell < 0 {
		m.swell = 0
	}
	if m.swell >= creeperFuseTicks {
		m.swell = creeperFuseTicks
		h.explodeCreeper(players, m)
	}
}

// creeperSetSwellDir is setSwellDir: the synced direction the client swells
// its model by.
func (h *hub) creeperSetSwellDir(players map[int32]*tracked, m *mob, dir int8) {
	cur := int8(-1)
	if m.swellDir > 0 {
		cur = 1
	}
	m.swellDir = dir
	if dir != cur {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(creeperStateMeta(m.eid, int32(dir))))
	}
}

// explodeCreeper removes the creeper and detonates: crater the terrain,
// damage + knock back every entity in range, spill any destroyed containers.
func (h *hub) explodeCreeper(players map[int32]*tracked, m *mob) {
	delete(h.mobs, m.eid)
	h.gridDirty()
	h.entityGone(players, m.dim, m.eid)
	h.shadowGoneAll(m.eid) // retract any cross-seam shadow of it
	radius := blastRadius
	if m.charged { // Creeper.explodeCreeper: a charged creeper blasts at twice the radius
		radius = blastRadius * 2
	}
	power := float64(radius)
	if !h.rules.MobGriefing {
		radius = 0 // gamerule: creepers hurt but leave the terrain alone
	}
	h.blastChargedCreeper, h.blastSkullDropped = m.charged, false
	h.explodeBy(players, m.dim, m.x, m.y+0.5, m.z, radius, power, blastMob, mobDisplayName(m.etype), withHiveRelease(), withBlastCause(m.eid, true))
	h.blastChargedCreeper = false
}

// creeperStateMeta builds set_entity_data for the creeper fuse state (index 16,
// VarInt): -1 idle, +1 primed — what makes the client render the swell + flash.
func creeperStateMeta(eid int32, state int32) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCreeperState)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, state)
	return protocol.AppendU8(b, itemMetaEnd)
}

func dist3(x1, y1, z1, x2, y2, z2 float64) float64 {
	dx, dy, dz := x1-x2, y1-y2, z1-z2
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}
