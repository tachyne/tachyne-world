package server

// Advancements: the vanilla 1.21.11 tree (advancements_gen.go, distilled by
// scripts/gen_advancements.py) + the engine-side criteria tracker. The engine
// owns criteria evaluation, grant state, persistence, and chat announcements;
// the display tree crosses the attach protocol once per join (MsgAdvTree) and
// per-player progress streams as MsgAdvProgress — wire composition is the
// gateways' job (render770).
//
// Criteria whose trigger the engine can't observe yet are generated with
// unmatchable: true — their advancements are visible in the client's tree but
// unobtainable until the mechanic lands (see ~/minecraft/TODO.md parity list).

// advDisplay is one advancement's UI face. title/desc are client translate
// keys (Java clients render them natively); titleEN/descEN are the resolved
// English strings for server-side text (chat announce, Bedrock fallback).
// x,y is the vanilla tidy-tree layout, computed at generation time.
type advDisplay struct {
	title, desc     string
	titleEN, descEN string
	icon            int32 // item id (itemByName space)
	frame           int8  // 0 task, 1 challenge, 2 goal (wire enum order)
	background      string
	showToast       bool
	announceChat    bool
	hidden          bool
	x, y            float32
}

// advCriterion is one criterion, distilled to what the engine can match.
type advCriterion struct {
	name    string
	trigger string // short trigger name (no namespace)

	entity   string    // entity-typed triggers ("" = any)
	items    [][]int32 // inventory_changed (all sets present) / the criterion's item, tool or weapon (items[0])
	block    string    // placed_block, simple form
	biome    string    // location / item_used_on_block biome
	dim      int32     // changed_dimension target, or a player-location dimension (0/1/2)
	hasDim   bool
	minLevel int // construct_beacon minimum pyramid tier

	// The generic condition schema (gen_advancements.py distill): each
	// trigger uses the few it needs; the matcher below reads them.
	blocks       []string          // block set (tags expanded) for the block at the site
	props        map[string]string // required block-state properties there
	locChecks    [][]advLocCheck   // placed_block: OR of AND-groups of offset checks
	structure    string            // location: the structure the player stands in
	smokey       bool              // item_used_on_block: a campfire within 5 below
	equipFeet    []int32           // location: boots worn
	baby         int8              // entity: 0 adult, 1 baby, -1 any (zero value; see genBaby)
	hasBaby      bool
	variant      string    // entity variant (frog)
	effects      []string  // effects_changed: every one must be active
	sourceEntity string    // effects_changed: what granted it
	recipe       string    // recipe_crafted / crafter_recipe_crafted
	ingredients  [][]int32 // recipe_crafted: each set must be among the inputs
	lootTable    string    // player_generates_container_loot
	damageDirect []string  // *_hurt_*: the direct entity's type (tags expanded)
	damageTag    string    // *_hurt_*: a damage-type tag the hit must carry
	mainhand     []int32   // *_hurt_*: the attacker's held item
	minDealt     float64
	blocked      bool     // entity_hurt_player: the hit was shield-blocked
	victims      []string // killed_by_arrow: victim types, with multiplicity
	minUnique    int      // killed_by_arrow: distinct victim types
	minCount     int      // bee_nest_destroyed bees / spear_mobs
	signal       int      // target_hit signal strength
	minDistH     float64
	minDistY     float64
	minDistAbs   float64
	maxDistAbs   float64
	startYMin    float64
	endYMax      float64
	vehicle      string
	passenger    string
	lookingAt    string
	bystander    string
	noFire       bool
	cause        string
	enchant      string
	toolPred     string

	unmatchable bool // trigger/conditions the engine can't observe yet
}

// advLocCheck is one placed_block location term: the block at an offset from
// the placed block must be one of blocks, with props.
type advLocCheck struct {
	dx, dy, dz int
	blocks     []string
	props      map[string]string
	ranges     []stateRange // resolved from blocks at init (advBlockSets)
}

// advNode is one advancement. reqs is the wire's OR-of-AND requirements
// (criterion names); display nil = invisible helper node.
type advNode struct {
	id, parent string
	criteria   []advCriterion
	reqs       [][]string
	display    *advDisplay
	xp         int32 // rewards.experience
}
