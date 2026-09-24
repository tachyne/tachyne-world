package server

import (
	"fmt"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /setblock and /fill (SetBlockCommand, FillCommand). Both are op commands:
// the operator names a block state ("stone", "oak_stairs[facing=north]") and
// a position or a box, and the hub writes it in with the usual neighbour
// updates. A fill is capped at vanilla's command_modification_block_limit
// (32768 blocks).

const fillLimit = 32768

// evSetBlocks is one /setblock or /fill, run on the hub.
type evSetBlocks struct {
	eid        int32
	dim        int
	from, to   blockPos
	state      uint32
	mode       string // replace (default), destroy, keep; /fill also hollow, outline
	filter     uint32 // /fill … replace <filter>: only cells holding this block
	hasFilter  bool
	single     bool // /setblock: its own feedback
	fillWanted int
}

func (evSetBlocks) isHubEvent() {}

// parseBlockState reads a block argument: a name, with or without the
// minecraft: namespace, and optionally [prop=value,…].
func parseBlockState(arg string) (uint32, bool) {
	name, props := strings.TrimPrefix(arg, "minecraft:"), ""
	if i := strings.IndexByte(name, '['); i >= 0 {
		if !strings.HasSuffix(name, "]") {
			return 0, false
		}
		name, props = name[:i], name[i+1:len(name)-1]
	}
	if _, _, ok := worldgen.BlockRangeOK(name); !ok {
		return 0, false
	}
	st := worldgen.BlockID(name)
	if props == "" {
		return st, true
	}
	info, ok := worldgen.InfoForState(st)
	if !ok {
		return 0, false
	}
	for _, kv := range strings.Split(props, ",") {
		k, v, found := strings.Cut(strings.TrimSpace(kv), "=")
		if !found || !info.HasProperty(k) {
			return 0, false
		}
		ns := worldgen.SetProperty(info, st, k, v)
		if worldgen.GetProperty(info, ns, k) != v {
			return 0, false // not a value this property takes
		}
		st = ns
	}
	return st, true
}

func (s *Server) cmdSetblock(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 4 || len(args) > 5 {
		p.tell("Usage: /setblock <x> <y> <z> <block> [destroy|keep|replace]")
		return
	}
	x, y, z, ok := parsePosition(args[:3], p.x, p.y, p.z, p.yaw, p.pitch)
	st, sok := parseBlockState(args[3])
	if !ok || !sok {
		p.tell("Usage: /setblock <x> <y> <z> <block> [destroy|keep|replace]")
		return
	}
	mode := "replace"
	if len(args) == 5 {
		mode = args[4]
		if mode != "destroy" && mode != "keep" && mode != "replace" {
			p.tell("Usage: /setblock <x> <y> <z> <block> [destroy|keep|replace]")
			return
		}
	}
	pos := blockPos{floorInt(x), floorInt(y), floorInt(z)}
	s.hub.post(evSetBlocks{eid: p.eid, dim: p.dim, from: pos, to: pos, state: st, mode: mode, single: true})
}

func (s *Server) cmdFill(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	usage := "Usage: /fill <from> <to> <block> [destroy|hollow|keep|outline|replace [<filter>]]"
	if len(args) < 7 {
		p.tell(usage)
		return
	}
	x0, y0, z0, ok0 := parsePosition(args[0:3], p.x, p.y, p.z, p.yaw, p.pitch)
	x1, y1, z1, ok1 := parsePosition(args[3:6], p.x, p.y, p.z, p.yaw, p.pitch)
	st, sok := parseBlockState(args[6])
	if !ok0 || !ok1 || !sok {
		p.tell(usage)
		return
	}
	e := evSetBlocks{eid: p.eid, dim: p.dim, state: st, mode: "replace",
		from: blockPos{min(floorInt(x0), floorInt(x1)), min(floorInt(y0), floorInt(y1)), min(floorInt(z0), floorInt(z1))},
		to:   blockPos{max(floorInt(x0), floorInt(x1)), max(floorInt(y0), floorInt(y1)), max(floorInt(z0), floorInt(z1))}}
	if len(args) >= 8 {
		switch e.mode = args[7]; e.mode {
		case "destroy", "hollow", "keep", "outline":
		case "replace":
			if len(args) >= 9 {
				f, ok := parseBlockState(args[8])
				if !ok {
					p.tell(usage)
					return
				}
				e.filter, e.hasFilter = f, true
			}
		default:
			p.tell(usage)
			return
		}
	}
	n := (e.to.x - e.from.x + 1) * (e.to.y - e.from.y + 1) * (e.to.z - e.from.z + 1)
	if n > fillLimit {
		p.tell(fmt.Sprintf("Too many blocks in the specified area (maximum %d, specified %d)", fillLimit, n))
		return
	}
	e.fillWanted = n
	s.hub.post(e)
}

// applySetBlocks writes a /setblock or /fill on the hub.
func (h *hub) applySetBlocks(players map[int32]*tracked, e evSetBlocks) {
	w := h.worldFor(e.dim)
	t := players[e.eid]
	tell := func(msg string) {
		if t != nil {
			t.p.trySendEv(chatEv(msg))
		}
	}
	if w == nil {
		return
	}
	changed := 0
	for x := e.from.x; x <= e.to.x; x++ {
		for y := e.from.y; y <= e.to.y; y++ {
			for z := e.from.z; z <= e.to.z; z++ {
				if !h.inWorldYIn(e.dim, y) {
					continue
				}
				edge := x == e.from.x || x == e.to.x || y == e.from.y || y == e.to.y || z == e.from.z || z == e.to.z
				want := e.state
				switch e.mode {
				case "hollow":
					if !edge {
						want = worldgen.Air
					}
				case "outline":
					if !edge {
						continue
					}
				}
				old := w.At(x, y, z)
				if e.mode == "keep" && old != worldgen.Air {
					continue
				}
				if e.hasFilter && old != e.filter && !sameBlockFamily(old, e.filter) { // any state of the filter's block
					continue
				}
				if old == want {
					continue
				}
				pos := blockPos{x, y, z}
				if e.mode == "destroy" && old != worldgen.Air { // level.destroyBlock(pos, true)
					h.toNearbyEv(players, e.dim, float64(x), float64(z), blockBreakEvent(x, y, z, old))
					if ds := h.evalBlockLoot(lootCtx{state: old, rng: h.rng.Intn, randf: h.rng.Float64}); ds != nil {
						for _, d := range ds {
							h.spawnBlockDrop(players, e.dim, d.item, d.count, x, y, z)
						}
					}
				}
				h.setBlockAt(players, e.dim, pos, want)
				changed++
			}
		}
	}
	switch {
	case e.single && changed == 0:
		tell("Could not set the block")
	case e.single:
		tell(fmt.Sprintf("Changed the block at %d, %d, %d", e.from.x, e.from.y, e.from.z))
	case changed == 0:
		tell("No blocks were filled")
	default:
		tell(fmt.Sprintf("Successfully filled %d block(s)", changed))
	}
}
