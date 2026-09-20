package server

import "github.com/tachyne/tachyne-common/protocol"

// A potion's look on the client: the liquid's colour and the effect lines in
// its tooltip both come from ONE component, potion_contents. Without it every
// brew renders the default purple and lists nothing — a Healing and a Poison
// are indistinguishable in the hand. The component carries the effects
// themselves, so the client mixes the colour exactly as vanilla does
// (PotionContents.getColorOptional) rather than trusting a number from here.
//
// The engine still needs the mix of its own for the SPLASH burst, which is a
// level event carrying a colour rather than an item.

// potionColorBase is PotionContents.BASE_POTION_COLOR: the water-bottle blue a
// potion with no visible effect falls back to.
const potionColorBase = 0x385DC6

// effectColorByID is effectColorByName keyed by the registry id the rest of
// the engine speaks. Built here rather than generated so the two tables can
// never disagree about which effect is which.
var effectColorByID = func() map[int32]int32 {
	m := make(map[int32]int32, len(effectNames))
	for name, id := range effectNames {
		if c, ok := effectColorByName[name]; ok {
			m[id] = c
		}
	}
	return m
}()

// potionColor mixes a kind's effect colours the way vanilla does: each
// channel averaged over the effects, weighted by level.
func potionColor(kind int8) int32 {
	var r, g, b, n int
	for _, e := range potionEffects(kind) {
		c, ok := effectColorByID[e.id]
		if !ok {
			continue
		}
		w := e.amp + 1
		r += w * int((c>>16)&0xff)
		g += w * int((c>>8)&0xff)
		b += w * int(c&0xff)
		n += w
	}
	if n == 0 {
		return potionColorBase
	}
	return int32((r/n)<<16 | (g/n)<<8 | (b / n))
}

// potionComponentBytes is the potion_contents payload for a brewed kind:
// no potion holder (the potion registry renumbers between versions and the
// engine does not translate it), no custom colour (the effects give the
// client the real one), the effects, and no custom name — the stack's own
// custom_name already carries the label.
func potionComponentBytes(kind int8) []byte {
	effs := potionEffects(kind)
	b := []byte{0, 0} // no potion holder, no custom colour
	b = protocol.AppendVarInt(b, int32(len(effs)))
	for _, e := range effs {
		ticks := int32(e.ticks)
		if ticks == 0 {
			ticks = 1 // vanilla never sends a zero duration
		}
		b = protocol.AppendVarInt(b, e.id+1) // holder ref = registry id + 1
		b = protocol.AppendVarInt(b, int32(e.amp))
		b = protocol.AppendVarInt(b, ticks)
		b = append(b, 0, 1, 1, 0) // ambient, showParticles, showIcon, no hidden
	}
	return append(b, 0) // no custom name
}
