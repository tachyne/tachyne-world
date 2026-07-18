package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestWardenSonicBoomAndDigAway(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 5, 65, 5
	players := map[int32]*tracked{1: pl}
	w := h.spawnMob(players, entityWarden, 8, 65, 5) // 3 blocks away: in sonic range
	if w == nil {
		t.Fatal("warden spawn returned nil")
	}

	// Cooldown starts at 0, target in range → the boom lands and pierces armour.
	h.wardenTick(players, w)
	if pl.health >= maxHealth {
		t.Fatalf("sonic boom should damage the player, health=%v", pl.health)
	}
	if w.sonicCD == 0 {
		t.Fatal("firing a sonic boom should start its cooldown")
	}

	// With no player at all, the warden digs away (despawns) after the cap.
	empty := map[int32]*tracked{}
	w2 := h.spawnMob(empty, entityWarden, 200, 65, 200)
	eid := w2.eid
	for i := 0; i <= wardenDigAwayUpd+1; i++ {
		if h.mobs[eid] == nil {
			break
		}
		h.wardenTick(empty, w2)
	}
	if h.mobs[eid] != nil {
		t.Fatal("warden with no target should dig away (despawn)")
	}
}
