package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// RailState and BaseRailBlock's placement and neighbour logic: a rail
// connects to the rails beside it (and a block up or down) — on placement
// every neighbour that can take another connection turns to meet it, a
// junction of three follows a signal when one arrives, and a slope loses
// its rail when the block it climbs onto goes. Powered and activator
// rails pass their power along up to eight rails of the same kind
// (PoweredRailBlock.findPoweredRailSignal).
//
// Everything runs on the hub goroutine in the simulation dimension
// (h.rsDim), and every write is setBlockAndUpdate: the state, then the
// neighbours' updates.

// railState is one RailState: a rail, its shape's two connections.
type railState struct {
	pos      blockPos
	state    uint32
	straight bool
	conns    []blockPos
}

// isRailAt is BaseRailBlock.isRail(level, pos) in the simulation dimension.
func (h *hub) isRailAt(p blockPos) bool {
	return h.inWorldYIn(h.rsDim, p.y) && isAnyRail(h.rsWorld().At(p.x, p.y, p.z))
}

func newRailState(pos blockPos, state uint32) *railState {
	r := &railState{pos: pos, state: state, straight: isSpecialRail(state)}
	r.updateConnections(railShape(state))
	return r
}

func (r *railState) updateConnections(shape int) {
	p := r.pos
	n, s, w, e := blockPos{p.x, p.y, p.z - 1}, blockPos{p.x, p.y, p.z + 1}, blockPos{p.x - 1, p.y, p.z}, blockPos{p.x + 1, p.y, p.z}
	up := func(b blockPos) blockPos { return blockPos{b.x, b.y + 1, b.z} }
	switch shape {
	case shapeNS:
		r.conns = []blockPos{n, s}
	case shapeEW:
		r.conns = []blockPos{w, e}
	case shapeAscE:
		r.conns = []blockPos{w, up(e)}
	case shapeAscW:
		r.conns = []blockPos{up(w), e}
	case shapeAscN:
		r.conns = []blockPos{up(n), s}
	case shapeAscS:
		r.conns = []blockPos{n, up(s)}
	case shapeSE:
		r.conns = []blockPos{e, s}
	case shapeSW:
		r.conns = []blockPos{w, s}
	case shapeNW:
		r.conns = []blockPos{w, n}
	case shapeNE:
		r.conns = []blockPos{e, n}
	default:
		r.conns = nil
	}
}

// railAt is RailState.getRail: the rail at the position, or a block above
// or below it.
func (h *hub) railAt(p blockPos) *railState {
	for _, dy := range []int{0, 1, -1} {
		q := blockPos{p.x, p.y + dy, p.z}
		if h.isRailAt(q) {
			return newRailState(q, h.rsWorld().At(q.x, q.y, q.z))
		}
	}
	return nil
}

func (r *railState) hasConnection(p blockPos) bool {
	for _, c := range r.conns {
		if c.x == p.x && c.z == p.z {
			return true
		}
	}
	return false
}

func (r *railState) connectsTo(o *railState) bool { return r.hasConnection(o.pos) }

// removeSoftConnections keeps only the connections a rail there returns.
func (h *hub) removeSoftConnections(r *railState) {
	out := r.conns[:0:0]
	for _, c := range r.conns {
		if o := h.railAt(c); o != nil && o.connectsTo(r) {
			out = append(out, o.pos)
		}
	}
	r.conns = out
}

func (r *railState) canConnectTo(o *railState) bool {
	return r.connectsTo(o) || len(r.conns) != 2
}

// countPotentialConnections counts the four sides with a rail level, above
// or below.
func (h *hub) countPotentialConnections(r *railState) int {
	n := 0
	for _, d := range horizNeighbors {
		p := blockPos{r.pos.x + d.x, r.pos.y, r.pos.z + d.z}
		if h.isRailAt(p) || h.isRailAt(blockPos{p.x, p.y + 1, p.z}) || h.isRailAt(blockPos{p.x, p.y - 1, p.z}) {
			n++
		}
	}
	return n
}

// railSlopes turns a straight shape into the slope toward a rail a block up
// at either end (the later check winning, as vanilla's does).
func (h *hub) railSlopes(r *railState, shape int) int {
	p := r.pos
	switch shape {
	case shapeNS:
		if h.isRailAt(blockPos{p.x, p.y + 1, p.z - 1}) {
			shape = shapeAscN
		}
		if h.isRailAt(blockPos{p.x, p.y + 1, p.z + 1}) {
			shape = shapeAscS
		}
	case shapeEW:
		if h.isRailAt(blockPos{p.x + 1, p.y + 1, p.z}) {
			shape = shapeAscE
		}
		if h.isRailAt(blockPos{p.x - 1, p.y + 1, p.z}) {
			shape = shapeAscW
		}
	}
	return shape
}

// railSet is setBlockAndUpdate for a rail's new state.
func (h *hub) railSet(players map[int32]*tracked, pos blockPos, state uint32) {
	h.rsSet(players, pos, state)
	h.nbAround(pos, 0, false)
	h.nbRun(players)
}

// connectTo is RailState.connectTo: the rail takes o as a connection and
// re-shapes around what it now connects.
func (h *hub) connectTo(players map[int32]*tracked, r, o *railState) {
	r.conns = append(r.conns, o.pos)
	p := r.pos
	n := r.hasConnection(blockPos{p.x, p.y, p.z - 1})
	s := r.hasConnection(blockPos{p.x, p.y, p.z + 1})
	w := r.hasConnection(blockPos{p.x - 1, p.y, p.z})
	e := r.hasConnection(blockPos{p.x + 1, p.y, p.z})
	shape := -1
	if n || s {
		shape = shapeNS
	}
	if w || e {
		shape = shapeEW
	}
	if !r.straight {
		switch {
		case s && e && !n && !w:
			shape = shapeSE
		case s && w && !n && !e:
			shape = shapeSW
		case n && w && !s && !e:
			shape = shapeNW
		case n && e && !s && !w:
			shape = shapeNE
		}
	}
	shape = h.railSlopes(r, shape)
	if shape < 0 {
		shape = shapeNS
	}
	r.state = railWith(r.state, shape, railPowered(r.state))
	h.railSet(players, r.pos, r.state)
}

// hasNeighborRail is RailState.hasNeighborRail: a rail there that, its own
// dead connections dropped, can take one more.
func (h *hub) hasNeighborRail(r *railState, p blockPos) bool {
	o := h.railAt(p)
	if o == nil {
		return false
	}
	h.removeSoftConnections(o)
	return o.canConnectTo(r)
}

// railPlace is RailState.place: the shape from the rails that can connect
// on each side, then, if it changed (or this is the first placement), the
// write and each connected rail turning to meet it.
func (h *hub) railPlace(players map[int32]*tracked, r *railState, hasSignal, first bool, def int) *railState {
	p := r.pos
	n := h.hasNeighborRail(r, blockPos{p.x, p.y, p.z - 1})
	s := h.hasNeighborRail(r, blockPos{p.x, p.y, p.z + 1})
	w := h.hasNeighborRail(r, blockPos{p.x - 1, p.y, p.z})
	e := h.hasNeighborRail(r, blockPos{p.x + 1, p.y, p.z})
	shape := -1
	ns, we := n || s, w || e
	if ns && !we {
		shape = shapeNS
	}
	if we && !ns {
		shape = shapeEW
	}
	se, sw, ne, nw := s && e, s && w, n && e, n && w
	if !r.straight {
		if se && !n && !w {
			shape = shapeSE
		}
		if sw && !n && !e {
			shape = shapeSW
		}
		if nw && !s && !e {
			shape = shapeNW
		}
		if ne && !s && !w {
			shape = shapeNE
		}
	}
	if shape < 0 {
		switch {
		case ns && we:
			shape = def
		case ns:
			shape = shapeNS
		case we:
			shape = shapeEW
		}
		if !r.straight {
			if hasSignal {
				if se {
					shape = shapeSE
				}
				if sw {
					shape = shapeSW
				}
				if ne {
					shape = shapeNE
				}
				if nw {
					shape = shapeNW
				}
			} else {
				if nw {
					shape = shapeNW
				}
				if ne {
					shape = shapeNE
				}
				if sw {
					shape = shapeSW
				}
				if se {
					shape = shapeSE
				}
			}
		}
	}
	shape = h.railSlopes(r, shape)
	if shape < 0 {
		shape = def
	}
	r.updateConnections(shape)
	r.state = railWith(r.state, shape, railPowered(r.state))
	if first || h.rsWorld().At(p.x, p.y, p.z) != r.state {
		h.railSet(players, p, r.state)
		for _, c := range r.conns {
			if o := h.railAt(c); o != nil {
				h.removeSoftConnections(o)
				if o.canConnectTo(r) {
					h.connectTo(players, o, r)
				}
			}
		}
	}
	return r
}

// railUpdateDir is BaseRailBlock.updateDir.
func (h *hub) railUpdateDir(players map[int32]*tracked, pos blockPos, state uint32, first bool) uint32 {
	r := h.railPlace(players, newRailState(pos, state), h.inputPower(pos.x, pos.y, pos.z, false) > 0, first, railShape(state))
	return r.state
}

// railOnPlace is BaseRailBlock.onPlace for a rail that was not the same
// block before: its connections are made (updateDir, first), and a
// straight-only rail then reads its power (neighborChanged on itself).
func (h *hub) railOnPlace(players map[int32]*tracked, dim int, pos blockPos) {
	h.inDim(dim, func() {
		state := h.rsWorld().At(pos.x, pos.y, pos.z)
		if !isAnyRail(state) {
			return
		}
		state = h.railUpdateDir(players, pos, state, true)
		if isSpecialRail(state) {
			h.railNeighborChanged(players, pos, state)
		}
	})
}

// railNeighborChanged is BaseRailBlock.neighborChanged: a rail with no
// rigid floor, or a slope whose high side lost its block, breaks; else the
// rail's own updateState.
func (h *hub) railNeighborChanged(players map[int32]*tracked, pos blockPos, state uint32) {
	w := h.rsWorld()
	rigid := func(p blockPos) bool { return holdsBlock(w.At(p.x, p.y, p.z)) } // canSupportRigidBlock
	remove := !rigid(blockPos{pos.x, pos.y - 1, pos.z})
	switch railShape(state) {
	case shapeAscE:
		remove = remove || !rigid(blockPos{pos.x + 1, pos.y, pos.z})
	case shapeAscW:
		remove = remove || !rigid(blockPos{pos.x - 1, pos.y, pos.z})
	case shapeAscN:
		remove = remove || !rigid(blockPos{pos.x, pos.y, pos.z - 1})
	case shapeAscS:
		remove = remove || !rigid(blockPos{pos.x, pos.y, pos.z + 1})
	}
	if remove {
		h.rsSet(players, pos, fluidLeftBy(state)) // removeBlock: the water it held stays
		h.dropLoose(players, h.rsDim, pos, state)
		h.nbAround(pos, 0, false)
		h.nbRun(players)
		return
	}
	switch {
	case isPlainRail(state):
		// RailBlock.updateState: a junction of three follows a signal that
		// arrives from a signal source beside it.
		if h.signalSourceBeside(pos) && h.countPotentialConnections(newRailState(pos, state)) == 3 {
			h.railUpdateDir(players, pos, state, false)
		}
	case isDetectorRail(state):
		// DetectorRailBlock: pressed by carts, not by neighbours.
	default:
		h.poweredRailUpdate(players, pos, state)
	}
}

// fluidLeftBy is what a removed block leaves: its water if it held some.
func fluidLeftBy(state uint32) uint32 {
	if isWaterlogged(state) {
		return worldgen.WaterBase
	}
	return worldgen.Air
}

// signalSourceBeside stands in for RailBlock.updateState's "the block that
// changed is a signal source": is one of the six neighbours one.
func (h *hub) signalSourceBeside(pos blockPos) bool {
	w := h.rsWorld()
	for _, d := range allNeighbors {
		if h.isSignalSource(w.At(pos.x+d.x, pos.y+d.y, pos.z+d.z)) {
			return true
		}
	}
	return false
}

// poweredRailUpdate is PoweredRailBlock.updateState (powered and activator
// rails): powered by a signal beside it or through a line of powered rails
// of its own kind, up to eight away, in either direction.
func (h *hub) poweredRailUpdate(players map[int32]*tracked, pos blockPos, state uint32) {
	want := h.inputPower(pos.x, pos.y, pos.z, false) > 0 ||
		h.findPoweredRailSignal(pos, state, true, 0) || h.findPoweredRailSignal(pos, state, false, 0)
	if want == railPowered(state) {
		return
	}
	h.rsSet(players, pos, railWith(state, railShape(state), want))
	h.nbAround(blockPos{pos.x, pos.y - 1, pos.z}, 0, false)
	if s := railShape(state); s >= shapeAscE && s <= shapeAscS {
		h.nbAround(blockPos{pos.x, pos.y + 1, pos.z}, 0, false)
	}
	h.nbAround(pos, 0, false) // setBlockAndUpdate's own neighbour update
	h.nbRun(players)
}

// findPoweredRailSignal walks one step along the rail's line.
func (h *hub) findPoweredRailSignal(pos blockPos, state uint32, forward bool, depth int) bool {
	if depth >= 8 {
		return false
	}
	x, y, z := pos.x, pos.y, pos.z
	checkBelow := true
	shape := railShape(state)
	switch shape {
	case shapeNS:
		if forward {
			z++
		} else {
			z--
		}
	case shapeEW:
		if forward {
			x--
		} else {
			x++
		}
	case shapeAscE:
		if forward {
			x--
		} else {
			x, y, checkBelow = x+1, y+1, false
		}
		shape = shapeEW
	case shapeAscW:
		if forward {
			x, y, checkBelow = x-1, y+1, false
		} else {
			x++
		}
		shape = shapeEW
	case shapeAscN:
		if forward {
			z++
		} else {
			z, y, checkBelow = z-1, y+1, false
		}
		shape = shapeNS
	case shapeAscS:
		if forward {
			z, y, checkBelow = z+1, y+1, false
		} else {
			z--
		}
		shape = shapeNS
	}
	if h.sameRailWithPower(blockPos{x, y, z}, state, forward, depth, shape) {
		return true
	}
	return checkBelow && h.sameRailWithPower(blockPos{x, y - 1, z}, state, forward, depth, shape)
}

// sameRailWithPower is isSameRailWithPower: a powered rail of the same kind
// lying along the line, powered from beside or further along.
func (h *hub) sameRailWithPower(p blockPos, from uint32, forward bool, depth, dir int) bool {
	if !h.inWorldYIn(h.rsDim, p.y) {
		return false
	}
	st := h.rsWorld().At(p.x, p.y, p.z)
	if !isSpecialRail(st) || specialBase(st) != specialBase(from) || isDetectorRail(st) {
		return false
	}
	my := railShape(st)
	if dir == shapeEW && (my == shapeNS || my == shapeAscN || my == shapeAscS) {
		return false
	}
	if dir == shapeNS && (my == shapeEW || my == shapeAscE || my == shapeAscW) {
		return false
	}
	if !railPowered(st) {
		return false
	}
	if h.inputPower(p.x, p.y, p.z, false) > 0 {
		return true
	}
	return h.findPoweredRailSignal(p, st, forward, depth+1)
}
