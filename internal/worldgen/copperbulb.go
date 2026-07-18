package worldgen

// Copper bulbs are the one light source whose emission depends on block STATE
// (whether it is lit) and oxidation stage — a shape the per-block generated
// light table cannot express, since it keys on the unlit default (level 0). So
// the lit levels are reimplemented here from CopperBulbBlock and folded into
// lightEmissionTab (see lightfast.go), and the lit/powered state layout is
// exposed for the redstone toggle in the server.
//
// State layout per bulb (properties sorted [lit, powered], last varies fastest,
// bool order [true,false]) — confirmed against the 1.21.11 datagen report:
//
//	base+0 lit=true  powered=true
//	base+1 lit=true  powered=false
//	base+2 lit=false powered=true
//	base+3 lit=false powered=false   (default)
type bulbLine struct {
	base uint32
	lit  uint8 // block-light level its LIT states emit (unlit states emit 0)
}

// copperBulbLines is every bulb block (unwaxed + waxed) with its lit level by
// oxidation stage: unaffected 15, exposed 12, weathered 8, oxidized 4. Built at
// init; this file sorts before lightfast.go so its table build sees the slice.
var copperBulbLines []bulbLine

func init() {
	for _, e := range []struct {
		name string
		lvl  uint8
	}{
		{"copper_bulb", 15}, {"exposed_copper_bulb", 12},
		{"weathered_copper_bulb", 8}, {"oxidized_copper_bulb", 4},
		{"waxed_copper_bulb", 15}, {"waxed_exposed_copper_bulb", 12},
		{"waxed_weathered_copper_bulb", 8}, {"waxed_oxidized_copper_bulb", 4},
	} {
		if lo, ok := blockRangeSafe(e.name); ok {
			copperBulbLines = append(copperBulbLines, bulbLine{base: lo, lit: e.lvl})
		}
	}
}

// blockRangeSafe returns a block's min state id, ok=false if the name is unknown
// (a version without it) rather than panicking.
func blockRangeSafe(name string) (lo uint32, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	lo, _ = BlockRange(name)
	return lo, true
}

func bulbLineOf(state uint32) (bulbLine, bool) {
	for _, l := range copperBulbLines {
		if state >= l.base && state <= l.base+3 {
			return l, true
		}
	}
	return bulbLine{}, false
}

// IsCopperBulb reports whether state is any copper bulb (waxed or not).
func IsCopperBulb(state uint32) bool { _, ok := bulbLineOf(state); return ok }

// CopperBulbLit reports whether a bulb state is currently emitting.
func CopperBulbLit(state uint32) bool {
	l, ok := bulbLineOf(state)
	return ok && state-l.base < 2
}

// CopperBulbPowered reports the redstone-input latch bit of a bulb state.
func CopperBulbPowered(state uint32) bool {
	l, ok := bulbLineOf(state)
	return ok && (state-l.base)%2 == 0
}

// CopperBulbSet rebuilds a bulb state with the given lit/powered, preserving the
// oxidation line. Returns state unchanged if it is not a bulb.
func CopperBulbSet(state uint32, lit, powered bool) uint32 {
	l, ok := bulbLineOf(state)
	if !ok {
		return state
	}
	off := uint32(0)
	if !lit {
		off += 2
	}
	if !powered {
		off++
	}
	return l.base + off
}

// copperBulbEmission is the block-light a bulb state emits (0 for unlit or
// non-bulb) — used by lightfast.go to patch the emission table.
func copperBulbEmission(state uint32) uint8 {
	if l, ok := bulbLineOf(state); ok && state-l.base < 2 {
		return l.lit
	}
	return 0
}
