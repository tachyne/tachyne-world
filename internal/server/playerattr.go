package server

import (
	"github.com/tachyne/tachyne-world/internal/attribute"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Player attributes. Same pipeline the mobs use — vanilla makes no distinction
// between a player and any other living entity here, and neither should we:
// health boost, armour and the movement modifiers all want one place to live.

// newPlayerAttributes is Player.createAttributes: the starting point for a
// freshly joined player.
func newPlayerAttributes() *attribute.Map {
	a := attribute.NewMap()
	a.SetBase(attr.MaxHealth, maxHealth)
	return a
}

// playerAttrs returns the player's attribute map, creating it on first use —
// tests build a tracked player by hand and never go through the join path.
func (t *tracked) playerAttrs() *attribute.Map {
	if t.attrs == nil {
		t.attrs = newPlayerAttributes()
	}
	return t.attrs
}

// maxHP is the player's MAX_HEALTH — the ceiling every heal clamps to. Health
// itself is a float32 on the wire, so callers convert at the edge.
func (t *tracked) maxHP() float32 { return float32(t.playerAttrs().Value(attr.MaxHealth)) }
