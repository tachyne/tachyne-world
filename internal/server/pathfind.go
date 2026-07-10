package server

import (
	"container/heap"
	"math"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// A* pathfinding over the walkable heightmap. Target-seeking mobs (melee
// hunters, following pets) route AROUND walls, water and cliffs instead of
// steering blindly into them and jittering. The graph is 2.5-D: one node per
// (x,z) column, its height taken from MobFeet, with 8-way steps that may climb
// one block or drop a few. Paths are computed on a throttle and cached on the
// mob; each tick the mob just steers toward its next waypoint.

const (
	pathMaxNodes = 350 // node-expansion budget per search (bounds cost)
	pathMaxRange = 20  // search no farther than this from the mob (Chebyshev)
	pathMaxFall  = 3   // a step may drop at most this many blocks
	pathRefresh  = 40  // ticks before a path is recomputed (the target moves)
	pathReach    = 0.6 // horizontal distance at which a waypoint counts as reached
	pathRegoal   = 2.0 // if the goal moved more than this, replan
)

type pathPoint struct{ x, z int }

// pather is the walkability surface A* searches over — satisfied by *world.World.
type pather interface {
	Walkable(x, z int) bool
	MobFeet(x, z int) int
	TallObstacle(x, z int) bool
	Block(x, y, z int) uint32
}

// pathSteer returns a steering vector toward a goal (gx,gz) via a cached A*
// path, (re)planning when the path is missing, stale, consumed, or the goal
// has wandered. Falls back to steering straight at the goal when no path
// exists (open ground, or the goal is unreachable) so the mob still advances.
func (h *hub) pathSteer(m *mob, gx, gz float64) (float64, float64) {
	now := h.tick.Load()
	gxi, gzi := int(math.Floor(gx)), int(math.Floor(gz))
	sxi, szi := int(math.Floor(m.x)), int(math.Floor(m.z))

	stale := m.path == nil || m.pathIdx >= len(m.path) ||
		now-m.pathAt > pathRefresh ||
		math.Hypot(float64(gxi-m.pathGoal[0]), float64(gzi-m.pathGoal[1])) > pathRegoal
	if stale {
		var pw pather = h.worldFor(m.dim)
		if m.usesDoors {
			pw = doorPather{h.worldFor(m.dim)} // route through closed wooden doors
		}
		m.path = findPath(pw, malusFor(m.etype), sxi, szi, gxi, gzi)
		m.pathIdx = 0
		m.pathGoal = [2]int{gxi, gzi}
		m.pathAt = now
	}
	if len(m.path) == 0 {
		return straightSteer(m, gx, gz, standoffDist) // no route — head straight at it
	}
	// Advance past any waypoints we've already reached.
	for m.pathIdx < len(m.path) {
		wp := m.path[m.pathIdx]
		if math.Hypot((float64(wp.x)+0.5)-m.x, (float64(wp.z)+0.5)-m.z) <= pathReach {
			m.pathIdx++
			continue
		}
		break
	}
	if m.pathIdx >= len(m.path) {
		return straightSteer(m, gx, gz, standoffDist) // arrived at the path's end
	}
	// Steer fully toward the next waypoint (waypoints are ~1 block apart, so the
	// final-approach standoff must NOT apply here or the mob stalls short of each
	// one and never advances).
	wp := m.path[m.pathIdx]
	return straightSteer(m, float64(wp.x)+0.5, float64(wp.z)+0.5, 0.05)
}

// straightSteer is the naive "walk directly at the point" vector, scaled to the
// mob's speed. It yields zero within `stop` blocks — used as the standoff for
// the final target and as a near-zero reach for intermediate waypoints.
func straightSteer(m *mob, gx, gz, stop float64) (float64, float64) {
	dx, dz := gx-m.x, gz-m.z
	d := math.Hypot(dx, dz)
	if d < stop {
		return 0, 0
	}
	return dx / d * m.speed, dz / d * m.speed
}

// pathNode is a heap entry for the open set.
type pathNode struct {
	x, z int
	f    float64 // g + heuristic
}

type pathHeap []pathNode

func (p pathHeap) Len() int           { return len(p) }
func (p pathHeap) Less(i, j int) bool { return p[i].f < p[j].f }
func (p pathHeap) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *pathHeap) Push(x any)        { *p = append(*p, x.(pathNode)) }
func (p *pathHeap) Pop() any {
	old := *p
	n := len(old)
	it := old[n-1]
	*p = old[:n-1]
	return it
}

// findPath returns a list of (x,z) waypoints from (sx,sz) to (gx,gz) over
// walkable columns, or nil if the start itself is unwalkable / already at goal.
// If the goal can't be reached within the node budget, it returns a best-effort
// path to the explored column closest to the goal, so the mob still makes
// progress rather than freezing.
func findPath(w pather, prof malusProfile, sx, sz, gx, gz int) []pathPoint {
	if sx == gx && sz == gz {
		return nil
	}
	key := func(x, z int) int64 { return int64(x)<<32 | int64(uint32(z)) }
	h := func(x, z int) float64 { // octile-ish distance to the goal
		dx, dz := math.Abs(float64(gx-x)), math.Abs(float64(gz-z))
		return math.Max(dx, dz) + 0.41*math.Min(dx, dz)
	}
	// Memoise the (expensive) column queries: MobFeet scans a column and each
	// column is examined as a neighbour from up to 8 directions, so caching cuts
	// the world reads several-fold — the difference between ~7 ms and <1 ms.
	pw := &memoPather{w: w, feet: map[int64]int{}, walk: map[int64]bool{}, tall: map[int64]bool{}}

	open := &pathHeap{{sx, sz, h(sx, sz)}}
	gScore := map[int64]float64{key(sx, sz): 0}
	came := map[int64]pathPoint{}
	closed := map[int64]bool{}

	bestKey := key(sx, sz)
	bestH := h(sx, sz)
	expanded := 0

	for open.Len() > 0 && expanded < pathMaxNodes {
		cur := heap.Pop(open).(pathNode)
		ck := key(cur.x, cur.z)
		if closed[ck] {
			continue
		}
		closed[ck] = true
		expanded++

		if cur.x == gx && cur.z == gz {
			return reconstruct(came, pathPoint{gx, gz})
		}
		if hc := h(cur.x, cur.z); hc < bestH {
			bestH, bestKey = hc, ck
		}

		curFeet := pw.MobFeet(cur.x, cur.z)
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				nx, nz := cur.x+dx, cur.z+dz
				if maxi(abs(nx-sx), abs(nz-sz)) > pathMaxRange {
					continue
				}
				if !stepOK(pw, cur.x, cur.z, nx, nz, curFeet, dx != 0 && dz != 0) {
					continue
				}
				nk := key(nx, nz)
				if closed[nk] {
					continue
				}
				mal := prof[pathHazardKind(pw, nx, nz)]
				if mal < 0 {
					continue // impassable hazard for this mob — never route through it
				}
				step := 1.0
				if dx != 0 && dz != 0 {
					step = 1.41
				}
				ng := gScore[ck] + step + mal
				if g, ok := gScore[nk]; ok && ng >= g {
					continue
				}
				gScore[nk] = ng
				came[nk] = pathPoint{cur.x, cur.z}
				heap.Push(open, pathNode{nx, nz, ng + h(nx, nz)})
			}
		}
	}
	// Goal unreachable within budget: walk toward the closest column we saw.
	if bestKey == key(sx, sz) {
		return nil
	}
	return reconstruct(came, pathPoint{int(bestKey >> 32), int(int32(bestKey))})
}

func isCactus(s uint32) bool { return s >= worldgen.Cactus && s <= worldgen.Cactus+15 }
func isSweetBerry(s uint32) bool {
	return s >= worldgen.SweetBerryBush && s <= worldgen.SweetBerryBush+3
}

// hazardKind classifies the danger at a column's feet — the tachyne subset of
// vanilla PathType that matters here (lava/fire/cactus/berry; water is already
// impassable via Walkable). The per-mob malusProfile turns a kind into a cost.
type hazardKind int

const (
	hzNone hazardKind = iota
	hzLava
	hzFire
	hzCactus
	hzBerry
	nHazard
)

// pathHazardKind classifies a column by the block the mob would stand in (feet)
// or on (below) — WalkNodeEvaluator's block→PathType mapping.
func pathHazardKind(pw pather, x, z int) hazardKind {
	y := pw.MobFeet(x, z)
	feet := pw.Block(x, y, z)
	below := pw.Block(x, y-1, z)
	switch {
	case worldgen.IsLava(feet) || worldgen.IsLava(below):
		return hzLava
	case isCactus(feet) || isCactus(below):
		return hzCactus
	case isFire(feet):
		return hzFire
	case isSweetBerry(feet):
		return hzBerry
	}
	return hzNone
}

// malusProfile is a mob's movement penalty per hazard kind (vanilla
// Mob.getPathfindingMalus): negative = impassable (refuse the node), positive =
// extra cost paid only if there's no safer route.
type malusProfile [nHazard]float64

var (
	// defaultMalus — PathType table defaults: lava impassable, fire costly,
	// cactus impassable, berry a nuisance. Used by every non-fire-immune mob.
	defaultMalus = malusProfile{hzLava: -1, hzFire: 16, hzCactus: -1, hzBerry: 8}
	// striderMalus — striders walk on lava (LAVA 0, fire 0); they avoid water.
	striderMalus = malusProfile{hzLava: 0, hzFire: 0, hzCactus: -1, hzBerry: 8}
	// fireImmuneMalus — blaze/wither-skeleton/zombified-piglin/magma-cube/ghast/
	// zoglin: lava is costly-passable (8), fire is free.
	fireImmuneMalus = malusProfile{hzLava: 8, hzFire: 0, hzCactus: -1, hzBerry: 8}
)

// malusFor returns a mob type's pathfinding malus profile (the setPathfindingMalus
// overrides from the vanilla 1.21.5 entity classes).
func malusFor(etype int) malusProfile {
	switch etype {
	case entityStrider:
		return striderMalus
	case entityBlaze, entityMagmaCube, entityWitherSkeleton, entityZombifiedPiglin, entityGhast, entityZoglin:
		return fireImmuneMalus
	}
	return defaultMalus
}

// doorPather makes a closed WOODEN door passable to A* — a door-using mob
// (villager) plans a route through its own house door and opens it on arrival
// (updateVillagerDoors). A closed door's feet cell is already Walkable (it's dry
// land with no tree); the only barrier is TallObstacle flagging it a wall, so we
// clear that for wooden doors and leave iron/copper doors (and real fences) solid.
type doorPather struct{ w *world.World }

func (d doorPather) Walkable(x, z int) bool   { return d.w.Walkable(x, z) }
func (d doorPather) MobFeet(x, z int) int     { return d.w.MobFeet(x, z) }
func (d doorPather) Block(x, y, z int) uint32 { return d.w.Block(x, y, z) }
func (d doorPather) TallObstacle(x, z int) bool {
	if d.w.ClosedWoodenDoorFeet(x, z) {
		return false
	}
	return d.w.TallObstacle(x, z)
}

// memoPather caches per-column walkability lookups for the duration of one A*
// search (MobFeet in particular scans a column and is hit from many directions).
type memoPather struct {
	w    pather
	feet map[int64]int
	walk map[int64]bool
	tall map[int64]bool
}

func mpk(x, z int) int64 { return int64(x)<<32 | int64(uint32(z)) }

func (p *memoPather) MobFeet(x, z int) int {
	k := mpk(x, z)
	if v, ok := p.feet[k]; ok {
		return v
	}
	v := p.w.MobFeet(x, z)
	p.feet[k] = v
	return v
}
func (p *memoPather) Walkable(x, z int) bool {
	k := mpk(x, z)
	if v, ok := p.walk[k]; ok {
		return v
	}
	v := p.w.Walkable(x, z)
	p.walk[k] = v
	return v
}
func (p *memoPather) TallObstacle(x, z int) bool {
	k := mpk(x, z)
	if v, ok := p.tall[k]; ok {
		return v
	}
	v := p.w.TallObstacle(x, z)
	p.tall[k] = v
	return v
}
func (p *memoPather) Block(x, y, z int) uint32 { return p.w.Block(x, y, z) }

// stepOK reports whether a mob may step from (cx,cz) to (nx,nz): the destination
// must be walkable and not a tall obstacle, the height change within climb/fall
// limits, and — for a diagonal — both orthogonal cells must be clear so it can't
// cut a corner through a wall.
func stepOK(w pather, cx, cz, nx, nz, curFeet int, diag bool) bool {
	if !w.Walkable(nx, nz) || w.TallObstacle(nx, nz) {
		return false
	}
	if step := w.MobFeet(nx, nz) - curFeet; step > 1 || step < -pathMaxFall {
		return false
	}
	if diag {
		if !w.Walkable(cx, nz) || w.TallObstacle(cx, nz) ||
			!w.Walkable(nx, cz) || w.TallObstacle(nx, cz) {
			return false
		}
	}
	return true
}

// reconstruct walks the came-from chain back to the start, returning waypoints
// in travel order (start-exclusive: the first waypoint is the first step).
func reconstruct(came map[int64]pathPoint, end pathPoint) []pathPoint {
	key := func(p pathPoint) int64 { return int64(p.x)<<32 | int64(uint32(p.z)) }
	var rev []pathPoint
	cur := end
	for {
		rev = append(rev, cur)
		prev, ok := came[key(cur)]
		if !ok {
			break
		}
		cur = prev
	}
	// Reverse, dropping the start node (index len-1).
	out := make([]pathPoint, 0, len(rev))
	for i := len(rev) - 2; i >= 0; i-- {
		out = append(out, rev[i])
	}
	return out
}

func maxi(a, b int) int {
	if a > b {
		return a
	}
	return b
}
