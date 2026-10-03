package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The overworld's cave generator — the engine's own (native) or vanilla's
// noise caves and carvers (worldgen/vanillacaves.go) — is chosen once, when
// a world is made, and kept beside world.gob in worldgen.json: switching an
// existing world would reshape every cave anyone has explored, and cut
// through what they built in them. So -caves applies only to a NEW world (no
// edits and no stored choice); a world with a stored choice keeps it, and a
// world from before the choice existed (edits, no file) is native, as it was
// made. A -caves that disagrees is ignored, with a log line. The choice is
// written down at the first boot either way, so it is fixed from then on.
//
// It lives in its own file rather than settings.json because it must be
// known before the first chunk generates — settings.json is the hub's, and
// loads long after the world is up — and because, like the build guard's
// snapshot, it is an input to generation that belongs with the edits.

// caveGenFile is the world's generation choices, beside world.gob.
const caveGenFile = "worldgen.json"

// worldGenChoice is worldgen.json.
type worldGenChoice struct {
	Caves string `json:"caves"`
}

// caveGenPath is where this world's choice is kept ("" = in-memory world).
func (s *Server) caveGenPath() string {
	if s.WorldFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.WorldFile), caveGenFile)
}

// applyCaveMode decides the overworld's cave generator, applies it and
// writes it down. It runs at boot before any chunk is generated (and before
// the chunk cache attaches: vanilla caves key their own cache entries).
func (s *Server) applyCaveMode() error {
	want, flagSet := worldgen.CavesNative, s.Caves != ""
	if flagSet {
		m, ok := worldgen.ParseCaveMode(s.Caves)
		if !ok {
			return fmt.Errorf("invalid -caves %q (want native or vanilla)", s.Caves)
		}
		want = m
	}
	path := s.caveGenPath()
	if path == "" { // a world that lives in memory is new every boot
		s.world.SetCaves(want)
		return nil
	}
	var stored worldGenChoice
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("%s: %w", path, err)
	default:
		// A file that does not parse must stop the boot: guessing would
		// regenerate a vanilla world's caves as native ones, or the reverse.
		if err := json.Unmarshal(data, &stored); err != nil {
			return fmt.Errorf("%s: %w (fix or restore it; the cave generator cannot be guessed)", path, err)
		}
	}
	mode := worldgen.CavesNative
	switch {
	case stored.Caves != "":
		m, ok := worldgen.ParseCaveMode(stored.Caves)
		if !ok {
			return fmt.Errorf("%s: unknown caves %q (want native or vanilla)", path, stored.Caves)
		}
		mode = m
		if flagSet && want != m {
			log.Printf("caves: -caves %s ignored: this world was made with %s caves (%s), and changing them would reshape every cave", want, m, path)
		}
	case s.world.EditCount() > 0:
		// Made before the choice existed: native, as it was generated.
		if flagSet && want != worldgen.CavesNative {
			log.Printf("caves: -caves %s ignored: this world already has %d edits on native caves; the choice is made when a world is new", want, s.world.EditCount())
		}
	default:
		mode = want // a new world takes the flag
	}
	s.world.SetCaves(mode)
	if stored.Caves == "" {
		out, err := json.MarshalIndent(worldGenChoice{Caves: mode.String()}, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(path, append(out, '\n')); err != nil {
			return fmt.Errorf("saving the cave choice to %s: %w", path, err)
		}
	}
	log.Printf("caves: %s (%s)", mode, path)
	return nil
}
