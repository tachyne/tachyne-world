package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The overworld's generator — the engine's own (native) or vanilla 26.3's
// (worldgen/vanillagen.go: its noise router, surface rules, biomes and
// placement, under a world preset) — and, in a native world, its cave
// generator — the engine's own or vanilla's noise caves and carvers
// (worldgen/vanillacaves.go) — are chosen once, when a world is made, and
// kept beside world.gob in worldgen.json: switching an existing world would
// reshape every block anyone has explored, and cut through what they built.
// So -generator, -preset and -caves apply only to a NEW world (no edits and
// no stored choice); a world with a stored choice keeps it, and a world
// from before a choice existed (edits, or a file without it) is native, as
// it was made. A flag that disagrees is ignored, with a log line. The
// choices are written down at the first boot either way, so they are fixed
// from then on. A vanilla world's caves are vanilla's: they are part of its
// terrain.
//
// It lives in its own file rather than settings.json because it must be
// known before the first chunk generates — settings.json is the hub's, and
// loads long after the world is up — and because, like the build guard's
// snapshot, it is an input to generation that belongs with the edits.

// caveGenFile is the world's generation choices, beside world.gob.
const caveGenFile = "worldgen.json"

// worldGenChoice is worldgen.json.
type worldGenChoice struct {
	Caves     string `json:"caves"`
	Generator string `json:"generator,omitempty"`
	Preset    string `json:"preset,omitempty"`
}

// caveGenPath is where this world's choice is kept ("" = in-memory world).
func (s *Server) caveGenPath() string {
	if s.WorldFile == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(s.WorldFile), caveGenFile)
}

// genFlags reads -generator, -preset and -caves.
func (s *Server) genFlags() (gen worldgen.GeneratorMode, genSet bool, preset worldgen.WorldPreset, caves worldgen.CaveMode, cavesSet bool, err error) {
	if s.Generator != "" {
		m, ok := worldgen.ParseGeneratorMode(s.Generator)
		if !ok {
			return 0, false, 0, 0, false, fmt.Errorf("invalid -generator %q (want native or vanilla)", s.Generator)
		}
		gen, genSet = m, true
	}
	if s.Preset != "" {
		p, ok := worldgen.ParseWorldPreset(s.Preset)
		if !ok {
			return 0, false, 0, 0, false, fmt.Errorf("invalid -preset %q (want normal, large_biomes, amplified, single_biome_surface, caves, floating_islands or flat)", s.Preset)
		}
		preset = p
	}
	if s.Caves != "" {
		m, ok := worldgen.ParseCaveMode(s.Caves)
		if !ok {
			return 0, false, 0, 0, false, fmt.Errorf("invalid -caves %q (want native or vanilla)", s.Caves)
		}
		caves, cavesSet = m, true
	}
	return gen, genSet, preset, caves, cavesSet, nil
}

// applyCaveMode decides the world's generator and cave generator, applies
// them and writes them down. It runs at boot before any chunk is generated
// (and before the chunk cache attaches: both tag their cache keys).
func (s *Server) applyCaveMode() error {
	wantGen, genSet, wantPreset, wantCaves, cavesSet, err := s.genFlags()
	if err != nil {
		return err
	}
	path := s.caveGenPath()
	if path == "" { // a world that lives in memory is new every boot
		return s.applyGen(wantGen, wantPreset, wantCaves)
	}
	var stored worldGenChoice
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("%s: %w", path, err)
	default:
		// A file that does not parse must stop the boot: guessing would
		// regenerate a vanilla world as a native one, or the reverse.
		if err := json.Unmarshal(data, &stored); err != nil {
			return fmt.Errorf("%s: %w (fix or restore it; the generator cannot be guessed)", path, err)
		}
	}
	edits := s.world.EditCount()

	// The generator.
	gen, preset := worldgen.GeneratorNative, worldgen.PresetNormal
	switch {
	case stored.Generator != "":
		m, ok := worldgen.ParseGeneratorMode(stored.Generator)
		if !ok {
			return fmt.Errorf("%s: unknown generator %q (want native or vanilla)", path, stored.Generator)
		}
		gen = m
		if m == worldgen.GeneratorVanilla && stored.Preset != "" {
			p, ok := worldgen.ParseWorldPreset(stored.Preset)
			if !ok {
				return fmt.Errorf("%s: unknown preset %q", path, stored.Preset)
			}
			preset = p
		}
		if genSet && wantGen != gen {
			log.Printf("generator: -generator %s ignored: this world was made with the %s generator (%s), and changing it would reshape every block", wantGen, gen, path)
		}
	case stored.Caves != "" || edits > 0:
		// Made before the generator choice existed: native, as generated.
		if genSet && wantGen != worldgen.GeneratorNative {
			log.Printf("generator: -generator %s ignored: this world was made with the native generator; the choice is made when a world is new", wantGen)
		}
	default:
		gen, preset = wantGen, wantPreset // a new world takes the flags
	}
	if gen == worldgen.GeneratorNative {
		preset = worldgen.PresetNormal
	}

	// The caves: a vanilla world's are vanilla's; a native world's as chosen.
	caves := worldgen.CavesNative
	switch {
	case gen == worldgen.GeneratorVanilla:
		caves = worldgen.CavesVanilla
		if cavesSet && wantCaves != caves {
			log.Printf("caves: -caves %s ignored: a vanilla world's caves are vanilla's", wantCaves)
		}
	case stored.Caves != "":
		m, ok := worldgen.ParseCaveMode(stored.Caves)
		if !ok {
			return fmt.Errorf("%s: unknown caves %q (want native or vanilla)", path, stored.Caves)
		}
		caves = m
		if cavesSet && wantCaves != m {
			log.Printf("caves: -caves %s ignored: this world was made with %s caves (%s), and changing them would reshape every cave", wantCaves, m, path)
		}
	case edits > 0:
		// Made before the choice existed: native, as it was generated.
		if cavesSet && wantCaves != worldgen.CavesNative {
			log.Printf("caves: -caves %s ignored: this world already has %d edits on native caves; the choice is made when a world is new", wantCaves, edits)
		}
	default:
		caves = wantCaves // a new world takes the flag
	}

	if err := s.applyGen(gen, preset, caves); err != nil {
		return err
	}
	choice := worldGenChoice{Caves: caves.String(), Generator: gen.String()}
	if gen == worldgen.GeneratorVanilla {
		choice.Preset = preset.String()
	}
	if choice != stored {
		out, err := json.MarshalIndent(choice, "", "  ")
		if err != nil {
			return err
		}
		if err := writeAtomic(path, append(out, '\n')); err != nil {
			return fmt.Errorf("saving the generation choice to %s: %w", path, err)
		}
	}
	log.Printf("generator: %s, caves: %s (%s)", s.genLabel(), caves, path)
	return nil
}

// applyGen sets the overworld's generator and caves and remembers the
// generator for the other dimensions (applyDimGen).
func (s *Server) applyGen(gen worldgen.GeneratorMode, preset worldgen.WorldPreset, caves worldgen.CaveMode) error {
	s.genMode, s.genPreset = gen, preset
	if gen == worldgen.GeneratorVanilla {
		return s.world.SetGenerator(gen, preset)
	}
	s.world.SetCaves(caves)
	return nil
}

// applyDimGen gives another dimension's world the overworld's generator:
// the Nether's and End's vanilla generators install from the world seed
// through the world's UseVanillaDimension (the vanilla Nether and End);
// while a build has none, those dimensions keep the engine's own.
func (s *Server) applyDimGen(w *world.World) error {
	if s.genMode != worldgen.GeneratorVanilla {
		return nil
	}
	if u, ok := any(w).(interface{ UseVanillaDimension() bool }); ok {
		if !u.UseVanillaDimension() {
			log.Printf("generator: no vanilla generator for a dimension; the engine's own stands in")
		}
		return nil
	}
	return w.SetGenerator(s.genMode, s.genPreset)
}

func (s *Server) genLabel() string {
	if s.genMode == worldgen.GeneratorVanilla {
		return fmt.Sprintf("vanilla (%s)", s.genPreset)
	}
	return "native"
}
