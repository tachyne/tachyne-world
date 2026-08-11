package server

import (
	"container/heap"
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Flying pathfinding — the air-node A* behind a bee's trip home.
//
// Ported from FlyNodeEvaluator + PathFinder. The ground pathfinder in
// pathfind.go is two-dimensional by construction: it walks (x,z) and asks the
// world what the floor height is. A flier has no floor, so it needs the third
// axis as a real degree of freedom, and the neighbour set is vanilla's 26 —
// every axis, edge and corner move out of a cell.
//
// The diagonal rule is the part worth being careful about: vanilla only allows
// a diagonal when the orthogonal moves it is composed of are themselves
// passable (hasMalus on each component). Without that a bee slips through the
// gap between two diagonally-touching blocks, which looks like clipping
// through a wall.

const (
	// PathNavigation: maxVisitedNodes = floor(FOLLOW_RANGE * 16). A bee does
	// not override FOLLOW_RANGE, so it searches on the default 16 × 16.
	flyPathBudget = 256

	// Beyond this the search is not worth starting — a bee gives its hive up
	// at 48 anyway (Bee.TOO_FAR_DISTANCE).
	flyPathMaxRange = 64
)

// flyNode is one cell of the search.
type flyNode struct {
	pos    blockPos
	f      float64
	parent int
}

type flyHeap []flyNode

func (p flyHeap) Len() int           { return len(p) }
func (p flyHeap) Less(i, j int) bool { return p[i].f < p[j].f }
func (p flyHeap) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *flyHeap) Push(x any)        { *p = append(*p, x.(flyNode)) }
func (p *flyHeap) Pop() any          { old := *p; n := len(old); it := old[n-1]; *p = old[:n-1]; return it }
func flyDist(a, b blockPos) float64 { // Node.distanceTo — plain euclidean
	dx, dy, dz := float64(a.x-b.x), float64(a.y-b.y), float64(a.z-b.z)
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

// flyOffsets is FlyNodeEvaluator.getNeighbors in table form: the six axis
// moves, then the twelve edge diagonals, then the eight corners. `needs` lists
// the orthogonal components a diagonal is gated on — vanilla's hasMalus checks,
// which is what stops a flier cutting a corner through solid blocks.
var flyOffsets = func() []struct {
	d     [3]int
	needs [][3]int
} {
	type off = struct {
		d     [3]int
		needs [][3]int
	}
	axis := [][3]int{{0, 0, 1}, {0, 0, -1}, {-1, 0, 0}, {1, 0, 0}, {0, 1, 0}, {0, -1, 0}}
	out := make([]off, 0, 26)
	for _, a := range axis {
		out = append(out, off{d: a})
	}
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				n := abs(dx) + abs(dy) + abs(dz)
				if n < 2 {
					continue // already covered by the axis moves (and {0,0,0})
				}
				var needs [][3]int
				if dx != 0 {
					needs = append(needs, [3]int{dx, 0, 0})
				}
				if dy != 0 {
					needs = append(needs, [3]int{0, dy, 0})
				}
				if dz != 0 {
					needs = append(needs, [3]int{0, 0, dz})
				}
				out = append(out, off{d: [3]int{dx, dy, dz}, needs: needs})
			}
		}
	}
	return out
}()

// flyPassable reports whether a flier's cell is open, and what it costs.
// A negative cost means refused, mirroring vanilla's negative pathfinding
// malus. Air above a solid block is passable but dearer (PathType.WALKABLE
// carries +1), which is why vanilla's fliers cruise rather than skim.
func flyPassable(w *world.World, p blockPos) (float64, bool) {
	if w == nil {
		return 0, false
	}
	st := w.At(p.x, p.y, p.z)
	if worldgen.Collides(st) {
		return 0, false
	}
	if worldgen.IsLava(st) || isFire(st) {
		return 0, false // DAMAGE_FIRE / LAVA: refused outright
	}
	cost := 0.0
	if below := w.At(p.x, p.y-1, p.z); worldgen.IsSolidFull(below) {
		cost = 1 // PathType.WALKABLE
	}
	return cost, true
}

// findFlyPath is PathFinder.findPath for an airborne mob: A* from `from` to
// `to` through open cells, returning the waypoints to fly (excluding the start,
// including the goal). Nil when there is no route inside the node budget.
func findFlyPath(w *world.World, from, to blockPos, budget int) []blockPos {
	if w == nil || flyDist(from, to) > flyPathMaxRange {
		return nil
	}
	if _, ok := flyPassable(w, to); !ok {
		// The goal itself is solid — a hive block is. Aim for the open cell
		// beside it that we can actually occupy, nearest to the start.
		if adj, ok := flyOpenBeside(w, to, from); ok {
			to = adj
		} else {
			return nil
		}
	}
	nodes := []flyNode{{pos: from, parent: -1}}
	index := map[blockPos]int{from: 0}
	gScore := map[blockPos]float64{from: 0}
	open := &flyHeap{{pos: from, f: flyDist(from, to), parent: -1}}
	closed := map[blockPos]bool{}

	for open.Len() > 0 && len(closed) < budget {
		cur := heap.Pop(open).(flyNode)
		if closed[cur.pos] {
			continue
		}
		closed[cur.pos] = true
		if cur.pos == to {
			return flyUnwind(nodes, index, cur.pos)
		}
		for _, o := range flyOffsets {
			np := blockPos{cur.pos.x + o.d[0], cur.pos.y + o.d[1], cur.pos.z + o.d[2]}
			if closed[np] || np.y < worldgen.MinY || np.y >= w.Ceiling() {
				continue
			}
			cost, ok := flyPassable(w, np)
			if !ok {
				continue
			}
			// A diagonal is only available when its component moves are.
			blocked := false
			for _, need := range o.needs {
				side := blockPos{cur.pos.x + need[0], cur.pos.y + need[1], cur.pos.z + need[2]}
				if _, ok := flyPassable(w, side); !ok {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
			g := gScore[cur.pos] + flyDist(cur.pos, np) + cost
			if had, seen := gScore[np]; seen && g >= had {
				continue
			}
			gScore[np] = g
			if i, seen := index[np]; seen {
				nodes[i].parent = index[cur.pos]
			} else {
				index[np] = len(nodes)
				nodes = append(nodes, flyNode{pos: np, parent: index[cur.pos]})
			}
			heap.Push(open, flyNode{pos: np, f: g + flyDist(np, to)})
		}
	}
	return nil
}

// flyUnwind walks the parent chain back to the start and reverses it.
func flyUnwind(nodes []flyNode, index map[blockPos]int, goal blockPos) []blockPos {
	var rev []blockPos
	for i := index[goal]; i >= 0; i = nodes[i].parent {
		rev = append(rev, nodes[i].pos)
		if nodes[i].parent < 0 {
			break
		}
	}
	out := make([]blockPos, 0, len(rev))
	for i := len(rev) - 2; i >= 0; i-- { // drop the start cell
		out = append(out, rev[i])
	}
	return out
}

// flyOpenBeside finds the open cell touching a solid goal — the cell a bee can
// actually hover in to reach a hive — preferring the one nearest the start.
func flyOpenBeside(w *world.World, goal, from blockPos) (blockPos, bool) {
	best, bestD, found := blockPos{}, math.MaxFloat64, false
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				p := blockPos{goal.x + dx, goal.y + dy, goal.z + dz}
				if _, ok := flyPassable(w, p); !ok {
					continue
				}
				if d := flyDist(p, from); d < bestD {
					best, bestD, found = p, d, true
				}
			}
		}
	}
	return best, found
}

// --- following a route ------------------------------------------------------
//
// PathNavigation's half of the job: hold a path, hand out the waypoint being
// flown to, advance when it is reached, and recompute when the world or the
// errand has moved on.

const (
	flyNodeReach = 1.0 // how close counts as having reached a waypoint
	flyRepathAge = 20  // mob-updates before a route is considered stale
)

// flyWaypoint returns the cell the mob is currently flying toward.
func (m *mob) flyWaypoint() (blockPos, bool) {
	if m.flyIdx < 0 || m.flyIdx >= len(m.flyPath) {
		return blockPos{}, false
	}
	return m.flyPath[m.flyIdx], true
}

// flyClearPath drops the current route.
func (m *mob) flyClearPath() {
	m.flyPath, m.flyIdx, m.flyStale = nil, 0, 0
}

// flyTo keeps a mob routed to a goal, recomputing when the goal changes, the
// route runs out, or it goes stale. Returns false when no route exists — the
// caller then decides whether to give up on the goal (vanilla blacklists a
// hive it cannot reach rather than hovering at it forever).
func (h *hub) flyTo(m *mob, goal blockPos) bool {
	return h.flyToBudget(m, goal, flyPathBudget)
}

// flyToBudget is flyTo with the node budget spelled out — the close-in hive
// search runs at ten times the ordinary one, as vanilla's
// setMaxVisitedNodesMultiplier(10) does.
func (h *hub) flyToBudget(m *mob, goal blockPos, budget int) bool {
	m.flyStale++
	repath := m.flyGoal != goal || m.flyIdx >= len(m.flyPath) || m.flyStale > flyRepathAge
	if repath {
		from := blockPos{int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))}
		m.flyPath = findFlyPath(h.worldFor(m.dim), from, goal, budget)
		m.flyIdx, m.flyGoal, m.flyStale = 0, goal, 0
		if len(m.flyPath) == 0 {
			return false
		}
	}
	// Advance past every waypoint already reached — a fast mob can clear
	// several in one update, and stepping one at a time would make it
	// backtrack to a cell it has flown past.
	for m.flyIdx < len(m.flyPath) {
		n := m.flyPath[m.flyIdx]
		d := math.Sqrt(sq(float64(n.x)+0.5-m.x) + sq(float64(n.y)+0.5-m.y) + sq(float64(n.z)+0.5-m.z))
		if d > flyNodeReach {
			break
		}
		m.flyIdx++
	}
	return true
}

// flySteer is the ground-plane half of following a route: head for the current
// waypoint. Altitude is flyMove's job, off the same waypoint.
func flySteer(m *mob, speed float64) (float64, float64) {
	n, ok := m.flyWaypoint()
	if !ok {
		return 0, 0
	}
	dx, dz := float64(n.x)+0.5-m.x, float64(n.z)+0.5-m.z
	if d := math.Hypot(dx, dz); d > 0.01 {
		return dx / d * speed, dz / d * speed
	}
	return 0, 0
}
