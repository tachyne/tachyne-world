package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// GameType.isSurvival covers adventure: a zombie hunts an adventure player,
// and the blow lands.
func TestAdventurePlayersAreHuntedAndHurt(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	pl.gamemode = gmAdventure
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 3.5, 100, 0.5
	if got := h.nearestHuntable(players, dimOverworld, 0.5, 0.5, 16); got != pl {
		t.Error("an adventure player is not huntable")
	}
	before := pl.health
	h.hurtBy(players, pl, 4, dtGeneric, deathCause{})
	if pl.health >= before {
		t.Error("an adventure player took no damage")
	}
	pl.gamemode = gmCreative
	if got := h.nearestHuntable(players, dimOverworld, 0.5, 0.5, 16); got != nil {
		t.Error("a creative player is huntable")
	}
}
