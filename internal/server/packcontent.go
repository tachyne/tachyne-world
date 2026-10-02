package server

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// packContent is what a data pack load applies beyond functions, as
// ReloadableServerResources holds it: the tags (packtags.go), the recipes
// (packrecipes.go), the loot tables, predicates and item modifiers
// (packloot.go). Every load builds a whole new one off the hub; the hub
// installs it (installPackContent), so the engine's lookups see one load at
// a time.
type packContent struct {
	tags    *tagRegistry
	recipes *packRecipes

	loot       map[string]*lootTable // by lootKey; failed files stand as empty tables
	blockLoot  bool                  // some table is a block's (blocks/…)
	entityLoot bool                  // some table is a mob's (entities/…)
	predicates map[string][]lootCond
	modifiers  map[string]itemModifier
	predRaw    map[string]any // the decoded predicate files (inline predicates name them)
	modRaw     map[string]any // the decoded item modifier files
}

// activePack is the installed load. The engine runs one server per process;
// a test that loads packs puts nil back when it is done.
var activePack atomic.Pointer[packContent]

// currentPack is the installed load (nil before the first).
func currentPack() *packContent { return activePack.Load() }

// installPackContent makes a load the one the engine reads: its tags reach
// worldgen's lookups too.
func installPackContent(pc *packContent) {
	activePack.Store(pc)
	if pc == nil {
		worldgen.SetTagOverlay(nil)
		return
	}
	worldgen.SetTagOverlay(pc.tags.worldgenTagOverlay())
}

// packDataFiles is what a load collected from the selected packs beyond
// functions and function tags.
type packDataFiles struct {
	tags      map[string]map[string][]packFile // registry → tag id → files, bottom pack first
	recipes   map[string]packFile              // id → the top pack's file
	loot      map[string]packFile
	preds     map[string]packFile
	modifiers map[string]packFile
}

func newPackDataFiles() *packDataFiles {
	return &packDataFiles{
		tags:      map[string]map[string][]packFile{},
		recipes:   map[string]packFile{},
		loot:      map[string]packFile{},
		preds:     map[string]packFile{},
		modifiers: map[string]packFile{},
	}
}

// take files a data file (rest is its path below data/<ns>/) under the
// registry it belongs to; false when it is not data this server applies.
func (d *packDataFiles) take(packID, ns, rest string, read func() ([]byte, error)) bool {
	idOf := func(prefix string) (string, bool) {
		if !strings.HasPrefix(rest, prefix) || !strings.HasSuffix(rest, ".json") {
			return "", false
		}
		p := strings.TrimSuffix(strings.TrimPrefix(rest, prefix), ".json")
		if !validNamespace(ns) || !validIDPath(p) {
			log.Printf("datapacks: invalid path data/%s/%s in %s", ns, rest, packID)
			return "", false
		}
		return ns + ":" + p, true
	}
	for _, reg := range tagRegistries {
		if id, ok := idOf("tags/" + reg + "/"); ok {
			data, err := read()
			if err != nil {
				return true
			}
			if d.tags[reg] == nil {
				d.tags[reg] = map[string][]packFile{}
			}
			d.tags[reg][id] = append(d.tags[reg][id], packFile{pack: packID, data: data})
			return true
		}
	}
	for _, kind := range []struct {
		prefix string
		into   map[string]packFile
	}{
		{"recipe/", d.recipes},
		{"loot_table/", d.loot},
		{"predicate/", d.preds},
		{"item_modifier/", d.modifiers},
	} {
		if id, ok := idOf(kind.prefix); ok {
			if data, err := read(); err == nil {
				kind.into[id] = packFile{pack: packID, data: data} // a later pack's file wins
			}
			return true
		}
	}
	return false
}

// buildPackContent applies a load's files: tags first, as every other
// registry reads them.
func buildPackContent(d *packDataFiles, unapplied map[string]map[string]bool) *packContent {
	pc := &packContent{}
	pc.tags = buildTagRegistry(d.tags)
	pc.recipes = buildPackRecipes(d.recipes, pc.tags, unapplied)
	buildPackLoot(pc, d.preds, d.modifiers, d.loot)
	return pc
}

// summary is the log line's account of a load's content.
func (pc *packContent) summary() string {
	changed := 0
	for _, ids := range pc.tags.changed {
		changed += len(ids)
	}
	return fmt.Sprintf("%d changed tags, %d recipes, %d loot tables, %d predicates, %d item modifiers",
		changed, pc.recipes.count, len(pc.loot), len(pc.predicates)-len(vanillaPredicates), len(pc.modifiers))
}

// applyPackContent installs a reloaded load on the hub: the recipe books
// carried across to the new recipe ids and sent again, and the clients told
// of the registries the load changed.
func (h *hub) applyPackContent(players map[int32]*tracked, pc *packContent) {
	old := currentPack()
	installPackContent(pc)
	h.remapRecipeBooks(players, old, pc)
	h.onPackRegistriesChanged(players, pc)
}

// onPackRegistriesChanged is where a reload's changed tags reach the
// clients. Vanilla sends them with ClientboundUpdateTagsPacket during
// PlayerList.reloadResources; here they reach a client only through the
// gateway, which sends a client its tags in the configuration phase — so
// this is the hook for the w→gw reconfiguration frame tachyne-common is
// adding: emit it to every player with pc.tags.changedTags() (registry →
// tag id → member ids, a vanilla tag that stopped loading as an empty list)
// and the gateway re-runs configuration (or sends update_tags in play) with
// those tags merged over its built-in ones. Until that frame exists the
// changes apply to everything the engine decides (crafting, loot, commands,
// worldgen) but a client's own tag-driven predictions (which items a
// furnace slot takes, a tool's mining speed) keep vanilla's tags.
func (h *hub) onPackRegistriesChanged(players map[int32]*tracked, pc *packContent) {
	changed := pc.tags.changedTags()
	if len(changed) == 0 {
		return
	}
	regs := make([]string, 0, len(changed))
	for reg, ids := range changed {
		regs = append(regs, fmt.Sprintf("%s %d", reg, len(ids)))
	}
	sort.Strings(regs)
	log.Printf("datapacks: tags changed (%s); clients keep their configured tags until a reconfiguration", strings.Join(regs, ", "))
}
