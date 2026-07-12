package anvil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Options selects what to export from one dimension.
type Options struct {
	Dir       string // save-folder root (region files land in Dir/SubDir/region)
	SubDir    string // "" overworld, "DIM-1" nether, "DIM1" end (vanilla layout)
	CenterX   int32  // chunk coords of the export window's centre
	CenterZ   int32
	Radius    int32 // chunks; the window is (2r+1)² around the centre
	Timestamp uint32
	NoLight   bool         // skip the light flood (faster, flat-lit map)
	State     *ExportState // optional: keep old timestamps for unchanged chunks
	Workers   int          // concurrent chunk builders; 0 = GOMAXPROCS
	Progress  func(done, total int)
}

// Export writes one dimension's chunks — the radius window around the centre
// plus every chunk with player edits — as Anvil region files. With a State,
// unchanged chunks keep their previous header timestamp and untouched region
// files are skipped entirely, so watching renderers re-render only real
// changes. Returns the number of chunks whose content changed.
func Export(w *world.World, opt Options) (int, error) {
	set := map[[2]int32]bool{}
	for dz := -opt.Radius; dz <= opt.Radius; dz++ {
		for dx := -opt.Radius; dx <= opt.Radius; dx++ {
			set[[2]int32{opt.CenterX + dx, opt.CenterZ + dz}] = true
		}
	}
	for _, c := range w.EditedChunks() {
		set[c] = true
	}

	// Build chunks on a worker pool — chunk generation and the light flood
	// are the cost, and the world layer is already safe for concurrent
	// builders (the attach chunk streamer does exactly this). Bookkeeping
	// (state hashes, region grouping) happens under one mutex; it's
	// negligible next to a chunk build.
	regions := map[[2]int32]map[[2]int][]byte{}
	stamps := map[[2]int32]map[[2]int]uint32{}
	dirty := map[[2]int32]bool{}
	changed, done, total := 0, 0, len(set)

	workers := opt.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	jobs := make(chan [2]int32)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				cx, cz := c[0], c[1]
				ch := w.Chunk(cx, cz)
				var sky, blk [][4096]uint8
				if !opt.NoLight {
					ld := w.Light(cx, cz)
					sky, blk = ld.Sky, ld.Block
				}
				raw := ChunkNBT(cx, cz, worldgen.MinY, ch.Sections, ch.Biomes,
					ch.Heightmap, sky, blk)

				mu.Lock()
				ts := opt.Timestamp
				if opt.State != nil {
					key := fmt.Sprintf("%s:%d,%d", opt.SubDir, cx, cz)
					h := fnv1a(raw)
					if opt.State.Hash[key] == h && opt.State.Stamp[key] != 0 {
						ts = opt.State.Stamp[key] // unchanged: keep the old stamp
					} else {
						changed++
						opt.State.Hash[key] = h
						opt.State.Stamp[key] = ts
					}
				} else {
					changed++
				}

				rk := [2]int32{cx >> 5, cz >> 5}
				if regions[rk] == nil {
					regions[rk] = map[[2]int][]byte{}
					stamps[rk] = map[[2]int]uint32{}
				}
				lk := [2]int{int(cx & 31), int(cz & 31)}
				regions[rk][lk] = raw
				stamps[rk][lk] = ts
				if ts == opt.Timestamp {
					dirty[rk] = true
				}
				done++
				if opt.Progress != nil {
					opt.Progress(done, total)
				}
				mu.Unlock()
			}
		}()
	}
	for c := range set {
		jobs <- c
	}
	close(jobs)
	wg.Wait()

	for rk, chunks := range regions {
		path := filepath.Join(opt.Dir, opt.SubDir, "region",
			fmt.Sprintf("r.%d.%d.mca", rk[0], rk[1]))
		if opt.State != nil && !dirty[rk] && fileExists(path) {
			continue // nothing in this region changed and the file is there
		}
		if err := WriteRegion(path, chunks, stamps[rk]); err != nil {
			return changed, err
		}
	}
	return changed, nil
}
