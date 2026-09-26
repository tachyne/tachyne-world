package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// The ominous banner (Raid.getOminousBannerInstance): what a raid or patrol
// captain wears, and what the Voluntary Exile kill looks for on a raider's
// head. It is a white banner with a fixed component set — eight pattern
// layers, the "Ominous Banner" item name, uncommon rarity, the layer list
// hidden from the tooltip — which the stack carries as the one ominous flag:
// the eight layers are two past what a loom (and invStack.pats) holds.

var itemWhiteBanner = itemByName["white_banner"]

// ominousBannerLayers are getBannerComponentPatch's layers, in order.
var ominousBannerLayers = []attachproto.BannerLayer{
	{Pattern: "minecraft:rhombus", Color: "cyan"},
	{Pattern: "minecraft:stripe_bottom", Color: "light_gray"},
	{Pattern: "minecraft:stripe_center", Color: "gray"},
	{Pattern: "minecraft:border", Color: "light_gray"},
	{Pattern: "minecraft:stripe_middle", Color: "black"},
	{Pattern: "minecraft:half_horizontal", Color: "light_gray"},
	{Pattern: "minecraft:circle", Color: "light_gray"},
	{Pattern: "minecraft:border", Color: "black"},
}

// ominousBanner is Raid.getOminousBannerInstance: one ominous banner.
func ominousBanner() invStack {
	return invStack{item: itemWhiteBanner, count: 1, ominous: true}
}

// isOminousBanner is the item predicate the raid advancements use: a white
// banner carrying the ominous layers and name, whatever else it carries.
func isOminousBanner(st invStack) bool {
	return st.item == itemWhiteBanner && st.count > 0 && st.ominous
}

// isOminousLayers reports whether a placed banner's layers are the ominous
// banner's: BannerBlockEntity keeps the components, so breaking a placed
// one gives the ominous banner back.
func isOminousLayers(layers []attachproto.BannerLayer) bool {
	if len(layers) != len(ominousBannerLayers) {
		return false
	}
	for i, l := range layers {
		if bannerPatternQualified(l.Pattern) != ominousBannerLayers[i].Pattern || l.Color != ominousBannerLayers[i].Color {
			return false
		}
	}
	return true
}
