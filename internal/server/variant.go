package server

import (
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// Frog and axolotl colour variants — the two roster species whose look is a
// synced variant rather than a fixed model.
//
// Frog (FrogVariants + VariantUtils.selectVariantToSpawn): the variant is
// chosen by the spawn biome's tags — #spawns_cold_variant_frogs → cold,
// #spawns_warm_variant_frogs → warm, anything else temperate — and is a
// Holder<FrogVariant> at entity-data index 17 (the FROG_VARIANT serializer);
// the value is the frog_variant registry id in the order the engine declares
// (cold 0, temperate 1, warm 2). A tadpole grows into the variant of the
// biome it grows up in.
//
// Axolotl (Axolotl.finalizeSpawn): one in 1200 is blue, the rest one of the
// four common colours at random; a bred one takes a parent's colour, with
// the same 1-in-1200 blue mutation. A plain INT at index 17 (lucy 0, wild 1,
// gold 2, cyan 3, blue 4).
//
// The gateway session shifts index 17 → 18 and renumbers the frog serializer
// for 26.2 clients (tachyne-common FixFrogMeta / ShiftAgeableMobMeta).

const (
	frogCold = iota
	frogTemperate
	frogWarm
)

const (
	axolotlLucy = iota
	axolotlWild
	axolotlGold
	axolotlCyan
	axolotlBlue
)

const (
	metaIndexVariant    = 17
	metaTypeFrogVariant = protocol.FrogVariantSerializer770
	metaTypeVarIntFor   = 1 // INT
	axolotlBlueChance   = 1200
)

var (
	coldFrogBiomes = map[string]bool{
		"minecraft:snowy_plains": true, "minecraft:ice_spikes": true, "minecraft:frozen_peaks": true,
		"minecraft:jagged_peaks": true, "minecraft:snowy_slopes": true, "minecraft:frozen_ocean": true,
		"minecraft:deep_frozen_ocean": true, "minecraft:grove": true, "minecraft:deep_dark": true,
		"minecraft:frozen_river": true, "minecraft:snowy_taiga": true, "minecraft:snowy_beach": true,
	}
	warmFrogBiomes = map[string]bool{
		"minecraft:desert": true, "minecraft:warm_ocean": true, "minecraft:mangrove_swamp": true,
	}
)

// frogVariantFor is the spawn-condition priority order: cold and warm biome
// tags at priority 1, temperate as the priority-0 fallback. The End is in
// the cold tag, the Nether in the warm one; jungles, savannas and badlands
// come in through their family tags.
func frogVariantFor(dim int, biome string) int8 {
	switch dim {
	case dimNether:
		return frogWarm
	case dimEnd:
		return frogCold
	}
	if coldFrogBiomes[biome] {
		return frogCold
	}
	if warmFrogBiomes[biome] || isJungleBiome(biome) || isSavannaBiome(biome) || isBadlandsBiome(biome) ||
		strings.Contains(biome, "bamboo_jungle") {
		return frogWarm
	}
	return frogTemperate
}

// rollVariant sets a freshly spawned mob's variant (spawnMobCause). Only the
// two species that have one; anything else keeps variantSet=false so the
// join/reveal paths send nothing.
func (h *hub) rollVariant(m *mob) {
	switch m.etype {
	case entityFrog:
		m.variant = frogVariantFor(m.dim, h.worldFor(m.dim).BiomeAt(int(m.x), int(m.z)))
	case entityAxolotl:
		if h.rng.Intn(axolotlBlueChance) == 0 {
			m.variant = axolotlBlue
		} else {
			m.variant = int8(h.rng.Intn(4)) // getCommonSpawnVariant
		}
	default:
		return
	}
	m.variantSet = true
}

// inheritVariant is Axolotl.getBreedOffspring's colour rule for a bred
// baby: a parent's variant, or the 1-in-1200 blue mutation.
func (h *hub) inheritVariant(baby, a, b *mob) {
	if baby.etype != entityAxolotl {
		return
	}
	if h.rng.Intn(axolotlBlueChance) == 0 {
		baby.variant = axolotlBlue
	} else if h.rng.Intn(2) == 0 {
		baby.variant = a.variant
	} else {
		baby.variant = b.variant
	}
	baby.variantSet = true
}

// variantMeta is the set_entity_data body carrying a mob's variant, or nil
// when the species has none.
func variantMeta(m *mob) []byte {
	if m.etype == entityVillager || m.etype == entityZombieVillager {
		return villagerDataMeta(m) // clothes: type + profession + tier
	}
	if !m.variantSet {
		return nil
	}
	var typ int32
	switch m.etype {
	case entityFrog:
		typ = metaTypeFrogVariant
	case entityAxolotl:
		typ = metaTypeVarIntFor
	default:
		return nil
	}
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexVariant)
	b = protocol.AppendVarInt(b, typ)
	b = protocol.AppendVarInt(b, int32(m.variant))
	return protocol.AppendU8(b, itemMetaEnd)
}

// packVariant is the store form: variant + 1, 0 = none.
func packVariant(m *mob) int8 {
	if !m.variantSet {
		return 0
	}
	return m.variant + 1
}
