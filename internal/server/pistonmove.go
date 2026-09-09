package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The piston's structure resolver (vanilla PistonStructureResolver) and the
// move itself (PistonBaseBlock.moveBlocks). The resolver walks the line of
// blocks ahead, lets every slime or honey block in it drag along whatever
// it touches (slime sticks to anything but honey, honey to anything but
// slime), stops at 12, and separates what moves from what breaks.

var (
	cryingObsidianState              = worldgen.BlockBase("crying_obsidian")
	reinforcedDeepslate              = worldgen.BlockBase("reinforced_deepslate")
	respawnAnchorLo, respawnAnchorHi = worldgen.BlockRange("respawn_anchor")
)

func isStickyBlock(s uint32) bool { return isSlimeBlock(s) || isHoneyBlock(s) }

// canStickTogether: slime and honey refuse each other; anything else sticks
// to either.
func canStickTogether(a, b uint32) bool {
	if (isHoneyBlock(a) && isSlimeBlock(b)) || (isSlimeBlock(a) && isHoneyBlock(b)) {
		return false
	}
	return isStickyBlock(a) || isStickyBlock(b)
}

// pistonPushable is PistonBaseBlock.isPushable: whether a block at height y
// may move along push, arriving from dir (the direction the piston itself
// faces for the line ahead, the opposite when a sticky block drags it from
// behind). allowDestroy is true only for the blocks in the line ahead, which
// a push may break.
func (h *hub) pistonPushable(s uint32, y int, push [3]int, allowDestroy bool, dir [3]int) bool {
	if !h.inWorldY(y) {
		return false
	}
	if s == worldgen.Air {
		return true
	}
	if s == obsidianState || s == cryingObsidianState || s == reinforcedDeepslate ||
		(s >= respawnAnchorLo && s <= respawnAnchorHi) {
		return false
	}
	if push[1] < 0 && !h.inWorldY(y-1) || push[1] > 0 && !h.inWorldY(y+1) {
		return false
	}
	if isPistonBase(s) {
		if boolProp(s, "extended") {
			return false
		}
	} else {
		if worldgen.Hardness(s) < 0 {
			return false
		}
		switch pushReactionOf(s) {
		case pushBlock:
			return false
		case pushDestroy:
			return allowDestroy
		case pushOnly:
			return push == dir
		}
	}
	return !hasBlockEntity(s)
}

type pistonResolver struct {
	h         *hub
	pistonPos blockPos
	startPos  blockPos
	push      [3]int // the direction blocks move
	extending bool
	toPush    []blockPos
	toDestroy []blockPos
}

func stepPos(p blockPos, d [3]int, n int) blockPos {
	return blockPos{p.x + d[0]*n, p.y + d[1]*n, p.z + d[2]*n}
}

func neg(d [3]int) [3]int { return [3]int{-d[0], -d[1], -d[2]} }

func (r *pistonResolver) at(p blockPos) uint32 { return r.h.world.At(p.x, p.y, p.z) }

func (r *pistonResolver) indexOf(p blockPos) int {
	for i, q := range r.toPush {
		if q == p {
			return i
		}
	}
	return -1
}

// resolve is PistonStructureResolver.resolve.
func (r *pistonResolver) resolve() bool {
	r.toPush, r.toDestroy = r.toPush[:0], r.toDestroy[:0]
	s := r.at(r.startPos)
	if !r.h.pistonPushable(s, r.startPos.y, r.push, false, r.pistonDir()) {
		if r.extending && pushReactionOf(s) == pushDestroy {
			r.toDestroy = append(r.toDestroy, r.startPos)
			return true
		}
		return false
	}
	if !r.addBlockLine(r.startPos, r.push) {
		return false
	}
	for i := 0; i < len(r.toPush); i++ {
		p := r.toPush[i]
		if isStickyBlock(r.at(p)) && !r.addBranchingBlocks(p) {
			return false
		}
	}
	return true
}

// pistonDir is the direction the piston faces (the push direction when
// extending, its opposite when a sticky piston pulls).
func (r *pistonResolver) pistonDir() [3]int {
	if r.extending {
		return r.push
	}
	return neg(r.push)
}

// addBlockLine is PistonStructureResolver.addBlockLine: from a block, walk
// back through the sticky chain behind it, then forward along the push
// until air, a breakable block, or a collision with blocks already listed.
func (r *pistonResolver) addBlockLine(pos blockPos, dir [3]int) bool {
	s := r.at(pos)
	if s == worldgen.Air {
		return true
	}
	if !r.h.pistonPushable(s, pos.y, r.push, false, dir) {
		return true
	}
	if pos == r.pistonPos {
		return true
	}
	if r.indexOf(pos) >= 0 {
		return true
	}
	back := 1
	if back+len(r.toPush) > pistonMaxPush {
		return false
	}
	for isStickyBlock(s) {
		behind := stepPos(pos, neg(r.push), back)
		prev := s
		s = r.at(behind)
		if s == worldgen.Air || !canStickTogether(prev, s) ||
			!r.h.pistonPushable(s, behind.y, r.push, false, neg(r.push)) || behind == r.pistonPos {
			break
		}
		back++
		if back+len(r.toPush) > pistonMaxPush {
			return false
		}
	}
	added := 0
	for n := back - 1; n >= 0; n-- {
		r.toPush = append(r.toPush, stepPos(pos, neg(r.push), n))
		added++
	}
	for n := 1; ; n++ {
		ahead := stepPos(pos, r.push, n)
		if idx := r.indexOf(ahead); idx >= 0 {
			r.reorderAtCollision(added, idx)
			for i := 0; i <= idx+added; i++ {
				q := r.toPush[i]
				if isStickyBlock(r.at(q)) && !r.addBranchingBlocks(q) {
					return false
				}
			}
			return true
		}
		s = r.at(ahead)
		if s == worldgen.Air {
			return true
		}
		if !r.h.pistonPushable(s, ahead.y, r.push, true, r.push) || ahead == r.pistonPos {
			return false
		}
		if pushReactionOf(s) == pushDestroy {
			r.toDestroy = append(r.toDestroy, ahead)
			return true
		}
		if len(r.toPush) >= pistonMaxPush {
			return false
		}
		r.toPush = append(r.toPush, ahead)
		added++
	}
}

// reorderAtCollision moves the last `added` entries in front of the entry
// they ran into, so the list stays ordered far-to-near along the push.
func (r *pistonResolver) reorderAtCollision(added, idx int) {
	n := len(r.toPush)
	head := append([]blockPos{}, r.toPush[:idx]...)
	tail := append([]blockPos{}, r.toPush[n-added:]...)
	mid := append([]blockPos{}, r.toPush[idx:n-added]...)
	r.toPush = append(append(head, tail...), mid...)
}

// addBranchingBlocks lets a sticky block drag its side neighbours along.
func (r *pistonResolver) addBranchingBlocks(pos blockPos) bool {
	s := r.at(pos)
	for _, d := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if (d[0] != 0 && r.push[0] != 0) || (d[1] != 0 && r.push[1] != 0) || (d[2] != 0 && r.push[2] != 0) {
			continue // same axis as the push: that is the line itself
		}
		side := stepPos(pos, d, 1)
		if canStickTogether(r.at(side), s) && !r.addBlockLine(side, d) {
			return false
		}
	}
	return true
}

// movePistonBlocks is PistonBaseBlock.moveBlocks: resolve the structure in
// front of (or, pulling, beyond) the piston and shift it one cell. Reports
// whether anything moved. Overworld-only, like the rest of the block sim.
func (h *hub) movePistonBlocks(players map[int32]*tracked, pos blockPos, dir [3]int, extending bool) bool {
	front := stepPos(pos, dir, 1)
	if !extending && isPistonHead(h.world.At(front.x, front.y, front.z)) {
		h.setBlock(players, front, worldgen.Air)
	}
	r := &pistonResolver{h: h, pistonPos: pos, extending: extending}
	if extending {
		r.startPos, r.push = front, dir
	} else {
		r.startPos, r.push = stepPos(pos, dir, 2), neg(dir)
	}
	if !r.resolve() {
		return false
	}
	// What breaks drops its loot and goes.
	for i := len(r.toDestroy) - 1; i >= 0; i-- {
		p := r.toDestroy[i]
		s := h.world.At(p.x, p.y, p.z)
		for _, d := range h.rollDrops(s) {
			h.spawnItem(players, d.item, d.count, float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5)
		}
		h.setBlock(players, p, worldgen.Air)
	}
	// What moves: take every state first, clear the sources, then lay them
	// down one cell along; a source that is also a destination keeps its
	// new occupant.
	states := make([]uint32, len(r.toPush))
	for i, p := range r.toPush {
		states[i] = h.world.At(p.x, p.y, p.z)
	}
	dest := make(map[blockPos]uint32, len(r.toPush)+1)
	for i, p := range r.toPush {
		dest[stepPos(p, r.push, 1)] = states[i]
	}
	for _, p := range r.toPush {
		if _, isDest := dest[p]; !isDest {
			h.setBlock(players, p, worldgen.Air)
		}
	}
	// Each destination holds a moving_piston carrying its block for two
	// ticks (PistonBaseBlock.moveBlocks: MOVING_PISTON facing the piston's
	// way, the head marked as the source), then the block lands.
	base := h.world.At(pos.x, pos.y, pos.z)
	moving := movingPistonState(dir, false)
	for i := len(r.toPush) - 1; i >= 0; i-- {
		h.placeMoving(players, stepPos(r.toPush[i], r.push, 1), moving,
			movingBlock{moved: states[i], facing: dir, extending: extending})
	}
	if extending {
		head := headFor(base)
		dest[front] = head
		h.placeMoving(players, front, movingPistonState(dir, isSticky(base)),
			movingBlock{moved: head, facing: dir, extending: true, source: true})
	}
	for _, p := range r.toPush {
		h.scheduleAround(p, 1)
	}
	h.shoveOutOfBlocks(players, dest, r.push)
	return true
}

// shoveOutOfBlocks carries whoever stands where a block just arrived: a
// player or mob whose feet are now inside a moved block (or the new head)
// is pushed one cell along the move, as vanilla's moving piston does.
func (h *hub) shoveOutOfBlocks(players map[int32]*tracked, arrived map[blockPos]uint32, dir [3]int) {
	inside := func(x, y, z float64) (blockPos, bool) {
		for _, dy := range []float64{0.05, 1.0} {
			p := blockPos{int(math.Floor(x)), int(math.Floor(y + dy)), int(math.Floor(z))}
			if st, ok := arrived[p]; ok && worldgen.Collides(st) {
				return p, true
			}
		}
		return blockPos{}, false
	}
	for _, t := range players {
		if t.dim != 0 || t.dead {
			continue
		}
		if _, ok := inside(t.x, t.y, t.z); ok {
			h.teleportPlayer(players, t, t.x+float64(dir[0]), t.y+float64(dir[1]), t.z+float64(dir[2]))
			if dir[1] > 0 {
				t.peakY = t.y // lifted, not thrown: no fall damage for the rise
			}
		}
	}
	for _, m := range h.mobs {
		if m.dim != 0 || m.dying > 0 {
			continue
		}
		if _, ok := inside(m.x, m.y, m.z); ok {
			m.x, m.y, m.z = m.x+float64(dir[0]), m.y+float64(dir[1]), m.z+float64(dir[2])
			h.toNearbyEv(players, 0, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		}
	}
}
