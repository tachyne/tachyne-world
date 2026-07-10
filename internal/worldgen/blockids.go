package worldgen

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

// BlockRange returns a block's [minStateId, maxStateId] by name (panics if unknown).
func BlockRange(name string) (lo, hi uint32) {
	hi, ok := blockStateMax[name]
	if !ok {
		panic("worldgen: unknown block name " + name)
	}
	return blockBase(name), hi
}
