package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Snow golems (SnowGolem.aiStep + RangedAttackGoal): a trail of snow layers
// where they walk, a snowball every twenty ticks at the nearest hostile
// within ten blocks (harmless to all but a blaze), and melting — one damage
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
	if name, _ := worldgen.StateName(below); name == "minecraft:ice" || name == "minecraft:packed_ice" || name == "minecraft:barrier" {
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
	if m.attackCD > 0 {
		m.attackCD -= mobMoveInterval
		return
	}
	var target *mob
	best := snowGolemShootRange * snowGolemShootRange
	for _, o := range h.mobs {
		if !o.hostile || o.dying > 0 || o.dim != m.dim || o.etype == entitySnowGolem {
			continue
		}
		if d2 := dist3sq(o.x, o.y, o.z, m.x, m.y, m.z); d2 < best {
			best, target = d2, o
		}
	}
	if target == nil {
		return
	}
	h.snowGolemShoot(players, m, target)
	m.attackCD = snowGolemShootEvery
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
	h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
	h.playSoundDim(players, m.dim, "minecraft:entity.snow_golem.shoot", sndNeutral, m.x, m.y, m.z, 1, 0.4/(h.rng.Float32()*0.4+0.8))
}

// mobEyeHeight is roughly the mob's eye level above its feet.
func mobEyeHeight(m *mob) float64 { return m.box().h * 0.85 }
