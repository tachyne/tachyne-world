package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// buildWitherFrame lays the soul-sand T + three skulls (row along X) centred at
// (cx,topY,cz), leaving the final skull for checkWitherBuild to "complete".
func buildWitherFrame(w *world.World, cx, topY, cz int) {
	skull := worldgen.BlockBase("wither_skeleton_skull") + 16 // default
	for j := -1; j <= 1; j++ {
		w.SetBlock(cx+j, topY-1, cz, blockSoulSand) // arms
		w.SetBlock(cx+j, topY, cz, skull)           // skulls
	}
	w.SetBlock(cx, topY-2, cz, blockSoulSand) // stem
}

func TestWitherBuildSpawns(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	cx, cy, cz := 5, 70, 5
	buildWitherFrame(w, cx, cy, cz)

	before := len(h.mobs)
	h.checkWitherBuild(players, 0, cx+1, cy, cz, (worldgen.BlockBase("wither_skeleton_skull") + 16)) // "placed" the last skull
	if len(h.mobs) != before+1 {
		t.Fatalf("completing the frame should spawn a wither: %d mobs", len(h.mobs))
	}
	var wm *mob
	for _, m := range h.mobs {
		if m.etype == entityWither {
			wm = m
		}
	}
	if wm == nil {
		t.Fatal("no wither spawned")
	}
	if wm.spawnInvuln <= 0 {
		t.Fatal("a fresh wither should be charging (invulnerable)")
	}
	// The frame is consumed.
	if w.At(cx, cy, cz) != worldgen.Air || w.At(cx, cy-1, cz) != worldgen.Air || w.At(cx, cy-2, cz) != worldgen.Air {
		t.Fatal("the soul sand + skulls should be consumed on spawn")
	}
}

func TestWitherInvulnerableWhileCharging(t *testing.T) {
	m := &mob{etype: entityWither, health: witherHealth, spawnInvuln: 10}
	m.hurt(50)
	if m.health != witherHealth {
		t.Fatalf("a charging wither must take no damage, health=%d", m.health)
	}
	m.spawnInvuln = 0
	m.hurt(50)
	if m.health >= witherHealth {
		t.Fatalf("a charged wither should take damage, health=%d", m.health)
	}
}

func TestWitherChargeReleases(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	m := h.spawnSpecies(players, entityWither, 0, 5.5, 70, 5.5)
	m.health = witherHealth
	m.spawnInvuln = 3
	for i := 0; i < 3; i++ {
		h.updateWithers(players)
	}
	if m.spawnInvuln != 0 {
		t.Fatalf("charge should count down to 0, got %d", m.spawnInvuln)
	}
}

func TestNonSkullDoesNotSpawnWither(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	buildWitherFrame(w, 5, 70, 5)
	// A non-skull placement (e.g. soul sand) must not trigger the build.
	h.checkWitherBuild(players, 0, 6, 70, 5, blockSoulSand)
	for _, m := range h.mobs {
		if m.etype == entityWither {
			t.Fatal("only a wither skull completes the build")
		}
	}
}
