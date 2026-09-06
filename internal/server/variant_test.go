package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// readMeta decodes a one-entry variant body: eid, index, serializer, value.
func readMeta(t *testing.T, b []byte) (eid int32, idx byte, typ, val int32) {
	t.Helper()
	r := bytes.NewReader(b)
	eid, _ = protocol.ReadVarInt(r)
	idx, _ = r.ReadByte()
	typ, _ = protocol.ReadVarInt(r)
	val, _ = protocol.ReadVarInt(r)
	return
}

// Frog variants follow the spawn biome's tag: cold, warm, or temperate.
func TestFrogVariantByBiome(t *testing.T) {
	cases := map[string]int32{
		"minecraft:snowy_taiga": frogCold, "minecraft:frozen_river": frogCold, "minecraft:deep_dark": frogCold,
		"minecraft:desert": frogWarm, "minecraft:jungle": frogWarm, "minecraft:savanna_plateau": frogWarm,
		"minecraft:badlands": frogWarm, "minecraft:mangrove_swamp": frogWarm,
		"minecraft:swamp": frogTemperate, "minecraft:plains": frogTemperate, "minecraft:river": frogTemperate,
	}
	for biome, want := range cases {
		if got := frogVariantFor(dimOverworld, biome); got != want {
			t.Errorf("%s: variant %d, want %d", biome, got, want)
		}
	}
	if frogVariantFor(dimNether, "minecraft:nether_wastes") != frogWarm || frogVariantFor(dimEnd, "minecraft:the_end") != frogCold {
		t.Error("the Nether is in the warm tag, the End in the cold one")
	}
}

// A spawned frog or axolotl carries a variant; the metadata names it at
// index 17 under the right serializer; other species send nothing.
func TestVariantMetadataAndPersistence(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	frog := h.spawnSpecies(players, entityFrog, 0, 10.5, 70, 10.5)
	if !frog.variantSet {
		t.Fatal("a spawned frog must roll a variant")
	}
	eid, idx, typ, val := readMeta(t, variantMeta(frog))
	if eid != frog.eid || idx != metaIndexVariant {
		t.Fatalf("frog meta eid/index wrong: %d %d", eid, idx)
	}
	if typ != protocol.FrogVariantSerializer770 {
		t.Errorf("frog serializer %d, want %d", typ, protocol.FrogVariantSerializer770)
	}
	if val != int32(frog.variant) {
		t.Errorf("frog value %d, want %d", val, frog.variant)
	}

	ax := h.spawnSpecies(players, entityAxolotl, 0, 10.5, 60, 10.5)
	if !ax.variantSet || ax.variant < axolotlLucy || ax.variant > axolotlBlue {
		t.Fatalf("axolotl variant %d/%v", ax.variant, ax.variantSet)
	}
	if _, _, typ, _ := readMeta(t, variantMeta(ax)); typ != 1 {
		t.Errorf("axolotl serializer %d, want INT (1)", typ)
	}
	cow := h.spawnSpecies(players, entityZoglin, 0, 10.5, 70, 10.5)
	if variantMeta(cow) != nil {
		t.Error("a zoglin has no variant entry")
	}

	// Store round trip keeps the variant, and "unset" stays unset.
	ax.variant = axolotlBlue
	back := &mob{}
	back.etype = entityAxolotl
	if v := packVariant(ax); v != axolotlBlue+1 {
		t.Errorf("packed %d", v)
	} else {
		back.variant, back.variantSet = v-1, true
	}
	if back.variant != axolotlBlue {
		t.Error("blue lost in the store form")
	}
	if packVariant(cow) != 0 {
		t.Error("a species without a variant must pack to 0")
	}
	if int32(int8(back.variant)) != back.variant {
		t.Error("the store form must hold a full int32 (horse colour|markings<<8)")
	}

	// Over many rolls axolotls show every common colour and the rare blue.
	seen := map[int32]int{}
	for i := 0; i < 20000; i++ {
		m := &mob{etype: entityAxolotl}
		h.rollVariant(m)
		seen[m.variant]++
	}
	for v := int32(axolotlLucy); v <= axolotlBlue; v++ {
		if seen[v] == 0 {
			t.Errorf("axolotl variant %d never rolled", v)
		}
	}
	if seen[axolotlBlue] > 60 { // ≈ 20000/1200 ≈ 17 expected
		t.Errorf("blue rolled %d times in 20000, far above 1-in-1200", seen[axolotlBlue])
	}
}

// The temperature-variant tags: cold and warm farm animals by biome (a
// superset of the frog tags), temperate elsewhere; the End is cold, the
// Nether warm.
func TestFarmVariantByBiome(t *testing.T) {
	cases := map[string]int32{
		"minecraft:snowy_plains": tempCold, "minecraft:taiga": tempCold, "minecraft:stony_peaks": tempCold,
		"minecraft:windswept_hills": tempCold, "minecraft:cold_ocean": tempCold, "minecraft:old_growth_pine_taiga": tempCold,
		"minecraft:desert": tempWarm, "minecraft:jungle": tempWarm, "minecraft:savanna": tempWarm,
		"minecraft:eroded_badlands": tempWarm, "minecraft:lukewarm_ocean": tempWarm, "minecraft:mangrove_swamp": tempWarm,
		"minecraft:plains": tempTemperate, "minecraft:forest": tempTemperate, "minecraft:swamp": tempTemperate,
		"minecraft:snowy_taiga": tempCold,
	}
	for biome, want := range cases {
		if got := farmVariantFor(dimOverworld, biome); got != want {
			t.Errorf("%s: %d, want %d", biome, got, want)
		}
	}
	if farmVariantFor(dimNether, "minecraft:nether_wastes") != tempWarm || farmVariantFor(dimEnd, "minecraft:the_end") != tempCold {
		t.Error("the Nether is warm, the End cold")
	}
	// The temperature registries are declared cold, temperate, warm.
	for _, reg := range []string{"minecraft:pig_variant", "minecraft:cow_variant", "minecraft:chicken_variant", "minecraft:frog_variant"} {
		ids := registryIDs(reg)
		if ids["minecraft:cold"] != tempCold || ids["minecraft:temperate"] != tempTemperate || ids["minecraft:warm"] != tempWarm {
			t.Errorf("%s registry order %v", reg, ids)
		}
	}
}

// Wolf coats follow WolfVariants' biome table, pale elsewhere; the ids are
// the registry positions the gateways send.
func TestWolfVariantByBiome(t *testing.T) {
	cases := map[string]string{
		"minecraft:savanna": wolfSpotted, "minecraft:windswept_savanna": wolfSpotted,
		"minecraft:grove": wolfSnowy, "minecraft:old_growth_pine_taiga": wolfBlack,
		"minecraft:snowy_taiga": wolfAshen, "minecraft:bamboo_jungle": wolfRusty,
		"minecraft:forest": wolfWoods, "minecraft:old_growth_spruce_taiga": wolfChestnut,
		"minecraft:wooded_badlands": wolfStriped,
		"minecraft:plains":          wolfPale, "minecraft:taiga": wolfPale, "minecraft:flower_forest": wolfPale,
	}
	for biome, want := range cases {
		if got := wolfVariantFor(biome); got != wolfVariantID[want] {
			t.Errorf("%s: id %d, want %s (%d)", biome, got, want, wolfVariantID[want])
		}
	}
	if len(wolfVariantID) != 9 || wolfVariantID[wolfPale] != 3 || wolfVariantID[wolfAshen] != 0 {
		t.Errorf("wolf_variant registry ids %v", wolfVariantID)
	}
	if len(catVariantID) != 11 || len(catCommonCoats) != 10 || catVariantID[catAllBlack] != 0 {
		t.Errorf("cat_variant registry ids %v / common %v", catVariantID, catCommonCoats)
	}
}

// Rabbit and fox types by biome: snowy biomes give white/splotched rabbits
// and snow foxes, the desert gold rabbits, elsewhere brown/salt/black.
func TestRabbitAndFoxVariantByBiome(t *testing.T) {
	if rabbitVariantFor("minecraft:snowy_plains", 79) != rabbitWhite || rabbitVariantFor("minecraft:grove", 80) != rabbitWhiteSplotched {
		t.Error("snowy rabbits: white below 80, splotched from 80")
	}
	if rabbitVariantFor("minecraft:desert", 0) != rabbitGold || rabbitVariantFor("minecraft:desert", 99) != rabbitGold {
		t.Error("desert rabbits are gold")
	}
	if rabbitVariantFor("minecraft:plains", 49) != rabbitBrown || rabbitVariantFor("minecraft:plains", 50) != rabbitSalt ||
		rabbitVariantFor("minecraft:plains", 89) != rabbitSalt || rabbitVariantFor("minecraft:plains", 90) != rabbitBlack {
		t.Error("temperate rabbits: brown <50, salt <90, black")
	}
	if foxVariantFor("minecraft:snowy_taiga") != foxSnow || foxVariantFor("minecraft:taiga") != foxRed {
		t.Error("foxes: snow in the snowy biomes, red elsewhere")
	}
}

// Every variant species names its own entry: index and serializer per the
// 1.21.5 synched-data layout, llamas with strength alongside.
func TestVariantMetaEntries(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	cases := []struct {
		etype int
		idx   byte
		typ   int32
		max   int32
	}{
		{entityWolf, 22, protocol.WolfVariantSerializer770, 8},
		{entityCat, 19, protocol.CatVariantSerializer770, 10},
		{entityHorse, 18, 1, horseVariant(6, 4)},
		{entityParrot, 19, 1, 4},
		{entityRabbit, 17, 1, 5},
		{entityFox, 17, 1, 1},
		{entityMooshroom, 17, 1, 0},
		{entityPig, 18, protocol.PigVariantSerializer770, 2},
		{entityCow, 17, protocol.CowVariantSerializer770, 2},
		{entityChicken, 17, protocol.ChickenVariantSerializer770, 2},
	}
	for _, c := range cases {
		m := h.spawnSpecies(players, c.etype, 0, 10.5, 70, 10.5)
		if m == nil || !m.variantSet {
			t.Fatalf("%s: no variant rolled", entityNameByID[c.etype])
		}
		eid, idx, typ, val := readMeta(t, variantMeta(m))
		if eid != m.eid || idx != c.idx || typ != c.typ || val != m.variant || val < 0 || val > c.max {
			t.Errorf("%s: eid %d idx %d typ %d val %d (variant %d), want idx %d typ %d ≤ %d",
				entityNameByID[c.etype], eid, idx, typ, val, m.variant, c.idx, c.typ, c.max)
		}
	}
	// A llama's body: strength at 19 then the coat at 20, both INT.
	for _, et := range []int{entityLlama, entityTraderLlama} {
		l := h.spawnSpecies(players, et, 0, 10.5, 70, 10.5)
		if l.strength < 1 || l.strength > 5 || l.variant < 0 || l.variant >= llamaCoats {
			t.Fatalf("llama strength %d variant %d", l.strength, l.variant)
		}
		b := variantMeta(l)
		r := bytes.NewReader(b)
		protocol.ReadVarInt(r)
		idx, _ := r.ReadByte()
		typ, _ := protocol.ReadVarInt(r)
		val, _ := protocol.ReadVarInt(r)
		idx2, _ := r.ReadByte()
		typ2, _ := protocol.ReadVarInt(r)
		val2, _ := protocol.ReadVarInt(r)
		end, _ := r.ReadByte()
		if idx != 19 || typ != 1 || val != int32(l.strength) || idx2 != 20 || typ2 != 1 || val2 != l.variant || end != 0xff {
			t.Errorf("llama meta %x", b)
		}
	}
}

// Horses pack colour and markings; a herd shares its colour and rolls
// markings per horse; over many rolls every colour and marking appears.
func TestHorseVariantRollsAndHerds(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	colours, marks := map[int32]int{}, map[int32]int{}
	for i := 0; i < 2000; i++ {
		m := &mob{etype: entityHorse}
		h.rollVariant(m)
		colours[horseColour(m.variant)]++
		marks[horseMarks(m.variant)]++
	}
	if len(colours) != horseColours || len(marks) != horseMarkings {
		t.Errorf("colours %v markings %v", colours, marks)
	}
	var herd []*mob
	h.withSpawnGroup(func() {
		for i := 0; i < 6; i++ {
			herd = append(herd, h.spawnSpecies(players, entityHorse, 0, 10.5+float64(i), 70, 10.5))
		}
	})
	for _, m := range herd[1:] {
		if horseColour(m.variant) != horseColour(herd[0].variant) {
			t.Fatalf("a herd shares its colour: %d vs %d", m.variant, herd[0].variant)
		}
	}
	if h.spawnGroup != nil {
		t.Error("the spawn group must be cleared after the pack")
	}
	// Wolves in a pack share their coat too, and packs are independent.
	var pack []*mob
	h.withSpawnGroup(func() {
		for i := 0; i < 4; i++ {
			pack = append(pack, h.spawnSpecies(players, entityWolf, 0, 10.5+float64(i), 70, 10.5))
		}
	})
	for _, m := range pack[1:] {
		if m.variant != pack[0].variant {
			t.Fatal("a wolf pack shares its coat")
		}
	}
}

// Breeding: a foal draws colour and markings from its parents (or fresh),
// a cria's strength from the stronger parent, everyone else a parent's coat.
func TestVariantInheritance(t *testing.T) {
	h := newHub(world.New(1))
	a := &mob{etype: entityHorse, variant: horseVariant(horseBlack, horseMarkWhiteDots), variantSet: true}
	b := &mob{etype: entityHorse, variant: horseVariant(horseGray, horseMarkNone), variantSet: true}
	fresh := 0
	for i := 0; i < 900; i++ {
		foal := &mob{etype: entityHorse}
		h.inheritVariant(foal, a, b)
		if !foal.variantSet {
			t.Fatal("foal must inherit")
		}
		c := horseColour(foal.variant)
		if c != horseBlack && c != horseGray {
			fresh++
		}
	}
	if fresh == 0 || fresh > 200 { // 1/9 ≈ 100 expected
		t.Errorf("fresh colours %d of 900, want about a ninth", fresh)
	}
	la := &mob{etype: entityLlama, strength: 4, variant: llamaBrown, variantSet: true}
	lb := &mob{etype: entityLlama, strength: 1, variant: llamaGray, variantSet: true}
	seen := map[int8]bool{}
	for i := 0; i < 500; i++ {
		cria := &mob{etype: entityLlama}
		h.inheritVariant(cria, la, lb)
		if cria.strength < 1 || cria.strength > 5 || (cria.variant != llamaBrown && cria.variant != llamaGray) {
			t.Fatalf("cria strength %d variant %d", cria.strength, cria.variant)
		}
		seen[cria.strength] = true
	}
	if !seen[1] || !seen[4] {
		t.Errorf("cria strengths %v should span 1..4", seen)
	}
	wa := &mob{etype: entityWolf, variant: wolfVariantID[wolfAshen], variantSet: true}
	wb := &mob{etype: entityWolf, variant: wolfVariantID[wolfRusty], variantSet: true}
	pup := &mob{etype: entityWolf}
	h.inheritVariant(pup, wa, wb)
	if !pup.variantSet || (pup.variant != wa.variant && pup.variant != wb.variant) {
		t.Errorf("pup coat %d", pup.variant)
	}
	kit := &mob{etype: entityRabbit}
	ra := &mob{etype: entityRabbit, variant: rabbitGold, variantSet: true}
	h.inheritVariant(kit, ra, ra)
	if !kit.variantSet {
		t.Error("kit must inherit")
	}
	parrot := &mob{etype: entityParrot}
	h.inheritVariant(parrot, &mob{etype: entityParrot}, &mob{etype: entityParrot})
	if parrot.variantSet {
		t.Error("parrots don't breed: no inheritance rule")
	}
}

// Cats: all_black is only in the pool under a full moon; the other ten
// coats always are.
func TestCatVariantMoon(t *testing.T) {
	h := newHub(world.New(1))
	allBlack := catVariantID[catAllBlack]
	h.dayTime.Store(24000 * 2) // phase 2: half moon
	seen := map[int32]bool{}
	for i := 0; i < 3000; i++ {
		m := &mob{etype: entityCat}
		h.rollVariant(m)
		if m.variant == allBlack {
			t.Fatal("all_black rolled away from the full moon (and no swamp hut)")
		}
		seen[m.variant] = true
	}
	if len(seen) != 10 {
		t.Errorf("only %d common coats rolled", len(seen))
	}
	h.dayTime.Store(0) // full moon
	seen = map[int32]bool{}
	for i := 0; i < 3000; i++ {
		m := &mob{etype: entityCat}
		h.rollVariant(m)
		seen[m.variant] = true
	}
	if !seen[allBlack] || len(seen) != 11 {
		t.Errorf("full moon pool: %v", seen)
	}
}
