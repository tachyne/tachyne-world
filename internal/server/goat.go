package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Goats ram (GoatAi: PrepareRamNearestTarget + RamTarget). Off its cooldown
// a goat picks the nearest thing worth ramming, walks to a spot four to
// seven blocks straight out from it along a clear line, lowers its head
// for twenty ticks, and charges at three times its walk: whatever it hits
// takes its attack damage and a shove scaled by its speed; a charge into
// a log, stone, packed ice or one of the harder ores snaps a horn off
// instead. Then it waits 600–6000 ticks (a screaming goat 100–300).

const (
	goatRamMin, goatRamMax = 4, 7
	goatRamPrepareTicks    = 20
	goatRamWalkSpeed       = 1.25
	goatRamChargeSpeed     = 3.0
	goatRamPrepareTimeout  = 160
	goatRamChargeTimeout   = 200
	goatRamForceAdult      = 2.5
	goatRamForceBaby       = 1.0
	goatRamHornOdds        = 10 // UNIHORN_CHANCE 0.1: one goat in ten spawns with a single horn
)

// goat ram phases
const (
	goatIdle int8 = iota
	goatWalking
	goatPreparing
	goatCharging
)

// goatRamCooldown is TIME_BETWEEN_RAMS (or the screamer's).
func (h *hub) goatRamCooldown(m *mob) int {
	if m.screaming {
		return 100 + h.rng.Intn(201)
	}
	return 600 + h.rng.Intn(5401)
}

// snapsGoatHorn is #snaps_goat_horn: the natural logs, stone, packed ice and
// the iron, coal, copper and emerald ores.
func snapsGoatHorn(s uint32) bool {
	name, ok := worldgen.StateName(s)
	if !ok {
		return false
	}
	switch name {
	case "stone", "packed_ice", "iron_ore", "coal_ore", "copper_ore", "emerald_ore":
		return true
	}
	return worldgen.IsLog(s)
}

// goatRamTarget is PrepareRamNearestTarget's pick: the closest of the goat's
// NEAREST_VISIBLE_LIVING_ENTITIES (within its 16-block follow range, in
// line of sight) that passes RAM_TARGET_CONDITIONS — never another goat,
// an armour stand only with mobGriefing (armour stands are not mobs here),
// and only a target inside the world border.
func (h *hub) goatRamTarget(players map[int32]*tracked, m *mob) (x, z float64, ok bool) {
	r := m.followRange()
	best := r * r
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator {
			continue
		}
		d2 := (t.x-m.x)*(t.x-m.x) + (t.y-m.y)*(t.y-m.y) + (t.z-m.z)*(t.z-m.z)
		if d2 < best && h.withinBorder(t.dim, t.x, t.z) && h.mobSees(m, t) {
			best, x, z, ok = d2, t.x, t.z, true
		}
	}
	h.grid().nearby(m.dim, m.x, m.z, r, func(o *mob) {
		if o == m || o.dying > 0 || o.etype == entityGoat {
			return
		}
		d2 := (o.x-m.x)*(o.x-m.x) + (o.y-m.y)*(o.y-m.y) + (o.z-m.z)*(o.z-m.z)
		if d2 < best && h.withinBorder(o.dim, o.x, o.z) && h.mobSeesMob(m, o) {
			best, x, z, ok = d2, o.x, o.z, true
		}
	})
	return
}

// goatRamStart is calculateRammingStartPosition: from the target's cell,
// walk out in each cardinal direction over clear ground for up to seven
// cells; a line at least four long is a candidate, and a random one wins.
func (h *hub) goatRamStart(m *mob, tx, tz float64) (blockPos, bool) {
	w := h.worldFor(m.dim)
	ty := int(math.Floor(m.y))
	clear := func(x, z int) bool {
		return !worldgen.Collides(w.At(x, ty, z)) && !worldgen.Collides(w.At(x, ty+1, z)) && worldgen.Collides(w.At(x, ty-1, z))
	}
	tX, tZ := int(math.Floor(tx)), int(math.Floor(tz))
	if !clear(tX, tZ) {
		return blockPos{}, false
	}
	var out []blockPos
	for _, d := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} {
		x, z := tX, tZ
		n := 0
		for i := 0; i < goatRamMax; i++ {
			if !clear(x+d[0], z+d[1]) {
				break
			}
			x, z = x+d[0], z+d[1]
			n++
		}
		if n >= goatRamMin {
			out = append(out, blockPos{x, ty, z})
		}
	}
	if len(out) == 0 {
		return blockPos{}, false
	}
	return out[h.rng.Intn(len(out))], true
}

// goatStep runs the ram state machine; returns whether it holds the goat.
func (h *hub) goatStep(players map[int32]*tracked, m *mob) bool {
	if m.ramCD > 0 {
		m.ramCD -= mobMoveInterval
		return false
	}
	switch m.ramPhase {
	case goatIdle:
		if m.loveTicks > 0 || m.tempted {
			return false // the RAM activity yields to courting and tempting
		}
		tx, tz, ok := h.goatRamTarget(players, m)
		if !ok {
			m.ramCD = 20 // look again in a second
			return false
		}
		start, ok := h.goatRamStart(m, tx, tz)
		if !ok {
			m.ramCD = h.goatRamCooldown(m)
			return false
		}
		m.ramPhase, m.ramStart, m.ramTX, m.ramTZ, m.ramTicks = goatWalking, start, tx, tz, 0
		fallthrough
	case goatWalking:
		m.ramTicks += mobMoveInterval
		sx, sz := float64(m.ramStart.x)+0.5, float64(m.ramStart.z)+0.5
		dx, dz := sx-m.x, sz-m.z
		if d := math.Hypot(dx, dz); d > 0.5 {
			if m.ramTicks > goatRamPrepareTimeout {
				h.goatRamAbort(m)
				return false
			}
			sp := m.moveSpeed() * goatRamWalkSpeed
			m.vx, m.vz = dx/d*sp, dz/d*sp
			m.rest = 0
			return true
		}
		m.ramPhase, m.ramTicks = goatPreparing, 0
		m.vx, m.vz = 0, 0
		h.playSoundDim(players, m.dim, goatSound(m, "prepare_ram"), sndNeutral, m.x, m.y, m.z, 1, 1)
		return true
	case goatPreparing:
		m.vx, m.vz = 0, 0
		m.yaw = float32(math.Atan2(-(m.ramTX-m.x), m.ramTZ-m.z) * 180 / math.Pi)
		m.ramTicks += mobMoveInterval
		if m.ramTicks < goatRamPrepareTicks {
			return true
		}
		m.ramPhase, m.ramTicks = goatCharging, 0
		// RamTarget.start: the charge line runs from here to the far edge of
		// the target's cell.
		dx, dz := m.ramTX-m.x, m.ramTZ-m.z
		if d := math.Hypot(dx, dz); d > 1e-6 {
			m.ramDX, m.ramDZ = dx/d, dz/d
		}
		return true
	case goatCharging:
		m.ramTicks += mobMoveInterval
		sp := m.moveSpeed() * goatRamChargeSpeed
		m.vx, m.vz = m.ramDX*sp, m.ramDZ*sp
		m.rest = 0
		// Hit something in front?
		force := goatRamForceAdult
		if m.baby {
			force = goatRamForceBaby
		}
		f3 := math.Min(math.Max(m.moveSpeed()/attrToStep*1.65, 0.2), 3.0)
		dmg := float32(hostileMelee(m))
		for _, t := range players {
			if t.dim == m.dim && !t.dead && t.gamemode != gmCreative && t.gamemode != gmSpectator &&
				math.Abs(t.x-m.x) < 0.9 && math.Abs(t.z-m.z) < 0.9 && math.Abs(t.y-m.y) < 1.5 {
				h.hurtFrom(players, t, dmg, dtMobAttackNoAggro, deathCause{by: mobDisplayName(m.etype)}, fromMob(m.x, m.z))
				h.knockbackScaled(t, m.x-m.ramDX, m.z-m.ramDZ, f3*force/0.4)
				h.playSoundDim(players, m.dim, goatSound(m, "ram_impact"), sndNeutral, m.x, m.y, m.z, 1, 1)
				h.goatRamFinish(m)
				return true
			}
		}
		for _, o := range h.mobs {
			if o == m || o.dim != m.dim || o.dying > 0 || o.etype == entityGoat {
				continue
			}
			if math.Abs(o.x-m.x) < 0.9 && math.Abs(o.z-m.z) < 0.9 && math.Abs(o.y-m.y) < 1.5 {
				h.hurtMobOf(players, o, float64(dmg), dtMobAttackNoAggro)
				kb := f3 * force * o.kbScale()
				o.vx, o.vz, o.kb, o.reroute = m.ramDX*kb, m.ramDZ*kb, 3, 0
				h.mobKnockVelocity(players, o)
				h.playSoundDim(players, m.dim, goatSound(m, "ram_impact"), sndNeutral, m.x, m.y, m.z, 1, 1)
				h.goatRamFinish(m)
				return true
			}
		}
		// A horn-snapping block ahead?
		w := h.worldFor(m.dim)
		ax, ay, az := int(math.Floor(m.x+m.ramDX)), int(math.Floor(m.y)), int(math.Floor(m.z+m.ramDZ))
		if snapsGoatHorn(w.At(ax, ay, az)) || snapsGoatHorn(w.At(ax, ay+1, az)) {
			h.playSoundDim(players, m.dim, goatSound(m, "ram_impact"), sndNeutral, m.x, m.y, m.z, 1, 1)
			if h.goatDropHorn(players, m) {
				h.playSoundOn(players, m.eid, m.dim, "minecraft:entity.goat.horn_break", sndNeutral, m.x, m.y, m.z, 1, 1)
			}
			h.goatRamFinish(m)
			return true
		}
		// Past the target's cell, or out of time: done.
		if (m.x-m.ramTX)*m.ramDX+(m.z-m.ramTZ)*m.ramDZ > 0.5 || m.ramTicks > goatRamChargeTimeout {
			h.goatRamFinish(m)
		}
		return true
	}
	return false
}

func (h *hub) goatRamAbort(m *mob) {
	m.ramPhase, m.vx, m.vz = goatIdle, 0, 0
	m.ramCD = h.goatRamCooldown(m)
}

func (h *hub) goatRamFinish(m *mob) {
	m.ramPhase, m.vx, m.vz = goatIdle, 0, 0
	m.ramCD = h.goatRamCooldown(m)
}

// goatSound picks the plain or screaming variant of a goat sound.
func goatSound(m *mob, name string) string {
	if m.screaming {
		return "minecraft:entity.goat.screaming." + name
	}
	return "minecraft:entity.goat." + name
}

// goatDropHorn is Goat.dropHorn: one of the horns it still has comes off as
// a goat horn item with a random instrument of its kind.
func (h *hub) goatDropHorn(players map[int32]*tracked, m *mob) bool {
	if m.hornsGone >= 2 {
		return false
	}
	m.hornsGone++
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(goatMeta(m))) // the horn vanishes from the model
	it := h.spawnItemIn(players, m.dim, itemGoatHorn, 1, m.x, m.y+0.5, m.z)
	if it != nil {
		it.instrument = int8(h.rng.Intn(4)) // ponder, sing, seek, feel
		if m.screaming {
			it.instrument += 4 // admire, call, yearn, dream
		}
		h.refreshItemMeta(players, it)
	}
	return true
}
