package server

import "github.com/tachyne/tachyne-common/protocol"

// Goat and turtle synced state (Goat.DATA_IS_SCREAMING_GOAT / HAS_LEFT_HORN
// / HAS_RIGHT_HORN, Turtle.HAS_EGG / LAYING_EGG): what the client renders
// — a goat missing the horn it snapped off, a turtle showing the egg it
// carries and the digging pose while it lays. Indices are 1.21.5's; the
// gateways shift them for 26.2 (both species are in the ageable table).

const (
	metaIndexGoatScreaming = 17
	metaIndexGoatLeftHorn  = 18
	metaIndexGoatRightHorn = 19
	metaIndexTurtleHasEgg  = 17
	metaIndexTurtleLaying  = 18
)

// boolsMeta packs several boolean entries into one metadata body.
func boolsMeta(eid int32, entries ...struct {
	index byte
	v     bool
}) []byte {
	b := protocol.AppendVarInt(nil, eid)
	for _, e := range entries {
		b = protocol.AppendU8(b, e.index)
		b = protocol.AppendVarInt(b, metaTypeBool)
		b = protocol.AppendBool(b, e.v)
	}
	return protocol.AppendU8(b, itemMetaEnd)
}

type boolEntry = struct {
	index byte
	v     bool
}

// goatMeta: the screaming variant and which horns remain (the first horn
// lost is the right one, the second the left).
func goatMeta(m *mob) []byte {
	return boolsMeta(m.eid,
		boolEntry{metaIndexGoatScreaming, m.screaming},
		boolEntry{metaIndexGoatLeftHorn, m.hornsGone < 2},
		boolEntry{metaIndexGoatRightHorn, m.hornsGone < 1})
}

// turtleMeta: the egg it carries, and the digging pose while laying.
func turtleMeta(m *mob) []byte {
	return boolsMeta(m.eid,
		boolEntry{metaIndexTurtleHasEgg, m.hasEgg},
		boolEntry{metaIndexTurtleLaying, m.hasEgg && m.layCounter > 0})
}

// speciesStateMeta is the per-species synced state to re-assert for a
// late joiner, or nil when the species has none.
func speciesStateMeta(m *mob) []byte {
	switch m.etype {
	case entityGoat:
		if m.screaming || m.hornsGone > 0 {
			return goatMeta(m)
		}
	case entityTurtle:
		if m.hasEgg {
			return turtleMeta(m)
		}
	case entityCat:
		if m.lying || m.relaxOne {
			return catLieMeta(m)
		}
	case entityCamel:
		if m.poseTick != 0 {
			return camelPoseMeta(m)
		}
	case entityAxolotl:
		if m.axDead > 0 {
			return axolotlDeadMeta(m)
		}
	}
	return nil
}
