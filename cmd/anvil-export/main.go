// anvil-export writes a tachyne world out in the vanilla Anvil save format
// (level.dat + region/*.mca) so vanilla-world tooling — most usefully the
// BlueMap 3D web-map renderer — can consume it.
//
// The engine's world is a pure function of (seed, edits), so the exporter
// needs only the seed the server runs with and its edit files (world.gob,
// with nether.gob/end.gob alongside):
//
//	anvil-export -seed 12345 -world /var/world/world.gob -out ./save \
//	    -radius 32 -dims overworld,nether,end
//
// Exports the radius window around -center plus every chunk a player has
// touched. Point BlueMap's map config at the -out folder.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tachyne/tachyne-world/internal/anvil"
	"github.com/tachyne/tachyne-world/internal/world"
)

func main() {
	seed := flag.Int64("seed", 0, "world seed (must match the server's -seed)")
	worldFile := flag.String("world", "world.gob", "overworld edit file; nether.gob/end.gob are read from the same directory")
	out := flag.String("out", "tachyne-anvil", "output save folder")
	radius := flag.Int("radius", 32, "chunk radius exported around -center (plus all edited chunks)")
	center := flag.String("center", "0,0", "block x,z at the centre of the export window")
	dims := flag.String("dims", "overworld", "dimensions to export: overworld,nether,end")
	noLight := flag.Bool("nolight", false, "skip light computation (faster; map renders flat-lit)")
	name := flag.String("name", "tachyne", "world name written to level.dat")
	flag.Parse()

	cx, cz, err := parseCenter(*center)
	if err != nil {
		log.Fatalf("-center: %v", err)
	}
	dir := filepath.Dir(*worldFile)
	ts := uint32(time.Now().Unix())

	type dim struct {
		file, sub string
		open      func(int64, world.Store) (*world.World, error)
	}
	byName := map[string]dim{
		"overworld": {*worldFile, "", world.NewWithStore},
		"nether":    {filepath.Join(dir, "nether.gob"), "DIM-1", world.NewNether},
		"end":       {filepath.Join(dir, "end.gob"), "DIM1", world.NewEnd},
	}

	start := time.Now()
	total := 0
	for _, dn := range strings.Split(*dims, ",") {
		dn = strings.TrimSpace(dn)
		d, ok := byName[dn]
		if !ok {
			log.Fatalf("unknown dimension %q (want overworld, nether, end)", dn)
		}
		w, err := d.open(*seed, world.NewFileStore(d.file))
		if err != nil {
			log.Fatalf("%s: %v", dn, err)
		}
		last := 0
		n, err := anvil.Export(w, anvil.Options{
			Dir:     *out,
			SubDir:  d.sub,
			CenterX: int32(cx >> 4), CenterZ: int32(cz >> 4),
			Radius:    int32(*radius),
			Timestamp: ts,
			NoLight:   *noLight,
			Progress: func(done, total int) {
				if done-last >= 500 || done == total {
					log.Printf("%s: %d/%d chunks", dn, done, total)
					last = done
				}
			},
		})
		if err != nil {
			log.Fatalf("%s: export: %v", dn, err)
		}
		if dn == "overworld" {
			y := w.GroundY(cx, cz) + 1
			if err := anvil.WriteLevelDat(filepath.Join(*out, "level.dat"),
				*name, int32(cx), int32(y), int32(cz)); err != nil {
				log.Fatalf("level.dat: %v", err)
			}
		}
		total += n
	}
	fmt.Fprintf(os.Stderr, "exported %d chunks to %s in %s\n",
		total, *out, time.Since(start).Round(time.Millisecond))
}

func parseCenter(s string) (int, int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("want x,z")
	}
	x, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, err
	}
	z, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, err
	}
	return x, z, nil
}
