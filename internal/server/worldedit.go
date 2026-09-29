package server

import (
	"fmt"
	"math"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /setblock and /fill (SetBlockCommand, FillCommand). Both are op commands:
// the operator names a block state ("stone", "oak_stairs[facing=north]") and
// a position or a box, and the hub writes it in with the usual neighbour
// updates. A fill is capped at the max_block_modifications gamerule
// (fillLimit is its default).

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
	filterNBT  map[string]any    // …and the block entity data it must carry
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
	usage := "Usage: /fill <from> <to> <block>[{<nbt>}] [keep|replace [<filter> [<mode>]]|<mode>] — a mode is outline, hollow, destroy or strict"
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
	// FillCommand: <block> [replace [<filter> [<mode>]] | keep | <mode>],
	// where a mode is outline, hollow, destroy or strict.
	rest := args[7:]
	mode := func(m string) bool {
		switch m {
		case "outline", "hollow", "destroy":
			e.mode = m
		case "strict":
			e.strict = true
		default:
			return false
		}
		return true
	}
	switch {
	case len(rest) == 0:
	case rest[0] == "keep" && len(rest) == 1:
		e.mode = "keep"
	case rest[0] == "replace" && len(rest) <= 3:
		if len(rest) >= 2 {
			pred, msg := parseBlockPredicateNBT(rest[1])
			if msg != "" {
				p.tell(msg)
				return
			}
			e.filter, e.filterNBT = pred.state, pred.nbt
		}
		if len(rest) == 3 && !mode(rest[2]) {
			p.tell(usage)
			return
		}
	case len(rest) == 1 && mode(rest[0]):
	default:
		p.tell(usage)
		return
	}
	// The volume saturates rather than wrap: corners 30 million blocks apart
	// overflow even an int64.
	vol := float64(e.to.x-e.from.x+1) * float64(e.to.y-e.from.y+1) * float64(e.to.z-e.from.z+1)
	e.fillWanted = int(min(vol, math.MaxInt32))
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
	// The cap is read on the hub, where a /gamerule just before it has
	// already landed.
	if limit := h.blockLimit(); !e.single && e.fillWanted > limit {
		tell(fmt.Sprintf("Too many blocks in the specified area (maximum %d, but specified %d)", limit, e.fillWanted))
		return
	}
	changed, nbtErr := 0, ""
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
				if e.mode == "keep" && !isAirState(old) { // level.isEmptyBlock
					continue
				}
				if e.filter != nil && !e.filter(old) {
					continue
				}
				if e.filterNBT != nil && !nbtMatches(e.filterNBT, h.blockEntityNBT(simPos{dim: e.dim, blockPos: blockPos{x, y, z}}, old), true) {
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
					if msg := h.loadBlockEntityNBT(players, simPos{dim: e.dim, blockPos: pos}, want, e.nbt); msg != "" && nbtErr == "" {
						nbtErr = msg
					}
				}
				changed++
			}
		}
	}
	if nbtErr != "" {
		tell(nbtErr)
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

// loadBlockEntityNBT is BlockInput.place's loadWithComponents: the block
// entity data a /setblock or /fill gave goes into the fresh block entity —
// CustomName on a nameable block, a spawner's SpawnData and Delay, a
// container's Items, a sign's two sides, a banner's patterns.
func (h *hub) loadBlockEntityNBT(players map[int32]*tracked, pos simPos, state uint32, nbt map[string]any) string {
	if name := nbtName(nbt); name != "" && nameableBlock(state) {
		h.blockNames.set(pos, name)
	}
	if state == spawnerBlock {
		h.loadSpawnerNBT(pos, nbt)
		return ""
	}
	c, msg := h.carriedFromNBT(pos, state, nbt)
	if msg != "" {
		return msg
	}
	h.placeBlockEntity(players, pos, c, state)
	return ""
}

// isChestLikeContainer is a 27-slot container kept in h.chests: a chest of
// any kind, a barrel or a shulker box.
func isChestLikeContainer(s uint32) bool { return isChestBlock(s) || isBarrel(s) || isShulkerBox(s) }
