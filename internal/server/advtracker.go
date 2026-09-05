package server

// The advancement criteria tracker core: pure state + evaluation, no hub
// wiring (advancement_hooks.go plugs the engine's gameplay events in). Grant
// state is per player: advID → criterion name → obtained unix millis.

import (
	"sort"
	"time"

	attach "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// advByID and advByTrigger index the generated table once at init.
var advByID = func() map[string]*advNode {
	m := make(map[string]*advNode, len(advTable))
	for i := range advTable {
		m[advTable[i].id] = &advTable[i]
	}
	return m
}()

type advRef struct {
	node *advNode
	crit *advCriterion
}

var advByTrigger = func() map[string][]advRef {
	m := map[string][]advRef{}
	for i := range advTable {
		n := &advTable[i]
		for j := range n.criteria {
			c := &n.criteria[j]
			if c.unmatchable {
				continue
			}
			m[c.trigger] = append(m[c.trigger], advRef{n, c})
		}
	}
	return m
}()

// advBlockRanges resolves placed_block criteria block names to state-id ranges
// once. A name the block table doesn't know drops the criterion (rather than
// panicking in BlockRange at match time).
var advBlockRanges = func() map[*advCriterion][2]uint32 {
	m := map[*advCriterion][2]uint32{}
	for _, ref := range advByTrigger["placed_block"] {
		func() {
			defer func() { recover() }()
			lo, hi := worldgen.BlockRange(ref.crit.block)
			m[ref.crit] = [2]uint32{lo, hi}
		}()
	}
	return m
}()

// advTreeNode converts a table node to its attach-frame form.
func advTreeNode(n *advNode) attach.AdvNode {
	an := attach.AdvNode{ID: n.id, Parent: n.parent, Reqs: n.reqs}
	if d := n.display; d != nil {
		an.HasDisplay = true
		an.Title = d.title
		an.Desc = d.desc
		an.Icon = attach.ItemStack{ID: d.icon, Count: 1}
		an.Frame = int32(d.frame)
		an.Background = d.background
		an.ShowToast = d.showToast
		an.Announce = d.announceChat
		an.Hidden = d.hidden
		an.X = d.x
		an.Y = d.y
	}
	return an
}

// advState is one player's grant state.
type advState map[string]map[string]int64

// done reports whether the advancement's requirements are satisfied
// (OR-of-ANDs over obtained criteria — vanilla AdvancementRequirements.test).
func (s advState) done(n *advNode) bool {
	if len(n.reqs) == 0 {
		return false
	}
	obtained := s[n.id]
	for _, group := range n.reqs {
		hit := false
		for _, c := range group {
			if _, ok := obtained[c]; ok {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// grant marks one criterion obtained. Returns (criterionWasNew,
// advancementJustCompleted).
func (s advState) grant(n *advNode, crit string) (bool, bool) {
	m := s[n.id]
	if m == nil {
		m = map[string]int64{}
		s[n.id] = m
	}
	if _, ok := m[crit]; ok {
		return false, false
	}
	wasDone := s.done(n)
	m[crit] = time.Now().UnixMilli()
	return true, !wasDone && s.done(n)
}

// snapshot renders the player's full progress (the join-time Reset frame).
// Ordered for deterministic frames.
func (s advState) snapshot() attach.AdvProgress {
	p := attach.AdvProgress{Reset: true}
	ids := make([]string, 0, len(s))
	for id := range s {
		if _, ok := advByID[id]; !ok {
			continue // advancement gone from the table (data upgrade)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		p.Entries = append(p.Entries, attach.AdvProgressEntry{ID: id, Done: s[id]})
	}
	return p
}

// advMatch is a trigger payload: the facts an engine site knows when it
// fires. Fields unused by a trigger stay zero. The matcher below compares
// them with the criterion's distilled conditions (advCriterion).
type advMatch struct {
	entity     string                      // species/registry name for entity triggers
	item       int32                       // the item: consumed, used, tool, weapon, bucket, totem…
	inv        []int32                     // full inventory item-id set for inventory_changed
	blockState uint32                      // the block at the site (placed, used-on, entered, stepped on)
	blockAt    func(dx, dy, dz int) uint32 // placed_block: neighbours by offset
	biome      string
	structure  string
	smokey     bool
	dim        int32
	level      int // construct_beacon pyramid tier

	baby                  bool
	variant               string
	feet                  int32          // boots item
	effects               map[int32]bool // effects_changed: the player's active set
	sourceEntity          string         // effects_changed: who granted it
	recipe                string         // recipe_crafted / crafter_recipe_crafted
	ingredients           []int32        // recipe_crafted: the items consumed
	lootTable             string
	damageDirect          string          // *_hurt_*: the direct entity's type
	damageTags            map[string]bool // *_hurt_*: tags the damage type carries
	mainhand              int32           // attacker's held item
	dealt                 float64
	blocked               bool
	victims               []string // killed_by_arrow: types killed by this projectile so far
	count                 int      // bees inside / spear count
	signal                int      // target_hit
	distH, distY, distAbs float64
	startY, endY          float64
	vehicle               string
	passenger             string
	lookingAt             string
	bystander             bool
	noFire                bool
	cause                 string
	enchant               string
}

// advBlockSets resolves every criterion's block list to state ranges once.
var advBlockSets = func() map[*advCriterion][]stateRange {
	m := map[*advCriterion][]stateRange{}
	for i := range advTable {
		for j := range advTable[i].criteria {
			c := &advTable[i].criteria[j]
			if rs := blockNameRanges(c.blocks); len(rs) > 0 {
				m[c] = rs
			}
			for _, g := range c.locChecks {
				for k := range g {
					g[k].ranges = blockNameRanges(g[k].blocks)
				}
			}
		}
	}
	return m
}()

func blockNameRanges(names []string) []stateRange {
	var out []stateRange
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			out = append(out, stateRange{lo, hi})
		}
	}
	return out
}

// stateHasProps reports whether every required property holds on the state.
func stateHasProps(state uint32, props map[string]string) bool {
	if len(props) == 0 {
		return true
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return false
	}
	for k, v := range props {
		if worldgen.GetProperty(info, state, k) != v {
			return false
		}
	}
	return true
}

// blockMatches: the state is in the criterion's block set (an empty set is
// "any block") and carries the required properties.
func (c *advCriterion) blockMatches(state uint32) bool {
	if rs := advBlockSets[c]; len(rs) > 0 && !inRanges(rs, state) {
		return false
	}
	return stateHasProps(state, c.props)
}

func (m advMatch) itemIn(c *advCriterion) bool {
	return len(c.items) == 0 || containsID(c.items[0], m.item)
}

func (m advMatch) entityIs(c *advCriterion) bool {
	if c.entity != "" && c.entity != m.entity {
		return false
	}
	if c.hasBaby && (c.baby == 1) != m.baby {
		return false
	}
	return c.variant == "" || c.variant == m.variant
}

func (m advMatch) criterion(c *advCriterion) bool {
	switch c.trigger {
	case "inventory_changed":
		for _, pred := range c.items {
			if !containsAny(m.inv, pred) {
				return false
			}
		}
		return true
	case "consume_item", "fishing_rod_hooked", "filled_bucket", "used_totem", "shot_crossbow":
		return m.itemIn(c)
	case "item_durability_changed":
		return m.itemIn(c) && (c.vehicle == "" || c.vehicle == m.vehicle)
	case "player_killed_entity", "entity_killed_player", "bred_animals", "tame_animal",
		"summoned_entity", "thrown_item_picked_up_by_player":
		return m.entityIs(c)
	case "player_interacted_with_entity", "player_sheared_equipment", "thrown_item_picked_up_by_entity":
		return m.entityIs(c) && m.itemIn(c)
	case "placed_block":
		if len(c.locChecks) > 0 {
			if m.blockAt == nil {
				return false
			}
			for _, group := range c.locChecks {
				ok := true
				for _, chk := range group {
					st := m.blockAt(chk.dx, chk.dy, chk.dz)
					if (len(chk.ranges) > 0 && !inRanges(chk.ranges, st)) || !stateHasProps(st, chk.props) {
						ok = false
						break
					}
				}
				if ok {
					return true
				}
			}
			return false
		}
		r, ok := advBlockRanges[c]
		return ok && m.blockState >= r[0] && m.blockState <= r[1]
	case "item_used_on_block":
		if !c.blockMatches(m.blockState) || !m.itemIn(c) {
			return false
		}
		if c.biome != "" && c.biome != m.biome {
			return false
		}
		if c.smokey && !m.smokey {
			return false
		}
		return c.toolPred == "" || c.toolPred == "jukebox_playable" // the site only fires with a disc
	case "changed_dimension":
		return !c.hasDim || c.dim == m.dim
	case "location":
		if c.biome != "" && c.biome != m.biome {
			return false
		}
		if c.structure != "" && c.structure != m.structure {
			return false
		}
		if len(c.blocks) > 0 && !c.blockMatches(m.blockState) {
			return false
		}
		return len(c.equipFeet) == 0 || containsID(c.equipFeet, m.feet)
	case "construct_beacon":
		return m.level >= c.minLevel
	case "effects_changed":
		if c.sourceEntity != "" && c.sourceEntity != m.sourceEntity {
			return false
		}
		for _, name := range c.effects {
			id, ok := effectNames[name]
			if !ok || !m.effects[id] {
				return false
			}
		}
		return true
	case "recipe_crafted", "crafter_recipe_crafted":
		if c.recipe != "" && c.recipe != m.recipe {
			return false
		}
		for _, set := range c.ingredients { // each predicate set must be satisfied by one input
			if !containsAny(m.ingredients, set) {
				return false
			}
		}
		return true
	case "player_generates_container_loot":
		return c.lootTable == m.lootTable
	case "player_hurt_entity", "entity_hurt_player":
		if len(c.damageDirect) > 0 && !containsStr(c.damageDirect, m.damageDirect) {
			return false
		}
		if c.damageTag != "" && !m.damageTags[c.damageTag] {
			return false
		}
		if len(c.mainhand) > 0 && !containsID(c.mainhand, m.mainhand) {
			return false
		}
		if c.minDealt > 0 && m.dealt < c.minDealt {
			return false
		}
		return !c.blocked || m.blocked
	case "killed_by_arrow":
		if !m.itemIn(c) {
			return false
		}
		if c.minUnique > 0 {
			seen := map[string]bool{}
			for _, v := range m.victims {
				seen[v] = true
			}
			if len(seen) < c.minUnique {
				return false
			}
		}
		if len(c.victims) > 0 { // every listed victim must be matched by a distinct kill
			used := make([]bool, len(m.victims))
			for _, want := range c.victims {
				found := false
				for i, have := range m.victims {
					if !used[i] && (want == "" || want == have) {
						used[i], found = true, true
						break
					}
				}
				if !found {
					return false
				}
			}
		}
		return true
	case "using_item":
		return m.itemIn(c) && (c.lookingAt == "" || c.lookingAt == m.lookingAt)
	case "enter_block", "slide_down_block":
		return c.blockMatches(m.blockState)
	case "bee_nest_destroyed":
		if !c.blockMatches(m.blockState) || m.count < c.minCount {
			return false
		}
		return c.enchant == "" || c.enchant == m.enchant
	case "target_hit":
		return m.signal >= c.signal && m.distH >= c.minDistH
	case "levitation", "nether_travel", "ride_entity_in_lava":
		if m.distY < c.minDistY || m.distH < c.minDistH || m.distAbs < c.minDistAbs {
			return false
		}
		if c.vehicle != "" && c.vehicle != m.vehicle {
			return false
		}
		return !c.hasDim || c.dim == m.dim
	case "fall_from_height":
		return m.distY >= c.minDistY && m.startY >= c.startYMin && (c.endYMax == 0 || m.endY <= c.endYMax)
	case "fall_after_explosion":
		return m.distY >= c.minDistY && (c.cause == "" || c.cause == m.cause)
	case "lightning_strike":
		if c.bystander != "" && !m.bystander {
			return false
		}
		return !c.noFire || m.noFire
	case "started_riding":
		return (c.vehicle == "" || c.vehicle == m.vehicle) && (c.passenger == "" || c.passenger == m.passenger)
	case "spear_mobs":
		return m.count >= c.minCount
	case "slept_in_bed", "villager_trade", "enchanted_item", "brewed_potion",
		"cured_zombie_villager", "avoid_vibration", "hero_of_the_village",
		"kill_mob_near_sculk_catalyst":
		return true
	}
	return false
}

func containsStr(set []string, s string) bool {
	for _, v := range set {
		if v == s {
			return true
		}
	}
	return false
}

func containsID(set []int32, id int32) bool {
	for _, v := range set {
		if v == id {
			return true
		}
	}
	return false
}

func containsAny(have []int32, want []int32) bool {
	for _, h := range have {
		if containsID(want, h) {
			return true
		}
	}
	return false
}

// --- visibility (the vanilla server's rule) ---
//
// Vanilla never ships the whole tree: a node is sent iff it (or a descendant)
// is done, or a done node sits within VISIBILITY_DEPTH(2) ancestors with no
// hidden-unearned/undisplayed node closer. Everything else stays unsent — the
// tree reveals itself as a frontier around progress, and hidden advancements
// appear only once earned. Shipping the full tree instead draws connector
// lines into invisible hidden nodes on the client ("lines to nothing").

// advChildren / advRoots index the tree shape once (children sorted by id).
var advChildren, advRoots = func() (map[string][]*advNode, []*advNode) {
	kids := map[string][]*advNode{}
	var roots []*advNode
	for i := range advTable {
		n := &advTable[i]
		if n.parent == "" {
			roots = append(roots, n)
		} else {
			kids[n.parent] = append(kids[n.parent], n)
		}
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].id < roots[j].id })
	for _, v := range kids {
		sort.Slice(v, func(i, j int) bool { return v[i].id < v[j].id })
	}
	return kids, roots
}()

const advVisibilityDepth = 2

type advVisRule int8

const (
	advVisShow advVisRule = iota
	advVisHide
	advVisNoChange
)

func advRuleFor(n *advNode, done bool) advVisRule {
	switch {
	case n.display == nil:
		return advVisHide
	case done:
		return advVisShow
	case n.display.hidden:
		return advVisHide
	}
	return advVisNoChange
}

// visible computes the player's visible-node set — a faithful port of the
// vanilla evaluator: DFS with an ancestor-rule stack; an unfinished subtree
// is visible when the nearest decisive rule within self+2 ancestors is SHOW.
func (s advState) visible() map[string]bool {
	out := make(map[string]bool, 32)
	stack := []advVisRule{advVisNoChange, advVisNoChange, advVisNoChange}
	var walk func(n *advNode) bool
	walk = func(n *advNode) bool {
		selfDone := s.done(n)
		stack = append(stack, advRuleFor(n, selfDone))
		anyDone := selfDone
		for _, c := range advChildren[n.id] {
			anyDone = walk(c) || anyDone
		}
		vis := anyDone
		if !vis {
			for i := 0; i <= advVisibilityDepth; i++ {
				switch stack[len(stack)-1-i] {
				case advVisShow:
					vis = true
				case advVisHide:
					vis = false
				default:
					continue
				}
				break
			}
		}
		stack = stack[:len(stack)-1]
		if vis {
			out[n.id] = true
		}
		return anyDone
	}
	for _, r := range advRoots {
		walk(r)
	}
	return out
}

// visibleTree assembles the AdvTree frame for a visible-node set, in
// parent-before-child order.
func visibleTree(vis map[string]bool, skip map[string]bool) attach.AdvTree {
	t := attach.AdvTree{}
	var walk func(n *advNode)
	walk = func(n *advNode) {
		if vis[n.id] && !skip[n.id] {
			t.Nodes = append(t.Nodes, advTreeNode(n))
		}
		for _, c := range advChildren[n.id] {
			walk(c)
		}
	}
	for _, r := range advRoots {
		walk(r)
	}
	return t
}
