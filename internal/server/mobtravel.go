package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Walking travel. A walker is moved the way LivingEntity.aiStep moves a
// mob, every tick: the goals' wanted motion goes through the move control
// into the input (zza = speed, ×0.98), moveRelative turns it into
// acceleration — on the ground speed × 0.21600002 / friction³ (exactly
// speed on an ordinary block), in the air the 0.02 flying speed — Entity.move
// collides the box with the blocks' collision shapes (stepping up to
// STEP_HEIGHT), and then gravity and drag: 0.98 down, the floor's friction ×
// 0.91 across (0.91 in the air). In water it is travelInWater's 0.02 and
// 0.8 drag; a floater jumps against deep water (FloatGoal); in lava it is
// travelInLava. A knocked body, a spider on a wall, a fall and a bounce are
// all this one motion.
//
// The engine's goals still steer by wanted velocity (m.vx, m.vz in per-update
// step units): their length, back in vanilla units (moveSpeed's scale
// undone), is the setSpeed value, and their direction the heading. What a
// path would forbid — water, a hazard, a drop deeper than three — a walker
// declines before it steps (walkGuard), and a wall it cannot step onto
// makes it jump a block or turn away, as before.

const (
	mobInputDrag     = 0.98 // LivingEntity.applyInput: xxa, zza ×0.98
	mobFlyingSpeed   = 0.02 // LivingEntity.getFlyingSpeed off the ground
	mobAirDrag       = 0.91 // travelInAir's horizontal drag
	mobVertDrag      = 0.98 // …and vertical
	mobWaterSpeed    = 0.02 // travelInWater's moveRelative speed
	mobWaterSlow     = 0.8  // getWaterSlowDown
	mobJumpDelay     = 10   // aiStep: noJumpDelay after a jump
	mobClimbSpeed    = 0.2  // the climb up a ladder or a spider's wall
	mobClimbClamp    = 0.15 // handleOnClimbable
	mobDeadZone      = 0.003
	waterOutLift     = 0.3 // jumpOutOfFluid
	slimeStepBase    = 0.4 // SlimeBlock.stepOn: 0.4 + |dy| × 0.2
	bedRestitution   = 0.75
	slimeRestitution = 1.0
)

// travels reports whether this tick's movement for the mob is walking
// travel (and not a flight, a swim or an anchor of its own).
func (h *hub) travels(m *mob) bool {
	switch {
	case m.statik, m.geyserFly, m.flies, m.swims, m.etype == entityVex:
		return false
	case isAmphibious(m.etype) && h.inWater(m.dim, m.x, m.y, m.z):
		return false
	}
	return true
}

// wantedMove is the move control's output for this tick: the setSpeed
// value and the unit heading, from what the goals asked for. Nothing while
// a blow carries it.
//
// The goals ask in step units: the mob's moveSpeed (its MOVEMENT_SPEED in
// the engine's scale, a tuned species' step) times the goal's speed
// modifier. Undone, that is MoveControl's speedModifier × MOVEMENT_SPEED —
// for every species, the tuned ones too (their step only sets the scale the
// goals ask in), times the goal-speed base a species' engine goals are
// relative to (walkBaseMod).
func (m *mob) wantedMove() (speed, dx, dz float64) {
	if m.kbFlight || m.springing() {
		return 0, 0, 0
	}
	l := math.Hypot(m.vx, m.vz)
	if l < 1e-6 {
		return 0, 0, 0
	}
	speed = l / (attrToStep * stepScaleFor(m.etype)) * walkBaseMod(m.etype)
	return speed, m.vx / l, m.vz / l
}

// walkBaseMod is the vanilla speed modifier one of a species' engine goal
// paces stands for. The villager's goals are written against its brain's
// speedModifier 0.5 (VillagerGoalPackages: a stroll at "1" is 0.5 of
// MOVEMENT_SPEED); every other species' goals carry vanilla's modifiers.
func walkBaseMod(etype int) float64 {
	if etype == entityVillager {
		return 0.5
	}
	return 1
}

// landMoveControl is the speed a species' own move control sets on land
// for the speed its goal asked (MoveControl.setSpeed's argument), where
// that is not the plain value: a frog's SmoothSwimmingMoveControl walks at
// outsideWaterSpeedModifier 0.1, slowed while it is still turning
// (getTurningSpeedFactor); a turtle's TurtleMoveControl halves its speed on
// the ground each tick (to no less than 0.06) and closes an eighth of the
// way to the asked speed.
func (m *mob) landMoveControl(target, wx, wz float64) float64 {
	switch m.etype {
	case entityFrog:
		want := float32(math.Atan2(-wx, wz) * 180 / math.Pi)
		left := math.Abs(float64(wrapDeg(m.yaw - want)))
		turning := 1 - math.Min(1, math.Max(0, (left-10)/50))
		return target * 0.1 * turning
	case entityTurtle:
		s := m.ctlSpeed
		if m.onGround {
			s = math.Max(s/2, 0.06)
		}
		s += (target - s) * 0.125
		m.ctlSpeed = s
		return s
	}
	return target
}

// mobFluidHeight is how deep the body stands in water (or lava): the
// surface over its feet column, less its feet. 0 when not in it.
func (h *hub) mobFluidHeight(m *mob, lava bool) float64 {
	w := h.worldFor(m.dim)
	fx, fz := floorInt(m.x), floorInt(m.z)
	is := worldgen.HoldsWater
	if lava {
		is = worldgen.IsLava
	}
	y := floorInt(m.y)
	if !is(w.At(fx, y, fz)) {
		return 0
	}
	for i := 0; i < 64 && is(w.At(fx, y+1, fz)); i++ {
		y++
	}
	surface := float64(y) + fluidCellHeight(w.At(fx, y+1, fz), w.At(fx, y, fz), lava)
	if !lava && !worldgen.IsWater(w.At(fx, y, fz)) {
		surface = float64(y + 1) // a waterlogged block holds a full cell
	}
	return math.Max(0, surface-m.y)
}

// mobTravel is one tick of a walker's movement. goal is true on the tick
// its goals ran. Reports whether it left for a neighbour shard.
func (h *hub) mobTravel(players map[int32]*tracked, m *mob, goal bool) bool {
	w := h.worldFor(m.dim)
	// aiStep: motion below the dead zone is none.
	if math.Abs(m.dmx) < mobDeadZone {
		m.dmx = 0
	}
	if math.Abs(m.dmz) < mobDeadZone {
		m.dmz = 0
	}
	if math.Abs(m.vy) < mobDeadZone {
		m.vy = 0
	}
	// The crowd's shoves and the currents (pushMobs, applyFluidPush) land
	// in the motion, as Entity.push and the fluid push do.
	m.dmx += m.pushX
	m.dmz += m.pushZ
	m.pushX, m.pushZ = 0, 0
	// A spring (LeapAtTargetGoal, a pounce, a bounce on a bed, a cheer, a
	// long jump, a breeze's jump): its motion replaces the body's, and
	// travel flies it.
	switch {
	case m.leaping && (m.leapVX != 0 || m.leapVY != 0 || m.leapVZ != 0):
		m.dmx, m.vy, m.dmz = m.leapVX, m.leapVY, m.leapVZ
		m.leapVX, m.leapVY, m.leapVZ = 0, 0, 0
	case m.goatJumping && (m.goatVX != 0 || m.goatVY != 0 || m.goatVZ != 0):
		m.dmx, m.vy, m.dmz = m.goatVX, m.goatVY, m.goatVZ
		m.goatVX, m.goatVY, m.goatVZ = 0, 0, 0
	case m.brzJumpingNow() && (m.brzVX != 0 || m.brzVY != 0 || m.brzVZ != 0):
		m.dmx, m.vy, m.dmz = m.brzVX, m.brzVY, m.brzVZ
		m.brzVX, m.brzVY, m.brzVZ = 0, 0, 0
	}
	if m.jumpDelay > 0 {
		m.jumpDelay--
	}
	if m.etype == entityBlaze {
		h.blazeAirTick(m) // the slow fall, and the lift toward a target above
	}
	if m.hasEffect(effSlowFalling) > 0 || m.hasEffect(effLevitation) > 0 {
		m.airFall = 0 // aiStep: either keeps the fall distance at nothing
	}

	speed, wx, wz := m.wantedMove()
	hx, hz := wx, wz
	if speed > 0 {
		speed = m.landMoveControl(speed, wx, wz)
	} else if m.etype == entityTurtle {
		m.ctlSpeed = 0 // TurtleMoveControl: no wanted position, setSpeed(0)
	}
	if speed > 0 {
		// moveRelative turns the input by yRot: the walker goes the way its
		// body faces, which the move control turns toward the heading at
		// its turn a tick (turnBody).
		hx, hz = m.facing()
		aligned := hx*wx+hz*wz > 0.9998 // within about a degree of the heading
		if h.walkGuard(players, m, hx, hz, aligned) {
			speed = 0 // refused: it stands while it turns away
		}
	}

	water := h.mobFluidHeight(m, false)
	lava := 0.0
	if water == 0 {
		lava = h.mobFluidHeight(m, true)
	}
	thr := fluidJumpThresh
	if mobEyeHeight(m) < fluidJumpThresh {
		thr = 0
	}
	// FloatGoal: in water past the jump threshold (or any lava) a floater
	// jumps on four ticks in five.
	jumping := m.jumpWanted
	m.jumpWanted = false
	if mobFloats(m) && (water > thr || lava > 0) && h.rng.Float64() < floatJumpChance {
		jumping = true
	}
	// ClimbOnTopOfPowderSnowGoal: a snow walker in powder snow, with snow or
	// room above, jumps every tick — which climbs it up through the snow.
	inPowder := h.mobInPowderSnow(m)
	snowClimb := inPowder && canWalkOnPowderSnow(m)
	if inPowder && powderSnowWalkers[m.etype] {
		fx, fy, fz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
		if above := w.At(fx, fy+1, fz); isPowderSnow(above) || len(collisionShape(above)) == 0 {
			jumping = true
		}
	}
	// …and the move control jumps out of a block its feet are sunk in.
	if speed > 0 && m.onGround && h.feetSunk(m) {
		jumping = true
	}
	if jumping {
		switch {
		case water > 0 && (!m.onGround || water > thr):
			m.vy += liquidJumpStep // jumpInLiquid
		case lava > 0 && !(m.onGround && lava <= thr):
			m.vy += liquidJumpStep
		case m.onGround && m.jumpDelay == 0:
			if p := m.jumpPower(h); p > 1e-5 {
				m.vy = math.Max(p, m.vy) // jumpFromGround
				m.jumpDelay = mobJumpDelay
			}
		}
	} else {
		m.jumpDelay = 0
	}

	climbable := m.climbing || isClimbable(w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z)))
	oldY := m.y
	in := mobInputDrag * speed
	g := m.effectiveGravity(m.vy)
	switch {
	case water > 0 && waterGlider(m.etype):
		// Axolotl, Frog and Turtle.travelInWater: no gravity, a 0.9 drag
		// all round, and a swim the goals steer directly (their swimming
		// move controls), at the pace they ask for.
		if !m.kbFlight && !m.leaping {
			m.dmx, m.dmz = m.vx/mobGoalInterval, m.vz/mobGoalInterval
		}
		if gone, _ := h.mobMove(players, m, w); gone {
			return true
		}
		m.vy *= 0.9
		if m.etype == entityTurtle && !m.hasTarget && !m.turtleHoming {
			m.vy -= 0.005 // Turtle.travelInWater: an idle turtle sinks a little
		}
		h.jumpOutOfFluid(m, oldY)
	case water > 0:
		// travelInWater.
		slow, sp := mobWaterSlow, mobWaterSpeed
		if m.hasEffect(effDolphinsGrace) > 0 {
			slow = 0.96
		}
		m.dmx += hx * in * sp
		m.dmz += hz * in * sp
		falling := m.vy <= 0
		if gone, _ := h.mobMove(players, m, w); gone {
			return true
		}
		if m.horizColl && climbable {
			m.vy = mobClimbSpeed
		}
		m.dmx, m.dmz, m.vy = m.dmx*slow, m.dmz*slow, m.vy*0.8
		m.vy = fluidFallAdjust(g, falling, m.vy)
		h.jumpOutOfFluid(m, oldY)
	case lava > 0:
		// travelInLava.
		m.dmx += hx * in * mobWaterSpeed
		m.dmz += hz * in * mobWaterSpeed
		falling := m.vy <= 0
		if gone, _ := h.mobMove(players, m, w); gone {
			return true
		}
		if lava <= thr {
			m.dmx, m.dmz, m.vy = m.dmx*0.5, m.dmz*0.5, m.vy*0.8
			m.vy = fluidFallAdjust(g, falling, m.vy)
		} else {
			m.dmx, m.dmz, m.vy = m.dmx*0.5, m.dmz*0.5, m.vy*0.5
		}
		if g != 0 {
			m.vy -= g / 4
		}
		h.jumpOutOfFluid(m, oldY)
	default:
		// travelInAir.
		friction := 1.0
		if m.onGround {
			friction = blockFriction(w.At(floorInt(m.x), floorInt(m.y-0.500001), floorInt(m.z)))
		}
		fis := mobFlyingSpeed
		if m.onGround {
			fis = speed
			if friction > 0.6 {
				fis = speed * (0.21600002 / (friction * friction * friction))
			}
		}
		m.dmx += hx * in * fis
		m.dmz += hz * in * fis
		if climbable { // handleOnClimbable
			m.airFall = 0
			m.dmx = math.Max(-mobClimbClamp, math.Min(mobClimbClamp, m.dmx))
			m.dmz = math.Max(-mobClimbClamp, math.Min(mobClimbClamp, m.dmz))
			m.vy = math.Max(m.vy, -mobClimbClamp)
		}
		gone, _ := h.mobMove(players, m, w)
		if gone {
			return true
		}
		if (m.horizColl || jumping) && (climbable || snowClimb) {
			m.vy = mobClimbSpeed
		}
		if lvl := m.hasEffect(effLevitation); lvl > 0 {
			m.vy += (0.05*float64(lvl) - m.vy) * 0.2
		} else {
			m.vy -= g
		}
		if !m.goatJumping && !m.brzJumpingNow() { // a long jump discards friction (setDiscardFriction)
			drag := friction * mobAirDrag
			m.dmx, m.dmz, m.vy = m.dmx*drag, m.dmz*drag, m.vy*mobVertDrag
		}
	}
	if climbsWalls(m.etype) {
		h.setClimbing(players, m, m.horizColl) // Spider.tick: climbing exactly when it walked into something
	}
	h.mobBlockEffects(players, m)
	if speed > 0 && m.horizColl && goal && !climbsWalls(m.etype) {
		h.walkBlocked(players, m, hx, hz)
	}
	if m.onGround && m.vy <= 0 { // down again: the spring is over
		switch {
		case m.leaping:
			m.leaping = false
		case m.goatJumping:
			h.goatLanded(players, m)
		case m.brzJumpingNow():
			h.breezeLand(players, m)
		}
	}
	m.airborne = !m.onGround
	return false
}

// fluidFallAdjust is getFluidFallingAdjustedMovement's vertical part.
func fluidFallAdjust(g float64, falling bool, vy float64) float64 {
	if g == 0 {
		return vy
	}
	if falling && math.Abs(vy-0.005) >= 0.003 && math.Abs(vy-g/16) < 0.003 {
		return -0.003
	}
	return vy - g/16
}

// jumpOutOfFluid: a body stopped across in water or lava, with room above
// the bank, is lifted onto it.
func (h *hub) jumpOutOfFluid(m *mob, oldY float64) {
	if !m.horizColl {
		return
	}
	w := h.worldFor(m.dim)
	b := mobAABB(m).move(m.dmx, m.vy+0.6-m.y+oldY, m.dmz)
	var c collider
	c.gather(w, m, b)
	for _, s := range c.boxes {
		if s[3] > b[0] && s[0] < b[3] && s[4] > b[1] && s[1] < b[4] && s[5] > b[2] && s[2] < b[5] {
			return // not free
		}
	}
	if worldgen.HoldsWater(w.At(floorInt(b[0]+(b[3]-b[0])/2), floorInt(b[1]), floorInt(b[2]+(b[5]-b[2])/2))) {
		// isFree also refuses liquid: the bank must be dry for the lift.
		return
	}
	m.vy = waterOutLift
}

// jumpPower is LivingEntity.getJumpPower: JUMP_STRENGTH × the block's jump
// factor, plus Jump Boost.
func (m *mob) jumpPower(h *hub) float64 {
	p := m.jumpStrength() * h.mobJumpFactor(m)
	if lvl := m.hasEffect(effJumpBoost); lvl > 0 {
		p += 0.1 * float64(lvl)
	}
	return p
}

// mobJumpFactor is Entity.getBlockJumpFactor: honey under (or in) the feet
// halves a jump.
func (h *hub) mobJumpFactor(m *mob) float64 {
	w := h.worldFor(m.dim)
	if isHoneyBlock(w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z))) {
		return 0.5
	}
	if isHoneyBlock(w.At(floorInt(m.x), floorInt(m.y-0.500001), floorInt(m.z))) {
		return 0.5
	}
	return 1
}

// feetSunk is the move control's other jump: the block at the mob's
// position has a collision shape whose top is above its feet (not a door
// or a fence).
func (h *hub) feetSunk(m *mob) bool {
	w := h.worldFor(m.dim)
	fx, fy, fz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	s := w.At(fx, fy, fz)
	if s == worldgen.Air || worldgen.IsDoor(s) || worldgen.IsTallCollision(s) {
		return false
	}
	top := -1.0
	for _, b := range mobCollisionShape(m, s, fy) {
		top = math.Max(top, b[4])
	}
	return top >= 0 && m.y < float64(fy)+top
}

// mobMove is Entity.move(SELF, deltaMovement) for a walker: the stuck
// multiplier of the web or snow it is caught in, the collision, the
// position, the ground and collision flags, the fall it builds up and the
// landing, the bounce, and the block's speed factor. gone reports a hand-off
// to a neighbour shard.
func (h *hub) mobMove(players map[int32]*tracked, m *mob, w *world.World) (gone, moved bool) {
	dx, dy, dz := m.dmx, m.vy, m.dmz
	if m.stuckSet {
		dx, dy, dz = dx*m.stuckX, dy*m.stuckY, dz*m.stuckX
		m.dmx, m.vy, m.dmz = 0, 0, 0
		m.stuckSet = false
	}
	var c collider
	mx, my, mz := h.mobCollide(w, m, &c, dx, dy, dz)
	nx, ny, nz := m.x+mx, m.y+my, m.z+mz
	if mx != 0 || mz != 0 {
		if !h.ownedAt(nx, nz) {
			if h.migrateMobAcross(players, m, nx, nz) {
				return true, true
			}
			mx, mz = 0, 0 // the shard's edge is a wall
			nx, nz = m.x, m.z
		}
	}
	moved = mx != 0 || my != 0 || mz != 0
	m.x, m.y, m.z = nx, ny, nz
	xColl := math.Abs(dx-mx) >= 1e-5
	zColl := math.Abs(dz-mz) >= 1e-5
	m.horizColl = xColl || zColl
	vColl := dy != my
	if dy != 0 {
		m.onGround = vColl && dy < 0
	}
	// checkFallDamage: the fall builds while it goes down out of water,
	// and lands on the ground.
	if my < 0 && !h.inWater(m.dim, m.x, m.y, m.z) {
		m.airFall -= my
	}
	if m.onGround && m.airFall > 0 {
		fall := m.airFall
		m.airFall = 0
		h.mobFallOn(players, m, fall)
	}
	// restituteMovementAfterCollisions: a blocked axis loses its speed; a
	// landing on slime or a bed bounces back up.
	if xColl {
		m.dmx = 0
	}
	if zColl {
		m.dmz = 0
	}
	if vColl {
		rest := 0.0
		if dy < 0 && -m.vy > m.effectiveGravity(m.vy) {
			under := w.At(floorInt(m.x), floorInt(m.y-0.2), floorInt(m.z))
			switch {
			case isSlimeBlock(under):
				rest = slimeRestitution
			case isBedBlock(under):
				rest = bedRestitution
			}
		}
		if rest > 0 && m.vy != 0 {
			portion := my / m.vy
			comp := portion * m.effectiveGravity(m.vy)
			drag := 1 + portion*(mobVertDrag-1)
			m.vy = (comp - m.vy) * drag * rest
		} else {
			m.vy = 0
		}
	}
	// getBlockSpeedFactor: soul sand and honey slow the next step.
	if f := h.mobBlockSpeedFactor(m); f != 1 {
		m.dmx, m.dmz = m.dmx*f, m.dmz*f
	}
	return false, moved
}

// mobFallOn is the landing: Block.fallOn's damage and the trampling it
// does.
func (h *hub) mobFallOn(players map[int32]*tracked, m *mob, fall float64) {
	if fall > 0.5 {
		h.mobTrample(players, m, fall) // FarmlandBlock.fallOn
	}
	h.mobFallOnEgg(players, m) // TurtleEggBlock.fallOn
	if fall > m.safeFallDistance() {
		h.mobFall(players, m, fall)
	}
}

// mobBlockSpeedFactor is Entity.getBlockSpeedFactor with the mob's
// MOVEMENT_EFFICIENCY: the feet cell's factor, else the cell under.
func (h *hub) mobBlockSpeedFactor(m *mob) float64 {
	w := h.worldFor(m.dim)
	eff := 0.0
	if m.attrs != nil {
		eff = m.attrs.Peek(attr.MovementEfficiency)
	}
	factor := func(s uint32) float64 {
		f := 1.0
		if s == worldgen.SoulSand || isHoneyBlock(s) {
			f = 0.4
		}
		return f + eff*(1-f)
	}
	feet := w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z))
	if worldgen.IsWater(feet) || worldgen.IsBubbleColumn(feet) {
		return factor(feet)
	}
	if f := factor(feet); f != 1 {
		return f
	}
	return factor(w.At(floorInt(m.x), floorInt(m.y-0.500001), floorInt(m.z)))
}

// mobBlockEffects is applyEffectsFromBlocks after the move: a slime block's
// stepOn, the web, berry bush or powder snow that will catch the next move
// (makeStuckInBlock), a bubble column's lift or drag, and a honey wall's
// slide.
func (h *hub) mobBlockEffects(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	fx, fz := floorInt(m.x), floorInt(m.z)
	if m.onGround && math.Abs(m.vy) < 0.1 && isSlimeBlock(w.At(fx, floorInt(m.y-0.2), fz)) {
		k := slimeStepBase + math.Abs(m.vy)*0.2 // SlimeBlock.stepOn
		m.dmx, m.dmz = m.dmx*k, m.dmz*k
	}
	switch {
	case mobStuckInWeb(m) && h.inWebCell(m, fx, fz):
		m.stuckX, m.stuckY = 0.25, webStuckY
		if m.hasEffect(effWeaving) > 0 {
			m.stuckX, m.stuckY = 0.5, webStuckYWeaving
		}
		m.stuckSet, m.airFall = true, 0
	case m.etype != entityFox && m.etype != entityBee && m.etype != entityWither && h.inBerryBush(m.dim, m.x, m.y, m.z):
		m.stuckX, m.stuckY, m.stuckSet = 0.8, 0.75, true
		m.airFall = 0
	case m.etype != entityWither && isPowderSnow(w.At(fx, floorInt(m.y), fz)):
		m.stuckX, m.stuckY, m.stuckSet = powderSnowStuckXZ, powderSnowStuckY, true
		m.airFall = 0
	}
	if worldgen.HoldsWater(w.At(fx, floorInt(m.y), fz)) {
		m.airFall = 0 // in water the fall is forgotten
	}
	boxCells(m.y, m.box().h, func(cy int) bool {
		s := w.At(fx, cy, fz)
		if !worldgen.IsBubbleColumn(s) {
			return false
		}
		above := w.At(fx, cy+1, fz)
		top := !worldgen.Collides(above) && !worldgen.HoldsWater(above) && !worldgen.IsLava(above)
		switch {
		case s == worldgen.BubbleColumnDrag && top:
			m.vy = math.Max(columnTopDownCap, m.vy-columnDownStep)
		case s == worldgen.BubbleColumnDrag:
			m.vy = math.Max(columnDownCap, m.vy-columnDownStep)
		case top:
			m.vy = math.Min(columnTopUpCap, m.vy+columnTopUpStep)
		default:
			m.vy = math.Min(columnUpCap, m.vy+columnUpStep)
		}
		m.onGround = false
		return false
	})
	if !m.onGround && m.vy < -honeySlideMinFall && h.mobOnHoneyWall(m) {
		// HoneyBlock.doSlideMovement: the fall is held to a slide.
		m.vy = -honeySlideSpeed
		m.airFall = 0
		if h.rng.Intn(5) == 0 {
			h.playSoundDim(players, m.dim, "minecraft:block.honey_block.slide", sndBlock, m.x, m.y, m.z, 1, 1)
		}
		if h.rng.Intn(5) == 0 {
			h.toTracking(players, m.eid, m.dim, m.x, m.z, entityStatus(m.eid, honeySlideEventID))
		}
	}
}

// walkGuard is what a path would have refused: before the walker steps
// into the next column the way it faces it checks that column the way
// mobStepOK always has — no water, no hazard its kind avoids, no drop
// deeper than three — and on a refusal it stops, and, if it was facing the
// way it wants to go, turns away (one still turning toward its heading
// only waits). It reports a refusal.
func (h *hub) walkGuard(players map[int32]*tracked, m *mob, hx, hz float64, aligned bool) bool {
	// Where the centre will be a little ahead of the body.
	ahead := math.Max(0.6, m.box().w/2+0.3)
	px, pz := m.x+hx*ahead, m.z+hz*ahead
	if floorInt(px) == floorInt(m.x) && floorInt(pz) == floorInt(m.z) {
		return false
	}
	if h.walkSoftOK(m, px, pz) {
		return false
	}
	m.dmx, m.dmz = 0, 0
	if aligned {
		h.turnAway(m)
	}
	return true
}

// walkSoftOK is mobStepOK's rules that the collision does not already
// enforce: dry land (or water it floats across), no hazard its kind
// refuses, and no drop past pathMaxFall.
func (h *hub) walkSoftOK(m *mob, nx, nz float64) bool {
	w := h.worldFor(m.dim)
	fnx, fnz := floorInt(nx), floorInt(nz)
	cx, cz := floorInt(m.x), floorInt(m.z)
	prof := malusFor(m.etype)
	hazardOK := prof[pathHazardKind(w, fnx, fnz)] >= 0 || prof[pathHazardKind(w, cx, cz)] < 0
	if h.floatSwims(m, fnx, fnz) {
		return hazardOK
	}
	destOK := h.dryFooting(m, fnx, fnz) || !h.dryFooting(m, cx, cz)
	step := h.mobFeetAt(m, fnx, fnz, floorInt(m.y)) - floorInt(m.y)
	return destOK && hazardOK && step >= -pathMaxFall
}

// turnAway commits a blocked walker to a fresh random heading for a while,
// so it walks away from what stopped it instead of pressing on into it — a
// heading the walk rules allow, if one of a few tries finds it; else it
// stands.
func (h *hub) turnAway(m *mob) {
	ahead := math.Max(0.6, m.box().w/2+0.3)
	m.vx, m.vz = 0, 0
	for try := 0; try < 4; try++ {
		ang := h.rng.Float64() * 2 * math.Pi
		cx, sz := math.Cos(ang), math.Sin(ang)
		if px, pz := m.x+cx*ahead, m.z+sz*ahead; (floorInt(px) == floorInt(m.x) && floorInt(pz) == floorInt(m.z)) || h.walkSoftOK(m, px, pz) {
			m.vx, m.vz = cx*m.moveSpeed(), sz*m.moveSpeed()
			break
		}
	}
	m.reroute = 15 + h.rng.Intn(15)
}

// walkBlocked is a walker stopped across by something: a step a block
// high it jumps (the move control's jump on a path up), a closed door a
// hunting zombie beats on or a raiding vindicator breaks, and anything else
// it turns away from.
func (h *hub) walkBlocked(players map[int32]*tracked, m *mob, hx, hz float64) {
	ahead := math.Max(0.6, m.box().w/2+0.3)
	px, pz := m.x+hx*ahead, m.z+hz*ahead
	if m.breaksDoors && m.hasTarget {
		if door, ok := h.doorAhead(m, px, pz); ok && h.zombieBeatsDoor(players, m, door) {
			return
		}
	}
	if m.etype == entityVindicator && h.raiderInActiveRaid(m) {
		if door, ok := h.doorAhead(m, px, pz); ok && h.vindicatorAtDoor(players, m, door) {
			return
		}
	}
	if m.onGround && h.mobStepOK(m, px, pz) {
		step := h.mobFeetAt(m, floorInt(px), floorInt(pz), floorInt(m.y)) - floorInt(m.y)
		if step >= 1 {
			m.jumpWanted = true
			return
		}
	}
	if m.reroute == 0 {
		h.turnAway(m)
	}
}

// mobSupported reports whether the mob's feet rest on a collision shape's
// top in its column: where it was placed, it is standing.
func (h *hub) mobSupported(m *mob) bool {
	w := h.worldFor(m.dim)
	fx, fz := floorInt(m.x), floorInt(m.z)
	for _, cy := range [2]int{floorInt(m.y - 1e-4), floorInt(m.y)} {
		for _, b := range mobCollisionShape(m, w.At(fx, cy, fz), cy) {
			if top := float64(cy) + b[4]; math.Abs(top-m.y) < 1e-4 {
				return true
			}
		}
	}
	return false
}

// waterGlider is the walkers whose travelInWater is their own: no gravity
// and a 0.9 drag (Axolotl, Frog, Turtle).
func waterGlider(etype int) bool {
	return etype == entityAxolotl || etype == entityFrog || etype == entityTurtle
}

// springing reports a spring in flight: a leap, a long jump or a breeze's
// jump, on which the goals do not steer.
func (m *mob) springing() bool {
	return m.leaping || m.goatJumping || m.brzJumpingNow()
}

// brzJumpingNow is a breeze in the air on its long jump.
func (m *mob) brzJumpingNow() bool { return m.etype == entityBreeze && m.brzState == brzJumping }
