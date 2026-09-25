package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSoulFireBurnsTwice: BaseFireBlock's fireDamage is 1 for fire and 2
// for soul fire (SoulFireBlock), for whoever stands in it.
func TestSoulFireBurnsTwice(t *testing.T) {
	for _, c := range []struct {
		fire uint32
		want int
	}{{worldgen.BlockBase("fire"), 1}, {soulFire, 2}} {
		h := newHub(world.New(1))
		pl := survPlayer(h)
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		w := h.world
		w.SetBlock(3, 179, 3, worldgen.BlockBase("soul_sand"))
		w.SetBlock(3, 180, 3, c.fire)
		w.SetBlock(3, 181, 3, worldgen.Air)
		m := h.spawnMob(players, entityCow, 3.5, 180, 3.5)
		m.spawnInvuln = 0
		before := m.health
		h.mobContactTick(players)
		if got := before - m.health; got != c.want {
			t.Errorf("state %d: a cow standing in it lost %d health, want %d", c.fire, got, c.want)
		}
	}
}
