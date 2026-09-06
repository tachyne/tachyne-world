package server

import (
	"bytes"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// A librarian's clothes: index 18, the VILLAGER_DATA serializer, then type,
// profession (vanilla registry id 9) and tier; a zombie villager carries the
// same data at index 20.
func TestVillagerDataMeta(t *testing.T) {
	m := &mob{eid: 42, etype: entityVillager, tradeLevel: 3, variant: villagerTypeTaiga, variantSet: true}
	for i, n := range professionNames {
		if n == "librarian" {
			m.profession = i
		}
	}
	got := villagerDataMeta(m)
	want := protocol.AppendVarInt(nil, 42)
	want = append(want, 18)
	want = protocol.AppendVarInt(want, protocol.VillagerDataSerializer770)
	want = protocol.AppendVarInt(want, villagerTypeTaiga)
	want = protocol.AppendVarInt(want, 9)
	want = protocol.AppendVarInt(want, 3)
	want = append(want, itemMetaEnd)
	if !bytes.Equal(got, want) {
		t.Errorf("villager meta %x, want %x", got, want)
	}
	m.etype = entityZombieVillager
	if z := villagerDataMeta(m); z[1] != metaIndexZombieVillagerData {
		t.Errorf("zombie villager data index %d, want %d", z[1], metaIndexZombieVillagerData)
	}
	if variantMeta(m) == nil {
		t.Error("variantMeta must carry villager data so spawn/join/dimension sends include it")
	}
	if villagerTypeForBiome("minecraft:snowy_plains") != villagerTypeSnow || villagerTypeForBiome("minecraft:plains") != villagerTypePlains ||
		villagerTypeForBiome("minecraft:desert") != villagerTypeDesert || villagerTypeForBiome("minecraft:bamboo_jungle") != villagerTypeJungle {
		t.Error("biome → villager type mapping")
	}
}
