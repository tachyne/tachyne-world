package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Data pack advancements (data/<ns>/advancement/…): the advancement tree is
// vanilla's generated table with the packs' advancements merged in — a
// pack's file at an id replaces that advancement, and one that does not
// load removes it (and every advancement under it, as AdvancementTree drops
// a node whose parent is missing). The tracker reads the installed load's
// registry (curAdv), so a reload swaps the whole tree at once; each player
// is then sent the tree and their progress again
// (PlayerAdvancements.reload), keeping what they had earned on the
// advancements that remain.
//
// A pack criterion is distilled to the engine's matcher (advCriterion) when
// its trigger is one the engine fires and its conditions are ones it reads:
// the item, entity, block, dimension, biome, structure and recipe
// conditions the vanilla table uses, and minecraft:tick (every tick). Any
// other criterion is unmatchable: its advancement shows and /advancement
// grants it, but play never earns it — minecraft:impossible by design.
// Rewards are paid on completion: experience, a function run as the player
// (silently, at the gamemaster level), loot tables given to the player,
// and recipes unlocked.
//
// A tree that holds a pack's advancement is laid out again with vanilla's
// TreeNodePosition (the generator's port, here in Go).

// advRewards are an advancement's rewards beyond experience.
type advRewards struct {
	function string
	loot     []string
	recipes  []string
}

// advRegistry is one load's advancement tree and the tracker's indexes.
type advRegistry struct {
	byID        map[string]*advNode
	byTrigger   map[string][]advRef
	blockRanges map[*advCriterion][2]uint32
	blockSets   map[*advCriterion][]stateRange
	children    map[string][]*advNode
	roots       []*advNode
	nodes       []*advNode // every node, by id
}

// vanillaAdv is the generated table's registry.
var vanillaAdv = func() *advRegistry {
	r := &advRegistry{byID: advByID, byTrigger: advByTrigger, blockRanges: advBlockRanges,
		blockSets: advBlockSets, children: advChildren, roots: advRoots}
	for i := range advTable {
		r.nodes = append(r.nodes, &advTable[i])
	}
	return r
}()

var activeAdv atomic.Pointer[advRegistry]

// curAdv is the installed load's advancement registry (vanilla's when no
// pack changes advancements).
func curAdv() *advRegistry {
	if r := activeAdv.Load(); r != nil {
		return r
	}
	return vanillaAdv
}

// buildAdvRegistry merges a load's advancement files over vanilla's table;
// nil when the packs carry none.
func buildAdvRegistry(files map[string]packFile, tags *tagRegistry) *advRegistry {
	if len(files) == 0 {
		return nil
	}
	nodes := map[string]*advNode{}
	for i := range advTable {
		nodes[advTable[i].id] = &advTable[i]
	}
	packIDs := map[string]bool{}
	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := files[id]
		n, err := parsePackAdvancement(id, f.data, tags)
		if err != nil {
			log.Printf("datapacks: couldn't load advancement %s from %s: %v", id, f.pack, err)
			delete(nodes, id) // the file stands at its id: vanilla's is gone
			continue
		}
		nodes[id] = n
		packIDs[id] = true
	}
	// AdvancementTree.addAll: a node whose parent never loads is dropped,
	// and with it its subtree.
	for changed := true; changed; {
		changed = false
		for id, n := range nodes {
			if n.parent != "" && nodes[n.parent] == nil {
				log.Printf("datapacks: couldn't load advancement %s: its parent %s is missing", id, n.parent)
				delete(nodes, id)
				changed = true
			}
		}
	}
	r := &advRegistry{byID: nodes, byTrigger: map[string][]advRef{}, blockRanges: map[*advCriterion][2]uint32{},
		blockSets: map[*advCriterion][]stateRange{}, children: map[string][]*advNode{}}
	for _, n := range nodes {
		r.nodes = append(r.nodes, n)
		if n.parent == "" {
			r.roots = append(r.roots, n)
		} else {
			r.children[n.parent] = append(r.children[n.parent], n)
		}
	}
	sort.Slice(r.nodes, func(i, j int) bool { return r.nodes[i].id < r.nodes[j].id })
	sort.Slice(r.roots, func(i, j int) bool { return r.roots[i].id < r.roots[j].id })
	for _, v := range r.children {
		sort.Slice(v, func(i, j int) bool { return v[i].id < v[j].id })
	}
	// Lay out again every tree that holds a pack's node (its vanilla nodes
	// copied, so the generated table keeps its own layout).
	for i, root := range r.roots {
		if !advTreeHas(r, root, packIDs) {
			continue
		}
		r.roots[i] = advCopyTree(r, root, packIDs)
		advLayout(r, r.roots[i])
	}
	for _, n := range r.nodes { // the copies replaced the nodes the indexes hold
		n = r.byID[n.id]
		pack := packIDs[n.id]
		for j := range n.criteria {
			c := &n.criteria[j] // a vanilla node's copy shares the generated criteria
			if pack {
				if rs := blockNameRanges(c.blocks); len(rs) > 0 {
					r.blockSets[c] = rs
				}
				if c.trigger == "placed_block" && c.block != "" {
					if lo, hi, ok := worldgen.BlockRangeOK(c.block); ok {
						r.blockRanges[c] = [2]uint32{lo, hi}
					}
				}
			} else {
				if rs, ok := advBlockSets[c]; ok {
					r.blockSets[c] = rs
				}
				if br, ok := advBlockRanges[c]; ok {
					r.blockRanges[c] = br
				}
			}
			if !c.unmatchable {
				r.byTrigger[c.trigger] = append(r.byTrigger[c.trigger], advRef{n, c})
			}
		}
	}
	for i := range r.nodes {
		r.nodes[i] = r.byID[r.nodes[i].id]
	}
	return r
}

// advTreeHas reports whether a subtree holds one of the ids.
func advTreeHas(r *advRegistry, n *advNode, ids map[string]bool) bool {
	if ids[n.id] {
		return true
	}
	for _, c := range r.children[n.id] {
		if advTreeHas(r, c, ids) {
			return true
		}
	}
	return false
}

// advCopyTree replaces a subtree's vanilla nodes with copies (their display
// too) in the registry, so the layout can move them.
func advCopyTree(r *advRegistry, n *advNode, packIDs map[string]bool) *advNode {
	cp := n
	if !packIDs[n.id] {
		c := *n
		if n.display != nil {
			d := *n.display
			c.display = &d
		}
		cp = &c
		r.byID[n.id] = cp
	}
	kids := r.children[n.id]
	for i, k := range kids {
		kids[i] = advCopyTree(r, k, packIDs)
	}
	return cp
}

// ---- parsing --------------------------------------------------------------

// parsePackAdvancement reads Advancement.CODEC: parent, display, criteria,
// requirements and rewards.
func parsePackAdvancement(id string, data []byte, tags *tagRegistry) (*advNode, error) {
	var top struct {
		Parent       string                     `json:"parent"`
		Display      json.RawMessage            `json:"display"`
		Criteria     map[string]json.RawMessage `json:"criteria"`
		Requirements [][]string                 `json:"requirements"`
		Rewards      *struct {
			Experience int32    `json:"experience"`
			Function   string   `json:"function"`
			Loot       []string `json:"loot"`
			Recipes    []string `json:"recipes"`
		} `json:"rewards"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	if len(top.Criteria) == 0 {
		return nil, errors.New("Advancement criteria cannot be empty")
	}
	n := &advNode{id: id}
	if top.Parent != "" {
		n.parent = nsID(top.Parent)
	}
	names := make([]string, 0, len(top.Criteria))
	for name := range top.Criteria {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		var c struct {
			Trigger    string                     `json:"trigger"`
			Conditions map[string]json.RawMessage `json:"conditions"`
		}
		if err := json.Unmarshal(top.Criteria[name], &c); err != nil || c.Trigger == "" {
			return nil, fmt.Errorf("criterion %s has no trigger", name)
		}
		crit := distillCriterion(nsID(c.Trigger), c.Conditions, tags)
		crit.name = name
		n.criteria = append(n.criteria, crit)
	}
	if len(top.Requirements) == 0 { // AdvancementRequirements.allOf
		for _, name := range names {
			n.reqs = append(n.reqs, []string{name})
		}
	} else {
		used := map[string]bool{}
		for _, g := range top.Requirements {
			if len(g) == 0 {
				return nil, errors.New("Requirement entry cannot be empty")
			}
			for _, name := range g {
				if _, ok := top.Criteria[name]; !ok {
					return nil, fmt.Errorf("Unknown criterion in requirements: %s", name)
				}
				used[name] = true
			}
		}
		for _, name := range names {
			if !used[name] {
				return nil, fmt.Errorf("Advancement completion requirements did not exist in the criteria: %s", name)
			}
		}
		n.reqs = top.Requirements
	}
	if top.Rewards != nil {
		n.xp = top.Rewards.Experience
		rw := &advRewards{loot: top.Rewards.Loot, recipes: top.Rewards.Recipes}
		if top.Rewards.Function != "" {
			rw.function = nsID(top.Rewards.Function)
		}
		if rw.function != "" || len(rw.loot) > 0 || len(rw.recipes) > 0 {
			n.rewards = rw
		}
	}
	if len(top.Display) > 0 && string(top.Display) != "null" {
		d, err := parseAdvDisplay(top.Display)
		if err != nil {
			return nil, err
		}
		n.display = d
	}
	return n, nil
}

// parseAdvDisplay reads DisplayInfo.
func parseAdvDisplay(raw json.RawMessage) (*advDisplay, error) {
	var dd struct {
		Title       json.RawMessage `json:"title"`
		Description json.RawMessage `json:"description"`
		Icon        json.RawMessage `json:"icon"`
		Frame       string          `json:"frame"`
		Background  string          `json:"background"`
		ShowToast   *bool           `json:"show_toast"`
		Announce    *bool           `json:"announce_to_chat"`
		Hidden      bool            `json:"hidden"`
	}
	if err := json.Unmarshal(raw, &dd); err != nil {
		return nil, err
	}
	if dd.Title == nil || dd.Description == nil || dd.Icon == nil {
		return nil, errors.New("a display needs a title, a description and an icon")
	}
	d := &advDisplay{showToast: true, announceChat: true, hidden: dd.Hidden}
	d.title, d.titleEN = advText(dd.Title)
	d.desc, d.descEN = advText(dd.Description)
	icon, _, err := recipeResult(dd.Icon)
	if err != nil {
		return nil, fmt.Errorf("icon: %v", err)
	}
	d.icon = icon
	switch dd.Frame {
	case "", "task":
	case "challenge":
		d.frame = 1
	case "goal":
		d.frame = 2
	default:
		return nil, fmt.Errorf("unknown frame %s", dd.Frame)
	}
	if dd.Background != "" {
		d.background = strings.TrimPrefix(nsID(dd.Background), "minecraft:")
	}
	if dd.ShowToast != nil {
		d.showToast = *dd.ShowToast
	}
	if dd.Announce != nil {
		d.announceChat = *dd.Announce
	}
	return d, nil
}

// advText is a text component's key (a translate key, or the literal text
// the client then shows as it is) and its English text.
func advText(raw json.RawMessage) (key, en string) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, s
	}
	var c struct {
		Text      string `json:"text"`
		Translate string `json:"translate"`
		Fallback  string `json:"fallback"`
	}
	if json.Unmarshal(raw, &c) == nil {
		if c.Translate != "" {
			en := c.Fallback
			if en == "" {
				en = c.Translate
			}
			return c.Translate, en
		}
		return c.Text, c.Text
	}
	return "", ""
}

// ---- criteria -------------------------------------------------------------

// advAlwaysTriggers are the triggers whose criterion holds whenever it fires
// (no conditions read).
var advAlwaysTriggers = map[string]bool{
	"slept_in_bed": true, "villager_trade": true, "enchanted_item": true, "brewed_potion": true,
	"cured_zombie_villager": true, "avoid_vibration": true, "hero_of_the_village": true,
	"kill_mob_near_sculk_catalyst": true, "tick": true,
}

// distillCriterion is one pack criterion as the engine's matcher, or an
// unmatchable one.
func distillCriterion(trigger string, cond map[string]json.RawMessage, tags *tagRegistry) advCriterion {
	short := strings.TrimPrefix(trigger, "minecraft:")
	c := advCriterion{trigger: short}
	bad := advCriterion{trigger: short, unmatchable: true}
	if !strings.HasPrefix(trigger, "minecraft:") {
		return bad
	}
	read := map[string]bool{}
	use := func(k string) (json.RawMessage, bool) {
		v, ok := cond[k]
		read[k] = true
		return v, ok
	}
	switch {
	case advAlwaysTriggers[short]:
	case short == "inventory_changed":
		raw, ok := use("items")
		if ok {
			var preds []json.RawMessage
			if json.Unmarshal(raw, &preds) != nil {
				return bad
			}
			for _, p := range preds {
				items, ok := advItemPredicate(p, tags)
				if !ok {
					return bad
				}
				c.items = append(c.items, items)
			}
		}
	case short == "consume_item" || short == "filled_bucket" || short == "used_totem" ||
		short == "fishing_rod_hooked" || short == "shot_crossbow":
		if raw, ok := use("item"); ok {
			items, ok := advItemPredicate(raw, tags)
			if !ok {
				return bad
			}
			c.items = [][]int32{items}
		}
	case short == "player_killed_entity" || short == "entity_killed_player" || short == "tame_animal" ||
		short == "summoned_entity" || short == "bred_animals":
		key := "entity"
		if short == "bred_animals" {
			key = "child"
		}
		if raw, ok := use(key); ok {
			if !advEntityPredicate(raw, tags, &c) {
				return bad
			}
		}
	case short == "enter_block":
		if raw, ok := use("block"); ok {
			var name string
			if json.Unmarshal(raw, &name) != nil {
				return bad
			}
			c.blocks = []string{strings.TrimPrefix(nsID(name), "minecraft:")}
		}
	case short == "placed_block":
		if raw, ok := use("location"); ok {
			var conds []map[string]json.RawMessage
			if json.Unmarshal(raw, &conds) != nil || len(conds) != 1 {
				return bad
			}
			var typ, block string
			_ = json.Unmarshal(conds[0]["condition"], &typ)
			_ = json.Unmarshal(conds[0]["block"], &block)
			if nsID(typ) != "minecraft:block_state_property" || block == "" || len(conds[0]) != 2 {
				return bad
			}
			c.block = strings.TrimPrefix(nsID(block), "minecraft:")
		} else {
			return bad // any block: the matcher reads a named one
		}
	case short == "changed_dimension":
		if raw, ok := use("to"); ok {
			dim, ok := advDimension(raw)
			if !ok {
				return bad
			}
			c.dim, c.hasDim = dim, true
		}
	case short == "location":
		if raw, ok := use("player"); ok {
			pred, ok := advPlayerPredicate(raw)
			if !ok {
				return bad
			}
			loc, has := pred["location"]
			if !has || len(pred) != 1 {
				return bad
			}
			var l map[string]json.RawMessage
			if json.Unmarshal(loc, &l) != nil {
				return bad
			}
			for k, v := range l {
				var s string
				if json.Unmarshal(v, &s) != nil {
					return bad // a list or a tag: the matcher reads one
				}
				switch k {
				case "biomes":
					if strings.HasPrefix(s, "#") {
						return bad
					}
					c.biome = strings.TrimPrefix(nsID(s), "minecraft:")
				case "structures":
					if strings.HasPrefix(s, "#") {
						return bad
					}
					c.structure = strings.TrimPrefix(nsID(s), "minecraft:")
				case "dimension":
					dim, ok := advDimension(v)
					if !ok {
						return bad
					}
					c.dim, c.hasDim = dim, true
				default:
					return bad
				}
			}
		}
	case short == "recipe_crafted" || short == "crafter_recipe_crafted":
		if raw, ok := use("recipe_id"); ok {
			var id string
			if json.Unmarshal(raw, &id) != nil {
				return bad
			}
			c.recipe = bookRecipeName(nsID(id))
		}
	default:
		return bad
	}
	for k := range cond {
		if !read[k] {
			return bad // a condition the matcher does not read: it would match too much
		}
	}
	return c
}

// advItemPredicate reads an ItemPredicate's items (an id, a list, or a
// #tag); any other field is not read.
func advItemPredicate(raw json.RawMessage, tags *tagRegistry) ([]int32, bool) {
	var p map[string]json.RawMessage
	if json.Unmarshal(raw, &p) != nil {
		return nil, false
	}
	for k := range p {
		if k != "items" {
			return nil, false
		}
	}
	itemsRaw, ok := p["items"]
	if !ok {
		return nil, false
	}
	var v any
	_ = json.Unmarshal(itemsRaw, &v)
	var out []int32
	for _, n := range holderSetNames(v) {
		if t, isTag := strings.CutPrefix(n, "#"); isTag {
			members, ok := tags.members("item", nsID(t))
			if !ok {
				return nil, false
			}
			for _, m := range members {
				if it, ok := itemIDOf(m); ok {
					out = append(out, it)
				}
			}
			continue
		}
		it, ok := itemIDOf(n)
		if !ok {
			return nil, false
		}
		out = append(out, it)
	}
	return out, len(out) > 0
}

// advPlayerPredicate reads ContextAwarePredicate for the entity a
// criterion names: the short EntityPredicate form, or a single
// entity_properties condition on this. The predicate's fields come back.
func advPlayerPredicate(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var list []map[string]json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		if len(list) != 1 {
			return nil, false
		}
		var cond, ent string
		_ = json.Unmarshal(list[0]["condition"], &cond)
		_ = json.Unmarshal(list[0]["entity"], &ent)
		if nsID(cond) != "minecraft:entity_properties" || (ent != "" && ent != "this") {
			return nil, false
		}
		var pred map[string]json.RawMessage
		if json.Unmarshal(list[0]["predicate"], &pred) != nil {
			return nil, false
		}
		return pred, true
	}
	var pred map[string]json.RawMessage
	if json.Unmarshal(raw, &pred) != nil {
		return nil, false
	}
	return pred, true
}

// advEntityPredicate reads an entity condition naming a type or a type
// tag.
func advEntityPredicate(raw json.RawMessage, tags *tagRegistry, c *advCriterion) bool {
	pred, ok := advPlayerPredicate(raw)
	if !ok {
		return false
	}
	for k := range pred {
		if k != "type" {
			return false
		}
	}
	var typ string
	if json.Unmarshal(pred["type"], &typ) != nil {
		return false
	}
	if t, isTag := strings.CutPrefix(typ, "#"); isTag {
		members, ok := tags.members("entity_type", nsID(t))
		if !ok {
			return false
		}
		c.entities = []string{}
		for _, m := range members {
			c.entities = append(c.entities, strings.TrimPrefix(m, "minecraft:"))
		}
		if c.trigger != "player_killed_entity" && c.trigger != "entity_killed_player" {
			return false // only the kill triggers read a type set
		}
		return true
	}
	name := strings.TrimPrefix(nsID(typ), "minecraft:")
	if _, ok := entityByName[name]; !ok && name != "player" {
		return false
	}
	c.entity = name
	return true
}

// advDimension reads a dimension key as the engine's dimension.
func advDimension(raw json.RawMessage) (int32, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return 0, false
	}
	for id, d := range world.Dimensions {
		if d.Key == nsID(s) {
			return int32(id), true
		}
	}
	return 0, false
}

// ---- layout ---------------------------------------------------------------

// advTNP is TreeNodePosition (Buchheim's tidy tree, as vanilla runs it per
// tree): x the depth, y the row.
type advTNP struct {
	node             *advNode
	parent, prev     *advTNP
	childIndex       int
	children         []*advTNP
	ancestor, thread *advTNP
	x, y             float64
	mod, change      float64
	shift            float64
}

func newAdvTNP(r *advRegistry, n *advNode, parent, prev *advTNP, childIndex int, depth float64) *advTNP {
	t := &advTNP{node: n, parent: parent, prev: prev, childIndex: childIndex, x: depth, y: -1}
	t.ancestor = t
	var last *advTNP
	for _, ch := range r.children[n.id] {
		last = t.addChild(r, ch, last)
	}
	return t
}

func (t *advTNP) addChild(r *advRegistry, n *advNode, prev *advTNP) *advTNP {
	if n.display != nil {
		prev = newAdvTNP(r, n, t, prev, len(t.children)+1, t.x+1)
		t.children = append(t.children, prev)
		return prev
	}
	for _, gc := range r.children[n.id] {
		prev = t.addChild(r, gc, prev)
	}
	return prev
}

func (t *advTNP) firstWalk() {
	if len(t.children) == 0 {
		if t.prev != nil {
			t.y = t.prev.y + 1
		} else {
			t.y = 0
		}
		return
	}
	var def *advTNP
	for _, ch := range t.children {
		ch.firstWalk()
		if def == nil {
			def = ch.apportion(ch)
		} else {
			def = ch.apportion(def)
		}
	}
	t.executeShifts()
	mid := (t.children[0].y + t.children[len(t.children)-1].y) / 2
	if t.prev != nil {
		t.y = t.prev.y + 1
		t.mod = t.y - mid
	} else {
		t.y = mid
	}
}

func (t *advTNP) secondWalk(modSum, depth, mn float64) float64 {
	t.y += modSum
	t.x = depth
	mn = min(mn, t.y)
	for _, ch := range t.children {
		mn = ch.secondWalk(modSum+t.mod, depth+1, mn)
	}
	return mn
}

func (t *advTNP) thirdWalk(off float64) {
	t.y += off
	for _, ch := range t.children {
		ch.thirdWalk(off)
	}
}

func (t *advTNP) executeShifts() {
	shift, change := 0.0, 0.0
	for i := len(t.children) - 1; i >= 0; i-- {
		ch := t.children[i]
		ch.y += shift
		ch.mod += shift
		change += ch.change
		shift += ch.shift + change
	}
}

func (t *advTNP) prevOrThread() *advTNP {
	if t.thread != nil {
		return t.thread
	}
	if len(t.children) > 0 {
		return t.children[0]
	}
	return nil
}

func (t *advTNP) nextOrThread() *advTNP {
	if t.thread != nil {
		return t.thread
	}
	if len(t.children) > 0 {
		return t.children[len(t.children)-1]
	}
	return nil
}

func (t *advTNP) apportion(def *advTNP) *advTNP {
	if t.prev == nil {
		return def
	}
	vir, vor := t, t
	vil, vol := t.prev, t.parent.children[0]
	sir, sor := t.mod, t.mod
	sil, sol := vil.mod, vol.mod
	for vil.nextOrThread() != nil && vir.prevOrThread() != nil {
		vil = vil.nextOrThread()
		vir = vir.prevOrThread()
		vol = vol.prevOrThread()
		vor = vor.nextOrThread()
		vor.ancestor = t
		if shift := vil.y + sil - (vir.y + sir) + 1; shift > 0 {
			vil.getAncestor(t, def).moveSubtree(t, shift)
			sir += shift
			sor += shift
		}
		sil += vil.mod
		sir += vir.mod
		sol += vol.mod
		sor += vor.mod
	}
	if vil.nextOrThread() != nil && vor.nextOrThread() == nil {
		vor.thread = vil.nextOrThread()
		vor.mod += sil - sor
	} else {
		if vir.prevOrThread() != nil && vol.prevOrThread() == nil {
			vol.thread = vir.prevOrThread()
			vol.mod += sir - sol
		}
		def = t
	}
	return def
}

func (t *advTNP) moveSubtree(right *advTNP, shift float64) {
	if subtrees := float64(right.childIndex - t.childIndex); subtrees != 0 {
		right.change -= shift / subtrees
		t.change += shift / subtrees
	}
	right.shift += shift
	right.y += shift
	right.mod += shift
}

func (t *advTNP) getAncestor(other, def *advTNP) *advTNP {
	if t.ancestor != nil {
		for _, c := range other.parent.children {
			if c == t.ancestor {
				return t.ancestor
			}
		}
	}
	return def
}

func (t *advTNP) finalize() {
	if d := t.node.display; d != nil {
		d.x, d.y = float32(t.x), float32(t.y)
	}
	for _, ch := range t.children {
		ch.finalize()
	}
}

// advLayout lays one tree out.
func advLayout(r *advRegistry, root *advNode) {
	tp := newAdvTNP(r, root, nil, nil, 1, 0)
	tp.firstWalk()
	if mn := tp.secondWalk(0, 0, tp.y); mn < 0 {
		tp.thirdWalk(-mn)
	}
	tp.finalize()
}

// ---- the hub's side -------------------------------------------------------

// advPayRewards pays an advancement's rewards beyond experience
// (AdvancementRewards.grant): its loot tables into the player's inventory,
// its recipes into their book, and its function run as them.
func (h *hub) advPayRewards(players map[int32]*tracked, t *tracked, rw *advRewards) {
	if rw == nil {
		return
	}
	for _, id := range rw.loot {
		name := strings.TrimPrefix(nsID(id), "minecraft:")
		tbl, ok := lootForChest(name)
		if !ok {
			continue
		}
		ctx := &lootCtx{rng: h.rng.Intn, randf: h.rng.Float64, pos: blockPos{floorInt(t.x), floorInt(t.y), floorInt(t.z)},
			located: true, biomeAt: h.worldFor(t.dim).BiomeAt3D}
		for _, st := range splitStacks(h.evalChestStacks(tbl, ctx, 0)) {
			h.giveStack(players, t, st)
		}
	}
	if len(rw.recipes) > 0 {
		if t.rbKnown == nil {
			t.rbKnown, t.rbHighlight = map[int32]bool{}, map[int32]bool{}
		}
		var ids []int32
		for _, r := range rw.recipes {
			ids = append(ids, recipeIDsByName(bookRecipeName(nsID(r)))...)
		}
		h.awardRecipes(t, ids)
	}
	if rw.function != "" && h.advFunction != nil {
		h.advFunction(t.p, rw.function)
	}
}

// onPackAdvancementsChanged is PlayerAdvancements.reload for every player
// after a load that changed the tree: their state forgets the
// advancements that are gone, and they are sent the tree and their
// progress again.
func (h *hub) onPackAdvancementsChanged(players map[int32]*tracked, old, pc *packContent) {
	var a, b *advRegistry
	if old != nil {
		a = old.adv
	}
	if pc != nil {
		b = pc.adv
	}
	if a == b {
		return
	}
	reg := curAdv()
	for _, t := range players {
		if t.adv == nil || t.p.exec != nil {
			continue
		}
		for id := range t.adv {
			if _, ok := reg.byID[id]; !ok {
				delete(t.adv, id)
			}
		}
		h.advSendAll(t)
	}
}

// runRewardFunction is an advancement's reward function, run as
// AdvancementRewards.grant runs it: as the player, where they stand, with
// the output suppressed and at the gamemaster level — the function server
// source (functionPermission) running it through execute as/at the player.
// Called off the hub (the runner waits on it).
func (s *Server) runRewardFunction(p *player, id string) {
	lib := s.hub.functions.Load()
	if lib == nil || lib.function(id) == nil {
		return
	}
	line := "execute as " + uuidString(p.uuid) + " at @s run function " + id
	s.asFunctionServer(func(src *player, lim fnLimits) {
		s.runFunctions(src, []fnInstance{{id: id, lines: []string{line}}}, lim)
	})
}
