package server

import (
	"fmt"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /clone (CloneCommands): copy a box of blocks to another place, block
// entities and all.
//
//	/clone [from <dim>] <begin> <end> [to <dim>] <destination> [strict]
//	       [replace|masked|filtered <block>] [force|move|normal]
//
// The source box is read whole before anything is written, so an
// overlapping force/move behaves as vanilla's does. normal refuses an
// overlap; move clears the source (without drops) before the copy lands.
// masked skips air; filtered copies only the named block (a tag with #, and
// [properties] to pin). The box is capped at command_modification_block_limit
// (32768), both boxes must be loaded, and the three named corners must lie
// inside the world. Placement goes solid blocks first, then block entities,
// then everything that hangs off something, so a torch finds its wall;
// strict skips the neighbour pass that follows (nothing pops off).

type cloneMode int

const (
	cloneNormal cloneMode = iota
	cloneForce
	cloneMove
)

// cloneReq is one parsed /clone.
type cloneReq struct {
	srcDim, dstDim   int
	begin, end, dest blockPos
	strict           bool
	mask             func(uint32) bool // nil = every block
	mode             cloneMode
}

const cloneUsage = "Usage: /clone [from <dimension>] <begin> <end> [to <dimension>] <destination> [strict] [replace|masked|filtered <block>] [force|move|normal]"

// parseDimension is DimensionArgument over the three dimensions the engine
// runs.
func parseDimension(arg string) (int, bool) {
	switch strings.TrimPrefix(arg, "minecraft:") {
	case "overworld":
		return 0, true
	case "the_nether":
		return 1, true
	case "the_end":
		return 2, true
	}
	return 0, false
}

// parseBlockPredicate is BlockPredicateArgument: a block name or #tag, with
// optional [prop=value,…] that every matched state must carry. A bare name
// matches every state of the block.
func parseBlockPredicate(arg string) (func(uint32) bool, bool) {
	name, props := strings.TrimPrefix(arg, "#"), ""
	tag := strings.HasPrefix(arg, "#")
	if i := strings.IndexByte(name, '['); i >= 0 {
		if !strings.HasSuffix(name, "]") {
			return nil, false
		}
		name, props = name[:i], name[i+1:len(name)-1]
	}
	name = strings.TrimPrefix(name, "minecraft:")
	var members map[string]bool
	if tag {
		names, ok := blockTagMembers(name)
		if !ok {
			return nil, false
		}
		members = map[string]bool{}
		for _, n := range names {
			members[n] = true
		}
	} else {
		if _, _, ok := worldgen.BlockRangeOK(name); !ok {
			return nil, false
		}
		members = map[string]bool{name: true}
	}
	want := map[string]string{}
	if props != "" {
		for _, kv := range strings.Split(props, ",") {
			k, v, found := strings.Cut(strings.TrimSpace(kv), "=")
			if !found {
				return nil, false
			}
			want[k] = v
		}
	}
	if !tag && len(want) > 0 { // a named block's properties must be its own
		info, ok := worldgen.InfoForState(worldgen.BlockID(name))
		if !ok {
			return nil, false
		}
		for k := range want {
			if !info.HasProperty(k) {
				return nil, false
			}
		}
	}
	return func(st uint32) bool {
		n, ok := worldgen.StateName(st)
		if !ok || !members[n] {
			return false
		}
		if len(want) == 0 {
			return true
		}
		info, ok := worldgen.InfoForState(st)
		if !ok {
			return false
		}
		for k, v := range want {
			if !info.HasProperty(k) || worldgen.GetProperty(info, st, k) != v {
				return false
			}
		}
		return true
	}, true
}

// blockTagMembers is a generated block tag's member names; a tag the engine
// was not generated with is an unknown tag, not a crash.
func blockTagMembers(name string) (names []string, ok bool) {
	defer func() {
		if recover() != nil {
			names, ok = nil, false
		}
	}()
	return worldgen.BlockTagNames(name), true
}

func (s *Server) cmdClone(p *player, args []string) {
	if !s.isOp(p.name) { // CloneCommands: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	req, msg := parseClone(args, p)
	if msg != "" {
		p.tell(msg)
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		if msg := s.hub.runClone(players, p, req); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// parseClone reads the /clone grammar; a non-empty string is the error.
func parseClone(args []string, p *player) (cloneReq, string) {
	req := cloneReq{srcDim: p.dim, dstDim: p.dim}
	pos := func(a []string) (blockPos, bool) {
		if len(a) < 3 {
			return blockPos{}, false
		}
		x, y, z, ok := parsePosition(a[:3], p.x, p.y, p.z, p.yaw, p.pitch)
		return blockPos{floorInt(x), floorInt(y), floorInt(z)}, ok
	}
	if len(args) > 0 && args[0] == "from" {
		if len(args) < 2 {
			return req, cloneUsage
		}
		d, ok := parseDimension(args[1])
		if !ok {
			return req, fmt.Sprintf("Unknown dimension '%s'", args[1])
		}
		req.srcDim = d
		args = args[2:]
	}
	var ok bool
	if req.begin, ok = pos(args); !ok {
		return req, cloneUsage
	}
	if req.end, ok = pos(args[3:]); !ok {
		return req, cloneUsage
	}
	args = args[6:]
	if len(args) > 0 && args[0] == "to" {
		if len(args) < 2 {
			return req, cloneUsage
		}
		d, ok := parseDimension(args[1])
		if !ok {
			return req, fmt.Sprintf("Unknown dimension '%s'", args[1])
		}
		req.dstDim = d
		args = args[2:]
	}
	if req.dest, ok = pos(args); !ok {
		return req, cloneUsage
	}
	args = args[3:]
	if len(args) > 0 && args[0] == "strict" {
		req.strict = true
		args = args[1:]
	}
	if len(args) > 0 {
		switch args[0] {
		case "replace":
			args = args[1:]
		case "masked":
			req.mask = func(st uint32) bool { return !psAir(st) } // BlockState.isAir: air, cave air, void air
			args = args[1:]
		case "filtered":
			if len(args) < 2 {
				return req, cloneUsage
			}
			f, ok := parseBlockPredicate(args[1])
			if !ok {
				return req, "Unknown block type '" + args[1] + "'"
			}
			req.mask = f
			args = args[2:]
		}
	}
	if len(args) > 0 {
		switch args[0] {
		case "normal":
		case "force":
			req.mode = cloneForce
		case "move":
			req.mode = cloneMove
		default:
			return req, cloneUsage
		}
		args = args[1:]
	}
	if len(args) > 0 {
		return req, cloneUsage
	}
	return req, ""
}

// cloneCell is one source cell that passed the mask.
type cloneCell struct {
	src, dst blockPos
	state    uint32
	be       carriedBE
}

// runClone performs a parsed /clone on the hub. A non-empty string is the
// failure line.
func (h *hub) runClone(players map[int32]*tracked, caller *player, r cloneReq) string {
	if (r.srcDim == 1 && h.nether == nil) || (r.srcDim == 2 && h.end == nil) ||
		(r.dstDim == 1 && h.nether == nil) || (r.dstDim == 2 && h.end == nil) {
		return "That position is not loaded"
	}
	from0 := blockPos{min(r.begin.x, r.end.x), min(r.begin.y, r.end.y), min(r.begin.z, r.end.z)}
	from1 := blockPos{max(r.begin.x, r.end.x), max(r.begin.y, r.end.y), max(r.begin.z, r.end.z)}
	span := blockPos{from1.x - from0.x, from1.y - from0.y, from1.z - from0.z}
	dest1 := blockPos{r.dest.x + span.x, r.dest.y + span.y, r.dest.z + span.z}
	// The three named corners must be loaded and inside the world
	// (BlockPosArgument.getLoadedBlockPos), checked in argument order.
	for _, c := range []struct {
		dim int
		p   blockPos
	}{{r.srcDim, r.begin}, {r.srcDim, r.end}, {r.dstDim, r.dest}} {
		if !h.cloneLoaded(c.dim, c.p, c.p) {
			return "That position is not loaded"
		}
		if !h.inWorldYIn(c.dim, c.p.y) {
			return "That position is out of this world!"
		}
	}
	overlap := r.dest.x <= from1.x && dest1.x >= from0.x && r.dest.y <= from1.y && dest1.y >= from0.y &&
		r.dest.z <= from1.z && dest1.z >= from0.z
	if r.mode == cloneNormal && r.srcDim == r.dstDim && overlap {
		return "The source and destination areas cannot overlap"
	}
	area := int64(span.x+1) * int64(span.y+1) * int64(span.z+1)
	if area > fillLimit {
		return fmt.Sprintf("Too many blocks in the specified area (maximum %d, but specified %d)", fillLimit, area)
	}
	if !h.cloneLoaded(r.srcDim, from0, from1) || !h.cloneLoaded(r.dstDim, r.dest, dest1) {
		return "That position is not loaded"
	}

	// Read the whole source first, sorted into vanilla's three lists.
	src := h.worldFor(r.srcDim)
	var solid, withBE, other []cloneCell
	for z := from0.z; z <= from1.z; z++ {
		for y := from0.y; y <= from1.y; y++ {
			for x := from0.x; x <= from1.x; x++ {
				st := src.At(x, y, z)
				if r.mask != nil && !r.mask(st) {
					continue
				}
				c := cloneCell{src: blockPos{x, y, z}, state: st,
					dst: blockPos{r.dest.x + x - from0.x, r.dest.y + y - from0.y, r.dest.z + z - from0.z}}
				c.be = h.peekBlockEntity(simPos{dim: r.srcDim, blockPos: c.src}, r.mode != cloneMove)
				switch {
				case !c.be.empty():
					withBE = append(withBE, c)
				case fullCube(st):
					solid = append(solid, c)
				default:
					other = append(other, c)
				}
			}
		}
	}

	// Writes run without the per-block support sweep, as vanilla's clone
	// writes skip neighbour updates; the pass after the copy settles them.
	sweep := h.supportSweep
	h.supportSweep = true
	if r.mode == cloneMove {
		for _, list := range [][]cloneCell{solid, withBE, other} {
			for _, c := range list {
				sp := simPos{dim: r.srcDim, blockPos: c.src}
				h.discardBlockEntity(sp)
				h.setBlockAt(players, r.srcDim, c.src, worldgen.Air)
			}
		}
	}
	all := make([]cloneCell, 0, len(solid)+len(withBE)+len(other))
	all = append(append(append(all, solid...), withBE...), other...)
	count := 0
	for _, c := range all {
		if !h.inWorldYIn(r.dstDim, c.dst.y) {
			continue
		}
		dp := simPos{dim: r.dstDim, blockPos: c.dst}
		h.discardBlockEntity(dp) // what stood there goes without a drop
		h.setBlockAt(players, r.dstDim, c.dst, c.state)
		if c.state != barrierBlock { // the barrier pre-pass means only a barrier "changes nothing"
			count++
		}
	}
	for _, c := range withBE {
		if h.inWorldYIn(r.dstDim, c.dst.y) {
			h.placeBlockEntity(players, simPos{dim: r.dstDim, blockPos: c.dst}, c.be, c.state)
		}
	}
	h.supportSweep = sweep
	if !r.strict && !sweep {
		for i := len(all) - 1; i >= 0; i-- {
			if c := all[i]; h.inWorldYIn(r.dstDim, c.dst.y) {
				h.dropUnsupported(players, r.dstDim, c.dst)
			}
		}
	}
	if count == 0 {
		return "No blocks were cloned"
	}
	if caller != nil {
		h.cmdSuccess(players, caller, fmt.Sprintf("Successfully cloned %d block(s)", count), true)
	}
	return ""
}

// cloneLoaded is Level.hasChunksAt over a box: every chunk column it
// touches is loaded.
func (h *hub) cloneLoaded(dim int, a, b blockPos) bool {
	w := h.worldFor(dim)
	for cx := min(a.x, b.x) >> 4; cx <= max(a.x, b.x)>>4; cx++ {
		for cz := min(a.z, b.z) >> 4; cz <= max(a.z, b.z)>>4; cz++ {
			if !w.Loaded(int32(cx), int32(cz)) {
				return false
			}
		}
	}
	return true
}
