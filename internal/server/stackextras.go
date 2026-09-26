package server

import "github.com/tachyne/tachyne-common/protocol"

// The components the ominous banner, a loaded crossbow and a filled mob
// bucket carry, in CANONICAL (770) numbering like the rest of item.go's
// (the chain renumbers them per client version).
const (
	componentItemName         = 6  // item_name: a text component
	componentRarity           = 9  // rarity: the Rarity enum
	componentTooltipDisplay   = 15 // tooltip_display: hide flag + hidden component ids
	componentChargedProj      = 40 // charged_projectiles: a crossbow's loaded stacks
	componentBucketEntityData = 50 // bucket_entity_data: the bucketed mob's compound
	componentSalmonSize       = 77 // salmon/size
	componentFishPattern      = 79 // tropical_fish/pattern (the Pattern's packed id)
	componentFishBaseColor    = 80 // tropical_fish/base_color (dye)
	componentFishPatternColor = 81 // tropical_fish/pattern_color (dye)
	componentAxolotlVariant   = 91 // axolotl/variant

	rarityUncommon = 1 // Rarity.UNCOMMON
)

// extraComponents encodes the components above that st carries: how many,
// and their bytes (id + payload each), for stackComponents to append.
func extraComponents(st invStack) (int32, []byte) {
	var n int32
	var b []byte
	if st.ominous && st.item == itemWhiteBanner {
		// Raid.getBannerComponentPatch: the eight layers, the layer list
		// hidden from the tooltip, the name and uncommon rarity.
		b = protocol.AppendVarInt(b, componentBannerPats)
		b = protocol.AppendVarInt(b, int32(len(ominousBannerLayers)))
		for _, l := range ominousBannerLayers {
			color := int32(0)
			for i, name := range dyeName {
				if name == l.Color {
					color = int32(i)
				}
			}
			b = protocol.AppendVarInt(b, int32(bannerPatternIDs[l.Pattern])+1) // holder = id + 1
			b = protocol.AppendVarInt(b, color)
		}
		b = protocol.AppendVarInt(b, componentTooltipDisplay)
		b = append(b, 0) // the tooltip itself shows
		b = protocol.AppendVarInt(b, 1)
		b = protocol.AppendVarInt(b, componentBannerPats)
		b = protocol.AppendVarInt(b, componentItemName)
		b = append(b, translatableNBT("block.minecraft.ominous_banner")...)
		b = protocol.AppendVarInt(b, componentRarity)
		b = protocol.AppendVarInt(b, rarityUncommon)
		n += 4
	}
	if st.load.n > 0 && st.load.item != 0 {
		// charged_projectiles: the loaded stacks, each a whole Slot with its
		// own components (a tipped arrow's potion, a rocket's bursts). The
		// client draws the loaded crossbow from it and lists it in the
		// tooltip.
		b = protocol.AppendVarInt(b, componentChargedProj)
		b = protocol.AppendVarInt(b, int32(st.load.n))
		for i := 0; i < int(st.load.n); i++ {
			b = appendStack(b, st.load.ammo())
		}
		n++
	}
	if etype, ok := speciesByMobBucket[st.item]; ok {
		c, cb := bucketComponents(etype, st.cube)
		n += c
		b = append(b, cb...)
	}
	return n, b
}

// bucketComponents is what saveToBucketTag puts on a filled bucket: the
// species' variant components (which the bucket's tooltip reads) and
// bucket_entity_data (Health, and an axolotl's Age).
func bucketComponents(etype int, c cubeContent) (int32, []byte) {
	var n int32
	var b []byte
	if c.variant > 0 {
		v := c.variant - 1
		switch etype {
		case entityTropicalFish: // TropicalFish.packVariant's three fields
			b = protocol.AppendVarInt(b, componentFishPattern)
			b = protocol.AppendVarInt(b, v&0xffff)
			b = protocol.AppendVarInt(b, componentFishBaseColor)
			b = protocol.AppendVarInt(b, v>>16&0xff)
			b = protocol.AppendVarInt(b, componentFishPatternColor)
			b = protocol.AppendVarInt(b, v>>24&0xff)
			n += 3
		case entitySalmon:
			b = protocol.AppendVarInt(b, componentSalmonSize)
			b = protocol.AppendVarInt(b, v)
			n++
		case entityAxolotl:
			b = protocol.AppendVarInt(b, componentAxolotlVariant)
			b = protocol.AppendVarInt(b, v)
			n++
		}
	}
	if c.health > 0 {
		tag := protocol.NBTRoot()
		tag = protocol.NBTFloat(tag, "Health", float32(c.health-1))
		if etype == entityAxolotl {
			tag = protocol.NBTInt(tag, "Age", c.age)
		}
		b = protocol.AppendVarInt(b, componentBucketEntityData)
		b = append(b, protocol.NBTEnd(tag)...)
		n++
	}
	return n, b
}

// translatableNBT is a network-NBT {translate: key} text component.
func translatableNBT(key string) []byte {
	return protocol.NBTEnd(protocol.NBTString(protocol.NBTRoot(), "translate", key))
}
