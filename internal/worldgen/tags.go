package worldgen

import (
	"sort"
	"sync/atomic"
)

// TagOverlay is the block and item tags a server's data packs changed:
// each tag by the name the lookups below take (a minecraft tag by its bare
// path, another namespace's by its full id) with its members by bare block
// or item name. A tag here replaces the generated one; the server installs
// a new overlay on every data pack load (SetTagOverlay).
type TagOverlay struct {
	Blocks map[string][]string
	Items  map[string][]string
}

var tagOverlay atomic.Pointer[TagOverlay]

// SetTagOverlay installs the data packs' tag changes (nil: vanilla's tags).
// Lookups made after it see the new members; tables a caller built from a
// tag earlier keep what they read.
func SetTagOverlay(o *TagOverlay) { tagOverlay.Store(o) }

// blockTagNames is a block tag's members: the overlay's, else the generated.
func blockTagNames(name string) ([]string, bool) {
	if o := tagOverlay.Load(); o != nil {
		if names, ok := o.Blocks[name]; ok {
			return names, true
		}
	}
	names, ok := blockTags[name]
	return names, ok
}

// BlockTag is a vanilla block tag's members as block-state ranges (every
// state of each member block). The tag must be one gen_tags.py emits (or
// one a data pack added); any other name panics.
func BlockTag(name string) [][2]uint32 {
	names, ok := blockTagNames(name)
	if !ok {
		panic("worldgen: block tag " + name + " is not generated (add it to scripts/gen_tags.py)")
	}
	out := make([][2]uint32, 0, len(names))
	for _, n := range names {
		if lo, hi, ok := BlockRangeOK(n); ok {
			out = append(out, [2]uint32{lo, hi})
		}
	}
	return out
}

// ItemTag is a vanilla item tag's member item names. The tag must be one
// gen_tags.py emits (or one a data pack added); any other name panics.
func ItemTag(name string) []string {
	if o := tagOverlay.Load(); o != nil {
		if names, ok := o.Items[name]; ok {
			return names
		}
	}
	names, ok := itemTags[name]
	if !ok {
		panic("worldgen: item tag " + name + " is not generated (add it to scripts/gen_tags.py)")
	}
	return names
}

// BlockTagNames is a vanilla block tag's member block names. The tag must be
// one gen_tags.py emits (or one a data pack added); any other name panics.
func BlockTagNames(name string) []string {
	names, ok := blockTagNames(name)
	if !ok {
		panic("worldgen: block tag " + name + " is not generated (add it to scripts/gen_tags.py)")
	}
	return names
}

// BlockTagList is every block tag's name — the generated ones and any a
// data pack added — sorted.
func BlockTagList() []string {
	out := make([]string, 0, len(blockTags))
	for n := range blockTags {
		out = append(out, n)
	}
	if o := tagOverlay.Load(); o != nil {
		for n := range o.Blocks {
			if _, gen := blockTags[n]; !gen {
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}
