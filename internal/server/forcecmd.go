package server

import (
	"fmt"
	"strings"

	"github.com/tachyne/tachyne-world/internal/world"
)

// /forceload add <from> [<to>] | remove <from> [<to>] | remove all |
// query [<pos>] (ForceLoadCommand), in the caller's dimension. A forced chunk
// is pinned in the world's chunk cache, so it stays loaded and keeps random-
// and block-ticking with nobody near (runRandomTicks walks it). The set is
// saved in settings.json and re-pinned at boot, as vanilla keeps it in the
// dimension's saved data. Mobs are not held there: they still unload with
// the players' view, as the engine keeps them.

// forceLoadMax is ForceLoadCommand's MAX_CHUNK_LIMIT.
const forceLoadMax = 256

// forcedChunk is one saved forced chunk.
type forcedChunk struct {
	Dim int   `json:"d,omitempty"`
	X   int32 `json:"x"`
	Z   int32 `json:"z"`
}

type evForceLoadCmd struct {
	by     int32
	dim    int
	op     string // add, remove, remove all, query, list
	x0, z0 int    // block columns
	x1, z1 int
}

func (evForceLoadCmd) isHubEvent() {}

const forceLoadUsage = "Usage: /forceload add <from> [<to>] | remove <from> [<to>] | remove all | query [<pos>]"

func (s *Server) cmdForceLoad(p *player, args []string) {
	if !s.isOp(p.name) { // ForceLoadCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) == 0 {
		p.tell(forceLoadUsage)
		return
	}
	e := evForceLoadCmd{by: p.eid, dim: p.dim, op: args[0]}
	// A column position: two block coordinates, each absolute or ~relative.
	column := func(a []string) (int, int, bool) {
		if len(a) < 2 {
			return 0, 0, false
		}
		x, ok1 := parseCoord(a[0], p.x)
		z, ok2 := parseCoord(a[1], p.z)
		return floorInt(x), floorInt(z), ok1 && ok2
	}
	ok := false
	switch {
	case args[0] == "remove" && len(args) == 2 && args[1] == "all":
		e.op, ok = "remove all", true
	case (args[0] == "add" || args[0] == "remove") && (len(args) == 3 || len(args) == 5):
		var ok1, ok2 bool
		e.x0, e.z0, ok1 = column(args[1:3])
		e.x1, e.z1, ok2 = e.x0, e.z0, true
		if len(args) == 5 {
			e.x1, e.z1, ok2 = column(args[3:5])
		}
		ok = ok1 && ok2
	case args[0] == "query" && len(args) == 1:
		e.op, ok = "list", true
	case args[0] == "query" && len(args) == 3:
		e.x0, e.z0, ok = column(args[1:3])
	}
	if !ok {
		p.tell(forceLoadUsage)
		return
	}
	s.hub.post(e)
}

// chunkPosString is ChunkPos.toString.
func chunkPosString(cx, cz int) string { return fmt.Sprintf("[%d, %d]", cx, cz) }

// applyForceLoadCommand runs /forceload on the hub.
func (h *hub) applyForceLoadCommand(players map[int32]*tracked, e evForceLoadCmd) {
	tell := cmdTeller(players, e.by)
	w := h.worldFor(e.dim)
	dimName := dimRegistryName(e.dim)
	switch e.op {
	case "list":
		cs := w.ForcedChunks()
		names := make([]string, len(cs))
		for i, c := range cs {
			names[i] = chunkPosString(int(c[0]), int(c[1]))
		}
		switch len(cs) {
		case 0:
			tell("No force loaded chunks were found in " + dimName)
		case 1:
			tell(fmt.Sprintf("A force loaded chunk was found in %s at: %s", dimName, names[0]))
		default:
			tell(fmt.Sprintf("%d force loaded chunks were found in %s at: %s", len(cs), dimName, strings.Join(names, ", ")))
		}
		return
	case "query":
		cx, cz := e.x0>>4, e.z0>>4
		if w.Forced(int32(cx), int32(cz)) {
			tell(fmt.Sprintf("Chunk at %s in %s is marked for force loading", chunkPosString(cx, cz), dimName))
		} else {
			tell(fmt.Sprintf("Chunk at %s in %s is not marked for force loading", chunkPosString(cx, cz), dimName))
		}
		return
	case "remove all":
		for _, c := range w.ForcedChunks() {
			w.SetForced(c[0], c[1], false)
		}
		h.saveForced()
		tell("Unmarked all force loaded chunks in " + dimName)
		return
	}
	add := e.op == "add"
	minX, maxX := min(e.x0, e.x1), max(e.x0, e.x1)
	minZ, maxZ := min(e.z0, e.z1), max(e.z0, e.z1)
	if minX < -30000000 || minZ < -30000000 || maxX >= 30000000 || maxZ >= 30000000 {
		tell("That position is out of this world!")
		return
	}
	cx0, cz0, cx1, cz1 := minX>>4, minZ>>4, maxX>>4, maxZ>>4
	if n := int64(cx1-cx0+1) * int64(cz1-cz0+1); n > forceLoadMax {
		tell(fmt.Sprintf("Too many chunks in the specified area (maximum %d, but specified %d)", forceLoadMax, n))
		return
	}
	var tally cmdTally
	var fresh [][2]int32
	for x := cx0; x <= cx1; x++ {
		for z := cz0; z <= cz1; z++ {
			changed := w.SetForced(int32(x), int32(z), add)
			if changed && add {
				fresh = append(fresh, [2]int32{int32(x), int32(z)})
			}
			tally.track(chunkPosString(x, z), b2i(changed))
		}
	}
	if tally.nonZero > 0 {
		h.saveForced()
	}
	warmForced(w, fresh)
	from, to := chunkPosString(cx0, cz0), chunkPosString(cx1, cz1)
	switch who := tally.single(true); {
	case tally.nonZero == 0 && add:
		tell("No chunks were marked for force loading")
	case tally.nonZero == 0:
		tell("No chunks were removed from force loading")
	case who != "" && add:
		tell(fmt.Sprintf("Marked chunk %s in %s to be force loaded", who, dimName))
	case who != "":
		tell(fmt.Sprintf("Unmarked chunk %s in %s for force loading", who, dimName))
	case add:
		tell(fmt.Sprintf("Marked %d chunks in %s from %s to %s to be force loaded", tally.nonZero, dimName, from, to))
	default:
		tell(fmt.Sprintf("Unmarked %d chunks in %s from %s to %s for force loading", tally.nonZero, dimName, from, to))
	}
}

// saveForced writes every dimension's forced chunks into the settings.
func (h *hub) saveForced() {
	var out []forcedChunk
	for dim := dimOverworld; dim <= dimEnd; dim++ {
		w := h.worldFor(dim)
		if dim != dimOverworld && w == h.world {
			continue
		}
		for _, c := range w.ForcedChunks() {
			out = append(out, forcedChunk{Dim: dim, X: c[0], Z: c[1]})
		}
	}
	h.rules.Forced = out
	h.saveRules()
}

// restoreForced re-pins the saved forced chunks (boot, after loadRules).
func (h *hub) restoreForced() {
	for _, c := range h.rules.Forced {
		w := h.worldFor(c.Dim)
		if c.Dim != dimOverworld && w == h.world {
			continue // that dimension is not running
		}
		w.SetForced(c.X, c.Z, true)
		warmForced(w, [][2]int32{{c.X, c.Z}})
	}
}

// warmForced generates newly forced chunks off the hub goroutine: vanilla
// loads them at once, but a 256-chunk area generated inline would stall the
// tick. Each one is Loaded — and ticks — as soon as it is ready.
func warmForced(w *world.World, cs [][2]int32) {
	if len(cs) == 0 {
		return
	}
	go func() {
		for _, c := range cs {
			w.ForceLoad(int(c[0])*16, int(c[1])*16, 0)
		}
	}()
}
