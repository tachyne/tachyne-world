package anvil

import "sort"

// stateEntry is one Anvil palette entry: a block name plus its property
// values (pairs ordered as in the block's state definition).
type stateEntry struct {
	name  string
	props [][2]string // property name, value
}

// decodeState resolves a canonical numeric block-state id to its Anvil
// palette entry. Unknown ids (beyond the generated table) fall back to air
// rather than corrupting the save.
func decodeState(state uint32) stateEntry {
	i := sort.Search(len(blockDefs), func(i int) bool {
		return blockDefs[i].base > state
	}) - 1
	if i < 0 {
		return stateEntry{name: "minecraft:air"}
	}
	def := blockDefs[i]
	if len(def.props) == 0 {
		if state != def.base {
			// past the last block's range
			return stateEntry{name: "minecraft:air"}
		}
		return stateEntry{name: def.name}
	}
	rem := state - def.base
	count := uint32(1)
	for _, p := range def.props {
		count *= uint32(len(p.vals))
	}
	if rem >= count {
		return stateEntry{name: "minecraft:air"}
	}
	// Mixed radix, first property most significant: peel from the right.
	vals := make([][2]string, len(def.props))
	for j := len(def.props) - 1; j >= 0; j-- {
		p := def.props[j]
		n := uint32(len(p.vals))
		vals[j] = [2]string{p.name, p.vals[rem%n]}
		rem /= n
	}
	return stateEntry{name: def.name, props: vals}
}
