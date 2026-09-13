package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Mobs that keep clear of other mobs (AvoidEntityGoal, the mob-class
// registrations): a skeleton backs off from a wolf, a creeper from a cat
// or ocelot, a spider from an armadillo that has not rolled up, a rabbit
// from a wolf or any monster, a fox from a wild wolf or a polar bear, a
// dolphin from a guardian, the illagers from a creaking, and a wild wolf
// from a llama it judges too strong. Within the goal's range the mob
// picks a spot up to sixteen blocks away on the far side and walks there
// — sprinting while the threat is within seven — then looks again.

const (
	avoidPosAway  = 16.0 // DefaultRandomPos.getPosAway horizontal reach
	avoidSprintSq = 49.0 // tick(): sprint inside 7 blocks
	avoidTries    = 10   // random position attempts
	avoidGiveUp   = 100  // updates before the path is considered done
)

type avoidRule struct {
	dist         float64
	walk, sprint float64
	from         func(h *hub, m, o *mob) bool // is o something m avoids?
}

func isMonster(o *mob) bool { // the Monster class: hostiles that are not slimes, ghasts or phantoms
	return o.hostile && o.etype != entitySlime && o.etype != entityMagmaCube && o.etype != entityGhast && o.etype != entityPhantom
}

var avoidRules = map[int][]avoidRule{}

// avoidPlayerRules are the AvoidEntityGoal<Player> registrations: the
// same goal aimed at survival and adventure players (NO_CREATIVE_OR_
// SPECTATOR). when gates it on the mob's own state.
type avoidPlayerRule struct {
	dist         float64
	walk, sprint float64
	when         func(m *mob) bool
}

var avoidPlayerRules = map[int]avoidPlayerRule{
	entityRabbit: {8, 2.2, 2.2, func(m *mob) bool { return true }},       // RabbitAvoidEntityGoal<Player>
	entityFox:    {16, 1.6, 1.4, func(m *mob) bool { return true }},      // (trust and defending: open)
	entityOcelot: {16, 0.8, 1.33, func(m *mob) bool { return !m.tamed }}, // OcelotAvoidEntityGoal: !isTrusting
	entityCat:    {16, 0.8, 1.33, func(m *mob) bool { return !m.tamed }}, // CatAvoidEntityGoal: !isTame
	entityEvoker: {8, 0.6, 1.0, func(m *mob) bool { return true }},       // Evoker: keeps its distance
}

func init() {
	wolf := func(h *hub, m, o *mob) bool { return o.etype == entityWolf }
	wildWolf := func(h *hub, m, o *mob) bool { return o.etype == entityWolf && !o.tamed }
	cat := func(h *hub, m, o *mob) bool { return o.etype == entityCat || o.etype == entityOcelot }
	armadillo := func(h *hub, m, o *mob) bool { return o.etype == entityArmadillo && o.armState != 2 } // !isScared
	creaking := func(h *hub, m, o *mob) bool { return o.etype == entityCreaking }
	guardian := func(h *hub, m, o *mob) bool { return o.etype == entityGuardian || o.etype == entityElderGuardian }
	polarBear := func(h *hub, m, o *mob) bool { return o.etype == entityPolarBear }
	monster := func(h *hub, m, o *mob) bool { return isMonster(o) }
	llama := func(h *hub, m, o *mob) bool { // WolfAvoidEntityGoal: a wild wolf, strength ≥ nextInt(5)
		return o.etype == entityLlama && !m.tamed && int(o.strength) >= h.rng.Intn(5)
	}
	for _, e := range []int{entitySkeleton, entityStray, entityBogged} {
		avoidRules[e] = []avoidRule{{6, 1.0, 1.2, wolf}}
	}
	avoidRules[entityCreeper] = []avoidRule{{6, 1.0, 1.2, cat}}
	avoidRules[entitySpider] = []avoidRule{{6, 1.0, 1.2, armadillo}}
	avoidRules[entityCaveSpider] = []avoidRule{{6, 1.0, 1.2, armadillo}}
	avoidRules[entityWolf] = []avoidRule{{24, 1.5, 1.5, llama}}
	avoidRules[entityDolphin] = []avoidRule{{8, 1.0, 1.0, guardian}}
	avoidRules[entityFox] = []avoidRule{{8, 1.6, 1.4, wildWolf}, {8, 1.6, 1.4, polarBear}}
	avoidRules[entityRabbit] = []avoidRule{{10, 2.2, 2.2, wolf}, {4, 2.2, 2.2, monster}}
	avoidRules[entityIllusioner] = []avoidRule{{8, 1.0, 1.2, creaking}}
	avoidRules[entityPillager] = []avoidRule{{8, 1.0, 1.2, creaking}}
	avoidRules[entityEvoker] = []avoidRule{{8, 0.6, 1.0, creaking}}
}

// avoidScan is canUse: the nearest avoided mob (or player) within the
// rule's range (and three blocks of height), then a spot away from it.
func (h *hub) avoidScan(players map[int32]*tracked, m *mob) {
	if m.avoidLeft > 0 || m.panic > 0 || m.kb > 0 {
		return
	}
	if pr, ok := avoidPlayerRules[m.etype]; ok && pr.when(m) && !m.tempted { // a tempted cat holds its nerve (TemptGoal shares the priority)
		var threat *tracked
		bestD2 := pr.dist * pr.dist
		for _, t := range players {
			if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator || math.Abs(t.y-m.y) > 3 {
				continue
			}
			if d2 := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z); d2 < bestD2 {
				threat, bestD2 = t, d2
			}
		}
		if threat != nil {
			if fx, fz, ok := h.posAwayFrom(m, threat.x, threat.z); ok {
				m.avoidEID, m.avoidX, m.avoidZ, m.avoidLeft = threat.p.eid, fx, fz, avoidGiveUp
				m.avoidWalk, m.avoidSprint = pr.walk, pr.sprint
				m.avoidPlayer = true
				m.hasTarget = false
				m.rest = 0
				return
			}
		}
	}
	rules := avoidRules[m.etype]
	if len(rules) == 0 {
		return
	}
	maxR := 0.0
	for _, r := range rules {
		maxR = math.Max(maxR, r.dist)
	}
	var threat *mob
	var rule avoidRule
	bestD2 := math.Inf(1)
	h.grid().nearby(m.dim, m.x, m.z, maxR, func(o *mob) {
		if o == m || o.dying > 0 || math.Abs(o.y-m.y) > 3 {
			return
		}
		d2 := (o.x-m.x)*(o.x-m.x) + (o.z-m.z)*(o.z-m.z)
		if d2 >= bestD2 {
			return
		}
		for _, r := range rules {
			if d2 <= r.dist*r.dist && r.from(h, m, o) {
				threat, rule, bestD2 = o, r, d2
				return
			}
		}
	})
	if threat == nil {
		return
	}
	fx, fz, ok := h.posAwayFrom(m, threat.x, threat.z)
	if !ok {
		return
	}
	m.avoidEID, m.avoidX, m.avoidZ, m.avoidLeft = threat.eid, fx, fz, avoidGiveUp
	m.avoidWalk, m.avoidSprint = rule.walk, rule.sprint
	m.avoidPlayer = false
	m.hasTarget = false // the avoid goal outranks the attack goals (start(): a wolf-shy wolf drops its target)
	m.rest = 0
}

// posAwayFrom is DefaultRandomPos.getPosAway: a walkable spot up to
// sixteen blocks off, generally away from the threat, and no closer to it
// than the mob already is.
func (h *hub) posAwayFrom(m *mob, tx, tz float64) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	ax, az := m.x-tx, m.z-tz
	if d := math.Hypot(ax, az); d > 1e-6 {
		ax, az = ax/d, az/d
	} else {
		ang := h.rng.Float64() * 2 * math.Pi
		ax, az = math.Cos(ang), math.Sin(ang)
	}
	curD2 := (m.x-tx)*(m.x-tx) + (m.z-tz)*(m.z-tz)
	for i := 0; i < avoidTries; i++ {
		ang := (h.rng.Float64() - 0.5) * math.Pi / 1.5 // within ±60° of straight away
		dist := 6 + h.rng.Float64()*(avoidPosAway-6)
		dx := (ax*math.Cos(ang) - az*math.Sin(ang)) * dist
		dz := (ax*math.Sin(ang) + az*math.Cos(ang)) * dist
		fx, fz := m.x+dx, m.z+dz
		// A standable spot within seven blocks of the mob's own level (the
		// vertical reach), not the column's surface far above a cave.
		bx, bz, by := int(math.Floor(fx)), int(math.Floor(fz)), int(math.Floor(m.y))
		feet := w.MobFeetFrom(bx, bz, by)
		if feet-by > 7 || by-feet > 7 || !worldgen.Collides(w.At(bx, feet-1, bz)) ||
			worldgen.Collides(w.At(bx, feet, bz)) || worldgen.Collides(w.At(bx, feet+1, bz)) {
			continue
		}
		if (fx-tx)*(fx-tx)+(fz-tz)*(fz-tz) < curD2 {
			continue
		}
		return fx, fz, true
	}
	return 0, 0, false
}

// avoidStep walks the path: sprint pace while the threat is within seven
// blocks, walking pace beyond; done at the spot or when the path times
// out. Returns whether it holds the mob.
func (h *hub) avoidStep(players map[int32]*tracked, m *mob) bool {
	if m.avoidLeft <= 0 {
		return false
	}
	m.avoidLeft--
	dx, dz := m.avoidX-m.x, m.avoidZ-m.z
	d := math.Hypot(dx, dz)
	if d < 1 || m.avoidLeft <= 0 {
		m.avoidLeft = 0
		return false
	}
	speed := m.avoidWalk
	var tx, tz float64
	near := false
	if m.avoidPlayer {
		if t := h.playersRef[m.avoidEID]; t != nil {
			tx, tz, near = t.x, t.z, true
		}
	} else if o := h.mobs[m.avoidEID]; o != nil {
		tx, tz, near = o.x, o.z, true
	}
	if near && (tx-m.x)*(tx-m.x)+(tz-m.z)*(tz-m.z) < avoidSprintSq {
		speed = m.avoidSprint
	}
	sp := m.moveSpeed() * speed
	m.vx, m.vz = dx/d*sp, dz/d*sp
	m.hasTarget = false
	m.rest = 0
	return true
}
