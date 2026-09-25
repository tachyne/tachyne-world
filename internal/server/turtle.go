package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Turtles (Turtle.TurtleBreedGoal / TurtleLayEggGoal, TurtleEggBlock):
// breeding gives one parent an egg rather than a hatchling; she carries it
// home — the beach she came from — and lays one to four eggs on sand there
// after ten seconds of digging; the eggs crack twice and hatch on random
// ticks, mostly in the hour before dawn, into hatchlings whose home is the
// nest.

const (
	turtleHomeReach = 9.0 // closerToCenterThan(homePos, 9)
	turtleLayTicks  = 200 // layEggCounter > adjustedTickDelay(200)
	turtleLoveAfter = 600 // setInLoveTime(600) after laying — a fresh courtship cooldown
	turtleHomeSpeed = 1.0 // MoveToBlockGoal speed for the egg goal
	turtleEggSpread = 0.2
)

// turtleEggBase is the first turtle_egg state (eggs=1, hatch=0); a state
// is base + (eggs-1)*3 + hatch.
var turtleEggBase, _ = worldgen.BlockRange("turtle_egg")

// turtleEggState builds an egg block state.
func turtleEggState(eggs, hatch int) uint32 {
	return turtleEggBase + uint32((eggs-1)*3+hatch)
}

// turtleEggOf decodes an egg block state.
func turtleEggOf(s uint32) (eggs, hatch int, ok bool) {
	if s < turtleEggBase || s > turtleEggBase+11 {
		return 0, 0, false
	}
	n := int(s - turtleEggBase)
	return n/3 + 1, n % 3, true
}

// turtleEggAfterPlayerBreak is TurtleEggBlock.playerDestroy → decreaseEggs:
// a survival player mining a clutch takes ONE egg and leaves the rest, so the
// cell comes back with a count one lower (hatch progress kept). A single egg
// just goes. ok is false for anything that is not a clutch of two or more.
func turtleEggAfterPlayerBreak(broken uint32) (uint32, bool) {
	eggs, hatch, ok := turtleEggOf(broken)
	if !ok || eggs <= 1 {
		return 0, false
	}
	return turtleEggState(eggs-1, hatch), true
}

// turtleEggPlayerBroken is the sound half of decreaseEggs, which plays for
// every egg a survival player breaks — the last one included.
func (h *hub) turtleEggPlayerBroken(players map[int32]*tracked, e evBlock) {
	if !isTurtleEgg(e.broken) {
		return
	}
	if t := players[e.by]; t == nil || !isSurvival(t.gamemode) {
		return // creative skips playerDestroy
	}
	h.playSoundDim(players, e.dim, "minecraft:block.turtle_egg.break", sndBlock,
		float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5, 0.7, 0.9+h.rng.Float32()*0.2)
}

const (
	turtleWaterSearch = 24.0 // TurtleGoToWaterGoal's MoveToBlockGoal(…, 24)
	turtleHomeFar     = 64.0 // GoHome: only from more than this far out
	turtleHomeOdds    = 700  // …and one roll in seven hundred ticks
)

// isSandFloor is TurtleEggBlock.isSand: the #sand tag.
func isSandFloor(s uint32) bool {
	return s == worldgen.Sand || s == worldgen.RedSand || s == worldgen.BlockBase("suspicious_sand")
}

// turtleStep runs a turtle's egg errand: with an egg, head home; at home and
// on sand, dig for two hundred ticks and lay. Returns whether the errand
// holds the turtle this update.
func (h *hub) turtleStep(players map[int32]*tracked, m *mob) bool {
	if m.home == (blockPos{}) {
		m.home = blockPos{int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))} // where it started is its beach
	}
	if !m.hasEgg || m.baby {
		m.layCounter = 0
		return h.turtleWaterStep(m) || h.turtleTravelStep(m)
	}
	hx, hz := float64(m.home.x)+0.5, float64(m.home.z)+0.5
	dx, dz := hx-m.x, hz-m.z
	if d := math.Hypot(dx, dz); d > turtleHomeReach {
		sp := m.moveSpeed() * turtleHomeSpeed
		m.vx, m.vz = dx/d*sp, dz/d*sp
		m.rest, m.layCounter = 0, 0
		return true
	}
	w := h.worldFor(m.dim)
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	if worldgen.HoldsWater(w.At(fx, fy, fz)) || !isSandFloor(w.At(fx, fy-1, fz)) || w.At(fx, fy, fz) != worldgen.Air {
		m.layCounter = 0
		return false // not a nest spot: wander until it is
	}
	m.vx, m.vz = 0, 0
	if m.layCounter == 0 {
		m.layCounter = mobMoveInterval
		h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(turtleMeta(m))) // setLayingEgg(true): the digging pose
		return true
	}
	m.layCounter += mobMoveInterval
	if m.layCounter <= turtleLayTicks {
		return true
	}
	h.setBlockLive(players, m.dim, fx, fy, fz, turtleEggState(1+h.rng.Intn(4), 0))
	h.playSoundDim(players, m.dim, "minecraft:entity.turtle.lay_egg", sndBlock, float64(fx)+0.5, float64(fy), float64(fz)+0.5, 0.3, 0.9+h.rng.Float32()*0.2)
	m.hasEgg, m.layCounter = false, 0
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(turtleMeta(m)))
	m.breedCD = turtleLoveAfter
	return true
}

// turtleEggRandomTick is TurtleEggBlock.randomTick: on sand, in the hour
// before dawn (or one tick in five hundred), the clutch cracks a stage;
// past the second crack it hatches its count of hatchlings.
func (h *hub) turtleEggRandomTick(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	eggs, hatch, ok := turtleEggOf(state)
	if !ok {
		return false
	}
	w := h.worldFor(dim)
	if !isSandFloor(w.At(x, y-1, z)) {
		return true
	}
	// shouldUpdateHatchLevel reads TURTLE_EGG_HATCH_CHANCE from the day
	// timeline: 1.0 from day tick 21062 to 21905 (just before dawn), 0.002
	// the rest of the day.
	chance := float32(0.002)
	if t := h.dayTime.Load() % 24000; t >= 21062 && t < 21905 {
		chance = 1
	}
	if h.rng.Float32() >= chance {
		return true
	}
	cx, cy, cz := float64(x)+0.5, float64(y), float64(z)+0.5
	if hatch < 2 {
		h.levelEvent(players, dim, worldEventEggCrack, x, y, z, 0) // TurtleEggBlock.crack: sound + shell particles
		h.setBlockLive(players, dim, x, y, z, turtleEggState(eggs, hatch+1))
		return true
	}
	h.playSoundDim(players, dim, "minecraft:entity.turtle.egg_hatch", sndBlock, cx, cy, cz, 0.7, 0.9+h.rng.Float32()*0.2)
	h.setBlockLive(players, dim, x, y, z, worldgen.Air)
	for i := 0; i < eggs; i++ {
		baby := h.spawnMobIn(players, entityTurtle, dim, float64(x)+0.3+float64(i)*turtleEggSpread, float64(y), float64(z)+0.3)
		if baby == nil {
			continue
		}
		baby.baby, baby.growLeft = true, growUpTicks
		baby.home = blockPos{x, y, z}
		h.applySpecies(players, baby)
		h.toTracking(players, baby.eid, dim, baby.x, baby.z, metaEv(babyMeta(baby.eid, true)))
	}
	return true
}

// turtleWaterStep is TurtleGoToWaterGoal + TurtleGoHomeGoal: a turtle out of
// the water heads back to it (a hatchling at double pace, which is what gets
// the babies off the beach), and now and then a grown one that has drifted
// more than sixty-four blocks from the beach it was born on swims home.
func (h *hub) turtleWaterStep(m *mob) bool {
	if m.dying != 0 || m.panic > 0 || m.tempted {
		return false
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	if !h.inWater(m.dim, m.x, m.y+0.2, m.z) {
		// GoToWater: search 24 blocks, as the goal does.
		if x, z, ok := h.nearestWaterColumn(m, turtleWaterSearch); ok {
			sp := m.moveSpeed()
			if m.baby {
				sp *= 2 // the goal's speed modifier for a hatchling
			}
			dx, dz := x-m.x, z-m.z
			if d := math.Hypot(dx, dz); d > 1 {
				m.vx, m.vz = dx/d*sp, dz/d*sp
				m.rest = 0
				return true
			}
		}
		return false
	}
	if m.baby || m.home == (blockPos{}) {
		return false
	}
	// GoHome: one roll in seven hundred ticks, and only from far away.
	hx, hz := float64(m.home.x)+0.5, float64(m.home.z)+0.5
	if math.Hypot(hx-m.x, hz-m.z) <= turtleHomeFar {
		return false
	}
	if h.rng.Intn(turtleHomeOdds/mobMoveInterval) != 0 && !m.turtleHoming {
		return false
	}
	m.turtleHoming = true
	dx, dz := hx-m.x, hz-m.z
	d := math.Hypot(dx, dz)
	if d <= turtleHomeFar {
		m.turtleHoming = false
		return false
	}
	sp := m.moveSpeed()
	m.vx, m.vz = dx/d*sp, dz/d*sp
	m.rest = 0
	return true
}

const (
	turtleTravelXZ    = 512 // TurtleTravelGoal.start: a spot up to 512 blocks off
	turtleLegXZ       = 16  // tick: DefaultRandomPos.getPosTowards(16, 3, …, π/10)
	turtleLegWide     = 8   // …else (8, 7, …, π/2)
	turtleLegLoaded   = 34  // hasChunksAt(±34) around the leg's end
	turtleLegTries    = 10  // RandomPos's attempts
	turtleLegArrive   = 1.0 // navigation done
	turtleLegGiveUp   = 200 // updates on one leg before the path counts as done
	turtleStrollEvery = 100 // TurtleRandomStrollGoal(1.0, 100) against the default 120
)

// turtleTravelStep is TurtleTravelGoal: a turtle in the water, not going
// home, not carrying an egg and not courting swims on and on — each leg of
// up to sixteen blocks heads toward a fresh spot as much as 512 blocks off,
// so at sea it is always going somewhere. A leg that would end in ground
// not loaded, or out of the water, is no leg: the goal stops and picks
// again next time.
func (h *hub) turtleTravelStep(m *mob) bool {
	if m.dying != 0 || m.panic > 0 || m.tempted || m.hasEgg || m.turtleHoming || m.loveTicks > 0 ||
		!h.inWater(m.dim, m.x, m.y+0.2, m.z) {
		m.turtleLeg = false
		return false
	}
	if m.turtleLeg {
		if m.turtleLegTry++; m.turtleLegTry <= turtleLegGiveUp && math.Hypot(m.turtleLegX-m.x, m.turtleLegZ-m.z) > turtleLegArrive {
			h.steerTo(m, m.turtleLegX, m.turtleLegZ, 1.0)
			return true
		}
		m.turtleLeg = false // the path is done: the goal stops, and starts over with a new spot
	}
	// start: the far spot. Only its direction matters to the leg (the
	// engine's swimmers keep their level, so its height is not drawn).
	tx := m.x + float64(h.rng.Intn(2*turtleTravelXZ+1)-turtleTravelXZ)
	tz := m.z + float64(h.rng.Intn(2*turtleTravelXZ+1)-turtleTravelXZ)
	x, z, ok := h.turtleLegToward(m, tx, tz, turtleLegXZ, math.Pi/10)
	if !ok {
		x, z, ok = h.turtleLegToward(m, tx, tz, turtleLegWide, math.Pi/2)
	}
	if !ok || !h.turtleLegLoaded(m.dim, x, z) {
		// Stuck: the goal stops and picks again next time round. Nothing
		// else moves a turtle at sea (its stroll is for land), so it drifts.
		m.vx, m.vz = m.vx*0.6, m.vz*0.6
		return true
	}
	m.turtleLeg, m.turtleLegX, m.turtleLegZ, m.turtleLegTry = true, x, z, 0
	h.steerTo(m, x, z, 1.0)
	return true
}

// turtleLegToward is DefaultRandomPos.getPosTowards for a travelling turtle:
// a spot within reach, inside the arc about the heading to (tx, tz), that is
// water (TurtlePathNavigation.isStableDestination while travelling).
func (h *hub) turtleLegToward(m *mob, tx, tz float64, reach int, arc float64) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, 0, false
	}
	head := math.Atan2(tz-m.z, tx-m.x)
	y := floorInt(m.y)
	for i := 0; i < turtleLegTries; i++ {
		a := head + (2*h.rng.Float64()-1)*arc
		r := math.Sqrt(h.rng.Float64()) * float64(reach) * math.Sqrt2
		r = math.Min(r, float64(reach))
		x, z := m.x+math.Cos(a)*r, m.z+math.Sin(a)*r
		if worldgen.IsWater(w.At(floorInt(x), y, floorInt(z))) {
			return math.Floor(x) + 0.5, math.Floor(z) + 0.5, true
		}
	}
	return 0, 0, false
}

// turtleLegLoaded is the leg's hasChunksAt: every chunk within thirty-four
// blocks of its end is loaded.
func (h *hub) turtleLegLoaded(dim int, x, z float64) bool {
	w := h.worldFor(dim)
	if w == nil {
		return false
	}
	x0, z0 := (floorInt(x)-turtleLegLoaded)>>4, (floorInt(z)-turtleLegLoaded)>>4
	x1, z1 := (floorInt(x)+turtleLegLoaded)>>4, (floorInt(z)+turtleLegLoaded)>>4
	for cx := x0; cx <= x1; cx++ {
		for cz := z0; cz <= z1; cz++ {
			if !w.Loaded(int32(cx), int32(cz)) {
				return false
			}
		}
	}
	return true
}

// turtleRest is the idle spell before a turtle's next stroll on land:
// TurtleRandomStrollGoal runs on an interval of 100 where the other
// animals' strolls run on 120, so its rests are that much shorter.
func turtleRest(rest int) int { return rest * turtleStrollEvery / 120 }
