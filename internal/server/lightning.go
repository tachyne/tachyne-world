package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
)

// What a lightning bolt does to the mobs it hits (their thunderHit), and
// the Channeling trident that calls one down.

const metaIndexCreeperPowered = 17 // Creeper DATA_IS_POWERED (after SWELL_DIR at 16)

// lightningFireSecs is Entity.thunderHit's igniteForSeconds(8).
const lightningFireSecs = 8

// lightningTransforms is the struck mob's thunderHit: a creeper charges, a
// pig becomes a zombified piglin, a villager a witch.
func (h *hub) lightningTransforms(players map[int32]*tracked, m *mob) {
	switch m.etype {
	case entityCreeper:
		if !m.charged {
			m.charged = true
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(creeperPoweredMeta(m.eid, true)))
		}
	case entityPig:
		h.convertMob(players, m, entityZombifiedPiglin)
	case entityVillager:
		if h.npcs != nil && h.npcs[m.eid] != nil {
			return
		}
		h.convertMob(players, m, entityWitch)
	case entityMooshroom: // MushroomCow.thunderHit: red and brown swap
		if m.variant == mooshroomRed {
			m.variant = mooshroomBrown
		} else {
			m.variant = mooshroomRed
		}
		m.variantSet = true
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(variantMeta(m)))
	case entityTurtle: // Turtle.thunderHit: the bolt is always fatal
		h.hurtMobOf(players, m, math.MaxFloat32, dtLightningBolt)
	}
}

// creeperPoweredMeta is the charged creeper's aura flag.
func creeperPoweredMeta(eid int32, on bool) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCreeperPowered)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, on)
	return protocol.AppendU8(b, itemMetaEnd)
}

// skyOpen reports whether a cell can see the sky (LevelReader.canSeeSky):
// nothing but air between it and the top of the world.
func (h *hub) skyOpen(dim int, x, y, z float64) bool {
	if dim != dimOverworld {
		return false // the Nether and End have no weather
	}
	return floorInt(y) >= h.world.SurfaceFeet(floorInt(x), floorInt(z))
}

// channelingStrike is ThrownTrident's Channeling: in a thunderstorm, a hit
// under open sky calls a bolt down on the spot (the trident's own thunder
// sound goes with it).
func (h *hub) channelingStrike(players map[int32]*tracked, a *arrowEntity, dim int, x, y, z float64, struck *mob) {
	if !a.channeling || !h.thundering || !h.skyOpen(dim, x, y, z) {
		return
	}
	h.strikeLightning(players, x, y, z, false)
	h.playSound(players, "minecraft:item.trident.thunder", sndNeutral, x, y, z, 5, 1)
	if t := players[a.shooter]; t != nil && struck != nil {
		// "Very Very Frightening": the bolt's victim, by species.
		h.advance(players, t, "channeled_lightning", advMatch{entity: advEntityName[struck.etype]})
	}
}
