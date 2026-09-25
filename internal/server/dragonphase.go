package server

import (
	"container/heap"
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ender dragon's phase machine (EnderDragonPhaseManager and its eleven
// DragonPhaseInstances), run every tick from updateDragon.
//
// The phases are the fight. HOLDING_PATTERN flies a graph of 24 nodes —
// twelve on a ring of 60 about the portal, eight on a ring of 40, four on a
// ring of 20 — clockwise or back, and at the end of each leg rolls to land
// (1 in crystals+3) or to strafe the player nearest the portal. STRAFE_PLAYER
// flies at that player and looses a fireball once it has aimed for five
// ticks. LANDING_APPROACH routes to the podium, LANDING drops onto it,
// SITTING_SCANNING turns to face whoever is near, SITTING_ATTACKING roars,
// SITTING_FLAMING breathes a cloud — four breaths, then TAKEOFF — and a
// dragon hit hard enough while sitting takes off at once. CHARGING_PLAYER
// rushes a far player. DYING flies it back to the podium to die there.
//
// Phase instances keep their state across switches in vanilla (the manager
// caches one of each), so the fields that begin() does not reset — the two
// clockwise flags and the flame count — live on for the fight.

const (
	phaseHoldingPattern  = 0
	phaseStrafePlayer    = 1
	phaseLandingApproach = 2
	phaseLanding         = 3
	phaseTakeoff         = 4
	phaseSittingFlaming  = 5
	phaseSittingScanning = 6
	phaseSittingAttack   = 7
	phaseChargingPlayer  = 8
	phaseDying           = 9
	phaseHovering        = 10
)

// metaIndexDragonPhase is EnderDragon.DATA_PHASE (INT): after Mob's flags,
// with nothing between — the same on 26.2 and 26.3 (no AgeableMob).
const metaIndexDragonPhase = 16

func dragonPhaseMeta(eid int32, phase int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexDragonPhase)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(phase))
	return protocol.AppendU8(b, itemMetaEnd)
}

// dragonPoint is a node of the flight graph (pathfinder Node: whole blocks).
type dragonPoint struct{ x, y, z int }

func (a dragonPoint) dist(b dragonPoint) float64 {
	return math.Sqrt(float64(sqI(a.x-b.x) + sqI(a.y-b.y) + sqI(a.z-b.z)))
}

func sqI(v int) int { return v * v }

// dragonFlightSample is one DragonFlightHistory entry.
type dragonFlightSample struct {
	y   float64
	yaw float32
}

// dragonState is the dragon's brain: the phase and each phase's fields.
type dragonState struct {
	phase     int
	yRotA     float64
	target    [3]float64 // the phase's getFlyTargetLocation
	hasTarget bool
	path      []dragonPoint
	pathIdx   int

	holdCW, strafeCW bool  // the two phases' clockwise flags (kept across switches)
	fireballCharge   int   // STRAFE_PLAYER
	attackTarget     int32 // STRAFE_PLAYER: the player
	scanTime         int   // SITTING_SCANNING
	attackTicks      int   // SITTING_ATTACKING
	flameTicks       int   // SITTING_FLAMING
	flameCount       int   // …breaths this landing (kept; LANDING resets it)
	flameCloud       int32 // …the breath cloud it laid (discarded at end())
	chargeSince      int   // CHARGING_PLAYER: timeSinceCharge
	takeoffFirst     bool  // TAKEOFF: firstTick

	deathTime  int     // dragonDeathTime (tickDeath)
	dead       bool    // health reached 0: tickDeath is running
	sitDamage  float64 // sittingDamageReceived
	lastHealth int     // health at the end of the last tick (for sitDamage)

	hist     [64]dragonFlightSample // DragonFlightHistory
	histHead int
	histN    int

	nodes     [24]dragonPoint
	nodesMade bool
}

func (m *mob) dragon() *dragonState {
	if m.dragonAI == nil {
		m.dragonAI = &dragonState{phase: phaseHovering}
	}
	return m.dragonAI
}

// dragonNodeAdjacency is EnderDragon.findClosestNode's nodeAdjacency.
var dragonNodeAdjacency = [24]int{
	6146, 8197, 8202, 16404, 32808, 32848, 65696, 131392, 131712, 263424, 526848, 525313,
	1581057, 3166214, 2138120, 6373424, 4358208, 12910976, 9044480, 9706496,
	15216640, 13688832, 11763712, 8257536,
}

// dragonCrystalsAlive is EndDragonFight.aliveCrystals: the End's crystals.
func (h *hub) dragonCrystalsAlive() int {
	n := 0
	for _, c := range h.crystals {
		if c.dim == dimEnd {
			n++
		}
	}
	return n
}

// dragonPodium is getHeightmapPos(MOTION_BLOCKING_NO_LEAVES, podium): the
// top of the column at the portal's centre, the fight's origin.
func (h *hub) dragonPodium() dragonPoint {
	w := h.worldFor(dimEnd)
	if w == nil {
		return dragonPoint{0, worldgen.EndSurfaceY + 1, 0}
	}
	return dragonPoint{0, int(w.SurfaceY(0, 0)), 0}
}

// perchY is the height a landing dragon comes down to.
func (h *hub) perchY() float64 { return float64(h.dragonPodium().y) }

// dragonMakeNodes is findClosestNode's first-call node layout.
func (h *hub) dragonMakeNodes(d *dragonState) {
	if d.nodesMade {
		return
	}
	w := h.worldFor(dimEnd)
	for i := 0; i < 24; i++ {
		yAdj := 5
		var nx, nz int
		switch {
		case i < 12:
			a := 2 * (-math.Pi + math.Pi/12*float64(i))
			nx, nz = floorInt(60*math.Cos(a)), floorInt(60*math.Sin(a))
		case i < 20:
			a := 2 * (-math.Pi + math.Pi/8*float64(i-12))
			nx, nz = floorInt(40*math.Cos(a)), floorInt(40*math.Sin(a))
			yAdj += 10
		default:
			a := 2 * (-math.Pi + math.Pi/4*float64(i-20))
			nx, nz = floorInt(20*math.Cos(a)), floorInt(20*math.Sin(a))
		}
		top := 0
		if w != nil {
			top = int(w.SurfaceY(nx, nz))
		}
		d.nodes[i] = dragonPoint{nx, max(73, top+yAdj), nz}
	}
	d.nodesMade = true
}

// dragonClosestNode is findClosestNode(x, y, z): with no crystal left only
// the inner rings are flown.
func (h *hub) dragonClosestNode(d *dragonState, x, y, z float64) int {
	h.dragonMakeNodes(d)
	best, bestD := 0, 10000.0
	start := 0
	if h.dragonCrystalsAlive() == 0 {
		start = 12
	}
	p := dragonPoint{floorInt(x), floorInt(y), floorInt(z)}
	for i := start; i < 24; i++ {
		n := d.nodes[i]
		if dd := float64(sqI(n.x-p.x) + sqI(n.y-p.y) + sqI(n.z-p.z)); dd < bestD {
			best, bestD = i, dd
		}
	}
	return best
}

// dragonPathHeap is the open set (BinaryHeap by f).
type dragonPathHeap struct {
	idx []int
	f   *[24]float64
}

func (q dragonPathHeap) Len() int           { return len(q.idx) }
func (q dragonPathHeap) Less(i, j int) bool { return q.f[q.idx[i]] < q.f[q.idx[j]] }
func (q dragonPathHeap) Swap(i, j int)      { q.idx[i], q.idx[j] = q.idx[j], q.idx[i] }
func (q *dragonPathHeap) Push(x any)        { q.idx = append(q.idx, x.(int)) }
func (q *dragonPathHeap) Pop() any          { n := len(q.idx); v := q.idx[n-1]; q.idx = q.idx[:n-1]; return v }
func (q *dragonPathHeap) contains(i int) int {
	for at, x := range q.idx {
		if x == i {
			return at
		}
	}
	return -1
}

// dragonFindPath is EnderDragon.findPath: A* over the node graph from one
// node to another, ending at final when given; when the goal cannot be
// reached it goes as near as it can (nil when that is nowhere).
func (h *hub) dragonFindPath(d *dragonState, from, to int, final *dragonPoint) []dragonPoint {
	h.dragonMakeNodes(d)
	var g, f [24]float64
	var came [24]int
	var closed [24]bool
	for i := range came {
		came[i] = -1
	}
	goal := d.nodes[to]
	f[from] = d.nodes[from].dist(goal)
	open := &dragonPathHeap{f: &f}
	heap.Push(open, from)
	closest := from
	minIdx := 0
	if h.dragonCrystalsAlive() == 0 {
		minIdx = 12
	}
	build := func(end int) []dragonPoint {
		var out []dragonPoint
		for n := end; n >= 0; n = came[n] {
			out = append([]dragonPoint{d.nodes[n]}, out...)
		}
		if final != nil {
			out = append(out, *final)
		}
		return out
	}
	for open.Len() > 0 {
		cur := heap.Pop(open).(int)
		if d.nodes[cur] == goal {
			return build(cur)
		}
		if d.nodes[cur].dist(goal) < d.nodes[closest].dist(goal) {
			closest = cur
		}
		closed[cur] = true
		for n := minIdx; n < 24; n++ {
			if dragonNodeAdjacency[cur]&(1<<n) == 0 || closed[n] {
				continue
			}
			tg := g[cur] + d.nodes[cur].dist(d.nodes[n])
			at := open.contains(n)
			if at < 0 || tg < g[n] {
				came[n], g[n] = cur, tg
				f[n] = tg + d.nodes[n].dist(goal)
				if at >= 0 {
					heap.Fix(open, at)
				} else {
					heap.Push(open, n)
				}
			}
		}
	}
	if closest == from {
		return nil
	}
	return build(closest)
}

// setDragonPhase is EnderDragonPhaseManager.setPhase: the old phase ends,
// the new one begins, and the client is told (DATA_PHASE).
func (h *hub) setDragonPhase(players map[int32]*tracked, m *mob, phase int) {
	d := m.dragon()
	if d.phase == phase {
		return
	}
	if d.phase == phaseSittingFlaming && d.flameCloud != 0 { // DragonSittingFlamingPhase.end: the cloud goes
		delete(h.clouds, d.flameCloud)
		d.flameCloud = 0
	}
	d.phase = phase
	switch phase { // begin()
	case phaseHoldingPattern, phaseLandingApproach, phaseStrafePlayer, phaseTakeoff:
		d.path, d.pathIdx, d.hasTarget = nil, 0, false
		if phase == phaseStrafePlayer {
			d.fireballCharge, d.attackTarget = 0, 0
		}
		if phase == phaseTakeoff {
			d.takeoffFirst = true
		}
	case phaseLanding, phaseHovering, phaseDying:
		d.hasTarget = false
	case phaseChargingPlayer:
		d.hasTarget, d.chargeSince = false, 0
	case phaseSittingScanning:
		d.scanTime = 0
	case phaseSittingAttack:
		d.attackTicks = 0
	case phaseSittingFlaming:
		d.flameTicks = 0
		d.flameCount++
	}
	if players != nil {
		h.toDimEv(players, m.dim, metaEv(dragonPhaseMeta(m.eid, phase)))
	}
}

// dragonSitting is DragonPhaseInstance.isSitting.
func dragonSitting(phase int) bool {
	switch phase {
	case phaseSittingFlaming, phaseSittingScanning, phaseSittingAttack, phaseHovering:
		return true
	}
	return false
}

// dragonFlySpeed and dragonTurnSpeed are the phase's getFlySpeed and
// getTurnSpeed.
func dragonFlySpeed(phase int) float64 {
	switch phase {
	case phaseLanding:
		return 1.5
	case phaseChargingPlayer, phaseDying:
		return 3
	case phaseHovering:
		return 1
	}
	return 0.6
}

func dragonTurnSpeed(m *mob, phase int) float64 {
	rot := math.Hypot(m.vx, m.vz) + 1
	dist := math.Min(rot, 40)
	if phase == phaseLanding {
		return dist / rot
	}
	return 0.7 / dist / rot
}

// dragonNavigateNext is navigateToNextPathNode: the next node, flown to at a
// random height up to twenty above it.
func (h *hub) dragonNavigateNext(d *dragonState) {
	if d.pathIdx >= len(d.path) {
		return
	}
	n := d.path[d.pathIdx]
	d.pathIdx++
	d.target = [3]float64{float64(n.x), float64(n.y) + float64(h.rng.Float32())*20, float64(n.z)}
	d.hasTarget = true
}

// dragonRingNode folds a node index onto the ring the fight flies: the
// outer twelve while the fight has crystals, else the inner eight.
func dragonRingNode(i int, outer bool) int {
	if outer {
		i %= 12
		if i < 0 {
			i += 12
		}
		return i
	}
	i -= 12
	i &= 7
	return i + 12
}

func (d *dragonState) pathDone() bool { return d.path == nil || d.pathIdx >= len(d.path) }

// dragonDistToTarget is targetLocation.distanceToSqr(dragon) (0 with none).
func dragonDistToTarget(m *mob) float64 {
	d := m.dragon()
	if !d.hasTarget {
		return 0
	}
	return dist3sq(d.target[0], d.target[1], d.target[2], m.x, m.y, m.z)
}

// dragonNearestToPodium is getNearestPlayer(forCombat ignoreLineOfSight,
// dragon, podium): the survival player nearest the podium.
func (h *hub) dragonNearestToPodium(players map[int32]*tracked) *tracked {
	p := h.dragonPodium()
	var best *tracked
	bestD := math.MaxFloat64
	for _, t := range players {
		if t.dim != dimEnd || t.dead || !isSurvival(t.gamemode) {
			continue
		}
		if dd := dist3sq(t.x, t.y, t.z, float64(p.x), float64(p.y), float64(p.z)); dd < bestD {
			best, bestD = t, dd
		}
	}
	return best
}

// dragonPhaseTick is the current phase's doServerTick.
func (h *hub) dragonPhaseTick(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	switch d.phase {
	case phaseHoldingPattern:
		if dd := dragonDistToTarget(m); dd < 100 || dd > 22500 {
			h.dragonHoldingNewTarget(players, m)
		}
	case phaseStrafePlayer:
		h.dragonStrafeTick(players, m)
	case phaseLandingApproach:
		if dd := dragonDistToTarget(m); dd < 100 || dd > 22500 {
			h.dragonLandingApproachNewTarget(players, m)
		}
	case phaseLanding:
		if !d.hasTarget {
			p := h.dragonPodium()
			d.target, d.hasTarget = [3]float64{float64(p.x) + 0.5, float64(p.y), float64(p.z) + 0.5}, true
		}
		if dragonDistToTarget(m) < 1 {
			d.flameCount = 0 // SITTING_FLAMING.resetFlameCount
			h.setDragonPhase(players, m, phaseSittingScanning)
		}
	case phaseTakeoff:
		if !d.takeoffFirst && d.path != nil {
			p := h.dragonPodium()
			if dist3sq(float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, m.x, m.y, m.z) >= 100 {
				h.setDragonPhase(players, m, phaseHoldingPattern)
			}
		} else {
			d.takeoffFirst = false
			h.dragonTakeoffNewTarget(m)
		}
	case phaseSittingScanning:
		h.dragonScanTick(players, m)
	case phaseSittingAttack:
		if d.attackTicks++; d.attackTicks > 40 {
			h.setDragonPhase(players, m, phaseSittingFlaming)
		}
	case phaseSittingFlaming:
		h.dragonFlameTick(players, m)
	case phaseChargingPlayer:
		switch {
		case !d.hasTarget:
			h.setDragonPhase(players, m, phaseHoldingPattern)
		case d.chargeSince > 0:
			if d.chargeSince++; d.chargeSince > 10 {
				h.setDragonPhase(players, m, phaseHoldingPattern)
			}
		default:
			if dd := dragonDistToTarget(m); dd < 100 || dd > 22500 {
				d.chargeSince++
			}
		}
	case phaseDying:
		if !d.hasTarget {
			p := h.dragonPodium()
			d.target, d.hasTarget = [3]float64{float64(p.x) + 0.5, float64(p.y), float64(p.z) + 0.5}, true
		}
		if dd := dragonDistToTarget(m); dd < 100 || dd > 22500 {
			m.health = 0 // DragonDeathPhase: at the podium it dies
		} else {
			m.health = 1
		}
	case phaseHovering:
		if !d.hasTarget {
			d.target, d.hasTarget = [3]float64{m.x, m.y, m.z}, true
		}
	}
}

// dragonHoldingNewTarget is DragonHoldingPatternPhase.findNewTarget.
func (h *hub) dragonHoldingNewTarget(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	if d.path != nil && d.pathDone() {
		crystals := h.dragonCrystalsAlive()
		if h.rng.Intn(crystals+3) == 0 {
			h.setDragonPhase(players, m, phaseLandingApproach)
			return
		}
		p := h.dragonPodium()
		near := h.dragonNearestToPodium(players)
		distSqr := 64.0
		if near != nil {
			distSqr = dist3sq(float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, near.x, near.y, near.z) / 512
		}
		if near != nil && (h.rng.Intn(int(distSqr+2)) == 0 || h.rng.Intn(crystals+2) == 0) {
			h.dragonStrafe(players, m, near)
			return
		}
	}
	if d.pathDone() {
		cur := h.dragonClosestNode(d, m.x, m.y, m.z)
		next := cur
		if h.rng.Intn(8) == 0 {
			d.holdCW = !d.holdCW
			next = cur + 6
		}
		if d.holdCW {
			next++
		} else {
			next--
		}
		// aliveCrystals() >= 0: always the outer ring while there is a fight.
		next = dragonRingNode(next, true)
		d.path, d.pathIdx = h.dragonFindPath(d, cur, next, nil), 0
		if d.path != nil {
			d.pathIdx++ // advance()
		}
	}
	h.dragonNavigateNext(d)
}

// dragonStrafe is strafePlayer: STRAFE_PLAYER at that player (setTarget).
func (h *hub) dragonStrafe(players map[int32]*tracked, m *mob, t *tracked) {
	h.setDragonPhase(players, m, phaseStrafePlayer)
	d := m.dragon()
	d.attackTarget = t.p.eid
	cur := h.dragonClosestNode(d, m.x, m.y, m.z)
	to := h.dragonClosestNode(d, t.x, t.y, t.z)
	fx, fz := floorInt(t.x), floorInt(t.z)
	xd, zd := float64(fx)-m.x, float64(fz)-m.z
	ho := math.Min(0.4+math.Sqrt(xd*xd+zd*zd)/80-1, 10)
	final := dragonPoint{fx, floorInt(t.y + ho), fz}
	d.path, d.pathIdx = h.dragonFindPath(d, cur, to, &final), 0
	if d.path != nil {
		d.pathIdx++
		h.dragonNavigateNext(d)
	}
}

// dragonStrafeTick is DragonStrafePlayerPhase.doServerTick.
func (h *hub) dragonStrafeTick(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	t := players[d.attackTarget]
	if t == nil || t.dead || t.dim != m.dim {
		h.setDragonPhase(players, m, phaseHoldingPattern)
		return
	}
	if d.path != nil && d.pathDone() {
		xd, zd := t.x-m.x, t.z-m.z
		ho := math.Min(0.4+math.Sqrt(xd*xd+zd*zd)/80-1, 10)
		d.target, d.hasTarget = [3]float64{t.x, t.y + ho, t.z}, true
	}
	if dd := dragonDistToTarget(m); dd < 100 || dd > 22500 {
		if d.pathDone() {
			cur := h.dragonClosestNode(d, m.x, m.y, m.z)
			next := cur
			if h.rng.Intn(8) == 0 {
				d.strafeCW = !d.strafeCW
				next = cur + 6
			}
			if d.strafeCW {
				next++
			} else {
				next--
			}
			next = dragonRingNode(next, h.dragonCrystalsAlive() > 0)
			d.path, d.pathIdx = h.dragonFindPath(d, cur, next, nil), 0
			if d.path != nil {
				d.pathIdx++
			}
		}
		h.dragonNavigateNext(d)
	}
	if dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) >= 4096 || !h.dragonSees(m, t) {
		if d.fireballCharge > 0 {
			d.fireballCharge--
		}
		return
	}
	d.fireballCharge++
	ax, az := t.x-m.x, t.z-m.z
	if l := math.Hypot(ax, az); l > 0 {
		ax, az = ax/l, az/l
	}
	yaw := float64(m.yaw) * math.Pi / 180
	dot := math.Sin(yaw)*ax - math.Cos(yaw)*az
	angle := math.Acos(math.Max(-1, math.Min(1, dot)))*180/math.Pi + 0.5
	if d.fireballCharge >= 5 && angle >= 0 && angle < 10 {
		h.launchDragonFireball(players, m, t)
		d.fireballCharge = 0
		if d.path != nil {
			d.pathIdx = len(d.path)
		}
		h.setDragonPhase(players, m, phaseHoldingPattern)
	}
}

// dragonSees is the dragon's hasLineOfSight, from its eyes.
func (h *hub) dragonSees(m *mob, t *tracked) bool { return h.mobSees(m, t) }

// launchDragonFireball is the strafe's shot: from just ahead of the head,
// at the target's middle (level event 1017 is its sound).
func (h *hub) launchDragonFireball(players map[int32]*tracked, m *mob, t *tracked) {
	head := dragonPartOf(m, 0)
	yaw := float64(m.yaw) * math.Pi / 180
	vx, vz := -math.Sin(yaw), math.Cos(yaw) // getViewVector: the dragon looks back along its own flight
	sx, sy, sz := head.x-vx, head.y+head.h*0.5+0.5, head.z-vz
	dx, dy, dz := t.x-sx, (t.y+0.9)-sy, t.z-sz
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	h.levelEvent(players, m.dim, worldEventDragonFireball, floorInt(m.x), floorInt(m.y), floorInt(m.z), 0)
	a := h.launchProjectileIn(players, entityDragonFireball, m.dim, sx, sy, sz,
		dx/d*hurtingSpeed, dy/d*hurtingSpeed, dz/d*hurtingSpeed)
	a.shooter, a.dmg, a.breaks, a.breath = m.eid, 0, true, true
}

// dragonLandingApproachNewTarget is DragonLandingApproachPhase.findNewTarget.
func (h *hub) dragonLandingApproachNewTarget(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	if d.pathDone() {
		cur := h.dragonClosestNode(d, m.x, m.y, m.z)
		p := h.dragonPodium()
		var to int
		if t := h.dragonNearestToPodium(players); t != nil {
			ax, az := t.x, t.z
			if l := math.Hypot(ax, az); l > 0 {
				ax, az = ax/l, az/l
			}
			to = h.dragonClosestNode(d, -ax*40, 105, -az*40)
		} else {
			to = h.dragonClosestNode(d, 40, float64(p.y), 0)
		}
		d.path, d.pathIdx = h.dragonFindPath(d, cur, to, &p), 0
		if d.path != nil {
			d.pathIdx++
		}
	}
	h.dragonNavigateNext(d)
	if d.path != nil && d.pathDone() {
		h.setDragonPhase(players, m, phaseLanding)
	}
}

// dragonTakeoffNewTarget is DragonTakeoffPhase.findNewTarget: away from the
// podium the way its head looks, onto the ring.
func (h *hub) dragonTakeoffNewTarget(m *mob) {
	d := m.dragon()
	cur := h.dragonClosestNode(d, m.x, m.y, m.z)
	lx, _, lz := h.dragonHeadLook(m)
	to := dragonRingNode(h.dragonClosestNode(d, -lx*40, 105, -lz*40), h.dragonCrystalsAlive() > 0)
	d.path, d.pathIdx = h.dragonFindPath(d, cur, to, nil), 0
	if d.path != nil {
		d.pathIdx++ // advance()
		h.dragonNavigateNext(d)
	}
}

// dragonHeadLook is getHeadLookVector: LANDING and TAKEOFF tip the view
// by the podium's distance, a sitting dragon looks 45° down, the rest
// straight along its (backward) view.
func (h *hub) dragonHeadLook(m *mob) (float64, float64, float64) {
	xRot := 0.0
	switch ph := m.dragon().phase; {
	case ph == phaseLanding || ph == phaseTakeoff:
		p := h.dragonPodium()
		dist := math.Max(math.Sqrt(dist3sq(float64(p.x)+0.5, float64(p.y)+0.5, float64(p.z)+0.5, m.x, m.y, m.z))/4, 1)
		xRot = -(6 / dist) * 1.5 * 5
	case dragonSitting(ph):
		xRot = -45
	}
	xr, yr := xRot*math.Pi/180, -float64(m.yaw)*math.Pi/180
	return math.Sin(yr) * math.Cos(xr), -math.Sin(xr), math.Cos(yr) * math.Cos(xr)
}

// dragonScanTick is DragonSittingScanningPhase.doServerTick.
func (h *hub) dragonScanTick(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	d.scanTime++
	var near *tracked
	bestD := 20.0 * 20.0
	for _, t := range players {
		if t.dim != m.dim || t.dead || !isSurvival(t.gamemode) || math.Abs(t.y-m.y) > 10 {
			continue
		}
		if dd := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); dd <= bestD && h.dragonSees(m, t) {
			near, bestD = t, dd
		}
	}
	if near != nil {
		if d.scanTime > 25 {
			h.setDragonPhase(players, m, phaseSittingAttack)
			return
		}
		ax, az := near.x-m.x, near.z-m.z
		if l := math.Hypot(ax, az); l > 0 {
			ax, az = ax/l, az/l
		}
		yaw := float64(m.yaw) * math.Pi / 180
		angle := math.Acos(math.Max(-1, math.Min(1, math.Sin(yaw)*ax-math.Cos(yaw)*az)))*180/math.Pi + 0.5
		if angle < 0 || angle > 10 {
			head := dragonPartOf(m, 0)
			xa, za := near.x-head.x, near.z-head.z
			delta := clampF(wrapDegrees(180-math.Atan2(xa, za)*180/math.Pi-float64(m.yaw)), -100, 100)
			d.yRotA *= 0.8
			dist := math.Sqrt(xa*xa+za*za) + 1
			rot := dist
			if dist > 40 {
				dist = 40
			}
			d.yRotA += delta * (0.7 / dist / rot)
			m.yaw += float32(d.yRotA)
		}
		return
	}
	if d.scanTime >= 100 {
		var far *tracked
		farD := 150.0 * 150.0
		for _, t := range players {
			if t.dim != m.dim || t.dead || !isSurvival(t.gamemode) {
				continue
			}
			if dd := dist3sq(t.x, t.y, t.z, m.x, m.y, m.z); dd <= farD && h.dragonSees(m, t) {
				far, farD = t, dd
			}
		}
		h.setDragonPhase(players, m, phaseTakeoff)
		if far != nil {
			h.setDragonPhase(players, m, phaseChargingPlayer)
			d.target, d.hasTarget = [3]float64{far.x, far.y, far.z}, true
		}
	}
}

// dragonFlameTick is DragonSittingFlamingPhase.doServerTick: ten ticks in,
// a breath cloud of radius five where the head points; after two hundred,
// another scan — or, the fourth time, takeoff.
func (h *hub) dragonFlameTick(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	d.flameTicks++
	switch {
	case d.flameTicks >= 200:
		if d.flameCount >= 4 {
			h.setDragonPhase(players, m, phaseTakeoff)
		} else {
			h.setDragonPhase(players, m, phaseSittingScanning)
		}
	case d.flameTicks == 10:
		head := dragonPartOf(m, 0)
		lx, lz := head.x-m.x, head.z-m.z
		if l := math.Hypot(lx, lz); l > 0 {
			lx, lz = lx/l, lz/l
		}
		x, z := head.x+lx*5/2, head.z+lz*5/2
		y0 := head.y + head.h*0.5
		y := y0
		w := h.worldFor(m.dim)
		for isAnyAir(w.At(floorInt(x), floorInt(y), floorInt(z))) {
			if y--; y < 0 {
				y = y0
				break
			}
		}
		y = float64(floorInt(y) + 1)
		d.flameCloud = h.spawnBreathCloud(m.dim, x, y, z)
	}
}
