package server

import "math"

// Behavior is an in-process, per-tick mob steering primitive — the high-frequency
// half of the living-world extension model. The other half, the NATS bus, is the
// CONTROL plane: it chooses which behavior a mob runs and can spawn mobs with one,
// but the 20-TPS steering math stays here in-process, where a network round-trip
// per tick would stall the authoritative tick loop. A Behavior returns the velocity
// the mob WANTS this tick; the hub blends it with momentum and caps it at speed.
//
// New primitives (flocking, pack hunting, river flow) implement this interface and
// register in behaviors below — then they're instantly spawnable/assignable over
// the bus with no further wiring.
type Behavior interface {
	steer(h *hub, m *mob) (vx, vz float64)
	name() string
}

// behaviors is the registry the bus resolves names against (spawn/behavior
// commands). Default mobs use "wander" — lone mobs milling about, no group
// mentality — so herding is opt-in, not forced on every server.
var behaviors = map[string]Behavior{
	"idle":    idleBehavior{},
	"wander":  wanderBehavior{},
	"herd":    herdBehavior{},
	"hostile": hostileBehavior{},
}

// idleBehavior stands still.
type idleBehavior struct{}

func (idleBehavior) name() string                        { return "idle" }
func (idleBehavior) steer(*hub, *mob) (float64, float64) { return 0, 0 }

// wanderBehavior ambles in a slowly-changing direction — the neutral default, so
// a vanilla-feeling server has solitary mobs wandering, no herd cohesion.
type wanderBehavior struct{}

func (wanderBehavior) name() string { return "wander" }
func (wanderBehavior) steer(h *hub, m *mob) (float64, float64) {
	const wander = 0.04
	vx := m.vx + (h.rng.Float64()*2-1)*wander
	vz := m.vz + (h.rng.Float64()*2-1)*wander
	if math.Hypot(vx, vz) < mobSpeed*0.5 { // nudge so they keep ambling, not freeze
		ang := h.rng.Float64() * 2 * math.Pi
		vx += math.Cos(ang) * mobSpeed
		vz += math.Sin(ang) * mobSpeed
	}
	return vx, vz
}

// herdBehavior is the first group primitive: cohesion toward the mob's herd goal,
// separation from crowded herd-mates, plus a little wander. A stray that drifts
// nearer another herd's goal defects to it, so membership follows proximity rather
// than being fixed for life.
type herdBehavior struct{}

func (herdBehavior) name() string { return "herd" }
func (herdBehavior) steer(h *hub, m *mob) (float64, float64) {
	const (
		sepRadius = 2.5   // closer than this counts as crowding
		cohesion  = 0.012 // pull toward the herd goal
		separate  = 0.05  // push off close herd-mates
		wander    = 0.012 // individual jitter
	)
	h.rejoinNearestHerd(m)
	hd := h.herds[m.herd]
	vx := (hd.x - m.x) * cohesion
	vz := (hd.z - m.z) * cohesion
	for _, o := range h.mobs {
		if o == m || o.etype != m.etype {
			continue
		}
		dx, dz := o.x-m.x, o.z-m.z
		if d2 := dx*dx + dz*dz; d2 < sepRadius*sepRadius && d2 > 1e-4 {
			inv := 1 / math.Sqrt(d2)
			vx -= dx * inv * separate
			vz -= dz * inv * separate
		}
	}
	vx += (h.rng.Float64()*2 - 1) * wander
	vz += (h.rng.Float64()*2 - 1) * wander
	return vx, vz
}

// rejoinNearestHerd lets a stray defect to whichever herd goal it is now closest
// to, so a cow that wanders between groups joins the one it ends up near. The
// margin is hysteresis — it must be meaningfully closer to switch, so a cow
// midway between two herds doesn't flip-flop every tick.
func (h *hub) rejoinNearestHerd(m *mob) {
	cur := h.herds[m.herd]
	best, bestD2 := m.herd, sq(cur.x-m.x)+sq(cur.z-m.z)
	for i, hd := range h.herds {
		if d2 := sq(hd.x-m.x) + sq(hd.z-m.z); d2 < bestD2-16 { // ~4 blocks closer to switch
			best, bestD2 = i, d2
		}
	}
	m.herd = best
}

// herdNear returns the index of the herd whose goal is nearest (x,z), creating a
// fresh herd rooted there if none exist yet — so a bus-spawned herd mob always
// has a goal to belong to.
func (h *hub) herdNear(x, z float64) int {
	if len(h.herds) == 0 {
		h.herds = append(h.herds, &herd{x: x, z: z})
		return 0
	}
	best, bestD2 := 0, math.Inf(1)
	for i, hd := range h.herds {
		if d2 := sq(hd.x-x) + sq(hd.z-z); d2 < bestD2 {
			best, bestD2 = i, d2
		}
	}
	return best
}

func sq(v float64) float64 { return v * v }
