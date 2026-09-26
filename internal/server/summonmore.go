package server

import (
	"encoding/binary"
	"strings"
)

// SummonCommand for the entities that are not mobs: EntityType.create
// makes each with its defaults and places it exactly where it was asked —
// no hop for a TNT charge (the constructor that hops is the block's), an
// eighty-tick fuse, a boat or cart off any rail or water, a projectile at
// rest that falls (or, a fireball, hangs), a lightning bolt that strikes.

// summonProjectiles are the thrown and shot entities /summon places at rest.
var summonProjectiles = entitySet("arrow", "spectral_arrow", "trident", "snowball", "egg",
	"fireball", "small_fireball", "dragon_fireball", "wither_skull", "wind_charge", "breeze_wind_charge")

// summonNonLiving names the non-living entity types /summon can make.
func summonNonLiving(name string) (int, bool) {
	name = strings.TrimPrefix(name, "minecraft:")
	et, ok := entityByName[name]
	if !ok {
		return 0, false
	}
	switch {
	case cartTypes[et], summonProjectiles[et]:
		return et, true
	case et == entityTNT, et == entityEndCrystal, et == entityLightning, et == entityFirework:
		return et, true
	}
	for _, b := range boatEntities {
		if b == et {
			return et, true
		}
	}
	return 0, false
}

// summonNonLivingAt places one; it reports whether e named such a type.
func (h *hub) summonNonLivingAt(players map[int32]*tracked, e evSummon) bool {
	switch et := e.etype; {
	case cartTypes[et] || isBoatType(et):
		h.addVehicle(players, e.dim, et, e.x, e.y, e.z, 0)
	case summonProjectiles[et]:
		h.launchProjectileIn(players, et, e.dim, e.x, e.y, e.z, 0, 0, 0)
	case et == entityTNT:
		pt := &primedTNT{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z, fuse: tntDefaultFuse}
		h.tnt = append(h.tnt, pt)
		h.showPrimedTNT(players, pt)
	case et == entityEndCrystal:
		c := &crystal{eid: h.allocEID(), dim: e.dim, x: e.x, y: e.y, z: e.z}
		binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
		h.crystals[c.eid] = c
		h.toDimEv(players, e.dim, entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
	case et == entityLightning:
		h.strikeLightning(players, e.dim, e.x, e.y, e.z, false)
	case et == entityFirework:
		h.spawnRocket(players, e.dim, e.x, e.y, e.z, 0, invStack{})
	default:
		return false
	}
	return true
}

// tntDefaultFuse is PrimedTnt's DEFAULT_FUSE_TIME.
const tntDefaultFuse = 80

// isBoatType reports a boat, chest boat or raft entity.
func isBoatType(et int) bool {
	for _, b := range boatEntities {
		if b == et {
			return true
		}
	}
	return false
}
