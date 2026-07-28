package server

import (
	"encoding/binary"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The evoker's spells.
//
// The evoker spawned, joined raids and dropped a totem, and never once cast
// anything — which is the whole of what an evoker is. Nothing summoned a vex
// either, though the entity id had been sitting in the registry all along.
//
// Two spells, on their own cadences, exactly as vanilla's goal pair runs them:
// fangs erupt from the ground toward whoever it is fighting, and a flight of
// three vexes is conjured when there are not already vexes about. Each has a
// wind-up before it lands, which is what gives a player time to close the gap
// or break line of sight.

var (
	entityEvokerFangs = entityID("evoker_fangs")

	// Fangs bite through armour like the magic they are, so this is the raw
	// figure vanilla deals rather than a weapon value.
	fangDamage = float32(6)
)

const (
	// SpellcasterUseSpellGoal timings, in ticks.
	fangWarmup      = 20  // wind-up before the fangs appear
	fangInterval    = 100 // between fang castings
	vexInterval     = 340 // between summonings
	fangLife        = 22  // how long a fang stands before sinking
	fangStrikeAfter = 8   // ticks after its own delay that a fang bites
	fangCloseRange  = 9.0 // squared: inside this the fangs come up in rings
	vexSummonCount  = 3
)

// evokerFang is one conjured fang: it waits out its delay, bites once, and
// sinks. delay counts down to the strike; life ends it.
type evokerFang struct {
	eid     int32
	dim     int
	x, y, z float64
	delay   int  // ticks until it bites (vanilla's per-fang warmup)
	bit     bool // it has already taken its one bite
	life    int
	owner   int32
}

// evokerCast runs both spell goals for one evoker. Called from the hostile
// update switch, so it ticks at the mob cadence rather than every tick.
func (h *hub) evokerCast(players map[int32]*tracked, m *mob) {
	now := h.tick.Load()
	t := h.nearestHuntable(players, m.dim, m.x, m.z, 16)
	if t == nil {
		return
	}
	// Vexes first: vanilla weights the summon down by how many are already
	// around, so a lone evoker conjures readily and a swarmed one rarely.
	if now >= m.vexNextAt {
		m.vexNextAt = now + vexInterval
		if h.rng.Intn(8)+1 > h.countMobsNear(entityVex, m.dim, m.x, m.z, 16) {
			h.summonVexes(players, m)
			return // one spell at a time
		}
	}
	if now >= m.fangNextAt {
		m.fangNextAt = now + fangInterval
		h.castFangs(players, m, t)
	}
}

// castFangs lays the fang pattern. Close in it is two rings around the
// evoker; at range it is a line walking out toward the target, each fang
// delayed a tick more than the last so the strike travels.
func (h *hub) castFangs(players map[int32]*tracked, m *mob, t *tracked) {
	minY := math.Min(t.y, m.y)
	maxY := math.Max(t.y, m.y) + 1
	bearing := math.Atan2(t.z-m.z, t.x-m.x)
	h.playSoundDim(players, m.dim, "minecraft:entity.evoker.prepare_attack", sndHostile, m.x, m.y, m.z, 1, 1)

	if dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) < fangCloseRange {
		for i := 0; i < 5; i++ {
			a := bearing + float64(i)*math.Pi*0.4
			h.placeFang(players, m, m.x+math.Cos(a)*1.5, m.z+math.Sin(a)*1.5, minY, maxY, fangWarmup)
		}
		for i := 0; i < 8; i++ {
			a := bearing + float64(i)*math.Pi*2/8 + math.Pi*2/5
			h.placeFang(players, m, m.x+math.Cos(a)*2.5, m.z+math.Sin(a)*2.5, minY, maxY, fangWarmup+3)
		}
		return
	}
	for i := 0; i < 16; i++ {
		reach := 1.25 * float64(i+1)
		h.placeFang(players, m, m.x+math.Cos(bearing)*reach, m.z+math.Sin(bearing)*reach,
			minY, maxY, fangWarmup+i)
	}
}

// placeFang finds the ground under (x,z) between maxY and minY and stands a
// fang on it. Vanilla searches DOWNWARD for the first block with a solid top
// face, which is why fangs climb stairs and skip over holes rather than
// hanging in the air.
func (h *hub) placeFang(players map[int32]*tracked, m *mob, x, z, minY, maxY float64, delay int) {
	w := h.worldFor(m.dim)
	bx, bz := int(math.Floor(x)), int(math.Floor(z))
	for y := int(math.Floor(maxY)); y >= int(math.Floor(minY))-1; y-- {
		below := w.At(bx, y-1, bz)
		if !worldgen.IsSolidFull(below) {
			continue
		}
		if worldgen.Collides(w.At(bx, y, bz)) {
			return // the standing space itself is blocked
		}
		f := &evokerFang{eid: h.allocEID(), dim: m.dim,
			x: x, y: float64(y), z: z, delay: delay, life: delay + fangLife, owner: m.eid}
		var uuid [16]byte
		binary.BigEndian.PutUint32(uuid[12:], uint32(f.eid))
		h.fangs = append(h.fangs, f)
		h.toNearbyEv(players, f.dim, f.x, f.z,
			entAdd(f.eid, entityEvokerFangs, uuid, f.x, f.y, f.z, float32(0), 0))
		return
	}
}

// updateFangs ticks every standing fang: it bites once, a moment after its
// own delay expires, then sinks when its life runs out.
func (h *hub) updateFangs(players map[int32]*tracked) {
	if len(h.fangs) == 0 {
		return
	}
	// Swap in a fresh slice rather than filtering in place: the same aliasing
	// trap the TNT tick documents — anything that appended a fang mid-loop
	// would be silently dropped.
	current := h.fangs
	h.fangs = nil
	for _, f := range current {
		f.delay--
		if !f.bit && f.delay <= -fangStrikeAfter {
			f.bit = true
			h.fangBite(players, f)
		}
		if f.life--; f.life <= 0 {
			h.toNearbyEv(players, f.dim, f.x, f.z, entGone(f.eid))
			continue
		}
		h.fangs = append(h.fangs, f)
	}
}

// fangBite hurts whatever is standing on the fang — players and mobs alike,
// but never the evoker that conjured it.
func (h *hub) fangBite(players map[int32]*tracked, f *evokerFang) {
	h.playSoundDim(players, f.dim, "minecraft:entity.evoker_fangs.attack", sndHostile, f.x, f.y, f.z, 1, 1)
	for _, t := range players {
		if t.dim != f.dim || t.dead || t.gamemode != gmSurvival {
			continue
		}
		if math.Abs(t.x-f.x) <= 0.7 && math.Abs(t.z-f.z) <= 0.7 && math.Abs(t.y-f.y) <= 2 {
			// indirect_magic bypasses armour, which is what makes an evoker
			// dangerous to a fully-kitted player. The tag says so, not this call.
			h.hurtBy(players, t, fangDamage, dtIndirectMagic,
				deathCause{key: causeMagic, by: mobDisplayName(entityEvoker)})
		}
	}
	for _, m := range h.mobs {
		if m.dim != f.dim || m.dying > 0 || m.eid == f.owner || m.etype == entityEvoker || m.etype == entityVex {
			continue // an evoker never bites its own kind with its own spell
		}
		if math.Abs(m.x-f.x) <= 0.7 && math.Abs(m.z-f.z) <= 0.7 && math.Abs(m.y-f.y) <= 2 {
			m.hurt(float64(fangDamage))
			if m.health <= 0 {
				h.killMob(players, m)
			}
		}
	}
}

// summonVexes conjures a flight of three, each with a limited life so an
// abandoned swarm eventually clears itself.
func (h *hub) summonVexes(players map[int32]*tracked, m *mob) {
	h.playSoundDim(players, m.dim, "minecraft:entity.evoker.prepare_summon", sndHostile, m.x, m.y, m.z, 1, 1)
	for i := 0; i < vexSummonCount; i++ {
		x := m.x + float64(-2+h.rng.Intn(5))
		z := m.z + float64(-2+h.rng.Intn(5))
		v := h.spawnSpecies(players, entityVex, m.dim, x, m.y+1, z)
		if v == nil {
			continue
		}
		v.hostile = true
		// Vanilla's setLimitedLife: 30-119 seconds, so a vex outlives the
		// fight it was summoned for but not the day.
		v.vexLife = 20 * (30 + h.rng.Intn(90))
	}
}

// updateVexLife expires summoned vexes. A vex with no limited life (spawned
// some other way) is left alone.
func (h *hub) updateVexLife(players map[int32]*tracked) {
	for _, m := range h.mobs {
		if m.vexLife <= 0 || m.dying > 0 {
			continue
		}
		if m.vexLife--; m.vexLife <= 0 {
			h.removeMob(players, m)
		}
	}
}

// dist3sq is dist3 without the square root, for vanilla's squared comparisons.
func dist3sq(x1, y1, z1, x2, y2, z2 float64) float64 {
	dx, dy, dz := x1-x2, y1-y2, z1-z2
	return dx*dx + dy*dy + dz*dz
}
