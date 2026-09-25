package server

import (
	"testing"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// waitFlags drains a watcher's queue until a shared-flags update for eid
// carries all the want bits and none of the not bits.
func waitFlags(t *testing.T, watcher *player, eid int32, want, not byte, what string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second) // a -race run on a shared CI runner is slow
	for time.Now().Before(deadline) {
		select {
		case o := <-watcher.out:
			if m, ok := o.ev.(attachproto.EntityMeta); ok && m.EID == eid && len(m.Meta) >= 3 && m.Meta[0] == 0 {
				if f := m.Meta[2]; f&want == want && f&not == 0 {
					return
				}
			}
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatalf("the watcher never saw %s", what)
}

// Other clients draw a player's pose from the shared flags (updatePlayerPose
// → getDesiredPose): crouching, sprinting and swimming must be in the byte.
func TestOthersSeeCrouchSprintAndSwim(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false // natural spawning ran ticks past a second on CI
	h.world.ForceLoad(0, 0, 2)
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			h.world.SetBlock(x, 99, z, worldgen.Stone)
			for y := 100; y <= 110; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
			for y := 100; y <= 104; y++ {
				h.world.SetBlock(x, y, z, worldgen.Water) // a pool, deep enough to swim in
			}
		}
	}
	startHub(t, h)
	swimmer := newPlayer(h.allocEID(), "swimmer", [16]byte{1})
	watcher := newPlayer(h.allocEID(), "watcher", [16]byte{2})
	h.post(evJoin{p: swimmer, x: 0.5, y: 105, z: 0.5, gamemode: gmSurvival})
	waitJoined(t, h, "swimmer")
	h.post(evJoin{p: watcher, x: 3.5, y: 105, z: 0.5, gamemode: gmSurvival})
	waitJoined(t, h, "watcher")

	h.post(evSneak{eid: swimmer.eid, sneaking: true})
	waitFlags(t, watcher, swimmer.eid, entFlagCrouching, 0, "the crouch bit")
	h.post(evSneak{eid: swimmer.eid, sneaking: false})
	waitFlags(t, watcher, swimmer.eid, 0, entFlagCrouching, "the crouch bit cleared")

	h.post(evMove{eid: swimmer.eid, x: 0.5, y: 101, z: 0.5, sprinting: true, teleport: true})
	waitFlags(t, watcher, swimmer.eid, entFlagSprinting|entFlagSwimming, 0, "the sprint and swim bits")
}
