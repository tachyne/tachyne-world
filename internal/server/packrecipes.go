package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Data pack recipes, merged into the engine's recipe model the way
// RecipeManager takes the reloaded recipe registry: a pack's file at an id
// replaces the vanilla recipe of that id — and one that cannot be made (a
// file that does not parse, an unknown type, an empty or unknown
// ingredient) removes it, which is how a pack takes a vanilla recipe away.
//
// Applied: crafting_shaped and crafting_shapeless (the crafting grid, the
// crafter, limited_crafting, the recipe book and its place-recipe),
// smelting, blasting, smoking and campfire_cooking (the cookers, the
// campfire, the furnace book), and stonecutting, smithing_transform,
// smithing_trim and brewing (packstations.go: the stonecutter's rows and
// the smithing table's item sets go to the clients again when a load
// changes them). A pack recipe is tried before the vanilla ones. The
// special and transmute crafting types are listed by /datapack as not
// applied and leave vanilla's recipe of their id in place.
//
// Pack crafting recipes use the generated recipes' own form: ingredient
// sets are indices into ingredientSets, continued past its end by the
// pack's sets (ingredientSet), so matching, the book frame and
// place-recipe read both alike. Book display ids for pack entries follow
// the generated ones (packBookFirstID); a book entry is saved by name, so a
// reload — which renumbers the pack entries — keeps what a player knew
// (remapRecipeBooks).

// cookCampfire is the campfire's cook table, beside cookFurnace, cookBlast
// and cookSmoker.
const cookCampfire int8 = 3

// packBookFirstID is the first display id of a pack's book entries, after
// the generated crafting and cooking entries.
var packBookFirstID = cookBookFirstID + int32(len(cookBookRecipes))

// packCraft is a pack's crafting recipe in the generated recipes' form.
type packCraft struct {
	name        string // its book name (bookRecipeName)
	shaped      bool
	W, H        uint8
	Cells       []uint16 // shaped: row-major ingredient sets, 0 = empty
	Ingredients []uint16 // shapeless: ingredient sets, sorted
	Result      int32
	Count       uint8
	bookID      int32
}

// packBookEntry is one book entry a pack adds: a crafting recipe, or one
// input of a cooking recipe.
type packBookEntry struct {
	name  string
	craft *packCraft
	cook  attachproto.CookingRecipe
}

// packRecipes is a load's pack recipes and what they take from vanilla.
type packRecipes struct {
	sets        [][]int32 // ingredient sets past ingredientSets
	setIndex    map[string]uint16
	shapedBy    map[uint16][]*packCraft // by W<<8|H, in id order
	shapelessBy map[int][]*packCraft    // by ingredient count, in id order
	cook        [4]map[int32]cookEntry  // per cook table: input item → recipe
	cookRemoved [4]map[int32]bool       // vanilla cook inputs taken away
	xpByOutput  map[int32]float64
	book        []packBookEntry    // display ids packBookFirstID+i
	bookByName  map[string]int32   // book name → pack display id
	idsByName   map[string][]int32 // recipe name (as /recipe takes it) → pack display ids
	removedBook map[int32]bool     // generated display ids no longer in the game
	removed     map[string]bool    // vanilla recipe names a pack replaced or removed
	count       int                // recipes applied
	station     *stationRecipes    // stonecutting, smithing and brewing (packstations.go)
	setOverride map[uint16][]int32 // vanilla ingredient sets re-resolved for changed tags (packretag.go)
}

func newPackRecipes() *packRecipes {
	pr := &packRecipes{
		setIndex:    map[string]uint16{},
		shapedBy:    map[uint16][]*packCraft{},
		shapelessBy: map[int][]*packCraft{},
		xpByOutput:  map[int32]float64{},
		bookByName:  map[string]int32{},
		idsByName:   map[string][]int32{},
		removedBook: map[int32]bool{},
		removed:     map[string]bool{},
		station:     newStationRecipes(),
	}
	for i := range pr.cook {
		pr.cook[i] = map[int32]cookEntry{}
		pr.cookRemoved[i] = map[int32]bool{}
	}
	return pr
}

// recipeSet is the load's pack recipes (nil when there is no load).
func (pc *packContent) recipeSet() *packRecipes {
	if pc == nil {
		return nil
	}
	return pc.recipes
}

// ingredientSet is the items a set index accepts: a generated set, or a
// pack recipe's.
func ingredientSet(set uint16) []int32 {
	if int(set) < len(ingredientSets) {
		if pr := currentPack().recipeSet(); pr != nil && pr.setOverride != nil {
			if s, ok := pr.setOverride[set]; ok {
				return s
			}
		}
		return ingredientSets[set]
	}
	if pr := currentPack().recipeSet(); pr != nil {
		if n := int(set) - len(ingredientSets); n < len(pr.sets) {
			return pr.sets[n]
		}
	}
	return nil
}

// bookRecipeName is the name a recipe id is saved and named by: a vanilla
// recipe's bare path, another namespace's full id.
func bookRecipeName(id string) string { return strings.TrimPrefix(id, "minecraft:") }

// cookTables are the cook tables in cook-kind order, with the station item
// the book files them under and the recipe type that fills them.
var cookTables = [4]struct {
	typ, station string
	cats         map[string]int32 // the recipe's "category" → recipe_book_category id
}{
	{"minecraft:smelting", "furnace", map[string]int32{"blocks": 5, "food": 4, "misc": 6}},
	{"minecraft:blasting", "blast_furnace", map[string]int32{"blocks": 7, "food": 8, "misc": 8}},
	{"minecraft:smoking", "smoker", map[string]int32{"blocks": 9, "food": 9, "misc": 9}},
	{"minecraft:campfire_cooking", "campfire", map[string]int32{"blocks": 12, "food": 12, "misc": 12}},
}

// cookKindOfStation is a cook key's station name as a cook kind.
func cookKindOfStation(station string) (int8, bool) {
	for i, t := range cookTables {
		if t.station == station {
			return int8(i), true
		}
	}
	return 0, false
}

// recipeUnsupportedTypes are the recipe types a pack may carry that this
// server does not take from a pack (vanilla's recipe of the id stays).
var recipeUnsupportedTypes = map[string]bool{
	"minecraft:crafting_dye": true, "minecraft:crafting_imbue": true, "minecraft:crafting_transmute": true,
	"minecraft:crafting_decorated_pot": true, "minecraft:crafting_special_bookcloning": true,
	"minecraft:crafting_special_mapextending": true, "minecraft:crafting_special_firework_rocket": true,
	"minecraft:crafting_special_firework_star": true, "minecraft:crafting_special_firework_star_fade": true,
	"minecraft:crafting_special_bannerduplicate": true, "minecraft:crafting_special_shielddecoration": true,
	"minecraft:crafting_special_repairitem": true,
}

// errRecipeUnsupported marks a recipe of a type the server does not apply.
var errRecipeUnsupported = errors.New("recipe type not applied by this server")

// buildPackRecipes reads the top pack's file for every recipe id. tags are
// the load's tags (an ingredient's #tag resolves against them). unapplied
// collects, per pack, the recipe types it carries that are not applied.
func buildPackRecipes(files map[string]packFile, tags *tagRegistry, unapplied map[string]map[string]bool) *packRecipes {
	pr := newPackRecipes()
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids) // the registry's order: a lower id is tried first
	for _, id := range ids {
		f := files[id]
		typ, err := pr.add(id, f.data, tags)
		switch {
		case errors.Is(err, errRecipeUnsupported):
			if unapplied[f.pack] == nil {
				unapplied[f.pack] = map[string]bool{}
			}
			unapplied[f.pack]["recipe ("+typ+")"] = true
			continue
		case err != nil:
			log.Printf("datapacks: couldn't parse recipe %s from %s: %v", id, f.pack, err)
		default:
			pr.count++
		}
		// Applied or failed, the file stands at its id: vanilla's recipe of
		// the same id is gone.
		if strings.HasPrefix(id, "minecraft:") {
			pr.removeVanilla(bookRecipeName(id))
		}
	}
	pr.retagVanilla(tags)
	pr.finishStations()
	return pr
}

// removeVanilla takes a vanilla recipe out of the game: its crafting book
// entry and match, or its cook entries and the inputs it cooked.
func (pr *packRecipes) removeVanilla(name string) {
	pr.removed[name] = true
	pr.removeVanillaStation(name)
	if id, ok := recipeIDByName[name]; ok && id < cookBookFirstID {
		pr.removedBook[id] = true
	}
	for _, key := range cookRecipeKeys[name] {
		if id, ok := recipeIDByName[key]; ok {
			pr.removedBook[id] = true
		}
		parts := strings.SplitN(key, "/", 3) // cook, station, input
		if len(parts) != 3 {
			continue
		}
		kind, ok := cookKindOfStation(parts[1])
		in, known := itemByName[parts[2]]
		if ok && known {
			pr.cookRemoved[kind][in] = true
		}
	}
}

// add parses one recipe file and adds what it makes. typ is its type.
func (pr *packRecipes) add(id string, data []byte, tags *tagRegistry) (typ string, err error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return "", err
	}
	if err := json.Unmarshal(top["type"], &typ); err != nil || typ == "" {
		return "", errors.New("no recipe type")
	}
	typ = nsID(typ)
	if recipeUnsupportedTypes[typ] {
		return typ, errRecipeUnsupported
	}
	if raw, ok := top["show_notification"]; ok { // the book's toast follows the unlock, as for vanilla entries
		var notify bool
		if err := json.Unmarshal(raw, &notify); err != nil {
			return typ, errors.New("bad show_notification")
		}
	}
	switch typ {
	case "minecraft:crafting_shaped":
		c, err := pr.parseShaped(top, tags)
		if err != nil {
			return typ, err
		}
		pr.addCraft(id, c)
	case "minecraft:crafting_shapeless":
		c, err := pr.parseShapeless(top, tags)
		if err != nil {
			return typ, err
		}
		pr.addCraft(id, c)
	default:
		if ok, err := pr.addStation(id, typ, top, tags); ok {
			return typ, err
		}
		for kind, t := range cookTables {
			if t.typ == typ {
				return typ, pr.addCook(id, int8(kind), top, tags)
			}
		}
		return typ, fmt.Errorf("unknown recipe type %s", typ)
	}
	return typ, nil
}

func (pr *packRecipes) addCraft(id string, c *packCraft) {
	c.name = bookRecipeName(id)
	c.bookID = packBookFirstID + int32(len(pr.book))
	pr.book = append(pr.book, packBookEntry{name: c.name, craft: c})
	pr.bookByName[c.name] = c.bookID
	pr.idsByName[c.name] = []int32{c.bookID}
	if c.shaped {
		k := uint16(c.W)<<8 | uint16(c.H)
		pr.shapedBy[k] = append(pr.shapedBy[k], c)
	} else {
		pr.shapelessBy[len(c.Ingredients)] = append(pr.shapelessBy[len(c.Ingredients)], c)
	}
}

// parseShaped is ShapedRecipe's codec: a pattern of at most 3×3 rows of
// one width, shrunk to its non-blank part; a key of single symbols (not the
// blank), every one used and every used one defined; a result.
func (pr *packRecipes) parseShaped(top map[string]json.RawMessage, tags *tagRegistry) (*packCraft, error) {
	var lines []string
	if err := json.Unmarshal(top["pattern"], &lines); err != nil {
		return nil, errors.New("bad pattern")
	}
	pattern := make([][]rune, len(lines)) // a symbol is one character, not one byte
	for i, l := range lines {
		pattern[i] = []rune(l)
	}
	switch {
	case len(pattern) > 3:
		return nil, errors.New("Invalid pattern: too many rows, 3 is maximum")
	case len(pattern) == 0:
		return nil, errors.New("Invalid pattern: empty pattern not allowed")
	}
	for _, line := range pattern {
		if len(line) > 3 {
			return nil, errors.New("Invalid pattern: too many columns, 3 is maximum")
		}
		if len(line) != len(pattern[0]) {
			return nil, errors.New("Invalid pattern: each row must be the same width")
		}
	}
	var key map[string]json.RawMessage
	if err := json.Unmarshal(top["key"], &key); err != nil {
		return nil, errors.New("bad key")
	}
	sets := map[rune]uint16{}
	for sym, raw := range key {
		r := []rune(sym)
		if len(r) != 1 {
			return nil, fmt.Errorf("Invalid key entry: '%s' is an invalid symbol (must be 1 character only).", sym)
		}
		if sym == " " {
			return nil, errors.New("Invalid key entry: ' ' is a reserved symbol.")
		}
		set, err := pr.ingredient(raw, tags)
		if err != nil {
			return nil, err
		}
		sets[r[0]] = set
	}
	rows := shrinkPattern(pattern)
	if len(rows) == 0 {
		return nil, errors.New("Invalid pattern: empty pattern not allowed")
	}
	c := &packCraft{shaped: true, W: uint8(len(rows[0])), H: uint8(len(rows))}
	unused := map[rune]bool{}
	for s := range sets {
		unused[s] = true
	}
	for _, line := range rows {
		for _, sym := range line {
			if sym == ' ' {
				c.Cells = append(c.Cells, 0)
				continue
			}
			set, ok := sets[sym]
			if !ok {
				return nil, fmt.Errorf("Pattern references symbol '%c' but it's not defined in the key", sym)
			}
			delete(unused, sym)
			c.Cells = append(c.Cells, set)
		}
	}
	if len(unused) > 0 {
		return nil, errors.New("Key defines symbols that aren't used in pattern")
	}
	var err error
	if c.Result, c.Count, err = recipeResult(top["result"]); err != nil {
		return nil, err
	}
	return c, nil
}

// shrinkPattern is ShapedRecipePattern.shrink: leading and trailing blank
// rows, and the blank columns every row has at either side, removed.
func shrinkPattern(pattern [][]rune) [][]rune {
	left, right, top, bottom := 1<<30, 0, 0, 0
	for i, line := range pattern {
		first := 0
		for first < len(line) && line[first] == ' ' {
			first++
		}
		last := len(line) - 1
		for last >= 0 && line[last] == ' ' {
			last--
		}
		left = min(left, first)
		right = max(right, last)
		if last < 0 {
			if top == i {
				top++
			}
			bottom++
		} else {
			bottom = 0
		}
	}
	if len(pattern) == bottom {
		return nil
	}
	out := make([][]rune, len(pattern)-bottom-top)
	for i := range out {
		out[i] = pattern[i+top][left : right+1]
	}
	return out
}

// parseShapeless is ShapelessRecipe's codec: one to nine ingredients and a
// result.
func (pr *packRecipes) parseShapeless(top map[string]json.RawMessage, tags *tagRegistry) (*packCraft, error) {
	var raws []json.RawMessage
	if err := json.Unmarshal(top["ingredients"], &raws); err != nil {
		return nil, errors.New("bad ingredients")
	}
	if len(raws) < 1 || len(raws) > 9 {
		return nil, fmt.Errorf("a shapeless recipe takes 1 to 9 ingredients, not %d", len(raws))
	}
	c := &packCraft{}
	for _, raw := range raws {
		set, err := pr.ingredient(raw, tags)
		if err != nil {
			return nil, err
		}
		c.Ingredients = append(c.Ingredients, set)
	}
	sort.Slice(c.Ingredients, func(i, j int) bool { return c.Ingredients[i] < c.Ingredients[j] })
	var err error
	if c.Result, c.Count, err = recipeResult(top["result"]); err != nil {
		return nil, err
	}
	return c, nil
}

// addCook is AbstractCookingRecipe's codec — ingredient, result, cookingtime,
// experience (0), category (misc) — entered in its cook table for every
// item the ingredient accepts that no earlier pack recipe took, each with
// its book entry.
func (pr *packRecipes) addCook(id string, kind int8, top map[string]json.RawMessage, tags *tagRegistry) error {
	set, err := pr.ingredient(top["ingredient"], tags)
	if err != nil {
		return err
	}
	out, _, err := recipeResult(top["result"]) // a cooker's output grows one at a time
	if err != nil {
		return err
	}
	var cook int
	if err := json.Unmarshal(top["cookingtime"], &cook); err != nil {
		return errors.New("No key cookingtime")
	}
	var xp float64
	if raw, ok := top["experience"]; ok {
		if err := json.Unmarshal(raw, &xp); err != nil {
			return errors.New("bad experience")
		}
	}
	catName := "misc"
	if raw, ok := top["category"]; ok {
		if err := json.Unmarshal(raw, &catName); err != nil {
			return errors.New("bad category")
		}
	}
	t := cookTables[kind]
	cat, ok := t.cats[catName]
	if !ok {
		return fmt.Errorf("unknown cooking category %s", catName)
	}
	name := bookRecipeName(id)
	station := itemByName[t.station]
	for _, in := range pr.sets[int(set)-len(ingredientSets)] {
		if _, taken := pr.cook[kind][in]; taken {
			continue
		}
		e := cookEntry{Out: out, Cook: cook, XP: xp, Cat: cat}
		pr.cook[kind][in] = e
		key := "cook/" + t.station + "/" + itemNameOf[in]
		// The generated entry for this input stands for the recipe this
		// one replaces.
		if gid, ok := recipeIDByName[key]; ok {
			pr.removedBook[gid] = true
		}
		bid := packBookFirstID + int32(len(pr.book))
		pr.book = append(pr.book, packBookEntry{name: key, cook: attachproto.CookingRecipe{
			ID: bid, Ingredient: in, Result: out, Count: 1, Cook: int32(cook), XP: float32(xp),
			Station: station, Category: cat,
		}})
		pr.bookByName[key] = bid
		pr.idsByName[name] = append(pr.idsByName[name], bid)
	}
	if xp > pr.xpByOutput[out] {
		pr.xpByOutput[out] = xp
	}
	return nil
}

// ingredient reads Ingredient.CODEC — an item id, a #tag, or a list of item
// ids, naming at least one item and no air — as a set index.
func (pr *packRecipes) ingredient(raw json.RawMessage, tags *tagRegistry) (uint16, error) {
	if raw == nil {
		return 0, errors.New("missing ingredient")
	}
	var items []int32
	var one string
	if json.Unmarshal(raw, &one) == nil {
		if tag, ok := strings.CutPrefix(one, "#"); ok {
			id, ok := parseResID(tag)
			if !ok {
				return 0, fmt.Errorf("not a valid tag id: %s", tag)
			}
			names, ok := tags.members("item", id)
			if !ok {
				return 0, fmt.Errorf("unknown item tag %s", id)
			}
			for _, n := range names {
				if it, ok := itemIDOf(n); ok {
					items = append(items, it)
				}
			}
		} else {
			it, ok := itemIDOf(one)
			if !ok {
				return 0, fmt.Errorf("unknown item %s", one)
			}
			items = append(items, it)
		}
	} else {
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return 0, errors.New("an ingredient is an item, a #tag or a list of items")
		}
		for _, s := range list {
			it, ok := itemIDOf(s)
			if !ok {
				return 0, fmt.Errorf("unknown item %s", s)
			}
			items = append(items, it)
		}
	}
	if len(items) == 0 {
		// RecipeManager: "can't be placed due to empty ingredients and will be ignored"
		return 0, errors.New("empty ingredient")
	}
	sort.Slice(items, func(i, j int) bool { return items[i] < items[j] })
	dedup := items[:1]
	for _, it := range items[1:] {
		if it != dedup[len(dedup)-1] {
			dedup = append(dedup, it)
		}
	}
	k := fmt.Sprint(dedup)
	if set, ok := pr.setIndex[k]; ok {
		return set, nil
	}
	n := len(ingredientSets) + len(pr.sets)
	if n > 0xffff {
		return 0, errors.New("too many distinct ingredients")
	}
	pr.sets = append(pr.sets, dedup)
	pr.setIndex[k] = uint16(n)
	return uint16(n), nil
}

// itemIDOf is an item id ("minecraft:stick", or "stick") as the item, never
// air.
func itemIDOf(s string) (int32, bool) {
	id, ok := parseResID(s)
	if !ok || !strings.HasPrefix(id, "minecraft:") || id == "minecraft:air" {
		return 0, false
	}
	it, ok := itemByName[strings.TrimPrefix(id, "minecraft:")]
	return it, ok
}

// recipeResult reads ItemStackTemplate: an item id, or {id, count 1–99,
// components}. The components are not carried (the crafted stack is the
// plain item).
func recipeResult(raw json.RawMessage) (int32, uint8, error) {
	if raw == nil {
		return 0, 0, errors.New("No key result")
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		it, ok := itemIDOf(s)
		if !ok {
			return 0, 0, fmt.Errorf("unknown result item %s", s)
		}
		return it, 1, nil
	}
	var r struct {
		ID    string `json:"id"`
		Count *int   `json:"count"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return 0, 0, errors.New("bad result")
	}
	it, ok := itemIDOf(r.ID)
	if !ok {
		return 0, 0, fmt.Errorf("unknown result item %s", r.ID)
	}
	n := 1
	if r.Count != nil {
		n = *r.Count
	}
	if n < 1 || n > 99 {
		return 0, 0, fmt.Errorf("result count %d is outside 1 to 99", n)
	}
	return it, uint8(n), nil
}

// ---- lookups the engine's recipe code makes ------------------------------

// matchPackCraft tries the pack's crafting recipes on a grid's non-empty
// bounding box (bw×bh, cell(r, c) its items) and its n items in ids.
func (pr *packRecipes) matchPackCraft(bw, bh int, cell func(r, c int) int32, ids []int32) (*packCraft, bool) {
	for _, rec := range pr.shapedBy[uint16(bw)<<8|uint16(bh)] {
		if shapedFits(rec.Cells, bw, bh, cell) {
			return rec, true
		}
	}
	for _, rec := range pr.shapelessBy[len(ids)] {
		if pairIngredients(ids, rec.Ingredients) {
			return rec, true
		}
	}
	return nil, false
}

// shapedFits is a shaped pattern against a bounding box, as written or
// mirrored.
func shapedFits(cells []uint16, bw, bh int, cell func(r, c int) int32) bool {
	direct, mirror := true, true
	for r := 0; r < bh && (direct || mirror); r++ {
		for c := 0; c < bw; c++ {
			want := cells[r*bw+c]
			if !cellFits(want, cell(r, c)) {
				direct = false
			}
			if !cellFits(want, cell(r, bw-1-c)) {
				mirror = false
			}
		}
	}
	return direct || mirror
}

// campfireRecipe is the campfire's recipe for an input item.
func campfireRecipe(item int32) (cookEntry, bool) { return cookerRecipe(cookCampfire, item) }

// craftView is a crafting book entry's recipe, generated or a pack's.
type craftView struct {
	shaped      bool
	W, H        uint8
	Cells       []uint16
	Ingredients []uint16
	Result      int32
	Count       uint8
}

// bookCraft is the crafting recipe behind a display id, if it is one and
// still in the game.
func bookCraft(id int32) (craftView, bool) {
	pr := currentPack().recipeSet()
	if id < 0 || (pr != nil && pr.removedBook[id]) {
		return craftView{}, false
	}
	switch {
	case int(id) < len(shapedRecipes):
		r := &shapedRecipes[id]
		return craftView{shaped: true, W: r.W, H: r.H, Cells: r.Cells, Result: r.Result, Count: r.Count}, true
	case int(id) < len(shapedRecipes)+len(shapelessRecipes):
		r := &shapelessRecipes[int(id)-len(shapedRecipes)]
		return craftView{Ingredients: r.Ingredients, Result: r.Result, Count: r.Count}, true
	case pr != nil && id >= packBookFirstID:
		if n := int(id - packBookFirstID); n < len(pr.book) && pr.book[n].craft != nil {
			c := pr.book[n].craft
			return craftView{shaped: c.shaped, W: c.W, H: c.H, Cells: c.Cells, Ingredients: c.Ingredients, Result: c.Result, Count: c.Count}, true
		}
	}
	return craftView{}, false
}

// bookCook is the cooking book entry behind a display id, if it is one and
// still in the game.
func bookCook(id int32) (attachproto.CookingRecipe, bool) {
	pr := currentPack().recipeSet()
	if pr != nil && pr.removedBook[id] {
		return attachproto.CookingRecipe{}, false
	}
	if n := int(id - cookBookFirstID); id >= cookBookFirstID && n < len(cookBookRecipes) {
		return cookBookRecipes[n], true
	}
	if pr != nil && id >= packBookFirstID {
		if n := int(id - packBookFirstID); n < len(pr.book) && pr.book[n].craft == nil {
			return pr.book[n].cook, true
		}
	}
	return attachproto.CookingRecipe{}, false
}

// recipeNameIn is the saved name of a display id under a load (pc nil: the
// generated recipes alone).
func recipeNameIn(pc *packContent, id int32) (string, bool) {
	pr := pc.recipeSet()
	if pr != nil {
		if pr.removedBook[id] {
			return "", false
		}
		if id >= packBookFirstID {
			if n := int(id - packBookFirstID); n < len(pr.book) {
				return pr.book[n].name, true
			}
			return "", false
		}
	}
	if id >= 0 && int(id) < len(recipeNames) {
		return recipeNames[id], true
	}
	return "", false
}

// recipeIDIn is the display id a saved name has under a load.
func recipeIDIn(pc *packContent, name string) (int32, bool) {
	pr := pc.recipeSet()
	if pr != nil {
		if id, ok := pr.bookByName[name]; ok {
			return id, true
		}
	}
	id, ok := recipeIDByName[name]
	if ok && pr != nil && pr.removedBook[id] {
		return 0, false
	}
	return id, ok
}

// allBookIDs is every display id in the game (/recipe give *).
func allBookIDs() []int32 {
	pr := currentPack().recipeSet()
	out := make([]int32, 0, len(recipeNames))
	for i := range recipeNames {
		if pr == nil || !pr.removedBook[int32(i)] {
			out = append(out, int32(i))
		}
	}
	if pr != nil {
		for i := range pr.book {
			out = append(out, packBookFirstID+int32(i))
		}
	}
	return out
}

// remapRecipeBooks carries every player's book across a load: each entry
// by its name to the id the new load gives it, the ones the new load lacks
// kept by name (ServerRecipeBook keeps recipe keys across a reload), and
// those that come back taken up again. The book is then sent whole, as
// PlayerList.reloadResources sends it.
func (h *hub) remapRecipeBooks(players map[int32]*tracked, old, nu *packContent) {
	for _, t := range players {
		if t.rbKnown == nil {
			continue
		}
		if t.rbDormant == nil {
			t.rbDormant = map[string]bool{}
		}
		remap := func(m map[int32]bool, dormant map[string]bool) map[int32]bool {
			out := make(map[int32]bool, len(m))
			for id := range m {
				name, ok := recipeNameIn(old, id)
				if !ok {
					continue
				}
				if nid, ok := recipeIDIn(nu, name); ok {
					out[nid] = true
				} else if dormant != nil {
					dormant[name] = true
				}
			}
			return out
		}
		t.rbKnown = remap(t.rbKnown, t.rbDormant)
		t.rbHighlight = remap(t.rbHighlight, nil)
		if t.rbTaken != nil {
			t.rbTaken = remap(t.rbTaken, nil)
		}
		for name := range t.rbDormant {
			if id, ok := recipeIDIn(nu, name); ok {
				t.rbKnown[id] = true
				delete(t.rbDormant, name)
			}
		}
		h.recipeSendInitial(t)
	}
}
