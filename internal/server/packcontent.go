package server

import (
	"encoding/json"
	"fmt"
	"log"
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

	// dialogs are the dialog registry entries the packs add or replace
	// (packdialog.go), by id, each in its data-pack JSON form.
	dialogs map[string]json.RawMessage
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
	dialogs   map[string]packFile
}

func newPackDataFiles() *packDataFiles {
	return &packDataFiles{
		tags:      map[string]map[string][]packFile{},
		recipes:   map[string]packFile{},
		loot:      map[string]packFile{},
		preds:     map[string]packFile{},
		modifiers: map[string]packFile{},
		dialogs:   map[string]packFile{},
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
		{"dialog/", d.dialogs},
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
	pc.dialogs = buildPackDialogs(d.dialogs, d.tags["dialog"])
	pc.tags = buildTagRegistry(d.tags, map[string]map[string]bool{"dialog": dialogIDSet(pc.dialogs)})
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
	return fmt.Sprintf("%d changed tags, %d recipes, %d loot tables, %d predicates, %d item modifiers, %d dialogs",
		changed, pc.recipes.count, len(pc.loot), len(pc.predicates)-len(vanillaPredicates), len(pc.modifiers), len(pc.dialogs))
}

// applyPackContent installs a reloaded load on the hub: the recipe books
// carried across to the new recipe ids and sent again, and the clients told
// of the registries the load changed.
func (h *hub) applyPackContent(players map[int32]*tracked, pc *packContent) {
	old := currentPack()
	installPackContent(pc)
	h.remapRecipeBooks(players, old, pc)
	h.onPackRegistriesChanged(players, old, pc)
}
