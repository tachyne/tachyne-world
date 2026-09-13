package server

// Lamb colour. Vanilla's Sheep.getBreedOffspring asks DyeColor.getMixedColor
// for the lamb's fleece: it lays the two parents' dyes side by side in a
// two-wide crafting input and, if that crafts a dye, the lamb wears that
// colour (red and yellow parents give an orange lamb, blue and red a purple
// one); otherwise it takes one parent's colour at random. tachyne routes the
// same question through its own crafting table so the lookup stays in step
// with the recipe data rather than a hand-kept table.

// dyeItemOfColor is the dye item of each fleece colour (registry order).
var dyeItemOfColor = func() []int32 {
	out := make([]int32, len(dyeColors))
	for i, c := range dyeColors {
		if id, ok := itemByName[c+"_dye"]; ok {
			out[i] = int32(id)
		}
	}
	return out
}()

// mixedFleeceColor is DyeColor.getMixedColor: the dye the two parents' dyes
// craft together, else one parent's colour at random.
func (h *hub) mixedFleeceColor(a, b int8) int8 {
	if c, ok := findColorMixInRecipes(a, b); ok {
		return c
	}
	if h.rng.Intn(2) == 0 {
		return a
	}
	return b
}

// findColorMixInRecipes is DyeColor.findColorMixInRecipes: the two dyes in a
// 2×1 crafting input, and the result only counts if it is itself a dye.
func findColorMixInRecipes(a, b int8) (int8, bool) {
	if a < 0 || b < 0 || int(a) >= len(dyeItemOfColor) || int(b) >= len(dyeItemOfColor) {
		return 0, false
	}
	da, db := dyeItemOfColor[a], dyeItemOfColor[b]
	if da == 0 || db == 0 {
		return 0, false
	}
	grid := make([]invStack, 4)
	grid[0] = invStack{item: da, count: 1}
	grid[1] = invStack{item: db, count: 1}
	item, n := matchRecipe(grid, 2)
	if n == 0 {
		return 0, false
	}
	c, ok := dyeItemColor[item]
	return c, ok
}
