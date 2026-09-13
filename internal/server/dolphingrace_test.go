package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDolphinsGrace: a dolphin near a sprint-swimming player follows them
// and grants Dolphin's Grace; a wading player gets nothing.
func TestDolphinsGrace(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -10; x <= 10; x++ {
		for z := -10; z <= 10; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 183; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	pl.x, pl.y, pl.z = 6.5, 181, 0.5
	d := h.spawnMob(players, entityDolphin, 0.5, 181, 0.5)
	if h.dolphinSwimWithPlayer(players, d) {
		t.Fatal("a player merely in the water is not swimming")
	}
	pl.sprinting = true
	if !h.dolphinSwimWithPlayer(players, d) || pl.hasEffect(effDolphinsGrace) == 0 || d.vx <= 0 {
		t.Fatalf("a swimming player is joined and graced: grace %d vx %.2f", pl.hasEffect(effDolphinsGrace), d.vx)
	}
	pl.x = 30
	if h.dolphinSwimWithPlayer(players, d) || d.dolphinSwimmer != 0 {
		t.Fatal("past sixteen blocks the dolphin gives up")
	}
}
