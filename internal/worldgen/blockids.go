package worldgen

import (
	"sort"
	"sync"
)

// Exported block name → state-id lookups, so the server package can name blocks
// instead of hard-coding numeric state ids that churn every Minecraft version.
// (blockBase/blockID themselves are generated in blockids_gen.go.)

// BlockBase returns a block's minStateId (base of its state range) by name,
// panicking on an unknown name. Add an in-range property offset to reach a
// specific state (state order within a block is stable across versions unless
// the block's property set changes).
func BlockBase(name string) uint32 { return blockBase(name) }

// BlockID returns a block's DEFAULT state id by name, panicking on an unknown name.
func BlockID(name string) uint32 { return blockID(name) }

// BlockRangeOK is BlockRange for names that may not exist in this version's
// registry — content added after the canonical version, or removed before it.
func BlockRangeOK(name string) (lo, hi uint32, ok bool) {
	hi, ok = blockStateMax[name]
	if !ok {
		return 0, 0, false
	}
	return blockBase(name), hi, true
}

// BlockRange returns a block's [minStateId, maxStateId] by name (panics if unknown).
func BlockRange(name string) (lo, hi uint32) {
	hi, ok := blockStateMax[name]
	if !ok {
		panic("worldgen: unknown block name " + name)
	}
	return blockBase(name), hi
}

// BlockRegistryID returns a block's id in the minecraft:block registry (what
// block_event and similar packets name), 0 and false for an unknown name.
func BlockRegistryID(name string) (uint32, bool) {
	id, ok := blockRegistryID[name]
	return id, ok
}

// stateRanges is the reverse of blockStateBase: every block's [Min, Max]
// state range with its name, sorted by Min, for StateName's lookup.
var (
	stateRanges     []stateRange
	stateRangesOnce sync.Once
)

type stateRange struct {
	lo, hi uint32
	name   string
}

func buildStateRanges() {
	stateRanges = make([]stateRange, 0, len(blockStateBase))
	for name, lo := range blockStateBase {
		stateRanges = append(stateRanges, stateRange{lo, blockStateMax[name], name})
	}
	sort.Slice(stateRanges, func(i, j int) bool { return stateRanges[i].lo < stateRanges[j].lo })
}

// StateName returns the block name (no namespace) owning a state id.
func StateName(state uint32) (string, bool) {
	stateRangesOnce.Do(buildStateRanges)
	i := sort.Search(len(stateRanges), func(i int) bool { return stateRanges[i].hi >= state })
	if i < len(stateRanges) && stateRanges[i].lo <= state && state <= stateRanges[i].hi {
		return stateRanges[i].name, true
	}
	return "", false
}

// StateProps returns a state's property values by name (nil when the block
// has no properties).
func StateProps(state uint32) map[string]string {
	info, ok := InfoForState(state)
	if !ok || len(info.Props) == 0 {
		return nil
	}
	out := make(map[string]string, len(info.Props))
	for _, p := range info.Props {
		out[p.Name] = GetProperty(info, state, p.Name)
	}
	return out
}
