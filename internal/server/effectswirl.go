package server

import (
	"sort"

	"github.com/tachyne/tachyne-common/protocol"
)

// Effect swirls. A living entity's active effects are synced entity data
// (LivingEntity.updateSynchronizedMobEffectParameters): the particles of
// its visible effects (DATA_EFFECT_PARTICLES) and whether every effect is
// ambient (DATA_EFFECT_AMBIENCE_ID). Every client — its own included —
// draws the coloured swirls from them; without them nobody saw that a
// player or a mob was under a potion at all.

const (
	metaEffectParticles = 10 // DATA_EFFECT_PARTICLES: List<ParticleOptions>
	metaEffectAmbience  = 11 // DATA_EFFECT_AMBIENCE_ID: BOOLEAN
	metaTypeParticles   = 18 // EntityDataSerializers.PARTICLES (canonical 770)

	particleEntityEffect = 20 // canonical 770 particle ids
	ambientSwirlAlpha    = 38 // MobEffect.AMBIENT_ALPHA: floor(38.25)
)

// effectColors are the MobEffect colours, by effect id (the registry order).
var effectColors = [...]int32{
	3402751, 9154528, 14270531, 4866583, 16762624, 16262179, 11101546, 16646020,
	5578058, 13458603, 9520880, 16750848, 10017472, 16185078, 2039587, 12779366,
	5797459, 4738376, 8889187, 7561558, 16284963, 2445989, 16262179, 9740385,
	13565951, 5882118, 12624973, 15978425, 1950417, 8954814, 745784, 4521796,
	2696993, 1484454, 14565464, 12438015, 7891290, 10092451, 9214860, 65518,
}

// effectSwirlParticle: the effects whose particle is not the coloured
// entity_effect (canonical particle ids; they carry no payload).
var effectSwirlParticle = map[int32]int32{
	effTrialOmen: 111, effRaidOmen: 110, effWindCharged: 24,
	effWeaving: 50, effOozing: 49, effInfested: 32,
}

// effectSwirlMeta is the two fields for an entity's current effects.
func effectSwirlMeta(eid int32, effs map[int32]*activeEffect) []byte {
	ids := make([]int32, 0, len(effs))
	allAmbient := true
	for id, e := range effs {
		if !e.ambient {
			allAmbient = false
		}
		if !e.noParticles { // MobEffectInstance.isVisible
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaEffectParticles)
	b = protocol.AppendVarInt(b, metaTypeParticles)
	b = protocol.AppendVarInt(b, int32(len(ids)))
	for _, id := range ids {
		if p, ok := effectSwirlParticle[id]; ok {
			b = protocol.AppendVarInt(b, p)
			continue
		}
		var color int32
		if int(id) < len(effectColors) {
			color = effectColors[id]
		}
		alpha := uint32(255)
		if effs[id].ambient {
			alpha = ambientSwirlAlpha
		}
		b = protocol.AppendVarInt(b, particleEntityEffect)
		b = protocol.AppendI32(b, int32(alpha<<24|uint32(color)&0xffffff))
	}
	b = protocol.AppendU8(b, metaEffectAmbience)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, allAmbient)
	return protocol.AppendU8(b, itemMetaEnd)
}

// syncPlayerSwirls sends a player's swirls to everyone near and to the
// player's own client.
func (h *hub) syncPlayerSwirls(players map[int32]*tracked, t *tracked) {
	ev := metaEv(effectSwirlMeta(t.p.eid, t.effects))
	h.toNearbyEv(players, t.dim, t.x, t.z, ev)
	t.p.trySendEv(ev)
}

// syncMobSwirls sends a mob's swirls to those tracking it.
func (h *hub) syncMobSwirls(players map[int32]*tracked, m *mob) {
	h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(effectSwirlMeta(m.eid, m.effects)))
}
