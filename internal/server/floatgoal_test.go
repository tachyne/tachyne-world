package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A cow in deep water comes up and bobs with its eyes clear, swims on, and
// never drowns; a zombie sinks and walks the bottom; shallow water is waded.
func TestFloatersBobInDeepWater(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 183; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	pl.x, pl.y, pl.z = 0.5, 184, 0.5
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	zombie := h.spawnMob(players, entityZombie, 3.5, 180, 3.5)
	if cow == nil || zombie == nil {
		t.Fatal("spawn returned nil")
	}
	cow.spawnInvuln, zombie.spawnInvuln = 0, 0
	cowHP := cow.health
	if !mobFloats(cow) || mobFloats(zombie) {
		t.Fatal("a cow floats and a zombie sinks")
	}
	want, ok := h.floatLevel(cow, 0, 0, 180)
	if !ok || math.Abs(want-(184-mobEyeHeight(cow)+floatEyeAbove)) > 1e-9 {
		t.Fatalf("float level %.2f ok=%v", want, ok)
	}
	for i := 0; i < 120; i++ {
		h.updateMobs(players)
	}
	if math.Abs(cow.y-want) > 0.01 {
		t.Errorf("the cow rides at y=%.2f, want %.2f", cow.y, want)
	}
	if int(math.Floor(cow.y+mobEyeHeight(cow))) != 184 {
		t.Errorf("the cow's eyes are at y=%.2f, under the surface", cow.y+mobEyeHeight(cow))
	}
	if zombie.y != 180 {
		t.Errorf("the zombie should walk the bottom, at y=%.2f", zombie.y)
	}
	// It can swim on through the deep water, where a walker's step rule would refuse the drop.
	cow.x, cow.z = 0.5, 0.5
	if !h.mobStepOK(cow, 1.5, 0.5) {
		t.Error("a floating cow could not swim to the next cell")
	}
	// A second of environment: the cow breathes, the zombie is going under.
	for i := 0; i < 20; i++ {
		h.mobEnvironment(players)
	}
	if cow.submerged != 0 || cow.health != cowHP {
		t.Errorf("the cow is drowning: submerged %d health %d", cow.submerged, cow.health)
	}
	if zombie.submerged == 0 {
		t.Error("the sunk zombie is not counting its time under")
	}
	// Shallow water is waded, not floated.
	for x := -8; x <= 8; x++ {
		for z := -8; z <= 8; z++ {
			for y := 181; y <= 183; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	if _, ok := h.floatLevel(cow, 0, 0, 180); ok {
		t.Error("one block of water is waded, not floated")
	}
}
