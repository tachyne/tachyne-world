package anvil

// Decode resolves a canonical numeric block-state id to its block name
// (e.g. "minecraft:oak_stairs") and property map (e.g. {"facing":"north",
// "half":"bottom"}). Unknown ids resolve to minecraft:air.
//
// It is the exported form of the Anvil palette's state decoder, for read-only
// consumers outside this package (notably the worldread facade, which needs
// state → (name, properties) to look up a block's vanilla model). The
// authoritative reverse table lives here (generated from vanilla's blocks.json
// report); worldgen only holds the forward name → id direction.
func Decode(state uint32) (name string, props map[string]string) {
	e := decodeState(state)
	props = make(map[string]string, len(e.props))
	for _, p := range e.props {
		props[p[0]] = p[1]
	}
	return e.name, props
}
