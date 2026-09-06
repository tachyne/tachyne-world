package server

import (
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// Mob visual variants — the roster species whose look is a synced variant
// rather than a fixed model. Each is rolled at spawn the way vanilla's
// finalizeSpawn does, inherited on breeding where getBreedOffspring does,
// synced as the species' own entity-data entry, and persisted (mobstore).
//
// HOLDER variants (frog, wolf, cat, pig, cow, chicken) are Holder<XVariant>
// entries whose wire value is the registry id in the order the gateways
// declare the registry (tachyne-common registries_gen.go, the same list for
// every served version) — so the ids here are looked up from that list at
// init rather than assumed. Their spawn rule is VariantUtils/PriorityProvider
// selection over the registry's SpawnPrioritySelectors: the highest priority
// whose condition holds wins, ties broken at random, priority-0 fallbacks
// last.
//
//   - Frog (FrogVariants): #spawns_cold_variant_frogs → cold,
//     #spawns_warm_variant_frogs → warm, else temperate.
//   - Wolf (WolfVariants): one biome (or biome tag) per coat at priority 1 —
//     spotted/#is_savanna, snowy/grove, black/old_growth_pine_taiga,
//     ashen/snowy_taiga, rusty/#is_jungle, woods/forest,
//     chestnut/old_growth_spruce_taiga, striped/#is_badlands — pale as the
//     fallback. The first wolf of a pack rolls; the pack shares (WolfPackData).
//   - Cat (CatVariants): ten coats are unconditional fallbacks; all_black is
//     priority 1 inside a #cats_spawn_as_black structure (the swamp hut) and
//     otherwise joins the random pool only when the moon is ≥0.9 bright (the
//     full moon).
//   - Pig/cow/chicken (PigVariants/CowVariants/ChickenVariants,
//     TemperatureVariants): #spawns_warm_variant_farm_animals → warm,
//     #spawns_cold_variant_farm_animals → cold, else temperate.
//
// INT variants are plain VarInt entries:
//
//   - Axolotl (Axolotl.finalizeSpawn): one in 1200 blue, else one of the
//     four common colours; a bred one takes a parent's, with the same
//     mutation.
//   - Horse (Horse.finalizeSpawn): colour | markings<<8, colour shared by the
//     herd (HorseGroupData), markings rolled per horse; a foal takes each
//     parent's colour 4/9 and a fresh one 1/9, markings 2/5 : 2/5 : 1/5.
//   - Llama + trader llama (Llama.finalizeSpawn): one of four coats shared by
//     the group (LlamaGroupData); strength 1+rand(3), or 1+rand(5) with 4%
//     odds (setRandomStrength). A cria takes a parent's coat and
//     1+rand(max(parents' strength)), +1 with 3% odds.
//   - Parrot (Parrot.finalizeSpawn): one of five at random; parrots don't
//     breed.
//   - Rabbit (Rabbit.getRandomRabbitVariant): #spawns_white_rabbits → white
//     80% / white_splotched 20%; #spawns_gold_rabbits → gold; else brown 50% /
//     salt 40% / black 10%; shared by the group. A kit re-rolls its biome 1
//     in 20, else takes a parent's.
//   - Fox (Fox.Variant.byBiome): #spawns_snow_foxes → snow, else red; shared
//     by the group; a kit takes a parent's.
//   - Mooshroom: red (0); brown comes only from lightning.
//
// Biome-tag membership is the 1.21.11 data (the engine's canonical version).
//
// The gateway session shifts every index ≥17 up one for 26.2 clients and
// renumbers the holder serializers (tachyne-common FixVariantMeta /
// ShiftAgeableMobMeta).

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

// Temperature variants (pig/cow/chicken) in registry order: cold, temperate,
// warm — the same order the frog registry uses.
const (
	tempCold = iota
	tempTemperate
	tempWarm
)

// Horse colours (Horse.Variant) and markings (Horse.Markings), packed as
// colour | markings<<8 in the synced INT.
const (
	horseWhite = iota
	horseCreamy
	horseChestnut
	horseBrown
	horseBlack
	horseGray
	horseDarkBrown
	horseColours = 7
)

const (
	horseMarkNone = iota
	horseMarkWhite
	horseMarkWhiteField
	horseMarkWhiteDots
	horseMarkBlackDots
	horseMarkings = 5
)

// Llama coats (Llama.Variant).
const (
	llamaCreamy = iota
	llamaWhite
	llamaBrown
	llamaGray
	llamaCoats = 4
)

// Parrot colours (Parrot.Variant): red_blue, blue, green, yellow_blue, gray.
const parrotColours = 5

// Rabbit types (Rabbit.Variant); evil (99) is the killer bunny, never rolled.
const (
	rabbitBrown = iota
	rabbitWhite
	rabbitBlack
	rabbitWhiteSplotched
	rabbitGold
	rabbitSalt
	rabbitEvil = 99
)

// Fox types (Fox.Variant).
const (
	foxRed = iota
	foxSnow
)

// Mooshroom types (MushroomCow.Variant).
const (
	mooshroomRed = iota
	mooshroomBrown
)

const (
	metaIndexVariant       = 17 // frog/axolotl/rabbit/fox/mooshroom/cow/chicken: the first own field
	metaIndexPigVariant    = 18
	metaIndexHorseVariant  = 18
	metaIndexCatVariant    = 19
	metaIndexParrotVariant = 19
	metaIndexLlamaStrength = 19
	metaIndexLlamaVariant  = 20
	metaIndexWolfVariant   = 22

	metaTypeFrogVariant    = protocol.FrogVariantSerializer770
	metaTypeWolfVariant    = protocol.WolfVariantSerializer770
	metaTypeCatVariant     = protocol.CatVariantSerializer770
	metaTypePigVariant     = protocol.PigVariantSerializer770
	metaTypeCowVariant     = protocol.CowVariantSerializer770
	metaTypeChickenVariant = protocol.ChickenVariantSerializer770
	metaTypeVarIntFor      = 1 // INT
	axolotlBlueChance      = 1200
)

// Wolf and cat coats by registry NAME; the wire id is the name's index in the
// gateways' registry list, resolved once at init (wolfVariantID/catVariantID).
const (
	wolfPale     = "minecraft:pale"
	wolfSpotted  = "minecraft:spotted"
	wolfSnowy    = "minecraft:snowy"
	wolfBlack    = "minecraft:black"
	wolfAshen    = "minecraft:ashen"
	wolfRusty    = "minecraft:rusty"
	wolfWoods    = "minecraft:woods"
	wolfChestnut = "minecraft:chestnut"
	wolfStriped  = "minecraft:striped"

	catAllBlack = "minecraft:all_black"
)

var (
	wolfVariantID = registryIDs("minecraft:wolf_variant")
	catVariantID  = registryIDs("minecraft:cat_variant")
	// catCommonCoats are the ten unconditional (priority-0 fallback) cat
	// coats: every registry entry but all_black.
	catCommonCoats = func() []int32 {
		var ids []int32
		for name, id := range catVariantID {
			if name != catAllBlack {
				ids = append(ids, id)
			}
		}
		sortInt32s(ids)
		return ids
	}()
)

// registryIDs maps a synced registry's entry names to their wire ids — the
// entry's position in the list the gateways send in Configuration.
func registryIDs(regID string) map[string]int32 {
	out := map[string]int32{}
	for _, reg := range protocol.SyncedRegistries {
		if reg.ID != regID {
			continue
		}
		for i, name := range reg.Entries {
			out[name] = int32(i)
		}
	}
	return out
}

func sortInt32s(a []int32) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

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
	// coldFarmBiomes is #spawns_cold_variant_farm_animals beyond the frog's
	// cold list (which it includes, plus #is_end): the cold oceans, every
	// taiga, the windswept family and stony peaks.
	coldFarmBiomes = map[string]bool{
		"minecraft:cold_ocean": true, "minecraft:deep_cold_ocean": true, "minecraft:old_growth_pine_taiga": true,
		"minecraft:old_growth_spruce_taiga": true, "minecraft:taiga": true, "minecraft:windswept_forest": true,
		"minecraft:windswept_gravelly_hills": true, "minecraft:windswept_hills": true, "minecraft:stony_peaks": true,
	}
	// warmFarmBiomes is #spawns_warm_variant_farm_animals beyond the frog's
	// warm list (which it includes, plus the jungle/savanna/badlands/nether
	// tags): the lukewarm oceans.
	warmFarmBiomes = map[string]bool{
		"minecraft:deep_lukewarm_ocean": true, "minecraft:lukewarm_ocean": true,
	}
	// snowyAnimalBiomes is #spawns_snow_foxes, identical to
	// #spawns_white_rabbits.
	snowyAnimalBiomes = map[string]bool{
		"minecraft:snowy_plains": true, "minecraft:ice_spikes": true, "minecraft:frozen_ocean": true,
		"minecraft:snowy_taiga": true, "minecraft:frozen_river": true, "minecraft:snowy_beach": true,
		"minecraft:frozen_peaks": true, "minecraft:jagged_peaks": true, "minecraft:snowy_slopes": true,
		"minecraft:grove": true,
	}
)

// frogVariantFor is the spawn-condition priority order: cold and warm biome
// tags at priority 1, temperate as the priority-0 fallback. The End is in
// the cold tag, the Nether in the warm one; jungles, savannas and badlands
// come in through their family tags.
func frogVariantFor(dim int, biome string) int32 {
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

// farmVariantFor is the pig/cow/chicken temperature rule: the farm-animal
// tags are supersets of the frog tags (same End/Nether membership).
func farmVariantFor(dim int, biome string) int32 {
	switch frogVariantFor(dim, biome) {
	case frogCold:
		return tempCold
	case frogWarm:
		return tempWarm
	}
	if coldFarmBiomes[biome] {
		return tempCold
	}
	if warmFarmBiomes[biome] {
		return tempWarm
	}
	return tempTemperate
}

// wolfVariantFor is WolfVariants' selector table: each coat names one biome
// or biome tag at priority 1; pale is the priority-0 fallback. The biomes
// are disjoint, so at most one priority-1 entry matches.
func wolfVariantFor(biome string) int32 {
	name := wolfPale
	switch {
	case isSavannaBiome(biome):
		name = wolfSpotted
	case biome == "minecraft:grove":
		name = wolfSnowy
	case biome == "minecraft:old_growth_pine_taiga":
		name = wolfBlack
	case biome == "minecraft:snowy_taiga":
		name = wolfAshen
	case isJungleBiome(biome):
		name = wolfRusty
	case biome == "minecraft:forest":
		name = wolfWoods
	case biome == "minecraft:old_growth_spruce_taiga":
		name = wolfChestnut
	case isBadlandsBiome(biome):
		name = wolfStriped
	}
	return wolfVariantID[name]
}

// rabbitVariantFor is Rabbit.getRandomRabbitVariant with its one
// hundred-sided roll.
func rabbitVariantFor(biome string, roll int) int32 {
	if snowyAnimalBiomes[biome] {
		if roll < 80 {
			return rabbitWhite
		}
		return rabbitWhiteSplotched
	}
	if biome == "minecraft:desert" {
		return rabbitGold
	}
	switch {
	case roll < 50:
		return rabbitBrown
	case roll < 90:
		return rabbitSalt
	}
	return rabbitBlack
}

// foxVariantFor is Fox.Variant.byBiome.
func foxVariantFor(biome string) int32 {
	if snowyAnimalBiomes[biome] {
		return foxSnow
	}
	return foxRed
}

// catSpawnsAsBlack is the StructureCheck on #cats_spawn_as_black (the swamp
// hut) — the priority-1 all_black selector. The generator can only be asked
// about the structures it lays down (structureAt); no swamp huts yet, so
// this holds only once they exist.
func (h *hub) catSpawnsAsBlack(dim int, x, z int) bool {
	return dim == dimOverworld && h.world != nil && h.structureAt(x, z) == "swamp_hut"
}

// catVariantRoll is CatVariants' selection: all_black outright in a swamp
// hut; otherwise a uniform pick from the ten common coats, joined by
// all_black when the moon is at least 0.9 bright (MoonBrightnessCheck —
// only the full moon's 1.0 qualifies).
func (h *hub) catVariantRoll(m *mob) int32 {
	if h.catSpawnsAsBlack(m.dim, floorInt(m.x), floorInt(m.z)) {
		return catVariantID[catAllBlack]
	}
	pool := catCommonCoats
	if moonBrightness(h.dayTime.Load()) >= 0.9 {
		pool = append(append([]int32{}, pool...), catVariantID[catAllBlack])
	}
	return pool[h.rng.Intn(len(pool))]
}

// llamaStrengthRoll is Llama.setRandomStrength: 1+rand(3), or 1+rand(5)
// with 4% odds — the rare strong llama.
func (h *hub) llamaStrengthRoll() int8 {
	n := 3
	if h.rng.Float32() < 0.04 {
		n = 5
	}
	return int8(1 + h.rng.Intn(n))
}

// horseVariant packs a colour and markings the way Horse.setVariantAndMarkings
// does.
func horseVariant(colour, markings int32) int32 { return colour&0xff | markings<<8&0xff00 }

// horseColour and horseMarks unpack a horse's synced variant.
func horseColour(v int32) int32 { return v & 0xff }
func horseMarks(v int32) int32  { return (v & 0xff00) >> 8 }

// spawnGroup is vanilla's SpawnGroupData for the species that share a
// variant across a natural pack (WolfPackData, HorseGroupData's colour,
// LlamaGroupData, RabbitGroupData, FoxGroupData): the first member rolls,
// the rest copy. A group-spawning loop scopes one with withSpawnGroup.
type spawnGroup struct {
	etype   int
	set     bool
	variant int32
}

// withSpawnGroup runs fn with a fresh spawn group in force (hub goroutine
// only — a plain field, like spawnCause).
func (h *hub) withSpawnGroup(fn func()) {
	old := h.spawnGroup
	h.spawnGroup = &spawnGroup{}
	fn()
	h.spawnGroup = old
}

// groupVariant returns the in-force group's variant for m's species, rolling
// it with roll for the first member.
func (h *hub) groupVariant(m *mob, roll func() int32) int32 {
	g := h.spawnGroup
	if g == nil {
		return roll()
	}
	if !g.set || g.etype != m.etype {
		g.etype, g.variant, g.set = m.etype, roll(), true
	}
	return g.variant
}

// spawnBiome is the biome under a mob's spawn position.
func (h *hub) spawnBiome(m *mob) string {
	if w := h.worldFor(m.dim); w != nil {
		return w.BiomeAt(floorInt(m.x), floorInt(m.z))
	}
	return "minecraft:plains"
}

// rollVariant sets a freshly spawned mob's variant (spawnMobCause) — the
// species' finalizeSpawn rule. Anything without one keeps variantSet=false
// so the join/reveal paths send nothing.
func (h *hub) rollVariant(m *mob) {
	switch m.etype {
	case entityFrog:
		m.variant = frogVariantFor(m.dim, h.spawnBiome(m))
	case entityAxolotl:
		if h.rng.Intn(axolotlBlueChance) == 0 {
			m.variant = axolotlBlue
		} else {
			m.variant = int32(h.rng.Intn(4)) // getCommonSpawnVariant
		}
	case entityWolf:
		m.variant = h.groupVariant(m, func() int32 { return wolfVariantFor(h.spawnBiome(m)) })
	case entityCat:
		m.variant = h.catVariantRoll(m)
	case entityHorse:
		colour := h.groupVariant(m, func() int32 { return int32(h.rng.Intn(horseColours)) })
		m.variant = horseVariant(colour, int32(h.rng.Intn(horseMarkings)))
	case entityLlama, entityTraderLlama:
		if m.strength == 0 {
			m.strength = h.llamaStrengthRoll()
		}
		m.variant = h.groupVariant(m, func() int32 { return int32(h.rng.Intn(llamaCoats)) })
	case entityParrot:
		m.variant = int32(h.rng.Intn(parrotColours))
	case entityRabbit:
		m.variant = h.groupVariant(m, func() int32 { return rabbitVariantFor(h.spawnBiome(m), h.rng.Intn(100)) })
	case entityFox:
		m.variant = h.groupVariant(m, func() int32 { return foxVariantFor(h.spawnBiome(m)) })
	case entityMooshroom:
		m.variant = mooshroomRed
	case entityPig, entityCow, entityChicken:
		m.variant = farmVariantFor(m.dim, h.spawnBiome(m))
	default:
		return
	}
	m.variantSet = true
}

// pickParent is the random.nextBoolean() parent choice every getBreedOffspring
// makes.
func (h *hub) pickParent(a, b *mob) *mob {
	if h.rng.Intn(2) == 0 {
		return a
	}
	return b
}

// inheritVariant is each species' getBreedOffspring colour rule for a bred
// baby.
func (h *hub) inheritVariant(baby, a, b *mob) {
	switch baby.etype {
	case entityAxolotl:
		// A parent's variant, or the 1-in-1200 blue mutation.
		if h.rng.Intn(axolotlBlueChance) == 0 {
			baby.variant = axolotlBlue
		} else {
			baby.variant = h.pickParent(a, b).variant
		}
	case entityWolf, entityCat, entityFox, entityPig, entityCow, entityChicken:
		baby.variant = h.pickParent(a, b).variant
	case entityLlama, entityTraderLlama:
		// Strength: 1 + rand(max of the parents'), one better 3% of the time.
		n := max(int(a.strength), int(b.strength), 1)
		s := h.rng.Intn(n) + 1
		if h.rng.Float32() < 0.03 {
			s++
		}
		baby.strength = int8(s)
		baby.variant = h.pickParent(a, b).variant
	case entityHorse:
		// Colour 4/9 each parent, 1/9 fresh; markings 2/5 : 2/5 : 1/5.
		var colour, marks int32
		switch n := h.rng.Intn(9); {
		case n < 4:
			colour = horseColour(a.variant)
		case n < 8:
			colour = horseColour(b.variant)
		default:
			colour = int32(h.rng.Intn(horseColours))
		}
		switch n := h.rng.Intn(5); {
		case n < 2:
			marks = horseMarks(a.variant)
		case n < 4:
			marks = horseMarks(b.variant)
		default:
			marks = int32(h.rng.Intn(horseMarkings))
		}
		baby.variant = horseVariant(colour, marks)
	case entityRabbit:
		// 1 in 20 kits re-roll from the biome; the rest take a parent's.
		if h.rng.Intn(20) == 0 {
			baby.variant = rabbitVariantFor(h.spawnBiome(baby), h.rng.Intn(100))
		} else {
			baby.variant = h.pickParent(a, b).variant
		}
	default:
		return
	}
	baby.variantSet = true
}

// variantEntry is one (index, serializer) pair of a species' variant.
type variantEntry struct {
	idx byte
	typ int32
}

// variantEntryFor names the entity-data entry a species' variant rides on;
// ok=false for species without one.
func variantEntryFor(etype int) (variantEntry, bool) {
	switch etype {
	case entityFrog:
		return variantEntry{metaIndexVariant, metaTypeFrogVariant}, true
	case entityAxolotl, entityRabbit, entityFox, entityMooshroom:
		return variantEntry{metaIndexVariant, metaTypeVarIntFor}, true
	case entityWolf:
		return variantEntry{metaIndexWolfVariant, metaTypeWolfVariant}, true
	case entityCat:
		return variantEntry{metaIndexCatVariant, metaTypeCatVariant}, true
	case entityHorse:
		return variantEntry{metaIndexHorseVariant, metaTypeVarIntFor}, true
	case entityLlama, entityTraderLlama:
		return variantEntry{metaIndexLlamaVariant, metaTypeVarIntFor}, true
	case entityParrot:
		return variantEntry{metaIndexParrotVariant, metaTypeVarIntFor}, true
	case entityPig:
		return variantEntry{metaIndexPigVariant, metaTypePigVariant}, true
	case entityCow:
		return variantEntry{metaIndexVariant, metaTypeCowVariant}, true
	case entityChicken:
		return variantEntry{metaIndexVariant, metaTypeChickenVariant}, true
	}
	return variantEntry{}, false
}

// variantMeta is the set_entity_data body carrying a mob's variant, or nil
// when the species has none. A llama's body also carries its strength (the
// INT just before the variant), which the client's chest screen reads.
func variantMeta(m *mob) []byte {
	if m.etype == entityVillager || m.etype == entityZombieVillager {
		return villagerDataMeta(m) // clothes: type + profession + tier
	}
	if !m.variantSet {
		return nil
	}
	e, ok := variantEntryFor(m.etype)
	if !ok {
		return nil
	}
	b := protocol.AppendVarInt(nil, m.eid)
	if m.etype == entityLlama || m.etype == entityTraderLlama {
		b = protocol.AppendU8(b, metaIndexLlamaStrength)
		b = protocol.AppendVarInt(b, metaTypeVarIntFor)
		b = protocol.AppendVarInt(b, int32(m.strength))
	}
	b = protocol.AppendU8(b, e.idx)
	b = protocol.AppendVarInt(b, e.typ)
	b = protocol.AppendVarInt(b, m.variant)
	return protocol.AppendU8(b, itemMetaEnd)
}

// packVariant is the store form: variant + 1, 0 = none.
func packVariant(m *mob) int32 {
	if !m.variantSet {
		return 0
	}
	return m.variant + 1
}
