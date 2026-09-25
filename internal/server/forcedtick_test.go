package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A forced chunk is entity ticking: with nobody online, a zombie in a
// /forceload'ed chunk still moves (here, it comes down out of the air), and
// it is not despawned for want of a player in the level.
func TestForcedChunkTicksMobsWithNobodyOnline(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	h.rules.DoMobSpawning = false
	w.ForceLoad(8, 8, 1)
	w.SetForced(0, 0, true)
	startHub(t, h)
	var z *mob
	top := float64(w.SurfaceY(8, 8)) + 20
	onHub(t, h, func() {
		z = h.spawnMob(h.playersRef, entityZombie, 8.5, top, 8.5)
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		var y float64
		var gone bool
		onHub(t, h, func() { y, gone = z.y, h.mobs[z.eid] == nil })
		if gone {
			t.Fatal("the zombie in the forced chunk was removed with nobody online")
		}
		if y < top-1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the zombie in the forced chunk never moved (y %.1f)", y)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
