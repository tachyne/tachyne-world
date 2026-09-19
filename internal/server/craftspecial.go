package server

// Vanilla's special crafting recipes — the ones no shaped or shapeless
// table can express because the result is made from what went in
// (RepairItemRecipe, TippedArrowRecipe, TransmuteRecipe for shulker boxes
// and bundles, BannerDuplicateRecipe, FireworkRocketRecipe,
// BookCloningRecipe). Armour dyeing, suspicious stew and the map recipes
// live beside their mechanics; these join them in craftResult.

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

// dyeColorName maps a dye item to its colour name (for the transmute
// recipes' result lookup).
var dyeColorName = func() map[int32]string {
	out := map[int32]string{}
	for _, name := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
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
	if res, ok := fireworkRocketMatch(grid); ok {
		return res, mapCraftNone, true
	}
	if res, ok := h.bookCloneMatch(grid); ok {
		return res, craftBookClone, true
	}
	return invStack{}, mapCraftNone, false
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

// fireworkRocketMatch is FireworkRocketRecipe without stars: one paper
// and one to three gunpowder make three rockets (the gunpowder count is
// the flight duration, which the engine's rocket does not yet carry).
func fireworkRocketMatch(grid []invStack) (invStack, bool) {
	paper, powder := 0, 0
	for _, st := range gridStacks(grid) {
		switch st.item {
		case itemPaper:
			paper += st.count
		case itemGunpowder:
			powder++
		default:
			return invStack{}, false
		}
	}
	if paper != 1 || powder < 1 || powder > 3 {
		return invStack{}, false
	}
	return invStack{item: itemFireworks, count: 3}, true
}

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
