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
	cases := map[string]int8{
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
	cow := h.spawnSpecies(players, entityCow, 0, 10.5, 70, 10.5)
	if variantMeta(cow) != nil {
		t.Error("a cow has no variant entry")
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

	// Over many rolls axolotls show every common colour and the rare blue.
	seen := map[int8]int{}
	for i := 0; i < 20000; i++ {
		m := &mob{etype: entityAxolotl}
		h.rollVariant(m)
		seen[m.variant]++
	}
	for v := int8(axolotlLucy); v <= axolotlBlue; v++ {
		if seen[v] == 0 {
			t.Errorf("axolotl variant %d never rolled", v)
		}
	}
	if seen[axolotlBlue] > 60 { // ≈ 20000/1200 ≈ 17 expected
		t.Errorf("blue rolled %d times in 20000, far above 1-in-1200", seen[axolotlBlue])
	}
}
