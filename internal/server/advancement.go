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

	entity   string    // player_killed_entity / entity_killed_player / bred_animals / tame_animal ("" = any)
	items    [][]int32 // inventory_changed (all predicate sets must be present) / consume_item (items[0])
	block    string    // placed_block
	biome    string    // location biome visit
	dim      int32     // changed_dimension target (0/1/2)
	hasDim   bool
	minLevel int // construct_beacon minimum pyramid tier

	unmatchable bool // trigger/conditions the engine can't observe yet
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
