package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Snow golems (SnowGolem.aiStep + RangedAttackGoal): a trail of snow layers
// where they walk, a snowball every twenty ticks at the monster its target
// goal holds once it is within ten blocks, walking in at 1.25 while it is
// further (harmless to all but a blaze), and melting — one damage
// a tick — in the biomes and dimension whose SNOW_GOLEM_MELTS is set:
// deserts, savannas, badlands and the Nether.

const (
	snowGolemShootRange = 10.0 // RangedAttackGoal attackRadius
	snowGolemShootEvery = 20   // attackIntervalMin (= max)
	snowballSpeed       = 1.6
	snowballInaccuracy  = 12.0
)

var snowLayerState = worldgen.BlockBase("snow") // layers=1

// snowGolemMelts is the SNOW_GOLEM_MELTS environment attribute.
func (h *hub) snowGolemMelts(m *mob) bool {
	if m.dim == 1 {
		return true
	}
	if m.dim != 0 {
		return false
	}
	switch h.world.Gen().BiomeName(int(math.Floor(m.x)), int(math.Floor(m.z))) {
	case "minecraft:desert", "minecraft:savanna", "minecraft:savanna_plateau", "minecraft:windswept_savanna",
		"minecraft:badlands", "minecraft:wooded_badlands", "minecraft:eroded_badlands":
		return true
	}
	return false
}

// snowLayerCanSurvive is SnowLayerBlock.canSurvive.
func snowLayerCanSurvive(below uint32) bool {
	if name, _ := worldgen.StateName(below); name == "ice" || name == "packed_ice" || name == "barrier" {
		return false
	}
	if below == worldgen.SoulSand || isHoneyBlock(below) || below == worldgen.Mud {
		return true
	}
	return worldgen.IsSolidFull(below) || below == snowLayerState+7 // a full 8-layer snow stack
}

// snowGolemStep runs one mob update: melt, trail, shoot.
func (h *hub) snowGolemStep(players map[int32]*tracked, m *mob) {
	if m.dying > 0 {
		return
	}
	if h.snowGolemMelts(m) {
		h.hurtMobOf(players, m, float64(mobMoveInterval), dtOnFire) // 1 a tick
		if m.health <= 0 {
			h.killMob(players, m)
			return
		}
	}
	if h.rules.MobGriefing {
		w := h.worldFor(m.dim)
		for i := 0; i < 4; i++ {
			x := int(math.Floor(m.x + float64(i%2*2-1)*0.25))
			y := int(math.Floor(m.y))
			z := int(math.Floor(m.z + float64(i/2%2*2-1)*0.25))
			if w.At(x, y, z) != worldgen.Air || !snowLayerCanSurvive(w.At(x, y-1, z)) {
				continue
			}
			h.setBlockLive(players, m.dim, x, y, z, snowLayerState)
		}
	}
	target := h.snowGolemTarget(m)
	if m.attackCD > 0 {
		m.attackCD -= mobMoveInterval
		return
	}
	if target == nil || dist3(target.x, target.y, target.z, m.x, m.y, m.z) > snowGolemShootRange || !h.mobSeesMob(m, target) {
		return // RangedAttackGoal: in its radius and in sight
	}
	h.snowGolemShoot(players, m, target)
	m.attackCD = snowGolemShootEvery
}

// snowGolemTarget is SnowGolem's NearestAttackableTargetGoal<Mob>(10, mustSee,
// Enemy): now and then (one in ten ticks) the nearest monster in sight
// within its FOLLOW_RANGE of 16, held as TargetGoal.canContinueToUse holds
// it — in range, and seen within the last sixty ticks.
func (h *hub) snowGolemTarget(m *mob) *mob {
	r := m.followRange()
	if t := h.mobs[m.snowTarget]; t != nil && t.hostile && t.dying == 0 && t.dim == m.dim &&
		dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= r {
		if h.mobSeesMob(m, t) {
			m.snowUnseen = 0
			return t
		}
		if m.snowUnseen += mobMoveInterval; m.snowUnseen <= targetUnseenMemory {
			return t
		}
	}
	m.snowTarget, m.snowUnseen = 0, 0
	if h.rng.Intn(10/mobMoveInterval) != 0 {
		return nil
	}
	var target *mob
	best := r
	h.grid().nearby(m.dim, m.x, m.z, r, func(o *mob) {
		if !o.hostile || o.dying > 0 || o.etype == entitySnowGolem {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best && h.mobSeesMob(m, o) {
			best, target = d, o
		}
	})
	if target != nil {
		m.snowTarget = target.eid
	}
	return target
}

// snowGolemChaseStep is RangedAttackGoal(1.25, 20, 10) moving the golem: a
// target out of its ten-block radius, or not yet seen for five ticks, is
// walked toward at 1.25; one in reach and in view is shot from where it
// stands. It reports whether it holds the golem.
func (h *hub) snowGolemChaseStep(m *mob) bool {
	t := h.mobs[m.snowTarget]
	if t == nil || t.dying > 0 || t.dim != m.dim {
		m.snowSeeTime = 0
		return false
	}
	if h.mobSeesMob(m, t) {
		m.snowSeeTime += mobMoveInterval
	} else {
		m.snowSeeTime = 0
	}
	if dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= snowGolemShootRange && m.snowSeeTime >= 5 {
		m.vx, m.vz = 0, 0 // stop navigation
		m.rest = 0
		return true
	}
	h.steerTo(m, t.x, t.z, 1.25)
	return true
}

// snowGolemShoot is SnowGolem.performRangedAttack.
func (h *hub) snowGolemShoot(players map[int32]*tracked, m *mob, t *mob) {
	ox, oy, oz := m.x, m.y+1.1, m.z
	dx, dz := t.x-ox, t.z-oz
	dy := (t.y + mobEyeHeight(t)) - oy
	dy += math.Hypot(dx, dz) * 0.2
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	dev := 0.0172275 * snowballInaccuracy
	tri := func() float64 { return dev * (h.rng.Float64() - h.rng.Float64()) }
	a := h.launchProjectileIn(players, entitySnowball, m.dim, ox, oy, oz,
		(dx/d+tri())*snowballSpeed, (dy/d+tri())*snowballSpeed, (dz/d+tri())*snowballSpeed)
	a.breaks, a.mobShot, a.shooter = true, true, m.eid
	m.yaw = float32(math.Atan2(-dx, dz) * 180 / math.Pi)
	h.toTracking(players, m.eid, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSoundDim(players, m.dim, "minecraft:entity.snow_golem.shoot", sndNeutral, m.x, m.y, m.z, 1, 0.4/(h.rng.Float32()*0.4+0.8))
}

// mobEyeHeight is roughly the mob's eye level above its feet.
