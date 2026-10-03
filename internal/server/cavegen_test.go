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
