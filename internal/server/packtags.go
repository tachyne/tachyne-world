package server

import (
	"log"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Data pack tags for the item, block, entity_type, fluid, worldgen/biome,
// enchantment, potion and dialog registries, as TagLoader loads them: for each tag id, the vanilla pack's
// file (vanillatags_gen.go) and then every selected pack's file, bottom to
// top — a "replace": true file dropping what came before — and each tag
// resolved after the tags it names. A tag missing a required element or tag
// does not load; an optional one that is missing is skipped. Elements are
// the registry's own: a pack cannot add an item, so a tag naming one this
// version lacks fails like any missing reference.
//
// The resolved tags are what the engine reads wherever it asks a tag by
// name at the time it asks: worldgen.BlockTag/ItemTag/BlockTagNames (the
// overlay below), the biome tags of /fillbiome and `execute if biome`, the
// item tags of `execute if items` and /clear-style item predicates, and
// every tag a pack's own recipes, loot tables and predicates name. Lookups
// the engine baked into a table at start-up (the vanilla recipes' own
// ingredient sets, the advancement tree's expanded tags) keep vanilla's
// members.

// tagRegistries are the registries whose tags packs may change, by their
// folder under data/<ns>/tags/.
var tagRegistries = []string{"item", "block", "entity_type", "fluid", "worldgen/biome", "enchantment", "potion", "dialog"}

// packTagSet is one load's resolved tags: registry → tag id → member ids, both
// as namespaced ids ("minecraft:planks" → "minecraft:oak_planks", …).
type packTagSet map[string]map[string][]string

// tagRegistry is the tags a pack load produced, and which of them the packs
// changed from the vanilla pack's own (new tags, tags with other members,
// and vanilla tags that no longer load).
type tagRegistry struct {
	tags    packTagSet
	changed map[string]map[string]bool
}

// members is a tag's member ids; ok is false when there is no such tag.
func (r *tagRegistry) members(reg, id string) ([]string, bool) {
	if r == nil {
		m, ok := vanillaTags()[reg][id]
		return m, ok
	}
	m, ok := r.tags[reg][id]
	return m, ok
}

// changedTags is the tags the packs changed, with their members as they now
// stand (an empty list for a vanilla tag that no longer loads) — what a
// client must be sent again (onPackRegistriesChanged).
func (r *tagRegistry) changedTags() packTagSet {
	out := packTagSet{}
	if r == nil {
		return out
	}
	for reg, ids := range r.changed {
		for id := range ids {
			if out[reg] == nil {
				out[reg] = map[string][]string{}
			}
			out[reg][id] = append([]string{}, r.tags[reg][id]...)
		}
	}
	return out
}

// vanillaTagRefs reads a generated vanilla file's entries.
func vanillaTagRefs(entries []string) []tagRef {
	out := make([]tagRef, 0, len(entries))
	for _, e := range entries {
		ref := tagRef{required: true}
		if s, ok := strings.CutSuffix(e, "?"); ok {
			e, ref.required = s, false
		}
		if s, ok := strings.CutPrefix(e, "#"); ok {
			e, ref.tag = s, true
		}
		ref.id = e
		out = append(out, ref)
	}
	return out
}

var (
	vanillaTagsOnce  sync.Once
	vanillaTagsBuilt packTagSet
)

// vanillaTags is the vanilla pack's tags alone, resolved once.
func vanillaTags() packTagSet {
	vanillaTagsOnce.Do(func() {
		vanillaTagsBuilt = packTagSet{}
		for _, reg := range tagRegistries {
			entries := map[string][]tagRef{}
			for id, ents := range vanillaTagFiles[reg] {
				entries[id] = vanillaTagRefs(ents)
			}
			vanillaTagsBuilt[reg] = resolveTags(reg, entries, true, nil)
		}
	})
	return vanillaTagsBuilt
}

var (
	biomeSetOnce sync.Once
	biomeSet     map[string]bool
)

// tagElementExists is TagLoader.ElementLookup for a registry: whether the
// id names one of its entries.
func tagElementExists(reg, id string) bool {
	ns, p, _ := strings.Cut(id, ":")
	switch reg {
	case "item":
		_, ok := itemByName[p]
		return ns == "minecraft" && ok
	case "block":
		_, _, ok := worldgen.BlockRangeOK(p)
		return ns == "minecraft" && ok
	case "entity_type":
		_, ok := entityByName[p]
		return ns == "minecraft" && ok
	case "fluid":
		switch id {
		case "minecraft:empty", "minecraft:water", "minecraft:flowing_water", "minecraft:lava", "minecraft:flowing_lava":
			return true
		}
		return false
	case "enchantment":
		_, ok := enchByName[p]
		return ns == "minecraft" && ok
	case "potion":
		_, ok := potionByVanillaName[p]
		return ns == "minecraft" && ok
	case "dialog": // the built-in ones; a load's own come as its extra elements
		return registryDialogs[id]
	case "worldgen/biome":
		biomeSetOnce.Do(func() {
			biomeSet = map[string]bool{}
			for _, b := range biomeIDs() {
				biomeSet[b] = true
			}
		})
		return biomeSet[id]
	}
	return false
}

// resolveTags is TagLoader.build for one registry: each tag built after the
// tags it names (a tag in a cycle never builds), its members in first
// order, once each. A tag missing a required reference is left out, and
// said so unless quiet.
func resolveTags(reg string, entries map[string][]tagRef, quiet bool, extra map[string]bool) map[string][]string {
	built := map[string][]string{}
	failed := map[string]bool{}
	visiting := map[string]bool{}
	var build func(id string) bool
	build = func(id string) bool {
		if _, ok := built[id]; ok {
			return true
		}
		if failed[id] || visiting[id] {
			return false
		}
		refs, ok := entries[id]
		if !ok {
			return false
		}
		visiting[id] = true
		defer delete(visiting, id)
		out := []string{}
		seen := map[string]bool{}
		add := func(m string) {
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}
		var missing []string
		for _, r := range refs {
			if r.tag {
				if build(r.id) {
					for _, m := range built[r.id] {
						add(m)
					}
				} else if r.required {
					missing = append(missing, r.String())
				}
				continue
			}
			if tagElementExists(reg, r.id) || extra[r.id] {
				add(r.id)
			} else if r.required {
				missing = append(missing, r.String())
			}
		}
		if len(missing) > 0 {
			if !quiet {
				log.Printf("datapacks: couldn't load %s tag %s as it is missing following references: %s", reg, id, strings.Join(missing, ", "))
			}
			failed[id] = true
			return false
		}
		built[id] = out
		return true
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		build(id)
	}
	return built
}

// buildTagRegistry merges the packs' tag files (registry → tag id → files,
// bottom pack first) onto the vanilla files and resolves every registry.
// extra is the elements the load's packs add to a registry (its dialogs).
func buildTagRegistry(files map[string]map[string][]packFile, extra map[string]map[string]bool) *tagRegistry {
	r := &tagRegistry{tags: packTagSet{}, changed: map[string]map[string]bool{}}
	van := vanillaTags()
	for _, reg := range tagRegistries {
		entries := map[string][]tagRef{}
		for id, ents := range vanillaTagFiles[reg] {
			entries[id] = vanillaTagRefs(ents)
		}
		ids := make([]string, 0, len(files[reg]))
		for id := range files[reg] {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			refs := append([]tagRef{}, entries[id]...)
			for _, f := range files[reg][id] {
				got, replace, err := parseTagFile(f.data)
				if err != nil {
					log.Printf("datapacks: couldn't read %s tag list %s in data pack %s: %v", reg, id, f.pack, err)
					continue
				}
				if replace {
					refs = nil
				}
				refs = append(refs, got...)
			}
			entries[id] = refs
		}
		if len(ids) == 0 {
			r.tags[reg] = van[reg] // nothing touched: the vanilla resolution as it is
			continue
		}
		built := resolveTags(reg, entries, false, extra[reg])
		r.tags[reg] = built
		ch := map[string]bool{}
		for id := range entries {
			before, wasThere := van[reg][id]
			after, isThere := built[id]
			if wasThere != isThere || !slices.Equal(before, after) {
				ch[id] = true
			}
		}
		if len(ch) > 0 {
			r.changed[reg] = ch
		}
	}
	return r
}

// tagMembers is a tag of the current load: its member ids, ok false when
// the tag does not exist. id is namespaced.
func tagMembers(reg, id string) ([]string, bool) {
	if pc := currentPack(); pc != nil {
		return pc.tags.members(reg, id)
	}
	return (*tagRegistry)(nil).members(reg, id)
}

// biomeTagMembers is a biome tag's biomes ("minecraft:…" ids).
func biomeTagMembers(id string) ([]string, bool) { return tagMembers("worldgen/biome", id) }

// itemTagIDs is an item tag's members as item ids, ok false when there is
// no such tag. id is namespaced.
func itemTagIDs(id string) ([]int32, bool) {
	names, ok := tagMembers("item", id)
	if !ok {
		return nil, false
	}
	out := make([]int32, 0, len(names))
	for _, n := range names {
		if it, ok := itemByName[strings.TrimPrefix(n, "minecraft:")]; ok {
			out = append(out, it)
		}
	}
	return out, true
}

// worldgenTagOverlay is the changed block and item tags in the form the
// worldgen package keeps its generated ones: a minecraft tag by its bare
// path, another namespace's by its full id, and members by bare name (every
// element is a vanilla one). nil when the packs changed none of them.
func (r *tagRegistry) worldgenTagOverlay() *worldgen.TagOverlay {
	if r == nil || (len(r.changed["block"]) == 0 && len(r.changed["item"]) == 0) {
		return nil
	}
	conv := func(reg string) map[string][]string {
		out := map[string][]string{}
		for id := range r.changed[reg] {
			key := strings.TrimPrefix(id, "minecraft:")
			names := make([]string, 0, len(r.tags[reg][id]))
			for _, m := range r.tags[reg][id] {
				names = append(names, strings.TrimPrefix(m, "minecraft:"))
			}
			out[key] = names
		}
		return out
	}
	return &worldgen.TagOverlay{Blocks: conv("block"), Items: conv("item")}
}
