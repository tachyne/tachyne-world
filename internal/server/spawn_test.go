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
