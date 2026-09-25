package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// entities/stray, bogged and parched: a player's kill may drop a tipped
// arrow, and the table's set_potion makes it an arrow of slowness, poison
// or weakness. The function was dropped when the tables were baked, so the
// arrows came out with no potion at all.
func TestSkeletonKinDropTippedArrowsWithTheirPotion(t *testing.T) {
	for _, c := range []struct {
		name  string
		etype int
		want  int8
	}{
		{"stray", entityStray, potSlowness},
		{"bogged", entityBogged, potPoison},
		{"parched", entityParched, potWeakness},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newHub(world.New(1))
			h.world.ForceLoad(0, 0, 1)
			h.rules.DoMobLoot = true
			pl := survPlayer(h)
			players := map[int32]*tracked{pl.p.eid: pl}
			h.playersRef = players
			for i := 0; i < 64; i++ {
				m := h.spawnMob(players, c.etype, 0.5, 180, 0.5)
				h.hurtByPlayerOn(m, pl) // a player's blow: the kill is theirs
				h.killMob(players, m)
				h.despawnMob(players, m) // the death animation over, the loot rolls
				for _, it := range h.items {
					if it.item != itemTippedArrow {
						continue
					}
					if it.potion != c.want {
						t.Fatalf("the %s's tipped arrow carries potion %d, want %d", c.name, it.potion, c.want)
					}
					return
				}
			}
			t.Fatalf("64 player kills of a %s dropped no tipped arrow", c.name)
		})
	}
}
