package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

func TestPlayerMaxHealthStartsAtTwenty(t *testing.T) {
	pl := testTracked()
	if pl.maxHP() != maxHealth {
		t.Errorf("max health %v, want %v", pl.maxHP(), maxHealth)
	}
	if pl.health != pl.maxHP() {
		t.Errorf("joined at %v/%v, want full", pl.health, pl.maxHP())
	}
	// A player built without the join path must still read a sane ceiling
	// rather than panicking on a nil map.
	bare := &tracked{}
	if bare.maxHP() != maxHealth {
		t.Errorf("map-less player max health %v, want %v", bare.maxHP(), maxHealth)
	}
}

// The point of the migration: regen now clamps to whatever MAX_HEALTH says, so
// a health-boost effect (or an enchantment, or a plugin) raises the ceiling
// instead of healing into a hard-coded 20.
func TestRegenClampsToRaisedMaxHealth(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl

	pl.playerAttrs().Get(attr.MaxHealth).AddModifier(attr.Modifier{
		Source: "effect:health_boost", Amount: 4, Op: attr.AddValue,
	})
	if pl.maxHP() != 24 {
		t.Fatalf("max health %v after the boost, want 24", pl.maxHP())
	}

	// Full food + saturation is vanilla's fast-regen branch; it should climb
	// past 20 now and stop at 24.
	pl.health, pl.food, pl.saturation = 20, maxFood, 5
	for i := 0; i < 200 && pl.health < 24; i++ {
		h.fastRegen(players)
		pl.saturation = 5 // keep the branch live
	}
	if pl.health != 24 {
		t.Errorf("regenerated to %v, want the raised ceiling 24", pl.health)
	}

	// Dropping the modifier drops the ceiling back.
	pl.playerAttrs().RemoveSource("effect:health_boost")
	if pl.maxHP() != maxHealth {
		t.Errorf("max health %v after the boost expired, want %v", pl.maxHP(), maxHealth)
	}
}
