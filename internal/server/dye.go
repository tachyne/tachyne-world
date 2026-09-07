package server

// Dyeing leather (vanilla DyedItemColor.applyDyes + the armor-dye special
// recipe + CauldronInteraction.DYED_ITEM): any dyeable piece plus one or
// more dyes anywhere in the grid blends into a new colour — the average of
// the dyes' colours (and the piece's own, if it already has one) rescaled so
// its brightest channel matches the average of their brightest channels —
// and a water cauldron washes it off again.

// leatherColor is the untinted leather colour the blend starts from when a
// piece has no dye yet is treated as no colour at all; vanilla only mixes
// what is there, so a first dye is that dye's colour exactly.
const leatherColor = 0xA06540

// dyeRGB is DyeColor.textureDiffuseColor per dye item.
var dyeRGB = func() map[int32]int32 {
	colors := map[string]int32{
		"white": 0xF9FFFE, "orange": 16351261, "magenta": 13061821, "light_blue": 3847130,
		"yellow": 16701501, "lime": 8439583, "pink": 15961002, "gray": 4673362,
		"light_gray": 0x9D9D97, "cyan": 1481884, "purple": 8991416, "blue": 3949738,
		"brown": 8606770, "green": 6192150, "red": 11546150, "black": 0x1D1D21,
	}
	out := map[int32]int32{}
	for name, rgb := range colors {
		if id, ok := itemByName[name+"_dye"]; ok {
			out[int32(id)] = rgb
		}
	}
	return out
}()

// dyeableItems is #minecraft:dyeable.
var dyeableItems = func() map[int32]bool {
	out := map[int32]bool{}
	for _, n := range []string{"leather_helmet", "leather_chestplate", "leather_leggings", "leather_boots", "leather_horse_armor", "wolf_armor"} {
		if id, ok := itemByName[n]; ok {
			out[int32(id)] = true
		}
	}
	return out
}()

func isDyeable(item int32) bool { return dyeableItems[item] }

// blendDyes is DyedItemColor.applyDyes on a piece's current colour (0 =
// none) and a list of dye colours.
func blendDyes(current int32, dyes []int32) int32 {
	var rs, gs, bs, maxs, n int32
	add := func(rgb int32) {
		r, g, b := rgb>>16&0xff, rgb>>8&0xff, rgb&0xff
		rs, gs, bs = rs+r, gs+g, bs+b
		maxs += max(r, max(g, b))
		n++
	}
	if current != 0 {
		add(current)
	}
	for _, d := range dyes {
		add(d)
	}
	if n == 0 {
		return 0
	}
	r, g, b := rs/n, gs/n, bs/n
	avgMax := float64(maxs) / float64(n)
	top := float64(max(r, max(g, b)))
	if top > 0 {
		r = int32(float64(r) * avgMax / top)
		g = int32(float64(g) * avgMax / top)
		b = int32(float64(b) * avgMax / top)
	}
	return r<<16 | g<<8 | b
}

// armorDyeMatch is ArmorDyeRecipe.matches + assemble: exactly one dyeable
// piece, at least one dye, nothing else.
func armorDyeMatch(grid []invStack) (invStack, bool) {
	var piece *invStack
	var dyes []int32
	for i := range grid {
		s := &grid[i]
		if s.item == 0 || s.count == 0 {
			continue
		}
		switch {
		case isDyeable(s.item):
			if piece != nil {
				return invStack{}, false
			}
			piece = s
		case dyeRGB[s.item] != 0:
			dyes = append(dyes, dyeRGB[s.item])
		default:
			return invStack{}, false
		}
	}
	if piece == nil || len(dyes) == 0 {
		return invStack{}, false
	}
	res := *piece
	res.count = 1
	res.color = blendDyes(piece.color, dyes)
	return res, true
}
