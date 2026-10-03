package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// caveServer is a server over a world file in dir, as Serve sets one up
// before it decides the caves.
func caveServer(t *testing.T, dir, flag string, edits bool) *Server {
	t.Helper()
	s := New()
	s.WorldFile = filepath.Join(dir, "world.gob")
	s.Caves = flag
	s.world = world.New(s.Seed)
	if edits {
		s.world.SetBlock(0, 200, 0, worldgen.Stone)
		if s.world.EditCount() == 0 {
			t.Fatal("the edit did not count")
		}
	}
	return s
}

func storedCaves(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, caveGenFile))
	if err != nil {
		t.Fatalf("no stored cave choice: %v", err)
	}
	var c worldGenChoice
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c.Caves
}

// A new world takes -caves vanilla and writes it down; the next boot keeps
// it with no flag, and a -caves native then is ignored.
func TestCaveModeNewWorldTakesTheFlag(t *testing.T) {
	dir := t.TempDir()
	s := caveServer(t, dir, "vanilla", false)
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if s.world.Caves() != worldgen.CavesVanilla || storedCaves(t, dir) != "vanilla" {
		t.Fatalf("new world: %v, stored %q", s.world.Caves(), storedCaves(t, dir))
	}
	for _, flag := range []string{"", "native"} {
		again := caveServer(t, dir, flag, true)
		if err := again.applyCaveMode(); err != nil {
			t.Fatal(err)
		}
		if again.world.Caves() != worldgen.CavesVanilla {
			t.Fatalf("reboot with -caves %q: %v", flag, again.world.Caves())
		}
	}
	if storedCaves(t, dir) != "vanilla" {
		t.Fatal("the stored choice changed")
	}
}

// A world from before the choice (edits, no file) stays native whatever the
// flag says, and is written down as native.
func TestCaveModeExistingWorldStaysNative(t *testing.T) {
	dir := t.TempDir()
	s := caveServer(t, dir, "vanilla", true)
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if s.world.Caves() != worldgen.CavesNative || storedCaves(t, dir) != "native" {
		t.Fatalf("existing world: %v, stored %q", s.world.Caves(), storedCaves(t, dir))
	}
}

// With no flag a new world is native, and that too is fixed: a later
// -caves vanilla on it is ignored even before anyone edits it.
func TestCaveModeDefaultIsNativeAndFixed(t *testing.T) {
	dir := t.TempDir()
	s := caveServer(t, dir, "", false)
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if s.world.Caves() != worldgen.CavesNative || storedCaves(t, dir) != "native" {
		t.Fatalf("default: %v, stored %q", s.world.Caves(), storedCaves(t, dir))
	}
	again := caveServer(t, dir, "vanilla", false)
	if err := again.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if again.world.Caves() != worldgen.CavesNative {
		t.Fatalf("a stored native choice gave way to the flag: %v", again.world.Caves())
	}
}

// A bad flag or an unreadable choice stops the boot rather than guess.
func TestCaveModeRefusesToGuess(t *testing.T) {
	dir := t.TempDir()
	if err := caveServer(t, dir, "tachyne", false).applyCaveMode(); err == nil {
		t.Error("an unknown -caves value was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, caveGenFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := caveServer(t, dir, "", true).applyCaveMode(); err == nil {
		t.Error("a corrupt worldgen.json was read as no choice")
	}
	if err := os.WriteFile(filepath.Join(dir, caveGenFile), []byte(`{"caves":"tachyne"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := caveServer(t, dir, "", true).applyCaveMode(); err == nil {
		t.Error("an unknown stored cave generator was accepted")
	}
}

// A world kept only in memory is new every boot: the flag applies, nothing
// is written.
func TestCaveModeInMemoryWorld(t *testing.T) {
	s := New()
	s.Caves = "vanilla"
	s.world = world.New(s.Seed)
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if s.world.Caves() != worldgen.CavesVanilla {
		t.Fatalf("in-memory world: %v", s.world.Caves())
	}
}

func storedChoice(t *testing.T, dir string) worldGenChoice {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, caveGenFile))
	if err != nil {
		t.Fatalf("no stored choice: %v", err)
	}
	var c worldGenChoice
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// A new world takes -generator vanilla and -preset, with vanilla caves, and
// writes them down; the next boot keeps them whatever the flags say.
func TestGeneratorNewWorldTakesTheFlag(t *testing.T) {
	dir := t.TempDir()
	s := caveServer(t, dir, "native", false)
	s.Generator, s.Preset = "vanilla", "amplified"
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	want := worldGenChoice{Caves: "vanilla", Generator: "vanilla", Preset: "amplified"}
	if got := storedChoice(t, dir); got != want || s.world.GeneratorMode() != worldgen.GeneratorVanilla ||
		s.world.Caves() != worldgen.CavesVanilla || s.world.Gen().Preset() != worldgen.PresetAmplified {
		t.Fatalf("new world: stored %+v, mode %v caves %v preset %v", got, s.world.GeneratorMode(), s.world.Caves(), s.world.Gen().Preset())
	}
	again := caveServer(t, dir, "", true)
	again.Generator, again.Preset = "native", "flat"
	if err := again.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if again.world.GeneratorMode() != worldgen.GeneratorVanilla || again.world.Gen().Preset() != worldgen.PresetAmplified {
		t.Fatalf("reboot: %v %v", again.world.GeneratorMode(), again.world.Gen().Preset())
	}
	if got := storedChoice(t, dir); got != want {
		t.Fatalf("the stored choice changed: %+v", got)
	}
}

// A world from before the generator choice — edits, or a worldgen.json
// holding only its caves — is native, and is written down as native.
func TestGeneratorExistingWorldStaysNative(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, caveGenFile), []byte(`{"caves":"vanilla"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s := caveServer(t, dir, "", false)
	s.Generator = "vanilla"
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if got := storedChoice(t, dir); got != (worldGenChoice{Caves: "vanilla", Generator: "native"}) ||
		s.world.GeneratorMode() != worldgen.GeneratorNative || s.world.Caves() != worldgen.CavesVanilla {
		t.Fatalf("caves-only file: stored %+v, mode %v caves %v", got, s.world.GeneratorMode(), s.world.Caves())
	}
	dir = t.TempDir()
	s = caveServer(t, dir, "", true)
	s.Generator = "vanilla"
	if err := s.applyCaveMode(); err != nil {
		t.Fatal(err)
	}
	if got := storedChoice(t, dir); got != (worldGenChoice{Caves: "native", Generator: "native"}) || s.world.GeneratorMode() != worldgen.GeneratorNative {
		t.Fatalf("edited world: stored %+v, mode %v", got, s.world.GeneratorMode())
	}
}

// Bad generator or preset values, flagged or stored, stop the boot.
func TestGeneratorRefusesToGuess(t *testing.T) {
	dir := t.TempDir()
	s := caveServer(t, dir, "", false)
	s.Generator = "tachyne"
	if err := s.applyCaveMode(); err == nil {
		t.Error("an unknown -generator was accepted")
	}
	s = caveServer(t, dir, "", false)
	s.Generator, s.Preset = "vanilla", "lumpy"
	if err := s.applyCaveMode(); err == nil {
		t.Error("an unknown -preset was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, caveGenFile), []byte(`{"caves":"vanilla","generator":"tachyne"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := caveServer(t, dir, "", true).applyCaveMode(); err == nil {
		t.Error("an unknown stored generator was accepted")
	}
}

// A vanilla world's spawn origin is the server's (NoiseSpawnFinder over
// the overworld's spawn targets; the 26.3 server's getOrigin for these
// seeds).
func TestVanillaWorldSpawnOrigin(t *testing.T) {
	for _, c := range []struct {
		seed   int64
		cx, cz int
	}{{1, 10, 10}, {5, 0, 0}, {-42, -7, -38}} {
		w := world.New(c.seed)
		if err := w.SetGenerator(worldgen.GeneratorVanilla, worldgen.PresetNormal); err != nil {
			t.Fatal(err)
		}
		x, z := noiseSpawnOrigin(w)
		if x>>4 != c.cx || z>>4 != c.cz {
			t.Errorf("seed %d: origin chunk %d,%d, the server's %d,%d", c.seed, x>>4, z>>4, c.cx, c.cz)
		}
	}
}
