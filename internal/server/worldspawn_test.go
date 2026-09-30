package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func newSpawnTestServer(t *testing.T, w *world.World) *Server {
	s := New()
	s.world = w
	s.hub = newTestHub(w)
	s.hub.rulesPath = filepath.Join(t.TempDir(), "settings.json")
	s.modes = newModeStore("", gmSurvival)
	s.hub.loadRules()
	return s
}

// A new world picks its spawn as setInitialSpawn does — the climate
// origin, then a safe surface near it — saves it, and a joining player
// arrives there standing on a full top face, out of any fluid.
func TestNewWorldSpawnIsChosenAndSaved(t *testing.T) {
	s := newSpawnTestServer(t, world.New(1))
	if !s.newWorldSpawn() {
		t.Fatal("a new world did not choose a spawn")
	}
	sp := s.hub.rules.WorldSpawn
	if sp == nil || !s.hub.hasWorldSpawn {
		t.Fatal("the chosen spawn was not saved and installed")
	}
	w := s.world
	if !worldgen.IsSturdyTop(w.At(sp.X, sp.Y-1, sp.Z)) {
		t.Errorf("spawn (%d,%d,%d) does not stand on a full top face: %d", sp.X, sp.Y, sp.Z, w.At(sp.X, sp.Y-1, sp.Z))
	}
	for dy := 0; dy <= 1; dy++ {
		if st := w.At(sp.X, sp.Y+dy, sp.Z); worldgen.HoldsWater(st) || worldgen.IsLava(st) {
			t.Errorf("spawn (%d,%d,%d) is in a fluid", sp.X, sp.Y, sp.Z)
		}
	}
	if x, y, z := s.joinSpawn(); x != float64(sp.X)+0.5 || y != float64(sp.Y) || z != float64(sp.Z)+0.5 {
		t.Errorf("a joining player arrives at %v,%v,%v, not the chosen spawn", x, y, z)
	}
	// Loaded again at the next boot, it is the saved one — never re-chosen.
	s2 := newSpawnTestServer(t, s.world)
	s2.hub.rulesPath = s.hub.rulesPath
	s2.hub.loadRules()
	if s2.newWorldSpawn() {
		t.Error("a world with a saved spawn chose another")
	}
}

// The climate origin is land that is not a river: the spawn target's
// continentalness and ridges, in the engine's climate.
func TestNoiseSpawnOriginFindsLand(t *testing.T) {
	w := world.New(1)
	x, z := noiseSpawnOrigin(w)
	if f := spawnFitness(w, x>>2<<2, z>>2<<2); f != 0 {
		t.Errorf("origin (%d,%d) misses the spawn target (%s, fitness %d)", x, z, w.BiomeAt(x, z), f)
	}
}

// An existing world — anything edited in any dimension — keeps its spawn,
// as does one given a spawn on the command line.
func TestExistingWorldSpawnNeverMoves(t *testing.T) {
	w := world.New(1)
	w.SetBlock(3, 200, 3, worldgen.Stone)
	s := newSpawnTestServer(t, w)
	if s.newWorldSpawn() || s.hub.rules.WorldSpawn != nil {
		t.Error("an edited world had its spawn chosen afresh")
	}
	s = newSpawnTestServer(t, world.New(1))
	s.SpawnSet, s.SpawnX, s.SpawnY, s.SpawnZ = true, 5, 64, 5
	if s.newWorldSpawn() {
		t.Error("a -spawn flag was overridden")
	}
}
