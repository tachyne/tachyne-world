package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// Creepers — the walking bomb. They stalk a player silently, and once close
// enough start the fuse: the client renders the classic swell from the state
// metadata, and 1.5 s later the hub carves the crater, damages everything in
// range and despawns the creeper. Backing off far enough defuses it (vanilla).

const (
	creeperHealth      = 20
	creeperIgniteRange = 3.0 // start the fuse within this horizontal distance
	creeperIgniteY     = 2.0 // …and this vertical tolerance
	creeperCancelRange = 7.0 // target escaping past this defuses (vanilla SwellGoal: 7)
	creeperFuseTicks   = 30  // 1.5 s of swelling before the bang

	metaIndexCreeperState = 16 // creeper metadata: fuse state (-1 idle, +1 primed)

	blastRadius     = 3   // crater: blocks destroyed within this sphere
	blastRange      = 6.0 // entity damage falls off linearly to zero here
	blastMaxDamage  = 30  // point-blank damage before armor (fatal unarmored — vanilla-ish)
	blastDropChance = 30  // % of destroyed blocks that drop their loot (vanilla ~1/power)
)

var (
	entityCreeper = entityID("creeper")     // minecraft:entity_type "creeper" (1.21.5)
	itemGunpowder = itemByName["gunpowder"] // drops
	itemBone      = itemByName["bone"]      // (skeleton drops live in combat.go's loot table)
)

// creeperBehavior stalks like a zombie, but holds perfectly still while the
// fuse burns (a creeper commits to the bang rather than chasing mid-swell).
type creeperBehavior struct{}

func (creeperBehavior) name() string { return "creeper" }
func (creeperBehavior) steer(h *hub, m *mob) (float64, float64) {
	if m.fuse > 0 {
		return 0, 0
	}
	return hostileBehavior{}.steer(h, m)
}

// creeperFuse runs at the mob-update cadence: ignite in range, tick the fuse,
// defuse if the target escapes, explode at zero.
func (h *hub) creeperFuse(players map[int32]*tracked, m *mob) {
	t := h.nearestHuntable(players, m.dim, m.x, m.z, creeperCancelRange)
	if m.fuse == 0 {
		if t == nil || math.Abs(t.y-m.y) > creeperIgniteY {
			return
		}
		if dx, dz := t.x-m.x, t.z-m.z; dx*dx+dz*dz > creeperIgniteRange*creeperIgniteRange {
			return
		}
		m.fuse = creeperFuseTicks
		m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi) // stare down the target
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(creeperStateMeta(m.eid, 1)))
		h.playSound(players, "minecraft:entity.creeper.primed", sndHostile, m.x, m.y, m.z, 1, 1)
		return
	}
	if t == nil { // escaped — stand down
		m.fuse = 0
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(creeperStateMeta(m.eid, -1)))
		return
	}
	if m.fuse -= mobMoveInterval; m.fuse <= 0 {
		h.explodeCreeper(players, m)
	}
}

// explodeCreeper removes the creeper and detonates: crater the terrain,
// damage + knock back every entity in range, spill any destroyed containers.
func (h *hub) explodeCreeper(players map[int32]*tracked, m *mob) {
	delete(h.mobs, m.eid)
	h.toNearbyEv(players, m.dim, m.x, m.z, entGone(m.eid))
	h.shadowGoneAll(m.eid) // retract any cross-seam shadow of it
	radius := blastRadius
	if !h.rules.MobGriefing {
		radius = 0 // gamerule: creepers hurt but leave the terrain alone
	}
	h.explodeAt(players, m.x, m.y+0.5, m.z, radius, blastMaxDamage)
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
