package server

// Vanilla's special crafting recipes — the ones no shaped or shapeless
// table can express because the result is made from what went in
// (RepairItemRecipe, TippedArrowRecipe, TransmuteRecipe for shulker boxes
// and bundles, BannerDuplicateRecipe, FireworkRocketRecipe,
// BookCloningRecipe). Armour dyeing, suspicious stew and the map recipes
// live beside their mechanics; these join them in craftResult.

import "strings"

// Crafting kinds beyond the map ones: what the take must do differently.
const (
	craftKeepPattern = 10 + iota // banner duplicate: the patterned banner is the remainder
	craftBookClone               // written-book copies are born at take time; the original stays
)

const repairBonusPercent = 5 // RepairItemRecipe: durability * 5 / 100 on top of what both had left

var (
	itemArrow     = int32(itemByName["arrow"])
	itemFireworks = int32(itemByName["firework_rocket"])
)

// dyeOrder is the DyeColor enum's order (the wire's dye varint).
var dyeOrder = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
	"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// dyeColorName maps a dye item to its colour name (for the transmute
// recipes' result lookup).
var dyeColorName = func() map[int32]string {
	out := map[int32]string{}
	for _, name := range dyeOrder {
		if id, ok := itemByName[name+"_dye"]; ok {
			out[int32(id)] = name
		}
	}
	return out
}()

// gridStacks is the non-empty cells of a grid.
func gridStacks(grid []invStack) []invStack {
	var out []invStack
	for _, st := range grid {
		if st.item != 0 && st.count > 0 {
			out = append(out, st)
		}
	}
	return out
}

// specialCraftMatch tries each special recipe; ok reports a match.
func (h *hub) specialCraftMatch(grid []invStack, w int) (invStack, int, bool) {
	if res, ok := repairMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := tippedArrowMatch(grid, w); ok {
		return res, mapCraftNone, true
	}
	if res, ok := transmuteMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := bannerDuplicateMatch(grid); ok {
		return res, craftKeepPattern, true
	}
	if res, ok := decoratedPotMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := h.fireworkStarMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := h.fireworkStarFadeMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := h.fireworkRocketMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := h.bookCloneMatch(grid); ok {
		return res, craftBookClone, true
	}
	if res, ok := shieldDecorationMatch(grid); ok {
		return res, mapCraftNone, true
	}
	return invStack{}, mapCraftNone, false
}

// bannerItemColor is a banner item's own colour in dye order.
var bannerItemColor = func() map[int32]int8 {
	out := map[int32]int8{}
	for i, name := range dyeOrder {
		if id, ok := itemByName[name+"_banner"]; ok {
			out[int32(id)] = int8(i)
		}
	}
	return out
}()

// shieldDecorationMatch is ShieldDecorationRecipe: a shield without
// patterns and any banner, nothing else, make the shield with the
// banner's layers and its colour as the base; the shield's own
// components (damage, name, enchantments) ride along.
func shieldDecorationMatch(grid []invStack) (invStack, bool) {
	s := gridStacks(grid)
	if len(s) != 2 {
		return invStack{}, false
	}
	var shield, banner *invStack
	for i := range s {
		switch {
		case bannerItems[s[i].item]:
			banner = &s[i]
		case s[i].item == int32(itemShield) && s[i].patCount() == 0:
			shield = &s[i]
		default:
			return invStack{}, false
		}
	}
	if shield == nil || banner == nil {
		return invStack{}, false
	}
	res := *shield
	res.count = 1
	res.pats = banner.pats
	res.shieldBase = bannerItemColor[banner.item] + 1
	return res, true
}

// repairMatch is RepairItemRecipe: two of the same damageable item and
// nothing else make one, with both remaining durabilities added plus five
// percent of the maximum; only curses survive the combining.
func repairMatch(grid []invStack) (invStack, bool) {
	s := gridStacks(grid)
	if len(s) != 2 || s[0].item != s[1].item || s[0].count != 1 || s[1].count != 1 {
		return invStack{}, false
	}
	maxDmg := itemMaxDurability[s[0].item]
	if maxDmg <= 0 {
		return invStack{}, false
	}
	remaining := (maxDmg - s[0].dmg) + (maxDmg - s[1].dmg) + maxDmg*repairBonusPercent/100
	res := invStack{item: s[0].item, count: 1, dmg: max(maxDmg-remaining, 0)}
	n := 0
	for _, src := range s {
		for _, e := range src.ench {
			if e.id != enchBindingCurse && e.id != enchVanishingCurse {
				continue
			}
			kept := false
			for i := 0; i < n; i++ {
				if res.ench[i].id == e.id {
					res.ench[i].lvl = max(res.ench[i].lvl, e.lvl)
					kept = true
				}
			}
			if !kept && n < len(res.ench) {
				res.ench[n] = e
				n++
			}
		}
	}
	return res, true
}

// tippedArrowMatch is TippedArrowRecipe: a lingering potion in the middle
// of a full 3x3 of arrows makes eight arrows tipped with it.
func tippedArrowMatch(grid []invStack, w int) (invStack, bool) {
	if w != 3 || len(grid) < 9 || grid[4].item != itemLingerPotion || grid[4].count <= 0 {
		return invStack{}, false
	}
	for i, st := range grid[:9] {
		if i == 4 {
			continue
		}
		if st.item != itemArrow || st.count <= 0 {
			return invStack{}, false
		}
	}
	return invStack{item: itemTippedArrow, count: 8, potion: grid[4].potion}, true
}

// transmuteMatch is the crafting_transmute recipes: a shulker box or a
// bundle and one dye make the dye's colour of it, contents and all.
func transmuteMatch(grid []invStack) (invStack, bool) {
	s := gridStacks(grid)
	if len(s) != 2 {
		return invStack{}, false
	}
	var src invStack
	var color string
	for _, st := range s {
		if name, ok := dyeColorName[st.item]; ok && color == "" {
			color = name
		} else if isShulkerBoxItem(st.item) || bundleItems[st.item] {
			src = st
		}
	}
	if color == "" || src.item == 0 {
		return invStack{}, false
	}
	kind := "shulker_box"
	if bundleItems[src.item] {
		kind = "bundle"
	}
	target, ok := itemByName[color+"_"+kind]
	if !ok || int32(target) == src.item {
		return invStack{}, false
	}
	res := src // TransmuteRecipe.createWithOriginalComponents: the contents ride along
	res.item, res.count = int32(target), 1
	return res, true
}

// bannerDuplicateMatch is BannerDuplicateRecipe: a patterned banner and a
// blank one of the same colour make a copy; the patterned one stays.
func bannerDuplicateMatch(grid []invStack) (invStack, bool) {
	s := gridStacks(grid)
	if len(s) != 2 || s[0].item != s[1].item || !bannerItems[s[0].item] {
		return invStack{}, false
	}
	var patterned, blank *invStack
	for i := range s {
		if s[i].pats[0].patPlus1 != 0 {
			patterned = &s[i]
		} else {
			blank = &s[i]
		}
	}
	if patterned == nil || blank == nil {
		return invStack{}, false
	}
	res := *patterned
	res.count = 1
	return res, true
}

// fireworkRocketMatch is FireworkRocketRecipe: one paper and one to three
// gunpowder make three rockets, the gunpowder count being the flight
// duration, and up to seven firework stars ride along as the bursts the
// rocket shows.
func (h *hub) fireworkRocketMatch(grid []invStack) (invStack, bool) {
	paper, powder := 0, 0
	var bursts []fireworkBurst
	for _, st := range gridStacks(grid) {
		switch {
		case st.item == itemPaper:
			paper += st.count
		case st.item == itemGunpowder:
			powder++
		case st.item == itemFireworkStar:
			if len(bursts) >= maxRocketBursts {
				return invStack{}, false
			}
			bursts = append(bursts, burstsOf(st)...)
		default:
			return invStack{}, false
		}
	}
	if paper != 1 || powder < 1 || powder > 3 {
		return invStack{}, false
	}
	return invStack{item: itemFireworks, count: 3, flight: int8(powder),
		starID: h.stars.intern(bursts)}, true
}

// fireworkStarMatch is FireworkStarRecipe: gunpowder, at least one dye, and
// at most one each of a shape item, glowstone dust (twinkle) and a diamond
// (trail). The dyes give the burst its colours, in grid order.
func (h *hub) fireworkStarMatch(grid []invStack) (invStack, bool) {
	burst := fireworkBurst{Shape: burstSmallBall}
	var powder, shapes, twinkles, trails int
	for _, st := range gridStacks(grid) {
		switch {
		case burstShapeOf[st.item] != 0:
			shapes++
			burst.Shape = burstShapeOf[st.item] - 1
		case st.item == itemGlowstone:
			twinkles++
			burst.Twinkle = true
		case st.item == itemDiamond:
			trails++
			burst.Trail = true
		case st.item == itemGunpowder:
			powder++
		default:
			c, isDye := dyeColorOf[st.item]
			if !isDye {
				return invStack{}, false
			}
			burst.Colors = append(burst.Colors, dyeFireworkColor[c])
		}
	}
	if powder != 1 || shapes > 1 || twinkles > 1 || trails > 1 || len(burst.Colors) == 0 {
		return invStack{}, false
	}
	return invStack{item: itemFireworkStar, count: 1,
		starID: h.stars.intern([]fireworkBurst{burst})}, true
}

// fireworkStarFadeMatch is FireworkStarFadeRecipe: one finished star and at
// least one dye give it the colours it fades to. Everything else about the
// star is kept.
func (h *hub) fireworkStarFadeMatch(grid []invStack) (invStack, bool) {
	var burst fireworkBurst
	stars, fade := 0, []int32(nil)
	for _, st := range gridStacks(grid) {
		if st.item == itemFireworkStar {
			stars++
			if b := burstsOf(st); len(b) == 1 {
				burst = b[0]
			}
			continue
		}
		c, isDye := dyeColorOf[st.item]
		if !isDye {
			return invStack{}, false
		}
		fade = append(fade, dyeFireworkColor[c])
	}
	if stars != 1 || len(fade) == 0 || len(burst.Colors) == 0 {
		return invStack{}, false
	}
	burst.Fade = fade
	return invStack{item: itemFireworkStar, count: 1,
		starID: h.stars.intern([]fireworkBurst{burst})}, true
}

// decoratedPotMatch is DecoratedPotRecipe: exactly four sherds (or bricks)
// in a diamond, and the pot wears them in the order the grid reads —
// back, left, right, front, which is the top, left, right and bottom cell.
// Vanilla has no data recipe for this one; the faces are what makes it
// worth crafting, so the recipe and the decoration are the same feature.
func decoratedPotMatch(grid []invStack) (invStack, bool) {
	if len(grid) != 9 {
		return invStack{}, false
	}
	// The four diamond cells, in vanilla's own order.
	var sh potSherds
	for i, cell := range []int{1, 3, 5, 7} {
		st := grid[cell]
		if st.item == 0 || st.count <= 0 || !potIngredients[st.item] {
			return invStack{}, false
		}
		sh[i] = st.item
	}
	for i, st := range grid { // nothing anywhere else
		if i == 1 || i == 3 || i == 5 || i == 7 {
			continue
		}
		if st.item != 0 && st.count > 0 {
			return invStack{}, false
		}
	}
	return invStack{item: itemDecoratedPot, count: 1, sherds: sh}, true
}

// potIngredients is #decorated_pot_ingredients: the brick and the pottery
// sherds. Named, not numbered, so a version bump cannot silently reshuffle it.
var potIngredients = func() map[int32]bool {
	m := map[int32]bool{int32(itemByName["brick"]): true}
	for name, id := range itemByName {
		if strings.HasSuffix(name, "_pottery_sherd") {
			m[int32(id)] = true
		}
	}
	delete(m, 0)
	return m
}()

var itemDecoratedPot = int32(itemByName["decorated_pot"])

// burstShapeOf maps the shape-setting ingredients to their shape, stored +1
// so the zero value means "not a shape item" (SHAPE_BY_ITEM).
var burstShapeOf = func() map[int32]int8 {
	m := map[int32]int8{}
	set := func(name string, shape int8) {
		if id := int32(itemByName[name]); id != 0 {
			m[id] = shape + 1
		}
	}
	set("fire_charge", burstLargeBall)
	set("feather", burstBurst)
	set("gold_nugget", burstStar)
	for _, head := range []string{"skeleton_skull", "wither_skeleton_skull", "creeper_head",
		"player_head", "dragon_head", "zombie_head", "piglin_head"} {
		set(head, burstCreeper)
	}
	return m
}()

// bookCloneMatch is BookCloningRecipe: a written book below generation
// two and any number of book-and-quills make that many copies, a
// generation up; the original is the remainder.
func (h *hub) bookCloneMatch(grid []invStack) (invStack, bool) {
	var src invStack
	copies := 0
	for _, st := range gridStacks(grid) {
		switch {
		case st.item == itemWrittenBook && st.bookID != 0 && src.item == 0:
			src = st
		case st.item == itemWritableBook:
			copies += st.count
		default:
			return invStack{}, false
		}
	}
	if src.item == 0 || copies == 0 || h.books == nil {
		return invStack{}, false
	}
	if b, ok := h.books.get(src.bookID); !ok || b.Gen >= 2 {
		return invStack{}, false // WrittenBookContent: a copy of a copy cannot be copied
	}
	return invStack{item: itemWrittenBook, count: copies, bookID: src.bookID}, true
}
