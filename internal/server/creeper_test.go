package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestCreeperFusesAndExplodes(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(20, 20)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	m := h.spawnHostile(players, entityCreeper, lx, lz)
	pl.x, pl.y, pl.z = m.x+1.5, m.y, m.z // right next to it

	h.creeperFuse(players, m)
	if m.fuse != creeperFuseTicks {
		t.Fatalf("creeper next to a player must ignite, fuse=%d", m.fuse)
	}

	groundY := int(m.y) - 1 // solid under its feet (findLand ⇒ standable)
	if h.world.At(lx, groundY, lz) == worldgen.Air {
		t.Fatal("test setup: expected solid ground under the creeper")
	}
	for i := 0; i < 40; i++ {
		if _, alive := h.mobs[m.eid]; !alive {
			break
		}
		h.creeperFuse(players, m)
	}
	if _, alive := h.mobs[m.eid]; alive {
		t.Fatal("fuse ran out but the creeper did not explode")
	}
	if h.world.At(lx, groundY, lz) != worldgen.Air {
		t.Fatal("explosion must carve a crater (ground block survived)")
	}
	if pl.health >= maxHealth {
		t.Fatalf("point-blank blast must hurt: health=%v", pl.health)
	}
}

func TestCreeperDefusesWhenTargetEscapes(t *testing.T) {
	h := newHub(world.New(1))
	lx, lz := h.findLand(40, 40)
	pl := testTracked()
	players := map[int32]*tracked{1: pl}

	m := h.spawnHostile(players, entityCreeper, lx, lz)
	pl.x, pl.y, pl.z = m.x+1.0, m.y, m.z
	h.creeperFuse(players, m)
	if m.fuse == 0 {
		t.Fatal("should have ignited")
	}

	pl.x = m.x + 30 // sprint away past the cancel range
	h.creeperFuse(players, m)
	if m.fuse != 0 {
		t.Fatalf("creeper must defuse when the target escapes, fuse=%d", m.fuse)
	}
	for i := 0; i < 60; i++ {
		h.creeperFuse(players, m)
	}
	if _, alive := h.mobs[m.eid]; !alive {
		t.Fatal("a defused creeper must not explode")
	}

	// While fusing it stands its ground (no chase mid-swell).
	m.fuse = 10
	if vx, vz := (creeperBehavior{}).steer(h, m); vx != 0 || vz != 0 {
		t.Fatal("a fusing creeper must hold still")
	}
}
