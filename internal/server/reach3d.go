package server

import (
	"container/heap"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// reach3D answers whether a walking mob can get within reach of a block, over
// the world's cells rather than its columns, the way vanilla's
// WalkNodeEvaluator reads it:
//   - a cell can be stood in when the block below it has collision (not a
//     fence, wall or door) and the cell and the one above are clear (a
//     villager also passes wooden doors);
//   - a step goes to the eight neighbours on the same level, up one block
//     where there is headroom, or down up to three;
//   - a diagonal step needs both of its side cells open.
// The 2.5-D column search (pathfind.go) takes one height per column, so it
// cannot see a bed under a roof, a floor above the ground or a second
// storey. This is what AcquirePoi's reachability asks. It reads only loaded
// chunks.

const reachMaxDrop = 3

type reachNode struct {
	x, y, z int
	f       float64
}

type reachHeap []reachNode

func (p reachHeap) Len() int           { return len(p) }
func (p reachHeap) Less(i, j int) bool { return p[i].f < p[j].f }
func (p reachHeap) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *reachHeap) Push(x any)        { *p = append(*p, x.(reachNode)) }
func (p *reachHeap) Pop() any {
	old := *p
	n := old[len(old)-1]
	*p = old[:len(old)-1]
	return n
}

// reach3D reports whether a path from the feet cell start reaches a cell
// within validRange (horizontally, and one more vertically) of target, in
// at most maxNodes expansions and never more than maxRange from start.
func reach3D(w *world.World, doors bool, start, target blockPos, validRange, maxRange, maxNodes int) bool {
	within := func(p blockPos) bool {
		return max(abs(p.x-target.x), abs(p.z-target.z)) <= validRange && abs(p.y-target.y) <= validRange+1
	}
	if within(start) {
		return true
	}
	loaded := func(x, z int) bool { return w.Loaded(int32(x>>4), int32(z>>4)) }
	open := func(x, y, z int) bool { // a cell a body passes through
		s := w.At(x, y, z)
		if doors && worldgen.IsWoodenDoor(s) {
			return true
		}
		return !worldgen.Collides(s) && !worldgen.IsLava(s)
	}
	stand := func(x, y, z int) bool { // a fence or wall is no floor: nothing steps up onto it
		below := w.At(x, y-1, z)
		return loaded(x, z) && worldgen.Collides(below) && !worldgen.IsTallCollision(below) && !worldgen.IsDoor(below) &&
			open(x, y, z) && open(x, y+1, z)
	}
	hDist := func(p blockPos) float64 {
		dx, dy, dz := abs(p.x-target.x), abs(p.y-target.y), abs(p.z-target.z)
		return float64(max(dx, dz)) + 0.41*float64(min(dx, dz)) + float64(dy)
	}
	g := map[blockPos]float64{start: 0}
	closed := map[blockPos]bool{}
	q := &reachHeap{{start.x, start.y, start.z, hDist(start)}}
	for expanded := 0; q.Len() > 0 && expanded < maxNodes; {
		n := heap.Pop(q).(reachNode)
		cur := blockPos{n.x, n.y, n.z}
		if closed[cur] {
			continue
		}
		closed[cur] = true
		expanded++
		if within(cur) {
			return true
		}
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				nx, nz := cur.x+dx, cur.z+dz
				if max(abs(nx-start.x), abs(nz-start.z)) > maxRange || !loaded(nx, nz) {
					continue
				}
				if dx != 0 && dz != 0 && !(open(cur.x+dx, cur.y, cur.z) && open(cur.x+dx, cur.y+1, cur.z) &&
					open(cur.x, cur.y, cur.z+dz) && open(cur.x, cur.y+1, cur.z+dz)) {
					continue // no cutting a corner
				}
				var next blockPos
				found := false
				switch {
				case stand(nx, cur.y, nz):
					next, found = blockPos{nx, cur.y, nz}, true
				case stand(nx, cur.y+1, nz) && open(cur.x, cur.y+2, cur.z):
					next, found = blockPos{nx, cur.y + 1, nz}, true // step up, with headroom
				default:
					for d := 1; d <= reachMaxDrop && open(nx, cur.y-d+1, nz); d++ {
						if stand(nx, cur.y-d, nz) {
							next, found = blockPos{nx, cur.y - d, nz}, true
							break
						}
					}
				}
				if !found || closed[next] {
					continue
				}
				cost := g[cur] + 1
				if dx != 0 && dz != 0 {
					cost = g[cur] + 1.41
				}
				if old, ok := g[next]; ok && cost >= old {
					continue
				}
				g[next] = cost
				heap.Push(q, reachNode{next.x, next.y, next.z, cost + hDist(next)})
			}
		}
	}
	return false
}
