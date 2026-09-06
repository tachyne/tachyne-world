package server

import (
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// Villager appearance — vanilla's VillagerData (the type of the biome a
// villager was born in, its profession and its trade tier), synced as entity
// metadata. Until this every villager rendered as an unemployed plains
// villager whatever it traded.

const (
	metaIndexVillagerData       = 18 // Villager: Entity 8 + LivingEntity 7 + Mob 1 + AgeableMob 1 + AbstractVillager 1
	metaIndexZombieConverting   = 19 // ZombieVillager: after Zombie's 16-18
	metaIndexZombieVillagerData = 20
	metaTypeVillagerData        = protocol.VillagerDataSerializer770
)

// professionRegistryID maps professionNames' order onto vanilla's
// villager_profession registry ids (identical through 26.2).
var professionRegistryID = map[string]int32{
	"none": 0, "armorer": 1, "butcher": 2, "cartographer": 3, "cleric": 4, "farmer": 5,
	"fisherman": 6, "fletcher": 7, "leatherworker": 8, "librarian": 9, "mason": 10,
	"nitwit": 11, "shepherd": 12, "toolsmith": 13, "weaponsmith": 14,
}

// Villager types in vanilla registry order.
const (
	villagerTypeDesert = iota
	villagerTypeJungle
	villagerTypePlains
	villagerTypeSavanna
	villagerTypeSnow
	villagerTypeSwamp
	villagerTypeTaiga
)

// villagerTypeForBiome is VillagerType.byBiome: the biome a villager is born
// in dresses it.
func villagerTypeForBiome(biome string) int8 {
	b := strings.TrimPrefix(biome, "minecraft:")
	switch {
	case strings.Contains(b, "desert"):
		return villagerTypeDesert
	case strings.Contains(b, "jungle"), strings.Contains(b, "bamboo"):
		return villagerTypeJungle
	case strings.Contains(b, "savanna"):
		return villagerTypeSavanna
	case strings.HasPrefix(b, "snowy"), strings.HasPrefix(b, "frozen"), b == "ice_spikes", b == "grove":
		return villagerTypeSnow
	case strings.Contains(b, "swamp"), strings.Contains(b, "mangrove"):
		return villagerTypeSwamp
	case strings.Contains(b, "taiga"):
		return villagerTypeTaiga
	}
	return villagerTypePlains
}

// villagerType settles a villager's type from its birthplace the first time
// it is needed (persisted with the mob afterwards).
func (h *hub) villagerType(m *mob) int8 {
	if !m.variantSet {
		if w := h.worldFor(m.dim); w != nil {
			m.variant = int32(villagerTypeForBiome(w.BiomeAt(floorInt(m.x), floorInt(m.z))))
		} else {
			m.variant = villagerTypePlains
		}
		m.variantSet = true
	}
	return int8(m.variant)
}

// villagerDataMeta builds the VillagerData entry for a villager or a zombie
// villager (the latter keeps the villager's data through the infection).
func villagerDataMeta(m *mob) []byte {
	idx := metaIndexVillagerData
	if m.etype == entityZombieVillager {
		idx = metaIndexZombieVillagerData
	}
	vtype := int32(villagerTypePlains)
	if m.variantSet {
		vtype = int32(m.variant)
	}
	var prof int32
	if m.profession >= 0 && m.profession < len(professionNames) {
		prof = professionRegistryID[professionNames[m.profession]]
	}
	level := int32(m.tradeLevel)
	if level < 1 {
		level = 1
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, byte(idx))
	b = protocol.AppendVarInt(b, metaTypeVillagerData)
	b = protocol.AppendVarInt(b, vtype)
	b = protocol.AppendVarInt(b, prof)
	b = protocol.AppendVarInt(b, level)
	return protocol.AppendU8(b, itemMetaEnd)
}

// sendVillagerData re-asserts a villager's clothes after a profession or
// tier change (spawn, join and dimension changes send it with the variant).
func (h *hub) sendVillagerData(players map[int32]*tracked, m *mob) {
	h.villagerType(m)
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(villagerDataMeta(m)))
}
