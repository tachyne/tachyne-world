package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An auto (x,z) spawn must land the player STANDING on the surface, never
// embedded in rock (the -spawn "x,z" parse leaves a bogus leftover Y that
// JoinRemote would otherwise use verbatim — this is the "spawned into rock" bug).
func TestAutoSpawnResolvesToStandableSurface(t *testing.T) {
	s := New()
	s.world = world.New(1)
	s.SpawnSet, s.SpawnAuto = true, true
	s.SpawnX, s.SpawnY, s.SpawnZ = -103, -31, -31 // Y=-31 is the buggy leftover

	s.resolveSpawn()

	if s.SpawnAuto {
		t.Error("spawn still marked auto after resolveSpawn")
	}
	fx, fy, fz := int(s.SpawnX), int(s.SpawnY), int(s.SpawnZ)
	if worldgen.Collides(s.world.At(fx, fy, fz)) {
		t.Errorf("spawn feet at (%d,%d,%d) are inside a solid block (%d)", fx, fy, fz, s.world.At(fx, fy, fz))
	}
	if !worldgen.Collides(s.world.At(fx, fy-1, fz)) && !worldgen.IsWater(s.world.At(fx, fy, fz)) {
		t.Errorf("spawn at (%d,%d,%d) is floating — no ground beneath and not in water", fx, fy, fz)
	}
	if s.SpawnY < 1 { // -31 leftover would fail this
		t.Errorf("spawn Y not resolved: %v", s.SpawnY)
	}
}

// vanilla isSpawnPositionOk ends in noCollision(getSpawnAABB): the whole mob
// has to fit. The engine tested two cells flat, which is right for a zombie
// (1.95) and wrong for an enderman (2.9) — so every two-high cave pocket was
// an enderman spawn site vanilla would have refused, and they built up far
// past anything a vanilla world shows (198 of them in the live world,
// LegionZA #14/#15/#16).
func TestTallMobsNeedTheirOwnHeadroom(t *testing.T) {
	if got := spawnClearCells(entityEnderman); got != 3 {
		t.Errorf("enderman (2.9 high) needs %d cells, want 3", got)
	}
	for _, e := range []int{entityZombie, entitySkeleton, entityCreeper, entitySpider} {
		if got := spawnClearCells(e); got != 2 {
			t.Errorf("%s needs %d cells, want 2", entityNameByID[e], got)
		}
	}
	if got := spawnClearCells(entityIronGolem); got != 3 { // 2.7
		t.Errorf("iron golem needs %d cells, want 3", got)
	}

	h := newHub(world.New(1))
	w := h.world
	x, y, z := 300, 70, 300
	// A pocket exactly two blocks high: stone floor, two clear, stone ceiling.
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x, y+1, z, worldgen.Air)
	w.SetBlock(x, y+2, z, worldgen.Stone)

	if !h.spawnPositionOK(dimOverworld, catMonster, entityZombie, x, y, z) {
		t.Fatal("a zombie should still fit a two-high pocket")
	}
	if h.spawnPositionOK(dimOverworld, catMonster, entityEnderman, x, y, z) {
		t.Fatal("an enderman must NOT fit a two-high pocket — it is 2.9 blocks tall")
	}

	// Open the ceiling and it fits.
	w.SetBlock(x, y+2, z, worldgen.Air)
	if !h.spawnPositionOK(dimOverworld, catMonster, entityEnderman, x, y, z) {
		t.Fatal("an enderman should fit once there are three clear cells")
	}
}
