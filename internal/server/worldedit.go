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

// blockLimit is the live max_block_modifications (any goroutine).
func (h *hub) blockLimit() int {
	if v := h.blockModLimit.Load(); v > 0 {
		return int(v)
	}
	return fillLimit
}

// evSetBlocks is one /setblock or /fill, run on the hub.
type evSetBlocks struct {
	eid        int32
	dim        int
	from, to   blockPos
	state      uint32
	mode       string            // replace (default), destroy, keep; /fill also hollow, outline
	filter     func(uint32) bool // /fill … replace <filter>: the BlockPredicate a cell must meet
	single     bool              // /setblock: its own feedback
	fillWanted int
	strict     bool           // strict: no neighbour or shape updates (UPDATE_SKIP_ALL_SIDEEFFECTS)
	nbt        map[string]any // the block entity data from {…} after the state
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
	usage := "Usage: /setblock <x> <y> <z> <block>[{<nbt>}] [destroy|keep|replace|strict]"
	if len(args) < 4 || len(args) > 5 {
		p.tell(usage)
		return
	}
	x, y, z, ok := parsePosition(args[:3], p.x, p.y, p.z, p.yaw, p.pitch)
	st, nbt, msg := parseBlockArg(args[3])
	if !ok || msg != "" {
		if msg == "" {
			msg = usage
		}
		p.tell(msg)
		return
	}
	mode := "replace"
	if len(args) == 5 {
		mode = args[4]
		if mode != "destroy" && mode != "keep" && mode != "replace" && mode != "strict" {
			p.tell(usage)
			return
		}
	}
	pos := blockPos{floorInt(x), floorInt(y), floorInt(z)}
	e := evSetBlocks{eid: p.eid, dim: p.dim, from: pos, to: pos, state: st, mode: mode, single: true, nbt: nbt}
	if mode == "strict" {
		e.mode, e.strict = "replace", true
	}
	s.hub.post(e)
}

// parseBlockArg is BlockStateArgument: a state and an optional {…} of block
// entity data.
func parseBlockArg(arg string) (uint32, map[string]any, string) {
	var nbt map[string]any
	if i := strings.IndexByte(arg, '{'); i >= 0 {
		v, err := parseSNBT(arg[i:])
		m, ok := v.(map[string]any)
		if err != nil || !ok {
			return 0, nil, "Invalid block entity data: " + arg[i:]
		}
		arg, nbt = arg[:i], m
	}
	st, ok := parseBlockState(arg)
	if !ok {
		return 0, nil, fmt.Sprintf("Unknown block type '%s'", nsID(arg))
	}
	return st, nbt, ""
}

func (s *Server) cmdFill(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	usage := "Usage: /fill <from> <to> <block>[{<nbt>}] [destroy|hollow|keep|outline|strict|replace [<filter>] [strict]]"
	if len(args) < 7 {
		p.tell(usage)
		return
	}
	x0, y0, z0, ok0 := parsePosition(args[0:3], p.x, p.y, p.z, p.yaw, p.pitch)
	x1, y1, z1, ok1 := parsePosition(args[3:6], p.x, p.y, p.z, p.yaw, p.pitch)
	st, nbt, msg := parseBlockArg(args[6])
	if !ok0 || !ok1 || msg != "" {
		if msg == "" {
			msg = usage
		}
		p.tell(msg)
		return
	}
	e := evSetBlocks{eid: p.eid, dim: p.dim, state: st, mode: "replace", nbt: nbt,
		from: blockPos{min(floorInt(x0), floorInt(x1)), min(floorInt(y0), floorInt(y1)), min(floorInt(z0), floorInt(z1))},
		to:   blockPos{max(floorInt(x0), floorInt(x1)), max(floorInt(y0), floorInt(y1)), max(floorInt(z0), floorInt(z1))}}
	if len(args) >= 8 {
		switch e.mode = args[7]; e.mode {
		case "destroy", "hollow", "keep", "outline":
		case "strict":
			e.mode, e.strict = "replace", true
		case "replace":
			rest := args[8:]
			if len(rest) > 0 && rest[len(rest)-1] == "strict" {
				e.strict, rest = true, rest[:len(rest)-1]
			}
			if len(rest) == 1 {
				arg := rest[0]
				if i := strings.IndexByte(arg, '{'); i >= 0 {
					arg = arg[:i] // block entity data in a predicate is not modelled; the block still has to match
				}
				f, ok := parseBlockPredicate(arg)
				if !ok {
					p.tell(blockPredicateError(rest[0]))
					return
				}
				e.filter = f
			} else if len(rest) > 1 {
				p.tell(usage)
				return
			}
		default:
			p.tell(usage)
			return
		}
	}
	n := (e.to.x - e.from.x + 1) * (e.to.y - e.from.y + 1) * (e.to.z - e.from.z + 1)
	if limit := s.hub.blockLimit(); n > limit {
		p.tell(fmt.Sprintf("Too many blocks in the specified area (maximum %d, specified %d)", limit, n))
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
				if e.filter != nil && !e.filter(old) {
					continue
				}
				if old == want && e.nbt == nil {
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
				if e.strict {
					// UPDATE_SKIP_ALL_SIDEEFFECTS: the block goes in with no
					// neighbour or shape updates and nothing scheduled.
					w.SetBlock(x, y, z, want)
					h.broadcastBlockIn(players, e.dim, x, y, z, want)
				} else {
					h.setBlockAt(players, e.dim, pos, want)
				}
				if old == spawnerBlock || want == spawnerBlock {
					h.dropSpawnerBE(simPos{dim: e.dim, blockPos: pos}) // a fresh block entity: an empty cage
				}
				if e.nbt != nil {
					h.loadBlockEntityNBT(simPos{dim: e.dim, blockPos: pos}, want, e.nbt)
				}
				changed++
			}
		}
	}
	switch {
	case e.single && changed == 0:
		tell("Could not set the block")
	case e.single:
		h.cmdOK(players, e.eid)(fmt.Sprintf("Changed the block at %d, %d, %d", e.from.x, e.from.y, e.from.z))
	case changed == 0:
		tell("No blocks were filled")
	default:
		h.cmdOK(players, e.eid)(fmt.Sprintf("Successfully filled %d block(s)", changed))
	}
}

// loadBlockEntityNBT applies the block entity data a /setblock or /fill gave:
// CustomName on a nameable block, and Items on a chest-like container
// (each {Slot, id, count}).
func (h *hub) loadBlockEntityNBT(pos simPos, state uint32, nbt map[string]any) {
	if name := nbtName(nbt); name != "" && nameableBlock(state) {
		h.blockNames.set(pos, name)
	}
	if state == spawnerBlock {
		h.loadSpawnerNBT(pos, nbt)
		return
	}
	items, ok := nbt["Items"].([]any)
	if !ok || !isChestLikeContainer(state) {
		return
	}
	c := &chest{}
	for _, raw := range items {
		it, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		slot, ok := snbtInt(it["Slot"])
		id, _ := it["id"].(string)
		item, known := itemByName[strings.TrimPrefix(id, "minecraft:")]
		if !ok || !known || slot < 0 || int(slot) >= len(c.slots) {
			continue
		}
		count := int64(1)
		if n, ok := snbtInt(it["count"]); ok && n > 0 {
			count = n
		}
		c.slots[slot] = invStack{item: item, count: int(count)}
	}
	h.chests[pos] = c
}

// isChestLikeContainer is a 27-slot container kept in h.chests: a chest of
// any kind, a barrel or a shulker box.
func isChestLikeContainer(s uint32) bool { return isChestBlock(s) || isBarrel(s) || isShulkerBox(s) }
