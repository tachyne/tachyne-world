package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Melee and entity interaction measure against the player's own
// entity_interaction_range (plus vanilla's three blocks of slack, eyes to
// box): a raised range reaches a zombie nine blocks off, the default does
// not, and a creative player gets vanilla's +2 on the attribute.
func TestReachFollowsInteractionRange(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	h.tick.Store(100)
	h.allocEID()
	far := h.spawnMob(players, entityZombie, 9.5, 70, 0.5)
	far.health = 100
	h.onAttack(players, evAttack{attacker: 1, target: far.eid})
	if far.health != 100 {
		t.Fatalf("a zombie 9 blocks off was hit at the default reach (health %d)", far.health)
	}
	pl.playerAttrs().SetBase(attr.EntityInteractionRange, 7)
	h.tick.Store(200)
	h.onAttack(players, evAttack{attacker: 1, target: far.eid})
	if far.health == 100 {
		t.Error("a raised entity_interaction_range did not reach the zombie")
	}
	pl.playerAttrs().SetBase(attr.EntityInteractionRange, 0)
	near := h.spawnMob(players, entityZombie, 4.5, 70, 0.5)
	near.health = 100
	h.tick.Store(300)
	h.onAttack(players, evAttack{attacker: 1, target: near.eid})
	if near.health != 100 {
		t.Error("a zero entity_interaction_range still hit a zombie 4 blocks off")
	}
	pl.playerAttrs().SetBase(attr.EntityInteractionRange, 3)
	pl.gamemode = gmCreative
	pl.updatePlayerAttributes()
	if v := pl.playerAttrs().Value(attr.EntityInteractionRange); v != 5 {
		t.Errorf("creative entity range %v, want 5", v)
	}
	if v := pl.playerAttrs().Value(attr.BlockInteractionRange); v != 5 {
		t.Errorf("creative block range %v, want 5", v)
	}
	pl.gamemode = gmSurvival
	pl.updatePlayerAttributes()
	if v := pl.playerAttrs().Value(attr.EntityInteractionRange); v != 3 {
		t.Errorf("survival entity range %v, want 3", v)
	}
}
