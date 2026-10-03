package server

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// /fillbiome <from> <to> <biome> [replace <filter>] (FillBiomeCommand): the
// quarts (4×4×4 cells) whose corner falls in the box, both corners snapped
// down to a quart, take the biome — those the filter (a biome or a #tag)
// passes. The count is of quarts whose biome actually changed; the box is
// capped by max_block_modifications as /fill is. The overrides live in each
// world (world/biomes.go), persist in biomes.json, and reach the viewers of
// the changed chunks as a chunks_biomes frame, as ChunkMap.resendBiomesForChunks
// sends them.

type fillBiomeReq struct {
	by       int32
	from, to blockPos
	biome    string
	filter   func(string) bool // nil: every biome
}

const fillBiomeUsage = "Usage: /fillbiome <from> <to> <biome> [replace <filter>]"

// knownBiome reports whether the biome registry has an entry.
func knownBiome(id string) bool { return slices.Contains(biomeIDs(), id) }

func parseFillBiome(p *player, args []string) (fillBiomeReq, string) {
	if len(args) != 7 && !(len(args) == 9 && args[7] == "replace") {
		return fillBiomeReq{}, fillBiomeUsage
	}
	x0, y0, z0, ok0 := parsePosition(args[0:3], p.x, p.y, p.z, p.yaw, p.pitch)
	x1, y1, z1, ok1 := parsePosition(args[3:6], p.x, p.y, p.z, p.yaw, p.pitch)
	if !ok0 || !ok1 {
		return fillBiomeReq{}, fillBiomeUsage
	}
	r := fillBiomeReq{by: p.eid,
		from: blockPos{floorInt(x0), floorInt(y0), floorInt(z0)},
		to:   blockPos{floorInt(x1), floorInt(y1), floorInt(z1)}}
	biome, ok := parseResourceID(args[6])
	if !ok || !knownBiome(biome) {
		return fillBiomeReq{}, fmt.Sprintf("Can't find element '%s' of type 'minecraft:worldgen/biome'", nsID(args[6]))
	}
	r.biome = biome
	if len(args) == 9 {
		f := args[8]
		if tag, isTag := strings.CutPrefix(f, "#"); isTag {
			id, ok := parseResourceID(tag)
			members, found := biomeTagMembers(id) // vanilla's or a data pack's
			if !ok || !found {
				return fillBiomeReq{}, fmt.Sprintf("Can't find tag '%s' of type 'minecraft:worldgen/biome'", nsID(tag))
			}
			r.filter = func(b string) bool { return slices.Contains(members, b) }
		} else {
			id, ok := parseResourceID(f)
			if !ok || !knownBiome(id) {
				return fillBiomeReq{}, fmt.Sprintf("Can't find element '%s' of type 'minecraft:worldgen/biome'", nsID(f))
			}
			r.filter = func(b string) bool { return b == id }
		}
	}
	return r, ""
}

func (s *Server) cmdFillBiome(p *player, args []string) {
	if !s.isOp(p.name) { // FillBiomeCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	r, msg := parseFillBiome(p, args)
	if msg != "" {
		p.tell(msg)
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		if msg := s.hub.runFillBiome(players, r); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// quartFloor is QuartPos.toBlock(QuartPos.fromBlock(v)).
func quartFloor(v int) int { return v >> 2 << 2 }

// runFillBiome executes a parsed /fillbiome on the hub; a non-empty string
// is the failure line.
func (h *hub) runFillBiome(players map[int32]*tracked, r fillBiomeReq) string {
	t := players[r.by]
	if t == nil {
		return ""
	}
	dim := t.dim
	w := h.worldFor(dim)
	if w == nil {
		return "That position is not loaded"
	}
	// BlockPosArgument.getLoadedBlockPos for each corner.
	for _, c := range []blockPos{r.from, r.to} {
		if !h.cloneLoaded(dim, c, c) {
			return "That position is not loaded"
		}
		if !h.inWorldYIn(dim, c.y) {
			return "That position is out of this world!"
		}
	}
	lo := blockPos{min(quartFloor(r.from.x), quartFloor(r.to.x)), min(quartFloor(r.from.y), quartFloor(r.to.y)),
		min(quartFloor(r.from.z), quartFloor(r.to.z))}
	hi := blockPos{max(quartFloor(r.from.x), quartFloor(r.to.x)), max(quartFloor(r.from.y), quartFloor(r.to.y)),
		max(quartFloor(r.from.z), quartFloor(r.to.z))}
	volume := int64(hi.x-lo.x+1) * int64(hi.y-lo.y+1) * int64(hi.z-lo.z+1)
	if limit := int64(h.blockLimit()); volume > limit {
		return fmt.Sprintf("Too many blocks in the specified volume (maximum %d, but specified %d)", limit, volume)
	}
	if !h.cloneLoaded(dim, lo, hi) {
		return "That position is not loaded"
	}
	count := 0
	changed := map[[2]int32]bool{}
	for x := lo.x; x <= hi.x; x += 4 {
		for z := lo.z; z <= hi.z; z += 4 {
			for y := lo.y; y <= hi.y; y += 4 {
				if !h.inWorldYIn(dim, y) {
					continue
				}
				if r.filter != nil && !r.filter(w.BiomeAt3D(x, y, z)) {
					continue
				}
				if w.SetBiome(x, y, z, r.biome) {
					count++
					changed[[2]int32{int32(x >> 4), int32(z >> 4)}] = true
				}
			}
		}
	}
	if count == 0 {
		return "No biome entries were changed"
	}
	h.saveBiomeOverrides()
	h.sendChunkBiomes(players, dim, changed)
	h.cmdOK(players, r.by)(fmt.Sprintf("%d biome entry/entries set between %d, %d, %d and %d, %d, %d",
		count, lo.x, lo.y, lo.z, hi.x, hi.y, hi.z))
	return ""
}

// sendChunkBiomes is ChunkMap.resendBiomesForChunks: each player in the
// dimension is sent the changed chunks in their view, in one frame.
func (h *hub) sendChunkBiomes(players map[int32]*tracked, dim int, changed map[[2]int32]bool) {
	w := h.worldFor(dim)
	keys := make([][2]int32, 0, len(changed))
	for k := range changed {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b [2]int32) int {
		if a[0] != b[0] {
			return int(a[0] - b[0])
		}
		return int(a[1] - b[1])
	})
	biomes := make(map[[2]int32][]string, len(keys))
	for _, k := range keys {
		biomes[k] = w.SectionBiomes(k[0], k[1])
	}
	for _, t := range players {
		if t.dim != dim {
			continue
		}
		var ev attachproto.ChunksBiomes
		for _, k := range keys {
			if abs(chunkFloor(t.x)-int(k[0])) <= viewRadius && abs(chunkFloor(t.z)-int(k[1])) <= viewRadius {
				ev.Chunks = append(ev.Chunks, attachproto.ChunkBiomes{CX: k[0], CZ: k[1], Biomes: biomes[k]})
			}
		}
		if len(ev.Chunks) > 0 {
			t.p.sendEvReliable(ev)
		}
	}
}

// biomesPathFor is where the biome overrides persist, beside the other
// stores.
func biomesPathFor(spawnPath string) string {
	if spawnPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(spawnPath), "biomes.json")
}

// loadBiomeOverrides installs the saved overrides into each world (boot,
// before any chunk is served) and remembers where they are saved.
func (h *hub) loadBiomeOverrides(path string) {
	h.biomePath = path
	if path == "" {
		return
	}
	var saved map[string]map[string]string
	if err := loadStore(path, &saved); err != nil {
		log.Fatal(err)
	}
	for ds, cells := range saved {
		id, err := strconv.Atoi(ds)
		w := h.worldFor(id)
		if err != nil || w == nil {
			log.Printf("biomes: no dimension %q here; its %d overrides are dropped", ds, len(cells))
			continue
		}
		m := make(map[world.Quart]string, len(cells))
		for k, b := range cells {
			var q world.Quart
			if _, err := fmt.Sscanf(k, "%d,%d,%d", &q[0], &q[1], &q[2]); err == nil {
				m[q] = b
			}
		}
		w.LoadBiomeOverrides(m)
	}
}

// saveBiomeOverrides writes every world's overrides.
func (h *hub) saveBiomeOverrides() {
	if h.biomePath == "" {
		return
	}
	out := map[string]map[string]string{}
	for id, w := range h.allDims() {
		m := w.BiomeOverrides()
		if len(m) == 0 {
			continue
		}
		cells := make(map[string]string, len(m))
		for q, b := range m {
			cells[fmt.Sprintf("%d,%d,%d", q[0], q[1], q[2])] = b
		}
		out[strconv.Itoa(id)] = cells
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	writeStore(h.biomePath, data)
}
