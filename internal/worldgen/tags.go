package worldgen

import "sort"

// BlockTag is a vanilla block tag's members as block-state ranges (every
// state of each member block). The tag must be one gen_tags.py emits; any
// other name panics.
func BlockTag(name string) [][2]uint32 {
	names, ok := blockTags[name]
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
// gen_tags.py emits; any other name panics.
func ItemTag(name string) []string {
	names, ok := itemTags[name]
	if !ok {
		panic("worldgen: item tag " + name + " is not generated (add it to scripts/gen_tags.py)")
	}
	return names
}

// BlockTagNames is a vanilla block tag's member block names. The tag must be
// one gen_tags.py emits; any other name panics.
func BlockTagNames(name string) []string {
	names, ok := blockTags[name]
	if !ok {
		panic("worldgen: block tag " + name + " is not generated (add it to scripts/gen_tags.py)")
	}
	return names
}

// BlockTagList is every generated block tag's name, sorted.
func BlockTagList() []string {
	out := make([]string, 0, len(blockTags))
	for n := range blockTags {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
